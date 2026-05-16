package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
)

// Encrypt encrypts a plain text string using AES-256 GCM with the provided key.
// It returns the hex-encoded ciphertext and the hex-encoded nonce.
func Encrypt(plaintext, key string) (string, string, error) {
	if len(key) != 32 {
		return "", "", fmt.Errorf("situational security: encryption key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), hex.EncodeToString(nonce), nil
}

// Decrypt decrypts a hex-encoded ciphertext using AES-256 GCM with the provided key and nonce.
func Decrypt(ciphertextHex, key, nonceHex string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("situational security: decryption key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}

	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return "", err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// GenerateSecret generates a random base62 string of a certain length.
func GenerateSecret(n int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := 0; i < n; i++ {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
}

// SecureCompare performs a constant-time string comparison to prevent timing attacks.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
