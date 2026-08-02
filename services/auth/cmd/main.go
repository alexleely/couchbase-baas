package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/couchbase/gocb/v2"
	"golang.org/x/oauth2"
	"github.com/crewjam/saml"

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

// Global Service Provider Certificate variables
var (
	spPrivateKey  *rsa.PrivateKey
	spCertificate *x509.Certificate
)

func init() {
	// Dynamically generate a self-signed keypair for SAML Service Provider on launch
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate dynamic SAML keypair: %v", err)
	}
	spPrivateKey = priv

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Couchbase-BaaS-SP",
			Organization: []string{"Couchbase Developer Platform"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		log.Fatalf("Failed to create dynamic SAML SP certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		log.Fatalf("Failed to parse dynamic SAML SP certificate: %v", err)
	}
	spCertificate = cert
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
	http.HandleFunc("POST /auth/v1/logout", handleLogout)

	// OIDC Social Login endpoints
	http.HandleFunc("GET /auth/v1/authorize", handleAuthorize)
	http.HandleFunc("GET /auth/v1/callback", handleCallback)

	// SAML SSO endpoints
	http.HandleFunc("GET /auth/v1/saml/metadata", handleSAMLMetadata)
	http.HandleFunc("GET /auth/v1/saml/authorize", handleSAMLAuthorize)
	http.HandleFunc("POST /auth/v1/saml/acs", handleSAMLACS)

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

	var req models.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	// Default to password grant if not provided
	if req.GrantType == "" {
		req.GrantType = "password"
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	if req.GrantType == "password" {
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.Password == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Email and password are required"})
			return
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
		_, refreshToken, err := createSession(userDoc.ID)
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Generate access token JWT
		accessToken, err := crypto.GenerateAccessToken(userDoc.ID, userDoc.Role)
		if err != nil {
			log.Printf("Failed to generate access token JWT: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.TokenResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresIn:    3600,
			TokenType:    "bearer",
		})
		return

	} else if req.GrantType == "refresh_token" {
		if req.RefreshToken == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "refresh_token is required"})
			return
		}

		// Lookup active session matching the refresh token
		queryStr := fmt.Sprintf("SELECT id, user_id, refresh_token, expires_at, revoked FROM `%s`.`auth`.`sessions` WHERE refresh_token = $1 LIMIT 1", bucketName)
		rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
			PositionalParameters: []interface{}{req.RefreshToken},
		})
		if err != nil {
			log.Printf("Database query error looking up session: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var sessionDoc models.Session
		if !rows.Next() {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid or expired refresh token"})
			return
		}

		err = rows.Row(&sessionDoc)
		if err != nil {
			log.Printf("Failed to deserialize session row: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Validate expiration and revocation
		if sessionDoc.Revoked || time.Now().After(sessionDoc.ExpiresAt) {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid or expired refresh token"})
			return
		}

		// Fetch the user's role from user document
		queryUser := fmt.Sprintf("SELECT role FROM `%s`.`auth`.`users` WHERE id = $1 LIMIT 1", bucketName)
		userRows, err := db.Instance.Cluster.Query(queryUser, &gocb.QueryOptions{
			PositionalParameters: []interface{}{sessionDoc.UserID},
		})
		if err != nil {
			log.Printf("Database query error looking up user role: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer userRows.Close()

		userRole := "authenticated"
		var userMap map[string]interface{}
		if userRows.Next() {
			if err := userRows.Row(&userMap); err == nil {
				if r, ok := userMap["role"].(string); ok {
					userRole = r
				}
			}
		}

		// Rotate the refresh token
		newRefreshToken, err := crypto.GenerateRefreshToken()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		sessionDoc.RefreshToken = newRefreshToken
		sessionDoc.ExpiresAt = time.Now().Add(30 * 24 * time.Hour) // Extend session 30 days

		// Update session doc in Couchbase
		_, err = db.Instance.SessionsCollection.Replace(sessionDoc.ID, sessionDoc, nil)
		if err != nil {
			log.Printf("Database error replacing session: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Generate new access token JWT
		accessToken, err := crypto.GenerateAccessToken(sessionDoc.UserID, userRole)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.TokenResponse{
			AccessToken:  accessToken,
			RefreshToken: newRefreshToken,
			ExpiresIn:    3600,
			TokenType:    "bearer",
		})
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ErrorResponse{Error: "Unsupported grant_type"})
}

// Invalidate Session (Logout)
func handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse Authorization Header: Bearer <JWT>
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Bearer authentication token is required"})
		return
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate access token
	claims, err := crypto.ValidateAccessToken(tokenStr)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid or expired access token"})
		return
	}

	var req models.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	if req.RefreshToken == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "refresh_token is required to invalidate the session"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Lookup session matching refresh token
	queryStr := fmt.Sprintf("SELECT id, user_id, refresh_token, expires_at, revoked FROM `%s`.`auth`.`sessions` WHERE refresh_token = $1 LIMIT 1", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{req.RefreshToken},
	})
	if err != nil {
		log.Printf("Database query error looking up session for logout: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sessionDoc models.Session
	if !rows.Next() {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Session not found"})
		return
	}

	err = rows.Row(&sessionDoc)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Security Check: Verify session owner matches JWT subject
	if sessionDoc.UserID != claims.Subject {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied: cannot revoke another user's session"})
		return
	}

	// Mark session as revoked in Couchbase
	sessionDoc.Revoked = true
	_, err = db.Instance.SessionsCollection.Replace(sessionDoc.ID, sessionDoc, nil)
	if err != nil {
		log.Printf("Database replace error revoking session: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	token, err := cfg.Exchange(r.Context(), code, oauth2.SetAuthURLParam("code_verifier", stateDoc.CodeVerifier))
	if err != nil {
		log.Printf("OIDC token exchange failed: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to authenticate code with provider"})
		return
	}

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
		userID := "usr_" + uuid.New().String()
		userDoc = models.User{
			ID:           userID,
			Email:        email,
			PasswordHash: "",
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

	finalURL := fmt.Sprintf("%s#access_token=%s&refresh_token=%s&expires_in=3600&token_type=bearer", clientRedirect, accessToken, refreshToken)
	http.Redirect(w, r, finalURL, http.StatusTemporaryRedirect)
}

// SAML SP Metadata endpoint
func handleSAMLMetadata(w http.ResponseWriter, r *http.Request) {
	spURL := getBaseSPURL()
	sp := saml.ServiceProvider{
		EntityID:    spURL + "/auth/v1/saml/metadata",
		Key:         spPrivateKey,
		Certificate: spCertificate,
		AcsURL:      spURL + "/auth/v1/saml/acs",
		MetadataURL: spURL + "/auth/v1/saml/metadata",
	}

	metadata, err := sp.MetadataWithConfig(nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to generate SP metadata"})
		return
	}

	xmlBytes, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to marshal SP metadata XML"})
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(xml.Header))
	w.Write(xmlBytes)
}

// SAML SSO Redirection endpoint
func handleSAMLAuthorize(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Domain query parameter is required"})
		return
	}

	var provider models.SAMLProvider
	err := db.Instance.SAMLProvidersCollection.Get(domain, nil).Content(&provider)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Unsupported enterprise domain or SSO not configured"})
		return
	}

	idpCertBytes, err := decodeBase64Cert(provider.IdPPublicCert)
	if err != nil {
		log.Printf("Invalid IdP X.509 Certificate in DB: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid SSO identity provider configuration"})
		return
	}

	spURL := getBaseSPURL()
	sp := saml.ServiceProvider{
		EntityID:    spURL + "/auth/v1/saml/metadata",
		Key:         spPrivateKey,
		Certificate: spCertificate,
		AcsURL:      spURL + "/auth/v1/saml/acs",
		MetadataURL: spURL + "/auth/v1/saml/metadata",
		IDPMetadata: &saml.EntityDescriptor{
			EntityID: provider.IdPEntityID,
			IDPSSODescriptors: []saml.IDPSSODescriptor{
				{
					SSOLocations: []saml.Endpoint{
						{
							Binding:  saml.HTTPRedirectBinding,
							Location: provider.IdPSSOURL,
						},
					},
					KeyDescriptors: []saml.KeyDescriptor{
						{
							Use: "signing",
							KeyInfo: saml.KeyInfo{
								X509Data: saml.X509Data{
									X509Certificates: []saml.X509Certificate{
										{Data: base64.StdEncoding.EncodeToString(idpCertBytes)},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	req, err := sp.MakeAuthenticationRequest(provider.IdPSSOURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		log.Printf("Failed to create SAML AuthnRequest: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	redirectURL, err := req.Redirect(domain, &sp)
	if err != nil {
		log.Printf("Failed to generate SAML redirect URL: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirectURL.String(), http.StatusTemporaryRedirect)
}

// SAML ACS Callback endpoint (HTTP-POST)
func handleSAMLACS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid form payload"})
		return
	}

	samlResponse := r.FormValue("SAMLResponse")
	domain := r.FormValue("RelayState")

	if samlResponse == "" || domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Missing SAMLResponse or RelayState"})
		return
	}

	var provider models.SAMLProvider
	err := db.Instance.SAMLProvidersCollection.Get(domain, nil).Content(&provider)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "SAML SSO session expired or domain unsupported"})
		return
	}

	idpCertBytes, err := decodeBase64Cert(provider.IdPPublicCert)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	spURL := getBaseSPURL()
	sp := saml.ServiceProvider{
		EntityID:    spURL + "/auth/v1/saml/metadata",
		Key:         spPrivateKey,
		Certificate: spCertificate,
		AcsURL:      spURL + "/auth/v1/saml/acs",
		MetadataURL: spURL + "/auth/v1/saml/metadata",
		IDPMetadata: &saml.EntityDescriptor{
			EntityID: provider.IdPEntityID,
			IDPSSODescriptors: []saml.IDPSSODescriptor{
				{
					SSOLocations: []saml.Endpoint{
						{
							Binding:  saml.HTTPRedirectBinding,
							Location: provider.IdPSSOURL,
						},
					},
					KeyDescriptors: []saml.KeyDescriptor{
						{
							Use: "signing",
							KeyInfo: saml.KeyInfo{
								X509Data: saml.X509Data{
									X509Certificates: []saml.X509Certificate{
										{Data: base64.StdEncoding.EncodeToString(idpCertBytes)},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	assertion, err := sp.ParseAndValidateAssertion(samlResponse, []string{})
	if err != nil {
		log.Printf("SAML Assertion Validation failure: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "SAML signature validation failed or token expired"})
		return
	}

	email := ""
	for _, attrStatement := range assertion.AttributeStatements {
		for _, attr := range attrStatement.Attributes {
			name := strings.ToLower(attr.Name)
			if name == "email" || name == "mail" || strings.HasSuffix(name, "emailaddress") {
				if len(attr.AttributeValues) > 0 {
					email = attr.AttributeValues[0].Value
					break
				}
			}
		}
	}

	if email == "" && assertion.Subject != nil && assertion.Subject.NameID != nil {
		email = assertion.Subject.NameID.Value
	}

	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Corporate email attribute missing in SAML assertion"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	queryStr := fmt.Sprintf("SELECT id, email, role FROM `%s`.`auth`.`users` WHERE email = $1 LIMIT 1", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{email},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var userDoc models.User
	userExists := rows.Next()

	if userExists {
		err = rows.Row(&userDoc)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else {
		userID := "usr_" + uuid.New().String()
		userDoc = models.User{
			ID:           userID,
			Email:        email,
			PasswordHash: "",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			Role:         "authenticated",
		}
		_, err = db.Instance.Collection.Insert(userID, userDoc, nil)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	_, refreshToken, err := createSession(userDoc.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	accessToken, err := crypto.GenerateAccessToken(userDoc.ID, userDoc.Role)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	clientRedirect := os.Getenv("CLIENT_REDIRECT_URL")
	if clientRedirect == "" {
		clientRedirect = "http://localhost:3000/callback"
	}

	finalURL := fmt.Sprintf("%s#access_token=%s&refresh_token=%s&expires_in=3600&token_type=bearer", clientRedirect, accessToken, refreshToken)
	http.Redirect(w, r, finalURL, http.StatusTemporaryRedirect)
}

// Helpers: SAML URL Resolvers
func getBaseSPURL() string {
	redirectURLBase := os.Getenv("REDIRECT_URL_BASE")
	if redirectURLBase == "" {
		redirectURLBase = "http://localhost:8001"
	}
	return redirectURLBase
}

// Helper: Decode PEM/DER Public X.509 Certificates
func decodeBase64Cert(certData string) ([]byte, error) {
	if strings.Contains(certData, "-----BEGIN CERTIFICATE-----") {
		block, _ := pem.Decode([]byte(certData))
		if block == nil {
			return nil, fmt.Errorf("invalid PEM block")
		}
		return block.Bytes, nil
	}
	
	clean := strings.Join(strings.Fields(certData), "")
	return base64.StdEncoding.DecodeToString(clean)
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

	var profileMap map[string]interface{}
	if err := json.Unmarshal(body, &profileMap); err != nil {
		return "", err
	}

	if provider == "google" {
		if email, ok := profileMap["email"].(string); ok {
			return email, nil
		}
	} else if provider == "github" {
		if email, ok := profileMap["email"].(string); ok && email != "" {
			return email, nil
		}
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
