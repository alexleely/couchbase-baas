package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type HealthResponse struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
}

func healthHandler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := HealthResponse{
			Status:    "healthy",
			Service:   serviceName,
			Version:   "1.0.0",
			Timestamp: time.Now().UTC(),
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func main() {
	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName == "" {
		serviceName = "gateway"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(serviceName))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": fmt.Sprintf("Couchbase Platform %s Service API Gateway", serviceName),
			"version": "1.0.0",
		})
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting %s service on port %s...", serviceName, port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
