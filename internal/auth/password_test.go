package auth

import (
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	pw := "correct-horse-battery-staple"

	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if len(hash) == 0 {
		t.Fatal("hash is empty")
	}
	if !stringsHasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("hash format wrong, got: %s", hash)
	}

	// Verify correct password.
	ok, err := VerifyPassword(pw, hash)
	if err != nil {
		t.Fatalf("VerifyPassword correct: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword returned false for correct password")
	}

	// Verify wrong password.
	ok, err = VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword wrong: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword returned true for wrong password")
	}

	// Verify malformed hash.
	_, err = VerifyPassword(pw, "not-a-hash")
	if err == nil {
		t.Fatal("expected error for malformed hash")
	}
}

func TestHashPasswordUniqueSalts(t *testing.T) {
	pw := "test-password"
	h1, _ := HashPassword(pw)
	h2, _ := HashPassword(pw)
	if h1 == h2 {
		t.Fatal("two hashes of same password should be different (unique salts)")
	}
	// Both should verify.
	if ok, _ := VerifyPassword(pw, h1); !ok {
		t.Fatal("h1 didn't verify")
	}
	if ok, _ := VerifyPassword(pw, h2); !ok {
		t.Fatal("h2 didn't verify")
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
