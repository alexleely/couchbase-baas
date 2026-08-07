package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	// Read downstream service addresses from environment or fall back to localhost defaults
	authURL, _ := url.Parse(getEnv("AUTH_SERVICE_URL", "http://localhost:8001"))
	dataURL, _ := url.Parse(getEnv("DATA_SERVICE_URL", "http://localhost:8002"))
	realtimeURL, _ := url.Parse(getEnv("REALTIME_SERVICE_URL", "http://localhost:8003"))
	storageURL, _ := url.Parse(getEnv("STORAGE_SERVICE_URL", "http://localhost:8004"))

	authProxy := httputil.NewSingleHostReverseProxy(authURL)
	dataProxy := httputil.NewSingleHostReverseProxy(dataURL)
	realtimeProxy := httputil.NewSingleHostReverseProxy(realtimeURL)
	storageProxy := httputil.NewSingleHostReverseProxy(storageURL)

	// Static assets file server for developer console dashboard
	fileServer := http.FileServer(http.Dir("./public"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Route HTTP request paths to downstream services
		if strings.HasPrefix(path, "/auth/") {
			authProxy.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "/rest/") {
			dataProxy.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "/realtime/") {
			realtimeProxy.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "/storage/") {
			storageProxy.ServeHTTP(w, r)
			return
		}
		if path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"service":"API Gateway","status":"UP","version":"0.1.0"}`))
			return
		}

		// Fallback to static developer console dashboard build assets
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("API Gateway Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start gateway server: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}
