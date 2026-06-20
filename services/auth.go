package services

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/golang-jwt/jwt/v5"
	base58lib "github.com/mr-tron/base58/base58"
	"github.com/redis/go-redis/v9"
	"github.com/vant-xyz/backend-code/db"
	"golang.org/x/crypto/bcrypt"
)

func jwtSecret() []byte {
	return []byte(os.Getenv("JWT_SECRET"))
}

func ValidateJWTSecret() {
	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("JWT_SECRET environment variable is required")
	}
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateJWT(email string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": email,
		"exp":   time.Now().Add(time.Hour * 72).Unix(),
	})
	return token.SignedString(jwtSecret())
}

func VerifyJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		email, ok := claims["email"].(string)
		if !ok {
			return "", errors.New("invalid token claims")
		}
		return email, nil
	}

	return "", errors.New("invalid token")
}

// ParseJWTClaims parses any Vantic JWT (v1 or v2) and returns its claims.
func ParseJWTClaims(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}

// GenerateV2JWT creates a JWT for a v2 wallet-auth user.
func GenerateV2JWT(walletPubkey string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"wallet_pubkey": walletPubkey,
		"exp":           time.Now().Add(time.Hour * 72).Unix(),
	})
	return token.SignedString(jwtSecret())
}

// GenerateNonce creates a cryptographically random 16-byte hex nonce.
func GenerateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nonceKey(address string) string { return "v2:nonce:" + address }

// StoreNonce stores a nonce in Redis with a 10-minute TTL.
func StoreNonce(ctx context.Context, address, nonce string) error {
	return db.RDB.Set(ctx, nonceKey(address), nonce, 10*time.Minute).Err()
}

// GetAndDeleteNonce atomically retrieves and deletes the nonce for an address.
func GetAndDeleteNonce(ctx context.Context, address string) (string, error) {
	nonce, err := db.RDB.GetDel(ctx, nonceKey(address)).Result()
	if err == redis.Nil {
		return "", errors.New("nonce not found or expired")
	}
	return nonce, err
}

// VerifyWalletSignature verifies an ed25519 signature from a Solana wallet.
// address is a base58 Solana pubkey; signature is the base58-encoded 64-byte sig.
func VerifyWalletSignature(address, message, signatureBase58 string) (bool, error) {
	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return false, fmt.Errorf("invalid address: %w", err)
	}

	sigBytes, err := base58lib.Decode(signatureBase58)
	if err != nil {
		return false, fmt.Errorf("invalid signature encoding: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature length: got %d, want %d", len(sigBytes), ed25519.SignatureSize)
	}

	return ed25519.Verify(ed25519.PublicKey(pubKey[:]), []byte(message), sigBytes), nil
}
