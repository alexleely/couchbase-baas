package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang-jwt/jwt/v5"

	storageDB "storage/internal/db"
	storageS3 "storage/internal/s3"
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
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Status string `json:"status"`
}

type CustomClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type FileMetadata struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	MimeType  string    `json:"mime_type"`
	OwnerID   string    `json:"owner_id"`
	Bucket    string    `json:"bucket"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8004"
	}

	// Initialize S3 client connection
	if err := storageS3.InitS3(); err != nil {
		log.Printf("[Warning] Failed to initialize S3 connection: %v. Running in offline/fallback mode.", err)
	}

	// Initialize Couchbase connection
	if err := storageDB.InitDB(); err != nil {
		log.Printf("[Warning] Failed to initialize Couchbase connection: %v. Running in offline/fallback mode.", err)
	}

	// Health check
	http.HandleFunc("GET /health", handleHealth)

	// Object storage endpoints (Go 1.22 path patterns)
	http.HandleFunc("POST /storage/v1/object/{bucket}/{filename}", handleUpload)
	http.HandleFunc("GET /storage/v1/object/{bucket}/{filename}", handleDownload)
	http.HandleFunc("DELETE /storage/v1/object/{bucket}/{filename}", handleDelete)

	log.Printf("Storage Service starting on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start storage service: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{
		Service: "Storage Service",
		Status:  "UP",
		Version: "0.1.0",
	})
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	bucketName := r.PathValue("bucket")
	filename := r.PathValue("filename")

	if bucketName == "" || filename == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Bucket and filename parameters are required"})
		return
	}

	if storageS3.Client == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "S3 connection is not initialized"})
		return
	}

	// Extract owner_id from JWT token if present
	ownerID, _ := authenticateRequest(r)

	ctx := r.Context()

	// Check if bucket exists, if not auto-provision it
	bucketExists := true
	_, err := storageS3.Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: &bucketName,
	})
	if err != nil {
		bucketExists = false
	}

	if !bucketExists {
		log.Printf("Bucket '%s' does not exist. Auto-provisioning bucket...", bucketName)
		_, err = storageS3.Client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: &bucketName,
		})
		if err != nil {
			log.Printf("Failed to auto-provision bucket: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to auto-provision bucket: %v", err)})
			return
		}
	}

	// Upload binary stream to MinIO
	_, err = storageS3.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucketName,
		Key:    &filename,
		Body:   r.Body,
	})
	if err != nil {
		log.Printf("PutObject failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to upload object: %v", err)})
		return
	}

	// Write metadata record to Couchbase storage.objects collection
	if storageDB.Instance != nil && storageDB.Instance.Bucket != nil {
		meta := FileMetadata{
			ID:        bucketName + "/" + filename,
			Name:      filename,
			Size:      r.ContentLength,
			MimeType:  r.Header.Get("Content-Type"),
			OwnerID:   ownerID,
			Bucket:    bucketName,
			Path:      filename,
			CreatedAt: time.Now(),
		}

		scopeObj := storageDB.Instance.Bucket.Scope("storage")
		colObj := scopeObj.Collection("objects")
		_, err = colObj.Upsert(meta.ID, meta, nil)
		if err != nil {
			log.Printf("[Warning] Failed to write file metadata to Couchbase: %v", err)
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CRUDResponse{
		Bucket: bucketName,
		Key:    filename,
		Status: "uploaded",
	})
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	bucketName := r.PathValue("bucket")
	filename := r.PathValue("filename")

	if bucketName == "" || filename == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Bucket and filename parameters are required"})
		return
	}

	if storageS3.Client == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "S3 connection is not initialized"})
		return
	}

	ctx := r.Context()

	// Download binary stream from MinIO
	output, err := storageS3.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucketName,
		Key:    &filename,
	})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Object not found"})
		return
	}
	defer output.Body.Close()

	if output.ContentType != nil {
		w.Header().Set("Content-Type", *output.ContentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	if output.ContentLength != nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", *output.ContentLength))
	}

	w.WriteHeader(http.StatusOK)
	io.Copy(w, output.Body)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	bucketName := r.PathValue("bucket")
	filename := r.PathValue("filename")

	if bucketName == "" || filename == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Bucket and filename parameters are required"})
		return
	}

	if storageS3.Client == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "S3 connection is not initialized"})
		return
	}

	ctx := r.Context()

	// Delete target object from MinIO
	_, err := storageS3.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucketName,
		Key:    &filename,
	})
	if err != nil {
		log.Printf("DeleteObject failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to delete object: %v", err)})
		return
	}

	// Delete metadata record from Couchbase storage.objects collection
	if storageDB.Instance != nil && storageDB.Instance.Bucket != nil {
		scopeObj := storageDB.Instance.Bucket.Scope("storage")
		colObj := scopeObj.Collection("objects")
		metaID := bucketName + "/" + filename
		_, err = colObj.Remove(metaID, nil)
		if err != nil {
			log.Printf("[Warning] Failed to delete file metadata from Couchbase: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(CRUDResponse{
		Bucket: bucketName,
		Key:    filename,
		Status: "deleted",
	})
}

// Helper: Authenticate client JWT access tokens and extract user ID
func authenticateRequest(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return "anon", errors.New("no auth header")
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
		return "anon", errors.New("invalid token")
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return "anon", errors.New("invalid claims")
	}

	return claims.Subject, nil
}
