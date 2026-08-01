package db

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/couchbase/gocb/v2"
)

type DB struct {
	Cluster    *gocb.Cluster
	Bucket     *gocb.Bucket
	Scope      *gocb.Scope
	Collection *gocb.Collection
}

var Instance *DB

// InitDB initializes the Couchbase connection, creates the 'auth' scope, 'users' collection,
// and required indexes programmatically.
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

	log.Printf("Connecting to Couchbase at %s (bucket: %s)...", connStr, bucketName)

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

	collectionsMgr := bucket.Collections()
	
	// Create scope if it doesn't exist
	scopes, err := collectionsMgr.GetAllScopes(nil)
	if err != nil {
		return fmt.Errorf("failed to list Couchbase scopes: %w", err)
	}

	authScopeExists := false
	for _, sc := range scopes {
		if sc.Name == "auth" {
			authScopeExists = true
			break
		}
	}

	if !authScopeExists {
		log.Println("Creating Couchbase scope 'auth'...")
		err = collectionsMgr.CreateScope("auth", nil)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create scope 'auth': %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	// Create collection 'users' under scope 'auth'
	scopes, err = collectionsMgr.GetAllScopes(nil)
	if err != nil {
		return fmt.Errorf("failed to list scopes after scope creation: %w", err)
	}

	var authScopeSpec *gocb.ScopeSpec
	for _, sc := range scopes {
		if sc.Name == "auth" {
			authScopeSpec = &sc
			break
		}
	}

	usersCollectionExists := false
	if authScopeSpec != nil {
		for _, col := range authScopeSpec.Collections {
			if col.Name == "users" {
				usersCollectionExists = true
				break
			}
		}
	}

	if !usersCollectionExists {
		log.Println("Creating Couchbase collection 'auth.users'...")
		colSpec := gocb.CollectionSpec{
			Name:      "users",
			ScopeName: "auth",
		}
		err = collectionsMgr.CreateCollection(colSpec, nil)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create collection 'users': %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	scope := bucket.Scope("auth")
	collection := scope.Collection("users")

	Instance = &DB{
		Cluster:    cluster,
		Bucket:     bucket,
		Scope:      scope,
		Collection: collection,
	}

	// Create indexes for SQL++ querying in background to avoid blocking server boot
	go func() {
		time.Sleep(3 * time.Second)
		log.Println("Configuring SQL++ query indexes on 'auth.users'...")

		_, err = cluster.Query(fmt.Sprintf("CREATE PRIMARY INDEX IF NOT EXISTS ON `%s`.`auth`.`users`", bucketName), nil)
		if err != nil {
			log.Printf("[Index Warning] Failed to create primary index: %v", err)
		}

		_, err = cluster.Query(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_users_email ON `%s`.`auth`.`users`(email)", bucketName), nil)
		if err != nil {
			log.Printf("[Index Warning] Failed to create idx_users_email secondary index: %v", err)
		}
		log.Println("Indexes configured successfully.")
	}()

	log.Println("Couchbase Database setup complete.")
	return nil
}
