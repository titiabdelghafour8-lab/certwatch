package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

// keyPrefix makes certwatch keys recognizable to humans and secret scanners.
const keyPrefix = "cw_"

const keyAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newAPIKey returns a fresh secret, its display prefix and its storage hash.
// Only the SHA-256 hash is stored; keys have ~190 bits of entropy so a fast
// hash is appropriate (unlike passwords).
func newAPIKey() (secret, prefix, hash string, err error) {
	b := make([]byte, 0, 33)
	max := big.NewInt(int64(len(keyAlphabet)))
	for i := 0; i < 33; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", "", "", err
		}
		b = append(b, keyAlphabet[n.Int64()])
	}
	secret = keyPrefix + string(b)
	return secret, secret[:len(keyPrefix)+6], hashKey(secret), nil
}

func hashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
