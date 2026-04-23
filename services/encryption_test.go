package services

import (
	"testing"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	plain := "my secret wallet private key"
	cipher, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	got, err := Decrypt(cipher)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if got != plain {
		t.Errorf("roundtrip got %q, want %q", got, plain)
	}
}

func TestEncryptDecrypt_WithCustomKey(t *testing.T) {
	t.Setenv("WALLET_ENCRYPTION_KEY", "custom-test-encryption-key-32!!!")
	plain := "another secret"
	cipher, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	got, err := Decrypt(cipher)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if got != plain {
		t.Errorf("roundtrip with custom key got %q, want %q", got, plain)
	}
}

func TestEncrypt_ProducesUniqueOutputEachCall(t *testing.T) {
	c1, _ := Encrypt("same input")
	c2, _ := Encrypt("same input")
	if c1 == c2 {
		t.Error("two encryptions of the same plaintext should differ (random nonce per call)")
	}
}

func TestDecrypt_InvalidHex_ReturnsError(t *testing.T) {
	_, err := Decrypt("not-valid-hex!!")
	if err == nil {
		t.Error("Decrypt with invalid hex should return error")
	}
}

func TestDecrypt_TooShortCiphertext_ReturnsError(t *testing.T) {
	// Valid hex but shorter than the AES-GCM nonce size (12 bytes).
	_, err := Decrypt("deadbeef")
	if err == nil {
		t.Error("Decrypt of too-short ciphertext should return error")
	}
}

func TestDecrypt_TamperedCiphertext_ReturnsError(t *testing.T) {
	ciphertext, err := Encrypt("original value")
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	last := ciphertext[len(ciphertext)-1]
	var flipped byte
	if last == 'a' {
		flipped = 'b'
	} else {
		flipped = 'a'
	}
	tampered := ciphertext[:len(ciphertext)-1] + string(flipped)
	_, err = Decrypt(tampered)
	if err == nil {
		t.Error("Decrypt of tampered ciphertext should return error")
	}
}

func TestGetEncryptionKey_DefaultLength_Is32(t *testing.T) {
	t.Setenv("WALLET_ENCRYPTION_KEY", "")
	key := GetEncryptionKey()
	if len(key) != 32 {
		t.Errorf("default key length = %d, want 32", len(key))
	}
}

func TestGetEncryptionKey_LongKey_TruncatedTo32(t *testing.T) {
	t.Setenv("WALLET_ENCRYPTION_KEY", "this-key-is-definitely-longer-than-thirty-two-bytes")
	key := GetEncryptionKey()
	if len(key) != 32 {
		t.Errorf("long key should be truncated to 32 bytes, got %d", len(key))
	}
}

func TestGetEncryptionKey_ShortKey_PaddedTo32(t *testing.T) {
	t.Setenv("WALLET_ENCRYPTION_KEY", "short")
	key := GetEncryptionKey()
	if len(key) != 32 {
		t.Errorf("short key should be padded to 32 bytes, got %d", len(key))
	}
}
