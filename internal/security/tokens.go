package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const TokenEntropyBytes = 32

// GenerateToken returns a URL-safe token with 256 bits of randomness.
func GenerateToken() (string, error) {
	raw := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Digest creates the server-pepper protected representation stored in SQLite.
func Digest(pepper []byte, token string) []byte {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(token))
	return mac.Sum(nil)
}

// Verify compares a presented token without leaking digest equality through timing.
func Verify(pepper []byte, token string, expected []byte) bool {
	actual := Digest(pepper, token)
	var candidate [sha256.Size]byte
	copy(candidate[:], expected)
	lengthMatches := subtle.ConstantTimeEq(int32(len(expected)), int32(len(actual)))
	return subtle.ConstantTimeCompare(actual, candidate[:]) == 1 && lengthMatches == 1
}
