package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/couchbase/gocb/v2"
	"github.com/google/uuid"

	"data/internal/db"
)

type HealthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CRUDResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8002"
	}

	// Initialize Couchbase connection
	if err := db.InitDB(); err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}

	// System endpoints
	http.HandleFunc("GET /health", handleHealth)

	// REST CRUD Dynamic endpoints (Go 1.22 path patterns)
	http.HandleFunc("POST /rest/v1/db/{scope}/{collection}", handleCreate)
	http.HandleFunc("GET /rest/v1/db/{scope}/{collection}/{id}", handleRead)
	http.HandleFunc("PUT /rest/v1/db/{scope}/{collection}/{id}", handleUpdate)
	http.HandleFunc("DELETE /rest/v1/db/{scope}/{collection}/{id}", handleDelete)

	log.Printf("Data API Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start data service: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{
		Service: "Data API Service",
		Status:  "UP",
		Version: "0.1.0",
	})
}

// POST /rest/v1/db/{scope}/{collection}
func handleCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")

	if scopeName == "" || collectionName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope and collection parameters are required"})
		return
	}

	var bodyMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&bodyMap); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	// Determine document ID
	id := r.URL.Query().Get("id")
	if id == "" {
		if val, ok := bodyMap["id"].(string); ok && val != "" {
			id = val
		} else if val, ok := bodyMap["_id"].(string); ok && val != "" {
			id = val
		} else {
			id = uuid.New().String()
		}
	}

	// Ensure document ID is set inside JSON payload
	bodyMap["id"] = id

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	_, err := collectionObj.Insert(id, bodyMap, nil)
	if err != nil {
		log.Printf("Create Insert failed: %v", err)
		if errors.Is(err, gocb.ErrDocumentExists) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Document with ID %s already exists", id)})
			return
		}
		if errors.Is(err, gocb.ErrCollectionNotFound) || errors.Is(err, gocb.ErrScopeNotFound) || strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Scope '%s' or collection '%s' does not exist", scopeName, collectionName)})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to write document to database"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "created",
	})
}

// GET /rest/v1/db/{scope}/{collection}/{id}
func handleRead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	var doc map[string]interface{}
	err := collectionObj.Get(id, nil).Content(&doc)
	if err != nil {
		log.Printf("Read Get failed: %v", err)
		if errors.Is(err, gocb.ErrDocumentNotFound) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Document not found"})
			return
		}
		if errors.Is(err, gocb.ErrCollectionNotFound) || errors.Is(err, gocb.ErrScopeNotFound) || strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Scope '%s' or collection '%s' does not exist", scopeName, collectionName)})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to fetch document from database"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(doc)
}

// PUT /rest/v1/db/{scope}/{collection}/{id}
func handleUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	var bodyMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&bodyMap); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	// Force id consistency
	bodyMap["id"] = id

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	_, err := collectionObj.Upsert(id, bodyMap, nil)
	if err != nil {
		log.Printf("Update Upsert failed: %v", err)
		if errors.Is(err, gocb.ErrCollectionNotFound) || errors.Is(err, gocb.ErrScopeNotFound) || strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Scope '%s' or collection '%s' does not exist", scopeName, collectionName)})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to upsert document"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "upserted",
	})
}

// DELETE /rest/v1/db/{scope}/{collection}/{id}
func handleDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	_, err := collectionObj.Remove(id, nil)
	if err != nil {
		log.Printf("Delete Remove failed: %v", err)
		if errors.Is(err, gocb.ErrDocumentNotFound) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Document not found"})
			return
		}
		if errors.Is(err, gocb.ErrCollectionNotFound) || errors.Is(err, gocb.ErrScopeNotFound) || strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Scope '%s' or collection '%s' does not exist", scopeName, collectionName)})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to delete document from database"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "deleted",
	})
}
