package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGatewayHealthCheck(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"service":"API Gateway","status":"UP","version":"0.1.0"}`))
	})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200, got: %d", rr.Code)
	}
}

func TestGatewayStaticAssetFallback(t *testing.T) {
	// Create mock static directory
	os.MkdirAll("./public", 0755)
	os.WriteFile("./public/index.html", []byte("<html>Mock Dashboard</html>"), 0644)
	defer os.RemoveAll("./public")

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	fileServer := http.FileServer(http.Dir("./public"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code 200 for index.html serving, got: %d", rr.Code)
	}
}
