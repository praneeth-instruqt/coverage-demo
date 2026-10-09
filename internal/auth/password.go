package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultIterations follows the OWASP recommendation for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600_000

	hashScheme = "pbkdf2-sha256"
	saltLength = 16
	keyLength  = 32
)

var errMalformedHash = errors.New("malformed password hash")

// PasswordHasher hashes and verifies passwords with PBKDF2-HMAC-SHA256.
type PasswordHasher struct {
	Iterations int
}

// NewPasswordHasher returns a hasher using the given iteration count,
// or DefaultIterations when iterations <= 0.
func NewPasswordHasher(iterations int) PasswordHasher {
	if iterations <= 0 {
		iterations = DefaultIterations
	}
	return PasswordHasher{Iterations: iterations}
}

// Hash returns an encoded hash of the form "pbkdf2-sha256$<iter>$<salt>$<key>".
func (h PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, h.Iterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, h.Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether password matches the encoded hash. The iteration
// count stored in the hash is used, so old hashes keep working if the
// configured count changes.
func (h PasswordHasher) Verify(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false, errMalformedHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false, errMalformedHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, fmt.Errorf("derive key: %w", err)
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
