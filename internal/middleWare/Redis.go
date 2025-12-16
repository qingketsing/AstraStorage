package middleware

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisConnection(address string) (*Redis, error) {
	ctx := context.Background()

	client := redis.NewClient(&redis.Options{
		Addr:         address,
		Password:     "", // 无密码
		DB:           0,  // 使用默认数据库
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// 测试连接
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	log.Printf("Redis 连接成功: %s", address)
	return &Redis{client: client, ctx: ctx}, nil
}

func (r *Redis) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// Set 设置键值对
func (r *Redis) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(r.ctx, key, value, expiration).Err()
}

// Get 获取值
func (r *Redis) Get(key string) (string, error) {
	return r.client.Get(r.ctx, key).Result()
}

// Delete 删除键
func (r *Redis) Delete(key string) error {
	return r.client.Del(r.ctx, key).Err()
}
