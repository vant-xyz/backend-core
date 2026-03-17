package test

import (
	"os"
	"testing"

	"github.com/vant-xyz/backend-code/services"
)

func TestPasswordHashing(t *testing.T) {
	password := "VantSecure123!"
	
	hash, err := services.HashPassword(password)
	if err != nil {
		t.Errorf("Failed to hash password: %v", err)
	}

	if hash == password {
		t.Errorf("Hash should not be equal to plain password")
	}

	if !services.CheckPasswordHash(password, hash) {
		t.Errorf("Password check failed for correct password")
	}

	if services.CheckPasswordHash("wrongpassword", hash) {
		t.Errorf("Password check should fail for wrong password")
	}
}

func TestJWTFlow(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret")
	defer os.Unsetenv("JWT_SECRET")

	email := "test@vant.xyz"
	
	token, err := services.GenerateJWT(email)
	if err != nil {
		t.Errorf("Failed to generate JWT: %v", err)
	}

	verifiedEmail, err := services.VerifyJWT(token)
	if err != nil {
		t.Errorf("Failed to verify JWT: %v", err)
	}

	if verifiedEmail != email {
		t.Errorf("Expected email %s, got %s", email, verifiedEmail)
	}
}

func TestVerifyInvalidJWT(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret")
	
	_, err := services.VerifyJWT("invalid.token.here")
	if err == nil {
		t.Errorf("Expected error for invalid JWT, got nil")
	}
}
