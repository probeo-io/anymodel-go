package anymodel

import (
	"crypto/rand"
	"encoding/base64"
)

// GenerateID creates a random ID with the given prefix.
func GenerateID(prefix string) string {
	b := make([]byte, 12)
	rand.Read(b)
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(b)
}
