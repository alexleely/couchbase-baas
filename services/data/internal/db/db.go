package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/couchbase/gocb/v2"
)

type DB struct {
	Cluster *gocb.Cluster
	Bucket  *gocb.Bucket
}

var Instance *DB

// InitDB connects to the Couchbase cluster and verifies the bucket connection.
func InitDB() error {
	connStr := os.Getenv("COUCHBASE_CONN_STR")
	if connStr == "" {
		connStr = "couchbase://localhost"
	}
	user := os.Getenv("COUCHBASE_USER")
	if user == "" {
		user = "Administrator"
	}
	pass := os.Getenv("COUCHBASE_PASS")
	if pass == "" {
		pass = "password"
	}
	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	log.Printf("Data Service connecting to Couchbase at %s (bucket: %s)...", connStr, bucketName)

	opts := gocb.ClusterOptions{
		Username: user,
		Password: pass,
	}

	var cluster *gocb.Cluster
	var err error

	// Retry connection to allow Couchbase service to warm up in Docker Compose environment
	for i := 1; i <= 12; i++ {
		cluster, err = gocb.Connect(connStr, opts)
		if err == nil {
			break
		}
		log.Printf("[Attempt %d/12] Couchbase connection failed: %v. Retrying in 5 seconds...", i, err)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to Couchbase after retries: %w", err)
	}

	bucket := cluster.Bucket(bucketName)

	// Wait until bucket is ready
	err = bucket.WaitUntilReady(20*time.Second, nil)
	if err != nil {
		return fmt.Errorf("couchbase bucket not ready: %w", err)
	}

	Instance = &DB{
		Cluster: cluster,
		Bucket:  bucket,
	}

	log.Println("Data Service Couchbase connection initialized successfully.")
	return nil
}
