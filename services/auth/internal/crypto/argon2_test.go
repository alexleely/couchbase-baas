package crypto

import (
	"testing"
)

func TestHashAndCompare(t *testing.T) {
	password := "my-secure-password-123!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	if hash == "" {
		t.Fatal("expected non-empty hash string")
	}

	// Match password
	match, err := ComparePassword(password, hash)
	if err != nil {
		t.Fatalf("failed to compare password: %v", err)
	}
	if !match {
		t.Error("expected password to match its generated hash")
	}

	// Wrong password
	match, err = ComparePassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("failed to compare wrong password: %v", err)
	}
	if match {
		t.Error("expected wrong password not to match hash")
	}
}

func TestInvalidHashFormat(t *testing.T) {
	_, err := ComparePassword("pass", "invalid-hash-string")
	if err != ErrInvalidHash {
		t.Errorf("expected ErrInvalidHash, got: %v", err)
	}
}
