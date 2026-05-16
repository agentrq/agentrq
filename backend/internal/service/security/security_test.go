package security

import (
	"strings"
	"testing"
)

func TestSecurity(t *testing.T) {
	key := "12345678901234567890123456789012" // 32 bytes
	plaintext := "hello situational security"

	t.Run("EncryptDecrypt", func(t *testing.T) {
		ciphertext, nonce, err := Encrypt(plaintext, key)
		if err != nil {
			t.Fatalf("failed to encrypt: %v", err)
		}
		if ciphertext == "" || nonce == "" {
			t.Fatal("empty ciphertext or nonce")
		}

		decrypted, err := Decrypt(ciphertext, key, nonce)
		if err != nil {
			t.Fatalf("failed to decrypt: %v", err)
		}
		if decrypted != plaintext {
			t.Errorf("expected %s, got %s", plaintext, decrypted)
		}
	})

	t.Run("InvalidKeySize", func(t *testing.T) {
		_, _, err := Encrypt(plaintext, "short")
		if err == nil {
			t.Error("expected error for short key in Encrypt")
		}
		
		_, err = Decrypt("ct", "short", "nonce")
		if err == nil {
			t.Error("expected error for short key in Decrypt")
		}
	})

	t.Run("InvalidHex", func(t *testing.T) {
		_, err := Decrypt("invalid hex", key, "abc")
		if err == nil {
			t.Error("expected error for invalid hex")
		}
		
		_, err = Decrypt("abc", key, "invalid hex")
		if err == nil {
			t.Error("expected error for invalid hex nonce")
		}
	})

	t.Run("DecryptFail", func(t *testing.T) {
		ciphertext, nonce, _ := Encrypt(plaintext, key)
		_, err := Decrypt(ciphertext, "different key 123456789012345678", nonce)
		if err == nil {
			t.Error("expected error for different key")
		}
	})

	t.Run("GenerateSecret", func(t *testing.T) {
		s, err := GenerateSecret(16)
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != 16 {
			t.Errorf("expected length 16, got %d", len(s))
		}
		// Check charset (very crude check)
		for _, c := range s {
			if !strings.ContainsAny(string(c), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
				t.Errorf("secret contains invalid character: %c", c)
			}
		}

		// Ensure different calls produce different results
		s2, _ := GenerateSecret(16)
		if s == s2 {
			t.Error("GenerateSecret produced same result twice")
		}
	})

	t.Run("SecureCompare", func(t *testing.T) {
		if !SecureCompare("hello", "hello") {
			t.Error("SecureCompare failed for identical strings")
		}
		if SecureCompare("hello", "world") {
			t.Error("SecureCompare succeeded for different strings")
		}
		if SecureCompare("hello", "hell") {
			t.Error("SecureCompare succeeded for strings of different length")
		}
	})
}
