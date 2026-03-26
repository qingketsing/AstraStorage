package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	rediscache "AstraStorage/internal/platform/redis/cache"
	redisclient "AstraStorage/internal/platform/redis/client"
	redislock "AstraStorage/internal/platform/redis/lock"
)

func TestRedisSentinelIntegration_MDSReadCacheAndWarmup(t *testing.T) {
	sentinels := strings.TrimSpace(os.Getenv("MDS_TEST_REDIS_SENTINELS"))
	if sentinels == "" {
		t.Skip("set MDS_TEST_REDIS_SENTINELS to run Redis Sentinel integration test")
	}

	cfg := redisclient.Config{
		Enabled:           true,
		SentinelEndpoints: splitNonEmpty(sentinels),
		Cache: redisclient.ReplicationGroupConfig{
			MasterSetName: "astra-cache",
		},
		Coord: redisclient.ReplicationGroupConfig{
			MasterSetName: "astra-coord",
		},
	}
	cfg = cfg.WithDefaults()

	bundle, err := redisclient.NewBundle(cfg)
	if err != nil {
		t.Fatalf("new redis bundle: %v", err)
	}
	defer bundle.Close()

	ctx := context.Background()
	if err := bundle.Cache().WriteClient().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush cache group: %v", err)
	}
	if err := bundle.Coord().WriteClient().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush coord group: %v", err)
	}

	locker := redislock.NewLocker(bundle.Coord().WriteClient(), time.Second)
	owner, err := redislock.NewOwnerToken()
	if err != nil {
		t.Fatalf("new owner token: %v", err)
	}
	acquired, err := locker.Acquire(ctx, "astra:test:lock", owner, time.Second)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected lock acquisition to succeed")
	}
	released, err := locker.Release(ctx, "astra:test:lock", owner)
	if err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if !released {
		t.Fatal("expected lock release to succeed")
	}

	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.SetReadCache(mds.NewRedisReadCache(bundle, cfg.Cache, cfg.Warmup.LockTTL))

	now := time.Now().UTC()
	if err := repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:         metadata.InodeID(metadata.RootInodeID),
		Path:       "/",
		Type:       metadata.InodeTypeDirectory,
		Status:     metadata.InodeStatusActive,
		LinkCount:  1,
		Generation: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil && err != store.ErrAlreadyExists {
		t.Fatalf("create root inode: %v", err)
	}
	if _, err := service.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "redis-int-inode",
		FileID:    "redis-int-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "redis-int.bin",
		Size:      64,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := repo.UpsertNode(ctx, metadata.NodeInfo{
		ID:        "node-a",
		Address:   "http://node-a.local",
		Healthy:   true,
		Capacity:  1000,
		Used:      100,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if err := repo.UpsertChunks(ctx, []metadata.ChunkMetadata{{
		ID:     "redis-int-chunk-0",
		FileID: "redis-int-file",
		Index:  0,
		Offset: 0,
		Size:   64,
		Status: metadata.ChunkStatusAvailable,
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}}); err != nil {
		t.Fatalf("upsert chunks: %v", err)
	}

	if _, err := service.GetFile(ctx, store.FileSelector{ID: "redis-int-file"}); err != nil {
		t.Fatalf("get file: %v", err)
	}
	if _, err := service.BuildDownloadPlan(ctx, "redis-int-file"); err != nil {
		t.Fatalf("build download plan: %v", err)
	}
	if _, err := service.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{}); err != nil {
		t.Fatalf("list children: %v", err)
	}
	if _, err := service.GetNode(ctx, "node-a"); err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, err := service.AllocateUploadTargets(ctx, mds.AllocateUploadTargetsRequest{
		FileID:     "redis-int-file",
		ChunkIndex: 0,
	}); err != nil {
		t.Fatalf("allocate upload targets: %v", err)
	}

	mustEventuallyExist(t, ctx, bundle, rediscache.FileMetaKey("redis-int-file"))
	mustEventuallyExist(t, ctx, bundle, rediscache.DownloadPlanKey("redis-int-file"))
	mustEventuallyExist(t, ctx, bundle, rediscache.DirectoryListKey(metadata.RootInodeID+":v0", 0, 0))
	mustEventuallyExist(t, ctx, bundle, rediscache.NodeHealthKey("node-a"))
	mustEventuallyExist(t, ctx, bundle, rediscache.HealthyNodesKey())

	if err := bundle.Cache().WriteClient().Del(ctx,
		rediscache.FileMetaKey("redis-int-file"),
		rediscache.DownloadPlanKey("redis-int-file"),
		rediscache.DirectoryListKey(metadata.RootInodeID+":v0", 0, 0),
		rediscache.NodeHealthKey("node-a"),
		rediscache.HealthyNodesKey(),
	).Err(); err != nil {
		t.Fatalf("delete warmed keys: %v", err)
	}
	if err := bundle.Cache().WriteClient().ZIncrBy(ctx, rediscache.HotFileSetKey(), 3, "redis-int-file").Err(); err != nil {
		t.Fatalf("mark hot file: %v", err)
	}
	if err := bundle.Cache().WriteClient().ZIncrBy(ctx, rediscache.HotDirectorySetKey(), 2, metadata.RootInodeID).Err(); err != nil {
		t.Fatalf("mark hot directory: %v", err)
	}
	if err := bundle.Cache().WriteClient().ZIncrBy(ctx, rediscache.HotNodeSetKey(), 1, "node-a").Err(); err != nil {
		t.Fatalf("mark hot node: %v", err)
	}

	runner := mds.NewRedisWarmupRunner(service, bundle, cfg.Warmup)
	if runner == nil {
		t.Fatal("expected warmup runner")
	}
	if err := runner.WarmStartup(ctx); err != nil {
		t.Fatalf("warm startup: %v", err)
	}

	mustEventuallyExist(t, ctx, bundle, rediscache.FileMetaKey("redis-int-file"))
	mustEventuallyExist(t, ctx, bundle, rediscache.DownloadPlanKey("redis-int-file"))
	mustEventuallyExist(t, ctx, bundle, rediscache.DirectoryListKey(metadata.RootInodeID+":v0", 0, 0))
	mustEventuallyExist(t, ctx, bundle, rediscache.NodeHealthKey("node-a"))
	mustEventuallyExist(t, ctx, bundle, rediscache.HealthyNodesKey())
}

func mustEventuallyExist(t *testing.T, ctx context.Context, bundle *redisclient.Bundle, key string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if n, err := bundle.Cache().WriteClient().Exists(ctx, key).Result(); err == nil && n > 0 {
			if _, err := bundle.Cache().ReadClient().Get(ctx, key).Result(); err == nil {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("key %q did not become visible through Sentinel-managed cache clients", key)
}

func splitNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
