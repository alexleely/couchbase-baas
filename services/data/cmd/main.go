package main

import (
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

// GET /rest/v1/db/{scope}/{collection} (Query translation and document listing)
func handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")

	if scopeName == "" || collectionName == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope and collection parameters are required"})
		return
	}

	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	// Translate URL parameters to N1QL/SQL++
	statement, params, err := parseURLQuery(r.URL.Query(), bucketName, scopeName, collectionName)
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

	// Check if sub-document query path is requested
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

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(val)
		return
	}

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

// PATCH /rest/v1/db/{scope}/{collection}/{id} (Sub-document updates)
func handlePatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scopeName := r.PathValue("scope")
	collectionName := r.PathValue("collection")
	id := r.PathValue("id")

	if scopeName == "" || collectionName == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Scope, collection, and document ID parameters are required"})
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

	var specs []gocb.MutateInSpec
	for path, val := range patchMap {
		specs = append(specs, gocb.UpsertSpec(path, val, nil))
	}

	_, err := collectionObj.MutateIn(id, specs, nil)
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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		ID:     id,
		Status: "mutated",
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

// POST /rest/v1/db/query
func handleQuery(w http.ResponseWriter, r *http.Request) {
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

// URL query parameter translator helper
func parseURLQuery(queryValues url.Values, bucketName, scope, collection string) (string, []interface{}, error) {
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
