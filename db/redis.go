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

type OrderFillUpdate struct {
	ID           string
	FilledQty    float64
	RemainingQty float64
	Status       models.OrderStatus
}

func RedisBatchUpdateFills(ctx context.Context, updates []OrderFillUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	keys := make([]string, len(updates))
	for i, u := range updates {
		keys[i] = orderKey(u.ID)
	}
	results, err := RDB.MGet(ctx, keys...).Result()
	if err != nil {
		return fmt.Errorf("mget fills: %w", err)
	}
	pipe := RDB.Pipeline()
	now := time.Now()
	for i, u := range updates {
		if results[i] == nil {
			continue
		}
		raw, ok := results[i].(string)
		if !ok {
			continue
		}
		var o models.Order
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			continue
		}
		o.FilledQty = u.FilledQty
		o.RemainingQty = u.RemainingQty
		o.Status = u.Status
		o.UpdatedAt = now
		data, err := json.Marshal(o)
		if err != nil {
			continue
		}
		pipe.Set(ctx, keys[i], data, orderTTL)
	}
	_, err = pipe.Exec(ctx)
	return err
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

func AsyncSyncFillToPG(o *models.Order) {
	snap := *o
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := UpsertOrderFill(ctx, &snap); err != nil {
			log.Printf("[Redis] PG fill upsert failed for order %s: %v", snap.ID, err)
		}
	}()
}
