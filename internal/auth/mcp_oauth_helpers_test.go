package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateToken_PrefixAndLength(t *testing.T) {
	out := generateToken("rill_", 16)
	if !strings.HasPrefix(out, "rill_") {
		t.Errorf("prefix lost: %q", out)
	}
	// 16 bytes hex = 32 chars + 5-char prefix.
	if len(out) != len("rill_")+32 {
		t.Errorf("length = %d, want %d", len(out), len("rill_")+32)
	}
	// Body must be lower-case hex.
	body := strings.TrimPrefix(out, "rill_")
	if _, err := hex.DecodeString(body); err != nil {
		t.Errorf("body not hex: %v (body=%q)", err, body)
	}
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 64)
	for i := 0; i < 64; i++ {
		s := generateToken("p_", 16)
		if seen[s] {
			t.Fatalf("duplicate token after %d iterations: %q", i, s)
		}
		seen[s] = true
	}
}

func TestGenerateClientID_Length(t *testing.T) {
	id := generateClientID()
	if len(id) != 32 { // 16 bytes -> 32 hex
		t.Errorf("client id length = %d, want 32", len(id))
	}
}

func TestGenerateClientSecret_Length(t *testing.T) {
	s := generateClientSecret()
	if len(s) != 64 { // 32 bytes -> 64 hex
		t.Errorf("client secret length = %d, want 64", len(s))
	}
}

func TestGenerateAuthCode_Length(t *testing.T) {
	c := generateAuthCode()
	if len(c) != 60 { // 30 bytes -> 60 hex
		t.Errorf("auth code length = %d, want 60", len(c))
	}
}

func TestGenerateMCPToken_PrefixedForValidation(t *testing.T) {
	// Token must start with the prefix that ValidateToken uses to route.
	tok := generateMCPToken()
	if !strings.HasPrefix(tok, "rill_mcp_v1_") {
		t.Errorf("MCP token missing required prefix: %q", tok)
	}
}

func TestPKCEVerifier_NonEmptyAndUnique(t *testing.T) {
	a := pkceVerifier()
	b := pkceVerifier()
	if a == "" || b == "" {
		t.Fatalf("pkceVerifier returned empty")
	}
	if a == b {
		t.Errorf("pkceVerifier produced duplicate values")
	}
	// Must be RFC 7636-compatible: base64url, no padding, 43-128 chars.
	if len(a) < 43 || len(a) > 128 {
		t.Errorf("pkceVerifier length = %d, must be 43-128 per RFC 7636", len(a))
	}
	if _, err := base64.RawURLEncoding.DecodeString(a); err != nil {
		t.Errorf("verifier not raw url-safe base64: %v", err)
	}
}

func TestPKCEChallengeS256_MatchesSHA256(t *testing.T) {
	verifier := "abc123" // deterministic input
	got := pkceChallengeS256(verifier)

	// Verify it's exactly base64url(sha256(verifier)) with no padding.
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != want {
		t.Errorf("pkceChallengeS256 = %q, want %q", got, want)
	}
	if strings.Contains(got, "=") {
		t.Errorf("S256 challenge must not contain '=' padding: %q", got)
	}
}

func TestPKCEChallengeS256_DifferentVerifiersDifferentChallenges(t *testing.T) {
	c1 := pkceChallengeS256("verifier-one")
	c2 := pkceChallengeS256("verifier-two")
	if c1 == c2 {
		t.Errorf("distinct verifiers produced identical challenges: %q", c1)
	}
}
