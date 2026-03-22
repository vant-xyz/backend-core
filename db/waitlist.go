package db

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func SaveWaitlistEntry(ctx context.Context, email, referralCode, referredByCode string) (bool, string, error) {
	var existingCode string
	err := Pool.QueryRow(ctx,
		`SELECT referral_code FROM waitlist WHERE email = $1`, email,
	).Scan(&existingCode)

	if err == nil {
		return true, existingCode, nil
	}
	if err != pgx.ErrNoRows {
		return false, "", fmt.Errorf("failed to check waitlist: %w", err)
	}

	_, err = Pool.Exec(ctx, `
		INSERT INTO waitlist (email, referral_code, referred_by, referral_count, created_at)
		VALUES ($1, $2, $3, 0, $4)
	`, email, referralCode, referredByCode, time.Now())
	if err != nil {
		return false, "", fmt.Errorf("failed to save waitlist entry: %w", err)
	}

	return false, referralCode, nil
}

func TrackReferral(referredByCode, newUserEmail string) {
	if referredByCode == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	searchCode := strings.ToUpper(referredByCode)

	var referrerEmail string
	err := Pool.QueryRow(ctx,
		`SELECT email FROM waitlist WHERE referral_code = $1 LIMIT 1`, searchCode,
	).Scan(&referrerEmail)

	if err == pgx.ErrNoRows {
		return
	}
	if err != nil {
		log.Printf("Error finding referrer: %v", err)
		return
	}

	if referrerEmail == newUserEmail {
		return
	}

	if _, err := Pool.Exec(ctx,
		`UPDATE waitlist SET referral_count = referral_count + 1 WHERE email = $1`,
		referrerEmail,
	); err != nil {
		log.Printf("Error updating referral count: %v", err)
	}
}