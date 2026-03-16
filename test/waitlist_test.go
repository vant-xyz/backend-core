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

func TestGenerateReferralCode(t *testing.T) {
	code1 := GenerateReferralCode()
	code2 := GenerateReferralCode()

	if len(code1) != 6 {
		t.Errorf("expected length 6, got %d", len(code1))
	}

	if code1 == code2 {
		t.Errorf("expected unique codes, got same: %s", code1)
	}
}
