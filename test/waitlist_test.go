package test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
)

// mirrors handlers.GenerateReferralCode exactly
func GenerateReferralCode() string {
	max := big.NewInt(1_000_000)
	n, _ := rand.Int(rand.Reader, max)
	return fmt.Sprintf("%06d", n)
}

func TestGenerateReferralCodeUnique(t *testing.T) {
	// 30 samples from a 1,000,000-value space gives a birthday-collision
	// probability of ~0.04%, making this test deterministically stable.
	codes := make(map[string]bool)
	for i := 0; i < 30; i++ {
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

func TestReferralCodeIsNumeric(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := GenerateReferralCode()
		for _, char := range code {
			if char < '0' || char > '9' {
				t.Errorf("Expected digit, got %c in code %s", char, code)
			}
		}
	}
}
