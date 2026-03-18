package test

import (
	"crypto/rand"
	"fmt"
	"testing"
)

func GenerateReferralCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%X", b)
}

func TestGenerateReferralCodeUnique(t *testing.T) {
	codes := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code := GenerateReferralCode()
		if codes[code] {
			t.Errorf("Collision found! Code %s was generated twice", code)
		}
		codes[code] = true
	}
}

func TestGenerateReferralCodeLength(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := GenerateReferralCode()
		if len(code) != 6 {
			t.Errorf("Expected length 6, got %d for code %s", len(code), code)
		}
	}
}

func TestReferralCodeIsHex(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := GenerateReferralCode()
		for _, char := range code {
			isHex := (char >= '0' && char <= '9') || (char >= 'A' && char <= 'F')
			if !isHex {
				t.Errorf("Expected hex character, got %c in code %s", char, code)
			}
		}
	}
}
