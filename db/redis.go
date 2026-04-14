package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vant-xyz/backend-code/models"
)

var RDB *redis.Client

func InitRedis() {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatal("REDIS_URL environment variable is required")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		opts = &redis.Options{Addr: redisURL}
	}

	RDB = redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}
	log.Println("[Redis] Connected")
}

const orderTTL = 30 * 24 * time.Hour

func orderKey(id string) string { return "order:" + id }

func RedisStoreOrder(ctx context.Context, o *models.Order) error {
	data, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("marshal order: %w", err)
	}
	return RDB.Set(ctx, orderKey(o.ID), data, orderTTL).Err()
}

func RedisGetOrder(ctx context.Context, id string) (*models.Order, error) {
	data, err := RDB.Get(ctx, orderKey(id)).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("order not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	var o models.Order
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func RedisUpdateOrderFill(ctx context.Context, id string, filledQty, remainingQty float64, status models.OrderStatus) error {
	data, err := RDB.Get(ctx, orderKey(id)).Bytes()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var o models.Order
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}
	o.FilledQty = filledQty
	o.RemainingQty = remainingQty
	o.Status = status
	o.UpdatedAt = time.Now()

	updated, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return RDB.Set(ctx, orderKey(id), updated, orderTTL).Err()
}

func AsyncSyncOrderToPG(o *models.Order, pgFn func(context.Context, *models.Order) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := pgFn(ctx, o); err != nil {
			log.Printf("[Redis] PG sync failed for order %s: %v", o.ID, err)
		}
	}()
}

func AsyncSyncFillToPG(id string, filledQty, remainingQty float64, status models.OrderStatus) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := UpdateOrderFill(ctx, id, filledQty, remainingQty, status); err != nil {
			log.Printf("[Redis] PG fill sync failed for order %s: %v", id, err)
		}
	}()
}
