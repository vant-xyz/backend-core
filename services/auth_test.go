package services

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestHashPassword_CheckPasswordHash_Roundtrip(t *testing.T) {
	hash, err := HashPassword("securepassword99")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if !CheckPasswordHash("securepassword99", hash) {
		t.Error("CheckPasswordHash should return true for the correct password")
	}
}

func TestCheckPasswordHash_WrongPassword_ReturnsFalse(t *testing.T) {
	hash, _ := HashPassword("correct")
	if CheckPasswordHash("wrong", hash) {
		t.Error("CheckPasswordHash should return false for the wrong password")
	}
}

func TestCheckPasswordHash_EmptyInput_ReturnsFalse(t *testing.T) {
	hash, _ := HashPassword("notempty")
	if CheckPasswordHash("", hash) {
		t.Error("CheckPasswordHash should return false for an empty password")
	}
}

func TestGenerateJWT_VerifyJWT_Roundtrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	email := "alice@vant.xyz"
	token, err := GenerateJWT(email)
	if err != nil {
		t.Fatalf("GenerateJWT error: %v", err)
	}
	got, err := VerifyJWT(token)
	if err != nil {
		t.Fatalf("VerifyJWT error: %v", err)
	}
	if got != email {
		t.Errorf("VerifyJWT returned %q, want %q", got, email)
	}
}

func TestVerifyJWT_TamperedToken_ReturnsError(t *testing.T) {
	_, err := VerifyJWT("this.is.notajwt")
	if err == nil {
		t.Error("VerifyJWT with a garbage token should return error")
	}
}

func TestVerifyJWT_EmptyToken_ReturnsError(t *testing.T) {
	_, err := VerifyJWT("")
	if err == nil {
		t.Error("VerifyJWT with an empty string should return error")
	}
}

func TestVerifyJWT_WrongSecret_ReturnsError(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	token, _ := GenerateJWT("bob@vant.xyz") // signed with the default secret

	t.Setenv("JWT_SECRET", "completely-different-secret-key")
	_, err := VerifyJWT(token)
	if err == nil {
		t.Error("VerifyJWT should fail when the secret doesn't match the signing key")
	}
}

func TestVerifyJWT_ExpiredToken_ReturnsError(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": "expired@vant.xyz",
		"exp":   time.Now().Add(-1 * time.Hour).Unix(),
	})
	signed, _ := tok.SignedString([]byte("vant-default-secret-key"))
	_, err := VerifyJWT(signed)
	if err == nil {
		t.Error("VerifyJWT should reject an expired token")
	}
}

func TestVerifyJWT_MissingEmailClaim_ReturnsError(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": time.Now().Add(72 * time.Hour).Unix(),
	})
	signed, _ := tok.SignedString([]byte("vant-default-secret-key"))
	_, err := VerifyJWT(signed)
	if err == nil {
		t.Error("VerifyJWT should return error when the email claim is missing")
	}
}
