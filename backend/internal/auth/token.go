package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newToken returns a random secret (given to the user) and its SHA-256
// hash (stored in the DB). Applies to sessions and one-time auth tokens.
func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	return tok, hashToken(tok), nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
