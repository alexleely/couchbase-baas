package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/couchbase/gocb/v2"
)

type DBInstance struct {
	Cluster *gocb.Cluster
	Bucket  *gocb.Bucket
}

var Instance *DBInstance

func InitDB() error {
	connStr := os.Getenv("COUCHBASE_CONN_STR")
	if connStr == "" {
		connStr = "couchbase://localhost"
	}
	username := os.Getenv("COUCHBASE_USERNAME")
	if username == "" {
		username = "Administrator"
	}
	password := os.Getenv("COUCHBASE_PASSWORD")
	if password == "" {
		password = "password"
	}
	bucketName := os.Getenv("COUCHBASE_BUCKET")
	if bucketName == "" {
		bucketName = "default"
	}

	log.Printf("Connecting to Couchbase cluster at %s...", connStr)
	opts := gocb.ClusterOptions{
		Authenticator: gocb.PasswordAuthenticator{
			Username: username,
			Password: password,
		},
		TimeoutsConfig: gocb.TimeoutsConfig{
			ConnectTimeout: 15 * time.Second,
		},
	}

	cluster, err := gocb.Connect(connStr, opts)
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	bucket := cluster.Bucket(bucketName)
	if err := bucket.WaitUntilReady(5*time.Second, nil); err != nil {
		log.Printf("Warning: bucket %s not ready yet: %v", bucketName, err)
	}

	Instance = &DBInstance{
		Cluster: cluster,
		Bucket:  bucket,
	}
	return nil
}
