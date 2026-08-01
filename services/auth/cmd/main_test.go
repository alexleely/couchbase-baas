package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexleely/couchbase-baas/services/auth/internal/models"
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
	rr = httptest.NewRecorder()

	handleSignup(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for short password: got %v want %v",
			rr.Code, http.StatusBadRequest)
	}
}
