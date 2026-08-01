package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auth/internal/models"
)

func TestSignupValidation(t *testing.T) {
	// Test invalid email format
	reqBody, _ := json.Marshal(models.SignupRequest{
		Email:    "invalidemail",
		Password: "password123",
	})
	req, _ := http.NewRequest("POST", "/auth/v1/signup", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()
	
	handleSignup(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for invalid email: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}

	// Test short password length
	reqBody, _ = json.Marshal(models.SignupRequest{
		Email:    "test@example.com",
		Password: "123",
	})
	req, _ = http.NewRequest("POST", "/auth/v1/signup", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()

	handleSignup(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for short password: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}
}

func TestTokenValidation(t *testing.T) {
	// Test missing credentials
	reqBody, _ := json.Marshal(models.SignupRequest{
		Email:    "",
		Password: "",
	})
	req, _ := http.NewRequest("POST", "/auth/v1/token", bytes.NewBuffer(reqBody))
	rr := httptest.NewRecorder()

	handleToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for empty credentials: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}
}

func TestOauthAuthorizeValidation(t *testing.T) {
	// Test missing provider parameter
	req, _ := http.NewRequest("GET", "/auth/v1/authorize", nil)
	rr := httptest.NewRecorder()

	handleAuthorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for missing provider: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}

	// Test invalid provider
	req, _ = http.NewRequest("GET", "/auth/v1/authorize?provider=invalid_provider", nil)
	rr = httptest.NewRecorder()

	handleAuthorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for invalid provider: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}
}

func TestSAMLMetadataGeneration(t *testing.T) {
	req, _ := http.NewRequest("GET", "/auth/v1/saml/metadata", nil)
	rr := httptest.NewRecorder()

	handleSAMLMetadata(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status code 200, got: %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("expected Content-Type application/xml, got: %s", contentType)
	}

	xmlBody := rr.Body.String()
	if !strings.Contains(xmlBody, "EntityDescriptor") {
		t.Errorf("expected SAML Metadata XML to contain EntityDescriptor tag, got:\n%s", xmlBody)
	}
}
