package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const refreshKeyPrefix = "fin:refresh:"

type TokenStore struct {
	client *redis.Client
	ttl    time.Duration
}

func ConnectRedis(redisURL string, refreshTTL time.Duration) (*TokenStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &TokenStore{client: client, ttl: refreshTTL}, nil
}

func (t *TokenStore) SaveRefresh(ctx context.Context, tokenID, email string) error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Set(ctx, refreshKeyPrefix+tokenID, email, t.ttl).Err()
}

func (t *TokenStore) ConsumeRefresh(ctx context.Context, tokenID string) (string, error) {
	if t == nil || t.client == nil {
		return "", fmt.Errorf("redis not configured")
	}
	email, err := t.client.GetDel(ctx, refreshKeyPrefix+tokenID).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("refresh token not found")
	}
	return email, err
}

func (t *TokenStore) Close() error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Close()
}

func (t *TokenStore) Ping(ctx context.Context) error {
	if t == nil || t.client == nil {
		return nil
	}
	return t.client.Ping(ctx).Err()
}
