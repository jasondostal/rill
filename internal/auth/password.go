package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — tuned for ~100ms per hash on modest hardware.
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// dummyHash is computed once at startup and used by login handlers to
// equalize timing between "user not found" and "wrong password" paths.
// The dummy password is unguessable — VerifyPassword against it will
// always return false, but the time cost mirrors a real check.
var dummyHash string

func init() {
	h, err := HashPassword("rill-dummy-hash-not-a-real-password-" + randHex(16))
	if err != nil {
		panic(fmt.Sprintf("auth: init dummy hash: %v", err))
	}
	dummyHash = h
}

// randHex returns n random bytes as a hex string.
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// DummyHash returns the package-level dummy hash for timing equalization.
func DummyHash() string { return dummyHash }

// HashPassword returns an argon2id-encoded string of the form:
// $argon2id$v=19$m=65536,t=2,p=4$<salt-b64>$<hash-b64>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword returns true if the password matches the encoded hash.
// Uses constant-time comparison.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid hash format")
	}
	var memory, tm uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &tm, &threads); err != nil {
		return false, fmt.Errorf("parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}
	// #nosec G115 -- argon2 hash length is fixed (32 bytes); uint32 cast is safe.
	actual := argon2.IDKey([]byte(password), salt, tm, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
