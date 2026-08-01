package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/couchbase/gocb/v2"
	"golang.org/x/oauth2"

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

	// User registration & login endpoints
	http.HandleFunc("POST /auth/v1/signup", handleSignup)
	http.HandleFunc("POST /auth/v1/token", handleToken)

	// OIDC Social Login endpoints
	http.HandleFunc("GET /auth/v1/authorize", handleAuthorize)
	http.HandleFunc("GET /auth/v1/callback", handleCallback)

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

	// Generate session
	sessionID, refreshToken, err := createSession(userDoc.ID)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to generate session tokens"})
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

// OIDC Authorize Request
func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Provider query parameter is required"})
		return
	}

	cfg, err := getOauthConfig(provider)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	state, err := crypto.GenerateRandomState()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Internal entropy state failure"})
		return
	}

	verifier, err := crypto.GenerateCodeVerifier()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Internal PKCE verifier failure"})
		return
	}

	// Cache verifier and state in Couchbase auth.oauth_states (TTL 5 mins)
	stateDoc := models.OAuthState{
		State:        state,
		Provider:     provider,
		CodeVerifier: verifier,
		CreatedAt:    time.Now(),
	}

	_, err = db.Instance.OAuthStatesCollection.Insert(state, stateDoc, &gocb.InsertOptions{
		Expiry: 300 * time.Second,
	})
	if err != nil {
		log.Printf("Failed to store PKCE oauth state in Couchbase: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to create secure login challenge"})
		return
	}

	challenge := crypto.GenerateCodeChallenge(verifier)
	authURL := cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// OIDC Callback Request
func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Missing code or state parameters from identity provider"})
		return
	}

	// Retrieve PKCE details from state mapping
	var stateDoc models.OAuthState
	err := db.Instance.OAuthStatesCollection.Get(state, nil).Content(&stateDoc)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid, expired, or CSRF-aborted state challenge"})
		return
	}
	_, _ = db.Instance.OAuthStatesCollection.Remove(state, nil)

	cfg, err := getOauthConfig(stateDoc.Provider)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Provider configuration lost"})
		return
	}

	// Exchange Authorization Code for Token (inject verifier)
	token, err := cfg.Exchange(r.Context(), code, oauth2.SetAuthURLParam("code_verifier", stateDoc.CodeVerifier))
	if err != nil {
		log.Printf("OIDC token exchange failed: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to authenticate code with provider"})
		return
	}

	// Fetch Profile from Provider UserInfo endpoints
	email, err := fetchOIDCProfileEmail(stateDoc.Provider, token.AccessToken)
	if err != nil {
		log.Printf("Failed to fetch provider user profile: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to retrieve identity attributes from provider"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Check if user already exists
	queryStr := fmt.Sprintf("SELECT id, email, role FROM `%s`.`auth`.`users` WHERE email = $1 LIMIT 1", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{email},
	})
	if err != nil {
		log.Printf("DB error looking up OIDC user: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error mapping social profile"})
		return
	}
	defer rows.Close()

	var userDoc models.User
	userExists := rows.Next()

	if userExists {
		err = rows.Row(&userDoc)
		if err != nil {
			log.Printf("Deserialization failed for social user: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else {
		// Register a new user automatically
		userID := "usr_" + uuid.New().String()
		userDoc = models.User{
			ID:           userID,
			Email:        email,
			PasswordHash: "", // OIDC users have no password hash
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			Role:         "authenticated",
		}
		_, err = db.Instance.Collection.Insert(userID, userDoc, nil)
		if err != nil {
			log.Printf("DB insert failure for OIDC registration: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Create platform session
	_, refreshToken, err := createSession(userDoc.ID)
	if err != nil {
		log.Printf("Failed to create SSO session: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	accessToken, err := crypto.GenerateAccessToken(userDoc.ID, userDoc.Role)
	if err != nil {
		log.Printf("Failed to sign access token for SSO: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	clientRedirect := os.Getenv("CLIENT_REDIRECT_URL")
	if clientRedirect == "" {
		clientRedirect = "http://localhost:3000/callback"
	}

	// Redirect to Client application with final credentials in the URL fragment
	finalURL := fmt.Sprintf("%s#access_token=%s&refresh_token=%s&expires_in=3600&token_type=bearer", clientRedirect, accessToken, refreshToken)
	http.Redirect(w, r, finalURL, http.StatusTemporaryRedirect)
}

// Helper: Setup Standard OAuth2 configurations dynamically
func getOauthConfig(provider string) (*oauth2.Config, error) {
	redirectURLBase := os.Getenv("REDIRECT_URL_BASE")
	if redirectURLBase == "" {
		redirectURLBase = "http://localhost:8001"
	}
	redirectURL := redirectURLBase + "/auth/v1/callback"

	provider = strings.ToLower(provider)
	switch provider {
	case "google":
		return &oauth2.Config{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  redirectURL,
			Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
		}, nil
	case "github":
		return &oauth2.Config{
			ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
			ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
			RedirectURL:  redirectURL,
			Scopes:       []string{"user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported identity provider: %s", provider)
	}
}

// Helper: Fetch OIDC profiles via provider APIs
func fetchOIDCProfileEmail(provider string, accessToken string) (string, error) {
	var url string
	provider = strings.ToLower(provider)
	if provider == "google" {
		url = "https://www.googleapis.com/oauth2/v2/userinfo"
	} else if provider == "github" {
		url = "https://api.github.com/user"
	} else {
		return "", fmt.Errorf("profile lookup not implemented for %s", provider)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("provider returned non-200 code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Dynamic JSON extraction
	var profileMap map[string]interface{}
	if err := json.Unmarshal(body, &profileMap); err != nil {
		return "", err
	}

	if provider == "google" {
		if email, ok := profileMap["email"].(string); ok {
			return email, nil
		}
	} else if provider == "github" {
		// GitHub might return email null if private, but standard profiles return it if public
		if email, ok := profileMap["email"].(string); ok && email != "" {
			return email, nil
		}
		// Fallback for private github email addresses: query user/emails API
		return fetchGitHubPrivateEmail(accessToken)
	}

	return "", fmt.Errorf("email claim missing in provider response payload")
}

// Fallback: Fetch private GitHub email addresses
func fetchGitHubPrivateEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var emails []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}

	for _, e := range emails {
		if primary, ok := e["primary"].(bool); ok && primary {
			if email, ok := e["email"].(string); ok {
				return email, nil
			}
		}
	}

	return "", fmt.Errorf("no primary email found on github account profile")
}

// Helper: Common Session Creation
func createSession(userID string) (string, string, error) {
	sessionID := "sess_" + uuid.New().String()
	refreshToken, err := crypto.GenerateRefreshToken()
	if err != nil {
		return "", "", err
	}

	sessionDoc := models.Session{
		ID:           sessionID,
		UserID:       userID,
		RefreshToken: refreshToken,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
		Revoked:      false,
	}

	_, err = db.Instance.SessionsCollection.Insert(sessionID, sessionDoc, nil)
	if err != nil {
		return "", "", err
	}

	return sessionID, refreshToken, nil
}
