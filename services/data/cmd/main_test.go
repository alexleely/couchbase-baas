package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestQueryInvalidJSON(t *testing.T) {
	req, err := http.NewRequest("POST", "/rest/v1/db/query", bytes.NewBuffer([]byte("{invalid-json}")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleQuery(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for invalid query JSON body, got: %d", rr.Code)
	}
}

func TestQueryEmptyStatement(t *testing.T) {
	req, err := http.NewRequest("POST", "/rest/v1/db/query", bytes.NewBuffer([]byte(`{"statement":""}`)))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handleQuery(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for empty query statement, got: %d", rr.Code)
	}
}

func TestURLQueryTranslator(t *testing.T) {
	values := url.Values{}
	values.Add("select", "id,name,age")
	values.Add("age", "gte.18")
	values.Add("status", "eq.active")
	values.Add("order", "created_at.desc")
	values.Add("limit", "10")
	values.Add("offset", "5")

	stmt, params, err := parseURLQuery(values, "default", "myscope", "mycollection")
	if err != nil {
		t.Fatalf("failed to parse URL query values: %v", err)
	}

	if !strings.Contains(stmt, "SELECT `id`, `name`, `age` FROM") {
		t.Errorf("projection fields parsing failed, got: %s", stmt)
	}

	if !strings.Contains(stmt, "ORDER BY `created_at` DESC") {
		t.Errorf("order parsing failed, got: %s", stmt)
	}
	if !strings.Contains(stmt, "LIMIT 10") {
		t.Errorf("limit parsing failed, got: %s", stmt)
	}
	if !strings.Contains(stmt, "OFFSET 5") {
		t.Errorf("offset parsing failed, got: %s", stmt)
	}

	if !strings.Contains(stmt, "`age` >= $") || !strings.Contains(stmt, "`status` = $") {
		t.Errorf("WHERE predicates parsing failed, got: %s", stmt)
	}

	if len(params) != 2 {
		t.Errorf("expected 2 bound query parameters, got: %d", len(params))
	}
}

func TestPatchInvalidJSON(t *testing.T) {
	req, err := http.NewRequest("PATCH", "/rest/v1/db/myscope/mycol/myid", bytes.NewBuffer([]byte("{invalid-json}")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handlePatch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for invalid PATCH body, got: %d", rr.Code)
	}
}

func TestPatchEmptyBody(t *testing.T) {
	req, err := http.NewRequest("PATCH", "/rest/v1/db/myscope/mycol/myid", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handlePatch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status code 400 for empty PATCH body, got: %d", rr.Code)
	}
}
