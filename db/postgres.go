package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func envInt32(key string, fallback int32) int32 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || v <= 0 {
		log.Printf("[DB] Invalid %s=%q, using fallback=%d", key, raw, fallback)
		return fallback
	}
	return int32(v)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("[DB] Invalid %s=%q, using fallback=%s", key, raw, fallback)
		return fallback
	}
	return d
}

func Init(databaseURL string) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatalf("[DB] Failed to parse DATABASE_URL: %v", err)
	}

	cfg.MaxConns = envInt32("PG_MAX_CONNS", 10)
	cfg.MinConns = envInt32("PG_MIN_CONNS", 1)
	if cfg.MinConns > cfg.MaxConns {
		log.Printf("[DB] PG_MIN_CONNS (%d) > PG_MAX_CONNS (%d); clamping min=max", cfg.MinConns, cfg.MaxConns)
		cfg.MinConns = cfg.MaxConns
	}

	cfg.MaxConnLifetime = envDuration("PG_CONN_MAX_LIFETIME", 30*time.Minute)
	cfg.MaxConnIdleTime = envDuration("PG_CONN_MAX_IDLE_TIME", 5*time.Minute)
	cfg.HealthCheckPeriod = envDuration("PG_HEALTH_CHECK_PERIOD", 1*time.Minute)
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("[DB] Failed to create connection pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("[DB] Failed to ping database: %v", err)
	}

	Pool = pool
	log.Printf(
		"[DB] Postgres pool established (max=%d min=%d max_life=%s max_idle=%s health_check=%s)",
		cfg.MaxConns, cfg.MinConns, cfg.MaxConnLifetime, cfg.MaxConnIdleTime, cfg.HealthCheckPeriod,
	)
}

func HealthCheck(ctx context.Context) error {
	if err := Pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}
	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
