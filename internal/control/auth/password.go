package auth

import (
	"crypto/subtle"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// DummyBcryptHash is a precomputed valid bcrypt hash used to prevent email enumeration
// via timing side-channels when checking non-existent users.
const DummyBcryptHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO.8H60aA2H.tX/vj4E3G/W2P6W8Rk8q."

// HashPassword hashes a raw password using bcrypt with standard cost.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters long")
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// If hash is empty, it uses DummyBcryptHash to maintain constant-time behavior.
func CheckPassword(password, hash string) bool {
	if hash == "" {
		hash = DummyBcryptHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ConstantTimeCompareStrings performs a constant-time comparison of two strings.
func ConstantTimeCompareStrings(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
