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
	})

	t.Run("SecureCompare", func(t *testing.T) {
		if !SecureCompare("hello", "hello") {
			t.Error("expected true for equal strings")
		}
		if SecureCompare("hello", "world") {
			t.Error("expected false for different strings")
		}
		if SecureCompare("hello", "hell") {
			t.Error("expected false for different length strings")
		}
		if !SecureCompare("", "") {
			t.Error("expected true for empty strings")
		}
	})

	t.Run("SignVerify", func(t *testing.T) {
		data := "sensitive info"
		signKey := "secret"
		sig := Sign(data, signKey)

		if sig == "" {
			t.Fatal("empty signature")
		}

		if !Verify(data, sig, signKey) {
			t.Error("failed to verify valid signature")
		}

		if Verify(data, "invalid signature", signKey) {
			t.Error("expected failure for invalid signature")
		}

		if Verify("tampered data", sig, signKey) {
			t.Error("expected failure for tampered data")
		}

		if Verify(data, sig, "wrong key") {
			t.Error("expected failure for wrong key")
		}
	})
}
