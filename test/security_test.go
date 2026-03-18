package test

import (
	"os"
	"testing"

	"github.com/vant-xyz/backend-code/services"
)

func TestEncryptionDecryption(t *testing.T) {
	os.Setenv("WALLET_ENCRYPTION_KEY", "this-is-a-32-byte-test-secret-key")
	defer os.Unsetenv("WALLET_ENCRYPTION_KEY")

	originalText := "5YNm3m2zG4dNHASc3W5w6d3gUaG6jQ4k7vY2g8eH9m2sZ6cE" // Sample Solana private key

	encryptedText, err := services.Encrypt(originalText)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	decryptedText, err := services.Decrypt(encryptedText)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if originalText != decryptedText {
		t.Errorf("Expected decrypted text to be '%s', but got '%s'", originalText, decryptedText)
	}
}
