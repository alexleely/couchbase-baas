package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleHealth)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200, got: %d", rr.Code)
	}
}

func TestUploadUninitializedClient(t *testing.T) {
	req, err := http.NewRequest("POST", "/storage/v1/object/b/f", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleUpload(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status code 500 when client is uninitialized, got: %d", rr.Code)
	}
}

func TestDownloadUninitializedClient(t *testing.T) {
	req, err := http.NewRequest("GET", "/storage/v1/object/b/f", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleDownload(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status code 500 when client is uninitialized, got: %d", rr.Code)
	}
}

func TestDeleteUninitializedClient(t *testing.T) {
	req, err := http.NewRequest("DELETE", "/storage/v1/object/b/f", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleDelete(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status code 500 when client is uninitialized, got: %d", rr.Code)
	}
}

func TestAuthenticateRequestFallback(t *testing.T) {
	req, err := http.NewRequest("GET", "/some-route", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	ownerID, err := authenticateRequest(req)
	if err == nil {
		t.Errorf("expected authentication error when no header is present")
	}
	if ownerID != "anon" {
		t.Errorf("expected default owner ID to be anon, got: %s", ownerID)
	}
}

func TestVerifySignedTokenInvalid(t *testing.T) {
	// Mismatched token details should evaluate to false
	valid := verifySignedToken("invalid-token-signature-here", "b", "f")
	if valid {
		t.Errorf("expected signature check to fail for invalid token")
	}
}

func TestSignURLRequiresAuth(t *testing.T) {
	req, err := http.NewRequest("POST", "/storage/v1/object/sign/b/f", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleSignURL(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status code 401 Unauthorized for unauthenticated URL signing requests, got: %d", rr.Code)
	}
}

func TestIsBucketPublicDefault(t *testing.T) {
	// Buckets should default to private (false) when database is uninitialized
	isPub := isBucketPublic("some-bucket")
	if isPub {
		t.Errorf("expected unconfigured bucket to default to private (false)")
	}
}
