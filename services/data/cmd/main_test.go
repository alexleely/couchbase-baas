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
		t.Fatalf("failed to create health check request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(handleHealth)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200, got: %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got: %s", contentType)
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	req, err := http.NewRequest("POST", "/rest/v1/db/myscope/mycol", bytes.NewBuffer([]byte("{invalid-json}")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	// Set Go 1.22 path values mock if using standard ServeMux, but direct call is simple
	handleCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for invalid JSON body, got: %d", rr.Code)
	}
}

func TestUpdateInvalidJSON(t *testing.T) {
	req, err := http.NewRequest("PUT", "/rest/v1/db/myscope/mycol/myid", bytes.NewBuffer([]byte("badjson")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for invalid JSON body on update, got: %d", rr.Code)
	}
}
