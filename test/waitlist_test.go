package test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
)

func GenerateReferralCode() string {
	max := big.NewInt(1000000)
	n, _ := rand.Int(rand.Reader, max)
	return fmt.Sprintf("%06d", n)
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

func TestReferralCodeIsNumeric(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := GenerateReferralCode()
		for _, char := range code {
			isNumeric := (char >= '0' && char <= '9')
			if !isNumeric {
				t.Errorf("Expected numeric character, got %c in code %s", char, code)
			}
		}
	}
}
