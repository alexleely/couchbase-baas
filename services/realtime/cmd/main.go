package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"realtime/internal/realtime"
)

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

type CustomClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for dev platform gateway
	},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8003"
	}

	// Initialize WebSockets Hub
	hub := realtime.NewHub()
	go hub.Run()

	// System health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Service: "Realtime Service",
			Status:  "UP",
			Version: "0.1.0",
		})
	})

	// WebSockets Upgrade endpoint
	http.HandleFunc("/realtime/v1/websocket", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	log.Printf("Realtime Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start realtime service: %v", err)
	}
}

func serveWs(hub *realtime.Hub, w http.ResponseWriter, r *http.Request) {
	// Authenticate connection via query parameter token
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Authentication token is required"))
		return
	}

	uid, err := verifyToken(tokenStr)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(fmt.Sprintf("Authentication failed: %v", err)))
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade HTTP connection to WebSocket: %v", err)
		return
	}

	client := realtime.NewClient(hub, conn, uid)
	hub.RegisterClient(client)

	go client.WritePump()
	go client.ReadPump()
}

func verifyToken(tokenStr string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "super-secret-key-do-not-use-in-production!"
	}

	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid or expired token")
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return "", fmt.Errorf("invalid token claims")
	}

	return claims.Subject, nil
}
