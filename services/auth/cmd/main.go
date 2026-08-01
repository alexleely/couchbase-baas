package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/couchbase/gocb/v2"

	"auth/internal/crypto"
	"auth/internal/db"
	"auth/internal/models"
)

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	// Initialize Couchbase connection (retry and auto-provision scopes/collections/indexes)
	if err := db.InitDB(); err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}

	// Health check endpoint
	http.HandleFunc("GET /health", handleHealth)

	// User registration endpoint
	http.HandleFunc("POST /auth/v1/signup", handleSignup)

	// User login & token endpoint
	http.HandleFunc("POST /auth/v1/token", handleToken)

	log.Printf("Auth Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start auth server: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{
		Service: "Auth Service",
		Status:  "UP",
		Version: "0.1.0",
	})
}

func handleSignup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req models.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	// Validation
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "A valid email address is required"})
		return
	}

	if len(req.Password) < 6 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Password must be at least 6 characters long"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Check if user already exists with this email using SQL++
	queryStr := fmt.Sprintf("SELECT id FROM `%s`.`auth`.`users` WHERE email = $1 LIMIT 1", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{req.Email},
	})
	if err != nil {
		log.Printf("Database query error checking duplicate email: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error checking email availability"})
		return
	}
	defer rows.Close()

	if rows.Next() {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "User with this email already exists"})
		return
	}

	// Hash password
	passwordHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		log.Printf("Password hashing error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Internal server error hashing password"})
		return
	}

	// Generate UUID
	userID := "usr_" + uuid.New().String()

	// Insert User Document
	userDoc := models.User{
		ID:           userID,
		Email:        req.Email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Role:         "authenticated",
	}

	_, err = db.Instance.Collection.Insert(userID, userDoc, nil)
	if err != nil {
		log.Printf("Couchbase write error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error creating user"})
		return
	}

	// Hide password hash in HTTP response
	userDoc.PasswordHash = ""

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(userDoc)
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req models.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Email and password are required"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Fetch user from Couchbase using SQL++
	queryStr := fmt.Sprintf("SELECT id, email, password_hash, role FROM `%s`.`auth`.`users` WHERE email = $1 LIMIT 1", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{req.Email},
	})
	if err != nil {
		log.Printf("Database query error looking up user: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error looking up user"})
		return
	}
	defer rows.Close()

	var userDoc models.User
	if !rows.Next() {
		// Return 401 Unauthorized to prevent email enumeration
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid email or password"})
		return
	}

	err = rows.Row(&userDoc)
	if err != nil {
		log.Printf("Failed to deserialize user row: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Internal database deserialization error"})
		return
	}

	// Compare password
	match, err := crypto.ComparePassword(req.Password, userDoc.PasswordHash)
	if err != nil || !match {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid email or password"})
		return
	}

	// Generate session ID and refresh token
	sessionID := "sess_" + uuid.New().String()
	refreshToken, err := crypto.GenerateRefreshToken()
	if err != nil {
		log.Printf("Failed to generate refresh token: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to generate session tokens"})
		return
	}

	// Store Session document in Couchbase auth.sessions
	sessionDoc := models.Session{
		ID:           sessionID,
		UserID:       userDoc.ID,
		RefreshToken: refreshToken,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour), // 30 days
		Revoked:      false,
	}

	_, err = db.Instance.SessionsCollection.Insert(sessionID, sessionDoc, nil)
	if err != nil {
		log.Printf("Failed to insert session into database: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to save session state"})
		return
	}

	// Generate access token JWT
	accessToken, err := crypto.GenerateAccessToken(userDoc.ID, userDoc.Role)
	if err != nil {
		log.Printf("Failed to generate access token JWT: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to generate access token"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(models.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    3600,
		TokenType:    "bearer",
	})
}
