package crypto

import (
	"strings"
	"testing"
)

func TestPKCEGeneration(t *testing.T) {
	// Test State Generation
	state, err := GenerateRandomState()
	if err != nil {
		t.Fatalf("failed to generate random state: %v", err)
	}
	if len(state) != 32 { // hex encoding of 16 bytes is 32 chars
		t.Errorf("expected state length 32, got %d", len(state))
	}

	// Test Code Verifier Generation
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("failed to generate code verifier: %v", err)
	}
	if len(verifier) != 64 {
		t.Errorf("expected verifier length 64, got %d", len(verifier))
	}

	// Check that only unreserved characters are present
	const unreserved = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	for _, char := range verifier {
		if !strings.ContainsRune(unreserved, char) {
			t.Errorf("verifier contains invalid PKCE character: %c", char)
		}
	}

	// Test Code Challenge Math
	// Reference vector from RFC 7636 Section 4.2
	testVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expectedChallenge := "E9Melhoa2OwvFrGMTJguCH5KVK3qkyqyFQ5GGGmXXh0"
	challenge := GenerateCodeChallenge(testVerifier)
	if challenge != expectedChallenge {
		t.Errorf("RFC 7636 challenge mismatch: expected %s, got %s", expectedChallenge, challenge)
	}
}
