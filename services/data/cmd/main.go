package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/couchbase/gocb/v2"
	"github.com/google/uuid"
	"github.com/golang-jwt/jwt/v5"

	"data/internal/db"
	"data/internal/models"
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

type CustomClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
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

	// Schema discovery endpoint
	http.HandleFunc("GET /rest/v1/db/schema", handleSchema)

	// REST CRUD Dynamic endpoints (Go 1.22 path patterns)
	http.HandleFunc("POST /rest/v1/db/{scope}/{collection}", handleCreate)
	http.HandleFunc("GET /rest/v1/db/{scope}/{collection}", handleList)
	http.HandleFunc("GET /rest/v1/db/{scope}/{collection}/{id}", handleRead)
	http.HandleFunc("PUT /rest/v1/db/{scope}/{collection}/{id}", handleUpdate)
	http.HandleFunc("PATCH /rest/v1/db/{scope}/{collection}/{id}", handlePatch)
	http.HandleFunc("DELETE /rest/v1/db/{scope}/{collection}/{id}", handleDelete)

	// SQL++ query endpoint
	http.HandleFunc("POST /rest/v1/db/query", handleQuery)

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

// GET /rest/v1/db/schema
func handleSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Query scopes
	scopeQuery := fmt.Sprintf("SELECT name FROM system:scopes WHERE bucket = '%s'", bucketName)
	rows, err := db.Instance.Cluster.Query(scopeQuery, nil)
	if err != nil {
		// Fallback setup in case system catalogs query is restricted
		results := []map[string]interface{}{
			{"name": "_default", "collections": []string{"_default"}},
			{"name": "auth", "collections": []string{"users", "sessions", "policies"}},
			{"name": "storage", "collections": []string{"objects", "buckets"}},
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"scopes": results})
		return
	}
	defer rows.Close()

	type Scope struct {
		Name        string   `json:"name"`
		Collections []string `json:"collections"`
	}

	scopesMap := make(map[string]*Scope)
	for rows.Next() {
		var row struct {
			Name string `json:"name"`
		}
		if err := rows.Row(&row); err == nil {
			if !strings.HasPrefix(row.Name, "_") || row.Name == "_default" {
				scopesMap[row.Name] = &Scope{Name: row.Name, Collections: []string{}}
			}
		}
	}

	// Query collections (keyspaces)
	keyspaceQuery := fmt.Sprintf("SELECT name, scope FROM system:keyspaces WHERE bucket = '%s'", bucketName)
	krows, err := db.Instance.Cluster.Query(keyspaceQuery, nil)
	if err == nil {
		defer krows.Close()
		for krows.Next() {
			var row struct {
				Name  string `json:"name"`
				Scope string `json:"scope"`
			}
			if err := krows.Row(&row); err == nil {
				if sc, ok := scopesMap[row.Scope]; ok {
					sc.Collections = append(sc.Collections, row.Name)
				}
			}
		}
	}

	var results []Scope
	for _, sc := range scopesMap {
		results = append(results, *sc)
	}

	if len(results) == 0 {
		results = []Scope{
			{Name: "_default", Collections: []string{"_default"}},
			{Name: "auth", Collections: []string{"users", "sessions", "policies"}},
			{Name: "storage", Collections: []string{"objects", "buckets"}},
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"scopes": results})
}

// POST /rest/v1/db/{scope}/{collection}
func handleCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
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

	// Trigger CDC Event asynchronously
	go triggerCDCEvent(scopeName, collectionName, "INSERT", id, bodyMap)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "created",
	})
}

// GET /rest/v1/db/{scope}/{collection} (Query translation and document listing)
func handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")

	if scopeName == "" || collectionName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope and collection parameters are required"})
		return
	}

	uid, role, err := authenticateRequest(r)
	if err != nil && err.Error() != "no auth header" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	bucketName := os.Getenv("COUCHBASE_CONTAINER_BUCKET")
	if bucketName == "" {
		bucketName = os.Getenv("COUCHBASE_BUCKET")
	}
	if bucketName == "" {
		bucketName = "default"
	}

	// Fetch SELECT policies
	policies, _ := fetchCollectionPolicies(scopeName, collectionName, "SELECT")

	// Translate URL parameters to N1QL/SQL++
	statement, params, err := parseURLQuery(r.URL.Query(), bucketName, scopeName, collectionName, policies, uid, role)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to parse query parameters: %v", err)})
		return
	}

	opts := gocb.QueryOptions{}
	if len(params) > 0 {
		opts.PositionalParameters = params
	}

	rows, err := db.Instance.Cluster.Query(statement, &opts)
	if err != nil {
		log.Printf("List query failed: %v (Statement: %s)", err, statement)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Database query failed: %v", err)})
		return
	}
	defer rows.Close()

	var results []interface{}
	for rows.Next() {
		var row interface{}
		if err := rows.Row(&row); err != nil {
			log.Printf("Failed to deserialize row: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to deserialize result rows"})
			return
		}
		results = append(results, row)
	}

	w.WriteHeader(http.StatusOK)
	if results == nil {
		results = []interface{}{}
	}
	json.NewEncoder(w).Encode(results)
}

// GET /rest/v1/db/{scope}/{collection}/{id}
func handleRead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	uid, role, err := authenticateRequest(r)
	if err != nil && err.Error() != "no auth header" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	path := r.URL.Query().Get("path")
	if path != "" {
		res, err := collectionObj.LookupIn(id, []gocb.LookupInSpec{
			gocb.GetSpec(path, nil),
		}, nil)
		if err != nil {
			log.Printf("Sub-document LookupIn failed: %v", err)
			if errors.Is(err, gocb.ErrDocumentNotFound) {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Document not found"})
				return
			}
			if errors.Is(err, gocb.ErrPathNotFound) || strings.Contains(err.Error(), "path not found") {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Path '%s' not found inside document", path)})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to query sub-document path"})
			return
		}

		var val interface{}
		err = res.ContentAt(0, &val)
		if err != nil {
			log.Printf("Sub-document content extraction failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to extract sub-document path content"})
			return
		}

		// Enforce RLS policy for sub-document if retrieved document holds ownership structures
		policies, _ := fetchCollectionPolicies(scopeName, collectionName, "SELECT")
		if len(policies) > 0 {
			var parentDoc map[string]interface{}
			err = collectionObj.Get(id, nil).Content(&parentDoc)
			if err == nil {
				allowed := false
				for _, policy := range policies {
					if evaluatePolicyInMemory(parentDoc, policy.Expression, uid, role) {
						allowed = true
						break
					}
				}
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied by Row-Level Security (RLS) policies"})
					return
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(val)
		return
	}

	var doc map[string]interface{}
	err = collectionObj.Get(id, nil).Content(&doc)
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

	// Enforce RLS
	policies, _ := fetchCollectionPolicies(scopeName, collectionName, "SELECT")
	if len(policies) > 0 {
		allowed := false
		for _, policy := range policies {
			if evaluatePolicyInMemory(doc, policy.Expression, uid, role) {
				allowed = true
				break
			}
		}
		if !allowed {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied by Row-Level Security (RLS) policies"})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(doc)
}

// PUT /rest/v1/db/{scope}/{collection}/{id}
func handleUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	uid, role, err := authenticateRequest(r)
	if err != nil && err.Error() != "no auth header" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	var bodyMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&bodyMap); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	bodyMap["id"] = id

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	// Enforce RLS on existing doc
	policies, _ := fetchCollectionPolicies(scopeName, collectionName, "UPDATE")
	if len(policies) > 0 {
		var existingDoc map[string]interface{}
		err = collectionObj.Get(id, nil).Content(&existingDoc)
		if err == nil {
			allowed := false
			for _, policy := range policies {
				if evaluatePolicyInMemory(existingDoc, policy.Expression, uid, role) {
					allowed = true
					break
				}
			}
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied by Row-Level Security (RLS) policies"})
				return
			}
		}
	}

	_, err = collectionObj.Upsert(id, bodyMap, nil)
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

	// Trigger CDC Event asynchronously
	go triggerCDCEvent(scopeName, collectionName, "UPDATE", id, bodyMap)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "upserted",
	})
}

// PATCH /rest/v1/db/{scope}/{collection}/{id}
func handlePatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	uid, role, err := authenticateRequest(r)
	if err != nil && err.Error() != "no auth header" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	var patchMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patchMap); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	if len(patchMap) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "PATCH body must contain at least one modification path"})
		return
	}

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	// Enforce RLS on existing doc
	policies, _ := fetchCollectionPolicies(scopeName, collectionName, "UPDATE")
	if len(policies) > 0 {
		var existingDoc map[string]interface{}
		err = collectionObj.Get(id, nil).Content(&existingDoc)
		if err == nil {
			allowed := false
			for _, policy := range policies {
				if evaluatePolicyInMemory(existingDoc, policy.Expression, uid, role) {
					allowed = true
					break
				}
			}
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied by Row-Level Security (RLS) policies"})
				return
			}
		}
	}

	var specs []gocb.MutateInSpec
	for path, val := range patchMap {
		specs = append(specs, gocb.UpsertSpec(path, val, nil))
	}

	_, err = collectionObj.MutateIn(id, specs, nil)
	if err != nil {
		log.Printf("Sub-document MutateIn failed: %v", err)
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
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to mutate sub-document paths"})
		return
	}

	// Trigger CDC Event asynchronously (send patched values)
	go triggerCDCEvent(scopeName, collectionName, "UPDATE", id, patchMap)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "mutated",
	})
}

// DELETE /rest/v1/db/{scope}/{collection}/{id}
func handleDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
		return
	}

	uid, role, err := authenticateRequest(r)
	if err != nil && err.Error() != "no auth header" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	scopeObj := db.Instance.Bucket.Scope(scopeName)
	collectionObj := scopeObj.Collection(collectionName)

	// Enforce RLS on existing doc
	policies, _ := fetchCollectionPolicies(scopeName, collectionName, "DELETE")
	if len(policies) > 0 {
		var existingDoc map[string]interface{}
		err = collectionObj.Get(id, nil).Content(&existingDoc)
		if err == nil {
			allowed := false
			for _, policy := range policies {
				if evaluatePolicyInMemory(existingDoc, policy.Expression, uid, role) {
					allowed = true
					break
				}
			}
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Access denied by Row-Level Security (RLS) policies"})
				return
			}
		}
	}

	_, err = collectionObj.Remove(id, nil)
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

	// Trigger CDC Event asynchronously
	go triggerCDCEvent(scopeName, collectionName, "DELETE", id, nil)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "deleted",
	})
}

// POST /rest/v1/db/query
func handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")

	var req models.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid JSON request body"})
		return
	}

	req.Statement = strings.TrimSpace(req.Statement)
	if req.Statement == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "SQL++ statement is required"})
		return
	}

	opts := gocb.QueryOptions{}
	if len(req.Parameters) > 0 {
		opts.PositionalParameters = req.Parameters
	}
	if len(req.NamedParameters) > 0 {
		opts.NamedParameters = req.NamedParameters
	}

	rows, err := db.Instance.Cluster.Query(req.Statement, &opts)
	if err != nil {
		log.Printf("SQL++ query execution failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Query execution failed: %v", err)})
		return
	}
	defer rows.Close()

	var results []interface{}
	for rows.Next() {
		var row interface{}
		if err := rows.Row(&row); err != nil {
			log.Printf("Failed to deserialize row: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to deserialize result rows"})
			return
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error during row iteration: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Error reading results"})
		return
	}

	resp := models.QueryResponse{
		Results: results,
	}

	metadata, err := rows.Metadata()
	if err == nil && metadata != nil {
		resp.Metadata = &models.QueryMetadata{
			RequestID:       metadata.RequestID,
			ClientContextID: metadata.ClientContextID,
			Status:          string(metadata.Status),
			Metrics:         metadata.Metrics,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Helper: Authenticate client JWT access tokens
func authenticateRequest(r *http.Request) (string, string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", "anon", errors.New("no auth header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", "", errors.New("Bearer authorization token is required")
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

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
		return "", "", errors.New("invalid or expired access token")
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return "", "", errors.New("invalid token claims")
	}

	return claims.Subject, claims.Role, nil
}

// Helper: Retrieve active RLS policies from Couchbase
func fetchCollectionPolicies(scopeName, collectionName, action string) ([]models.Policy, error) {
	bucketName := os.Getenv("COUCHBASE_CONTAINER_BUCKET")
	if bucketName == "" {
		bucketName = os.Getenv("COUCHBASE_BUCKET")
	}
	if bucketName == "" {
		bucketName = "default"
	}

	queryStr := fmt.Sprintf("SELECT id, scope, collection, action, expression FROM `%s`.`auth`.`policies` WHERE scope = $1 AND collection = $2 AND action = $3", bucketName)
	rows, err := db.Instance.Cluster.Query(queryStr, &gocb.QueryOptions{
		PositionalParameters: []interface{}{scopeName, collectionName, action},
	})
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var policies []models.Policy
	for rows.Next() {
		var policy models.Policy
		if err := rows.Row(&policy); err == nil {
			policies = append(policies, policy)
		}
	}
	return policies, nil
}

// Helper: Evaluate RLS expression in-memory against KV document attributes
func evaluatePolicyInMemory(doc map[string]interface{}, expression string, uid string, role string) bool {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true
	}

	operators := []string{"==", "!=", "=", "<>", ">=", "<=", ">", "<"}
	var op string
	var left, right string

	for _, o := range operators {
		if strings.Contains(expression, o) {
			op = o
			parts := strings.SplitN(expression, o, 2)
			left = strings.TrimSpace(parts[0])
			right = strings.TrimSpace(parts[1])
			break
		}
	}

	if op == "" {
		field := strings.Trim(expression, "` ")
		if val, ok := doc[field].(bool); ok {
			return val
		}
		return false
	}

	left = strings.Trim(left, "` ")
	val, ok := doc[left]
	if !ok {
		return false
	}

	compareVal := right
	if right == "$uid" {
		compareVal = uid
	} else if right == "$role" {
		compareVal = role
	} else {
		compareVal = strings.Trim(right, `"'`)
	}

	valStr := fmt.Sprintf("%v", val)

	switch op {
	case "==", "=":
		return valStr == compareVal
	case "!=", "<>":
		return valStr != compareVal
	default:
		return false
	}
}

// URL query parameter translator helper
func parseURLQuery(queryValues url.Values, bucketName, scope, collection string, policies []models.Policy, uid, role string) (string, []interface{}, error) {
	selectFields := "*"
	if selectVal := queryValues.Get("select"); selectVal != "" {
		fields := strings.Split(selectVal, ",")
		for i, f := range fields {
			fields[i] = fmt.Sprintf("`%s`", strings.TrimSpace(f))
		}
		selectFields = strings.Join(fields, ", ")
	}

	var whereClauses []string
	var params []interface{}
	paramIndex := 1

	operators := map[string]string{
		"eq":   "=",
		"neq":  "!=",
		"gt":   ">",
		"gte":  ">=",
		"lt":   "<",
		"lte":  "<=",
		"like": "LIKE",
	}

	for key, values := range queryValues {
		if key == "select" || key == "order" || key == "limit" || key == "offset" {
			continue
		}

		for _, val := range values {
			op := "="
			compareVal := val
			if parts := strings.SplitN(val, ".", 2); len(parts) == 2 {
				if mappedOp, ok := operators[parts[0]]; ok {
					op = mappedOp
					compareVal = parts[1]
				}
			}

			whereClauses = append(whereClauses, fmt.Sprintf("`%s` %s $%d", key, op, paramIndex))
			params = append(params, compareVal)
			paramIndex++
		}
	}

	// Enforce Row-Level Security (RLS) WHERE predicates
	if len(policies) > 0 {
		var RLSClauses []string
		for _, policy := range policies {
			expr := policy.Expression
			if strings.Contains(expr, "$uid") {
				expr = strings.ReplaceAll(expr, "$uid", fmt.Sprintf("$%d", paramIndex))
				params = append(params, uid)
				paramIndex++
			}
			if strings.Contains(expr, "$role") {
				expr = strings.ReplaceAll(expr, "$role", fmt.Sprintf("$%d", paramIndex))
				params = append(params, role)
				paramIndex++
			}
			RLSClauses = append(RLSClauses, fmt.Sprintf("(%s)", expr))
		}
		combinedRLS := "(" + strings.Join(RLSClauses, " OR ") + ")"
		whereClauses = append(whereClauses, combinedRLS)
	}

	// Build SQL++ statement
	stmt := fmt.Sprintf("SELECT %s FROM `%s`.`%s`.`%s`", selectFields, bucketName, scope, collection)
	if len(whereClauses) > 0 {
		stmt += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	if orderVal := queryValues.Get("order"); orderVal != "" {
		parts := strings.Split(orderVal, ".")
		field := parts[0]
		direction := "ASC"
		if len(parts) > 1 && strings.ToUpper(parts[1]) == "DESC" {
			direction = "DESC"
		}
		stmt += fmt.Sprintf(" ORDER BY `%s` %s", field, direction)
	}

	limit := "100"
	if limitVal := queryValues.Get("limit"); limitVal != "" {
		limit = limitVal
	}
	stmt += fmt.Sprintf(" LIMIT %s", limit)

	if offsetVal := queryValues.Get("offset"); offsetVal != "" {
		stmt += fmt.Sprintf(" OFFSET %s", offsetVal)
	}

	return stmt, params, nil
}

// Asynchronously push mutation details to the Realtime CDC webhook gateway
func triggerCDCEvent(scope, collection, event, id string, doc interface{}) {
	realtimeURL := os.Getenv("REALTIME_SERVICE_URL")
	if realtimeURL == "" {
		realtimeURL = "http://localhost:8003"
	}
	endpoint := realtimeURL + "/realtime/v1/cdc-gateway"

	payload := map[string]interface{}{
		"scope":      scope,
		"collection": collection,
		"event":      event,
		"id":         id,
		"doc":        doc,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[CDC Warning] Failed to marshal CDC event request body: %v", err)
		return
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("[CDC Warning] Failed to connect to Realtime CDC webhook: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[CDC Warning] Realtime CDC gateway returned error status code: %d", resp.StatusCode)
	}
}
