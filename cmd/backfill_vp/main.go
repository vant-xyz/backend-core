package main

import (
	"context"
	"log"
	"math"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	backfillPositions(ctx, pool)
	backfillVSEvents(ctx, pool)
	backfillDeposits(ctx, pool)
	backfillWithdrawals(ctx, pool)
	backfillAssetWithdrawals(ctx, pool)

	log.Println("[backfill] done")
}

func award(ctx context.Context, pool *pgxpool.Pool, userEmail string, isDemo bool, action string, points float64, refID string) {
	_, err := pool.Exec(ctx, `
		INSERT INTO vantic_points_ledger (user_email, is_demo, action, points, ref_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_email, action, ref_id) WHERE ref_id IS NOT NULL DO NOTHING`,
		userEmail, isDemo, action, points, refID, time.Now().UTC(),
	)
	if err != nil {
		log.Printf("[backfill] award failed user=%s action=%s ref=%s: %v", userEmail, action, refID, err)
	}
}

func backfillPositions(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_email, is_demo, status, realized_pnl
		FROM positions
	`)
	if err != nil {
		log.Fatalf("[backfill] positions query: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, userEmail, status string
		var isDemo bool
		var realizedPnL float64
		if err := rows.Scan(&id, &userEmail, &isDemo, &status, &realizedPnL); err != nil {
			log.Printf("[backfill] positions scan: %v", err)
			continue
		}
		award(ctx, pool, userEmail, isDemo, "trade_executed", 5, id+"_executed")
		if status == "SETTLED" {
			if realizedPnL > 0 {
				award(ctx, pool, userEmail, isDemo, "trade_won", 20, id+"_won")
			} else {
				award(ctx, pool, userEmail, isDemo, "trade_lost", -20, id+"_lost")
			}
		}
		if status == "CLOSED" {
			award(ctx, pool, userEmail, isDemo, "trade_executed", 5, id+"_close_executed")
			if realizedPnL > 0 {
				award(ctx, pool, userEmail, isDemo, "trade_won", 20, id+"_close_won")
			} else if realizedPnL < 0 {
				award(ctx, pool, userEmail, isDemo, "trade_lost", -20, id+"_close_lost")
			}
		}
		n++
	}
	log.Printf("[backfill] positions: processed %d rows", n)
}

func backfillVSEvents(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT e.id, e.creator_email, e.is_demo, e.outcome,
		       p.id AS part_id, p.user_email, p.confirmation
		FROM vs_events e
		JOIN vs_event_participants p ON p.vs_event_id = e.id
	`)
	if err != nil {
		log.Fatalf("[backfill] vs_events query: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	n := 0
	for rows.Next() {
		var evID, creatorEmail, outcome, partID, partEmail, confirmation string
		var isDemo bool
		if err := rows.Scan(&evID, &creatorEmail, &isDemo, &outcome, &partID, &partEmail, &confirmation); err != nil {
			log.Printf("[backfill] vs scan: %v", err)
			continue
		}
		if !seen[evID] {
			award(ctx, pool, creatorEmail, isDemo, "vs_created", 150, evID+"_vs_created")
			seen[evID] = true
		}
		if partEmail != creatorEmail {
			award(ctx, pool, partEmail, isDemo, "vs_joined", 125, evID+"_"+partEmail+"_vs_joined")
		}
		if outcome != "" {
			if confirmation == outcome {
				award(ctx, pool, partEmail, isDemo, "vs_won", 200, partID+"_vs_settle")
			} else {
				award(ctx, pool, partEmail, isDemo, "vs_lost", 100, partID+"_vs_settle")
			}
		}
		n++
	}
	log.Printf("[backfill] vs_events: processed %d participant rows", n)
}

func depositVP(usdAmount float64) float64 {
	return 50.0 * math.Pow(1.1, usdAmount-1.0)
}

func withdrawalVP(usdAmount float64) float64 {
	return 25.0 * math.Pow(1.3, usdAmount-1.0)
}

func assetSaleVP(usdAmount float64) float64 {
	return 60.0 * math.Pow(1.7, usdAmount-1.0)
}

func backfillDeposits(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_email, amount, nature FROM transactions WHERE type IN ('deposit', 'faucet')
	`)
	if err != nil {
		log.Fatalf("[backfill] deposits query: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, userEmail, nature string
		var amount float64
		if err := rows.Scan(&id, &userEmail, &amount, &nature); err != nil {
			log.Printf("[backfill] deposits scan: %v", err)
			continue
		}
		isDemo := nature == "demo"
		award(ctx, pool, userEmail, isDemo, "deposit", depositVP(amount), id)
		n++
	}
	log.Printf("[backfill] deposits: processed %d rows", n)
}

func backfillWithdrawals(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_email, amount, nature FROM transactions WHERE type = 'withdrawal'
	`)
	if err != nil {
		log.Fatalf("[backfill] withdrawals query: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, userEmail, nature string
		var amount float64
		if err := rows.Scan(&id, &userEmail, &amount, &nature); err != nil {
			log.Printf("[backfill] withdrawals scan: %v", err)
			continue
		}
		isDemo := nature == "demo"
		award(ctx, pool, userEmail, isDemo, "withdrawal", withdrawalVP(amount), id)
		n++
	}
	log.Printf("[backfill] withdrawals: processed %d rows", n)
}

func backfillAssetWithdrawals(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_email, amount, nature FROM transactions WHERE type = 'asset_withdrawal'
	`)
	if err != nil {
		log.Fatalf("[backfill] asset_withdrawals query: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id, userEmail, nature string
		var amount float64
		if err := rows.Scan(&id, &userEmail, &amount, &nature); err != nil {
			log.Printf("[backfill] asset_withdrawals scan: %v", err)
			continue
		}
		isDemo := nature == "demo"
		award(ctx, pool, userEmail, isDemo, "asset_sale", assetSaleVP(amount), id)
		n++
	}
	log.Printf("[backfill] asset_withdrawals: processed %d rows", n)
}
