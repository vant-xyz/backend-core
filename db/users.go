package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/utils"
)

func GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	row := Pool.QueryRow(ctx, `
		SELECT email, name, full_name, username, password,
		       vant_id, balance_id, socials, profile_image_url, created_at
		FROM users WHERE email = $1
	`, email)

	var u models.User
	if err := row.Scan(
		&u.Email, &u.Name, &u.FullName, &u.Username, &u.Password,
		&u.VantID, &u.BalanceID, &u.Socials, &u.ProfileImageURL, &u.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", email)
		}
		return nil, fmt.Errorf("failed to get user %s: %w", email, err)
	}
	return &u, nil
}

func CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check username: %w", err)
	}
	return exists, nil
}

// CreateUser inserts user, balance, and wallet rows atomically.
// Wallet generation (services.GenerateWallet) must happen in the caller
// (handler layer) to avoid an import cycle between db and services.
func CreateUser(ctx context.Context, email, hashedPassword string, wallet *models.Wallet) (*models.User, error) {
	balanceID := fmt.Sprintf("BAL_%s", utils.RandomAlphanumeric(10))
	vantID := fmt.Sprintf("VANTID_%s", utils.RandomNumbers(8))
	username := fmt.Sprintf("@user%s", utils.RandomAlphanumeric(6))
	now := time.Now()

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	user := models.User{
		Email:           email,
		Username:        username,
		Password:        hashedPassword,
		VantID:          vantID,
		BalanceID:       balanceID,
		Socials:         []string{},
		ProfileImageURL: "",
		CreatedAt:       now,
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO users (email, name, full_name, username, password, vant_id, balance_id, socials, profile_image_url, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, user.Email, user.Name, user.FullName, user.Username, user.Password,
		user.VantID, user.BalanceID, user.Socials, user.ProfileImageURL, user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO balances (id, email) VALUES ($1, $2)
	`, balanceID, email)
	if err != nil {
		return nil, fmt.Errorf("failed to insert balance: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO wallets (account_id, email, sol_public_key, sol_private_key, base_public_key, base_private_key, naira_account_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, wallet.AccountID, email, wallet.SolPublicKey, wallet.SolPrivateKey,
		wallet.BasePublicKey, wallet.BasePrivateKey, wallet.NairaAccountNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to insert wallet: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit user creation: %w", err)
	}

	return &user, nil
}

func UpdateUser(ctx context.Context, email string, updates map[string]interface{}) error {
	if username, ok := updates["username"].(string); ok {
		exists, err := CheckUsernameExists(ctx, username)
		if err != nil {
			return err
		}
		if exists {
			current, err := GetUserByEmail(ctx, email)
			if err != nil {
				return err
			}
			if current.Username != username {
				return fmt.Errorf("username already taken")
			}
		}
	}

	allowedFields := map[string]string{
		"username":          "username",
		"name":              "name",
		"full_name":         "full_name",
		"profile_image_url": "profile_image_url",
	}

	i := 1
	setClauses := ""
	args := []interface{}{}

	for k, v := range updates {
		col, ok := allowedFields[k]
		if !ok {
			continue
		}
		if i > 1 {
			setClauses += ", "
		}
		setClauses += fmt.Sprintf("%s = $%d", col, i)
		args = append(args, v)
		i++
	}

	if setClauses == "" {
		return nil
	}

	args = append(args, email)
	_, err := Pool.Exec(ctx,
		fmt.Sprintf("UPDATE users SET %s WHERE email = $%d", setClauses, i),
		args...,
	)
	return err
}

func UpdateUsername(ctx context.Context, email, username string) error {
	exists, err := CheckUsernameExists(ctx, username)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("username already taken")
	}
	_, err = Pool.Exec(ctx,
		`UPDATE users SET username = $1 WHERE email = $2`, username, email,
	)
	return err
}