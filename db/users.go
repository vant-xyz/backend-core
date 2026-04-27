package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vant-xyz/backend-code/models"
	"github.com/vant-xyz/backend-code/utils"
)

type UserAdminStats struct {
	Email           string     `json:"email"`
	Username        string     `json:"username"`
	VantID          string     `json:"vant_id"`
	ProfileImageURL string     `json:"profile_image_url"`
	CreatedAt       time.Time  `json:"created_at"`
	Balance         float64    `json:"balance"`
	DemoBalance     float64    `json:"demo_balance"`
	TotalOrders     int64      `json:"total_orders"`
	FilledOrders    int64      `json:"filled_orders"`
	WinCount        int64      `json:"win_count"`
	LossCount       int64      `json:"loss_count"`
	OpenPositions   int64      `json:"open_positions"`
	RealizedPnL     float64    `json:"realized_pnl"`
	LastTraded      *time.Time `json:"last_traded"`
}

const userStatsJoin = `
	FROM users u
	LEFT JOIN balances b ON b.email = u.email
	LEFT JOIN (
		SELECT user_email,
			COUNT(*) AS total_orders,
			COUNT(*) FILTER (WHERE status IN ('FILLED', 'PARTIALLY_FILLED')) AS filled_orders,
			MAX(updated_at) AS last_traded
		FROM orders
		GROUP BY user_email
	) os ON os.user_email = u.email
	LEFT JOIN (
		SELECT user_email,
			COUNT(*) FILTER (WHERE status = 'SETTLED' AND payout_amount > 0) AS wins,
			COUNT(*) FILTER (WHERE status = 'SETTLED' AND (payout_amount IS NULL OR payout_amount = 0)) AS losses,
			COUNT(*) FILTER (WHERE status = 'ACTIVE') AS open_positions,
			COALESCE(SUM(realized_pnl) FILTER (WHERE status = 'SETTLED'), 0) AS realized_pnl
		FROM positions
		GROUP BY user_email
	) ps ON ps.user_email = u.email
`

const userStatsSelect = `
	SELECT
		u.email, u.username, u.vant_id, COALESCE(u.profile_image_url, ''), u.created_at,
		COALESCE(b.naira, 0),
		COALESCE(b.demo_naira, 0),
		COALESCE(os.total_orders, 0),
		COALESCE(os.filled_orders, 0),
		COALESCE(ps.wins, 0),
		COALESCE(ps.losses, 0),
		COALESCE(ps.open_positions, 0),
		COALESCE(ps.realized_pnl, 0),
		os.last_traded
`

func scanUserStats(rows pgx.Rows) (*UserAdminStats, error) {
	var u UserAdminStats
	err := rows.Scan(
		&u.Email, &u.Username, &u.VantID, &u.ProfileImageURL, &u.CreatedAt,
		&u.Balance, &u.DemoBalance,
		&u.TotalOrders, &u.FilledOrders,
		&u.WinCount, &u.LossCount,
		&u.OpenPositions, &u.RealizedPnL,
		&u.LastTraded,
	)
	return &u, err
}

func GetAdminUsers(ctx context.Context, search, sortBy string, limit, offset int) ([]UserAdminStats, int64, error) {
	orderClause := "u.created_at DESC"
	switch sortBy {
	case "balance":
		orderClause = "COALESCE(b.naira, 0) DESC"
	case "win_rate":
		orderClause = "CASE WHEN COALESCE(ps.wins,0)+COALESCE(ps.losses,0)=0 THEN -1 ELSE COALESCE(ps.wins,0)::float/(COALESCE(ps.wins,0)+COALESCE(ps.losses,0)) END DESC"
	case "last_traded":
		orderClause = "os.last_traded DESC NULLS LAST"
	}

	args := []interface{}{}
	whereClause := ""
	if search != "" {
		whereClause = "WHERE u.email ILIKE $1 OR u.username ILIKE $1 OR u.vant_id ILIKE $1"
		args = append(args, "%"+search+"%")
	}

	var total int64
	countQ := "SELECT COUNT(*) " + userStatsJoin + " " + whereClause
	if err := Pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, limit, offset)

	q := userStatsSelect + userStatsJoin + " " + whereClause +
		fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderClause, limitIdx, offsetIdx)

	rows, err := Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var result []UserAdminStats
	for rows.Next() {
		u, err := scanUserStats(rows)
		if err != nil {
			continue
		}
		result = append(result, *u)
	}
	return result, total, rows.Err()
}

func GetAdminUserByEmail(ctx context.Context, email string) (*UserAdminStats, error) {
	q := userStatsSelect + userStatsJoin + " WHERE u.email = $1"
	rows, err := Pool.Query(ctx, q, email)
	if err != nil {
		return nil, fmt.Errorf("failed to query user %s: %w", email, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("user not found: %s", email)
	}
	u, err := scanUserStats(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan user %s: %w", email, err)
	}
	return u, nil
}

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