package mds

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/redis/cache"
	redisclient "AstraStorage/internal/platform/redis/client"
	redislock "AstraStorage/internal/platform/redis/lock"
	redis "github.com/redis/go-redis/v9"
)

const warmupLockKey = "astra:lock:warmup:tick"

type warmupHotsetBackend interface {
	ZRevRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd
}

type warmupLockBackend interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
}

type WarmupRunner struct {
	service *Service
	hotsets warmupHotsetBackend
	locker  *redislock.Locker
	cfg     redisclient.WarmupConfig
}

func NewRedisWarmupRunner(service *Service, bundle *redisclient.Bundle, cfg redisclient.WarmupConfig) *WarmupRunner {
	if service == nil || bundle == nil || bundle.Cache() == nil || bundle.Coord() == nil {
		return nil
	}
	return newWarmupRunner(service, bundle.Cache().WriteClient(), bundle.Coord().WriteClient(), cfg)
}

func newWarmupRunner(service *Service, hotsets warmupHotsetBackend, lockBackend warmupLockBackend, cfg redisclient.WarmupConfig) *WarmupRunner {
	if service == nil || hotsets == nil || lockBackend == nil {
		return nil
	}
	cfg = cfg.WithDefaults()
	return &WarmupRunner{
		service: service,
		hotsets: hotsets,
		locker:  redislock.NewLocker(lockBackend, cfg.LockTTL),
		cfg:     cfg,
	}
}

func (r *WarmupRunner) Run(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.WarmStartup(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.WarmBatch(ctx, r.cfg.BatchSize); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}
	}
}

func (r *WarmupRunner) WarmStartup(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return r.warm(ctx, r.cfg.StartupTopN)
}

func (r *WarmupRunner) WarmBatch(ctx context.Context, limit int) error {
	if r == nil {
		return nil
	}
	if limit <= 0 {
		limit = r.cfg.BatchSize
	}
	return r.warm(ctx, limit)
}

func (r *WarmupRunner) warm(ctx context.Context, limit int) error {
	token, err := redislock.NewOwnerToken()
	if err != nil {
		return err
	}
	acquired, err := r.locker.Acquire(ctx, warmupLockKey, token, r.cfg.LockTTL)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer func() {
		_, _ = r.locker.Release(ctx, warmupLockKey, token)
	}()

	if _, err := r.service.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("warmup root directory: %w", err)
	}
	if _, err := r.service.listHealthyNodes(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("warmup healthy nodes: %w", err)
	}

	fileIDs, err := r.loadHotset(ctx, cache.HotFileSetKey(), limit)
	if err != nil {
		return fmt.Errorf("warmup hot files: %w", err)
	}
	for _, fileID := range fileIDs {
		if _, err := r.service.GetFile(ctx, store.FileSelector{ID: metadata.FileID(fileID)}); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("warmup file %q: %w", fileID, err)
		}
		if _, err := r.service.BuildDownloadPlan(ctx, metadata.FileID(fileID)); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("warmup download plan %q: %w", fileID, err)
		}
	}

	dirIDs, err := r.loadHotset(ctx, cache.HotDirectorySetKey(), limit)
	if err != nil {
		return fmt.Errorf("warmup hot directories: %w", err)
	}
	if !slices.Contains(dirIDs, metadata.RootInodeID) {
		dirIDs = append([]string{metadata.RootInodeID}, dirIDs...)
	}
	for _, inodeID := range dirIDs {
		if _, err := r.service.ListChildren(ctx, metadata.InodeID(inodeID), store.ListOptions{}); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("warmup directory %q: %w", inodeID, err)
		}
	}

	nodeIDs, err := r.loadHotset(ctx, cache.HotNodeSetKey(), limit)
	if err != nil {
		return fmt.Errorf("warmup hot nodes: %w", err)
	}
	for _, nodeID := range nodeIDs {
		if _, err := r.service.GetNode(ctx, metadata.NodeID(nodeID)); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("warmup node %q: %w", nodeID, err)
		}
	}
	return nil
}

func (r *WarmupRunner) loadHotset(ctx context.Context, key string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	values, err := r.hotsets.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return values, err
}
