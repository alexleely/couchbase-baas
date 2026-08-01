package crypto

import (
	"testing"
	"time"
)

func TestAccessTokenGenerationAndValidation(t *testing.T) {
	userID := "usr_12345"
	role := "authenticated"

	token, err := GenerateAccessToken(userID, role)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	claims, err := ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("failed to validate generated access token: %v", err)
	}

	if claims.Subject != userID {
		t.Errorf("expected subject %s, got: %s", userID, claims.Subject)
	}

	if claims.Role != role {
		t.Errorf("expected role %s, got: %s", role, claims.Role)
	}

	// Verify expiration is in future
	if claims.ExpiresAt.Before(time.Now()) {
		t.Error("access token expiration time is in the past")
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	rt1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}

	rt2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("failed to generate second refresh token: %v", err)
	}

	if len(rt1) != 64 { // hex string of 32 bytes is 64 characters
		t.Errorf("expected length 64, got %d", len(rt1))
	}

	if rt1 == rt2 {
		t.Error("expected randomly generated refresh tokens to be distinct")
	}
}

func TestInvalidAccessToken(t *testing.T) {
	_, err := ValidateAccessToken("invalid.jwt.token")
	if err == nil {
		t.Error("expected validation to fail for invalid token string")
	}
}
