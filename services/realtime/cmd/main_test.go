package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"service":"Realtime Service","status":"UP","version":"0.1.0"}`))
	})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200, got: %d", rr.Code)
	}
}

func TestWebsocketUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWs(nil, w, r)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/realtime/v1/websocket")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got: %d", resp.StatusCode)
	}
}

func TestWebsocketInvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWs(nil, w, r)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/realtime/v1/websocket?token=invalid-jwt-token-string")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got: %d", resp.StatusCode)
	}
}

func TestCDCGatewayValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleCDCGateway(nil, w, r)
	}))
	defer server.Close()

	// Test invalid JSON body
	resp, err := http.Post(server.URL+"/realtime/v1/cdc-gateway", "application/json", bytes.NewBuffer([]byte("{invalid-json}")))
	if err != nil {
		t.Fatalf("failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status code 400 for invalid JSON body, got: %d", resp.StatusCode)
	}

	// Test missing required fields
	reqBody := `{"scope":"myscope","collection":"","event":"INSERT"}`
	resp, err = http.Post(server.URL+"/realtime/v1/cdc-gateway", "application/json", bytes.NewBuffer([]byte(reqBody)))
	if err != nil {
		t.Fatalf("failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status code 400 for missing collection parameter, got: %d", resp.StatusCode)
	}
}
