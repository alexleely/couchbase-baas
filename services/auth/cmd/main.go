package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Service: "Auth Service",
			Status:  "UP",
			Version: "0.1.0",
		})
	})

	log.Printf("Auth Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start auth server: %v", err)
	}
}
