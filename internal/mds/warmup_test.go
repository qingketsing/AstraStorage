package mds

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	rediscache "AstraStorage/internal/platform/redis/cache"
	redisclient "AstraStorage/internal/platform/redis/client"
)

func TestWarmupRunner_WarmStartupPrimesHotReadModels(t *testing.T) {
	repo := store.NewMemoryRepository()
	counting := &countingRepository{Repository: repo}
	backend := newFakeReadCacheBackend()
	svc, err := NewService(counting)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.SetReadCache(newServiceReadCache(backend, backend, backend, rediscache.Policy{
		FileMetaTTL:       time.Minute,
		FileMetaTTLJitter: 5 * time.Second,
		DownloadPlanTTL:   time.Minute,
		DirectoryListTTL:  time.Minute,
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
		HotspotWindow:     time.Minute,
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)
	mustSeedDownloadPlanFixture(t, ctx, repo, now)
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

	backend.ZIncrBy(ctx, rediscache.HotFileSetKey(), 3, "bundle-file")
	backend.ZIncrBy(ctx, rediscache.HotDirectorySetKey(), 2, metadata.RootInodeID)
	backend.ZIncrBy(ctx, rediscache.HotNodeSetKey(), 1, "node-a")

	runner := newWarmupRunner(svc, backend, backend, redisclient.WarmupConfig{
		Interval:    time.Hour,
		BatchSize:   4,
		LockTTL:     time.Second,
		StartupTopN: 4,
	})
	if runner == nil {
		t.Fatal("expected warmup runner to be created")
	}

	if err := runner.WarmStartup(ctx); err != nil {
		t.Fatalf("warm startup: %v", err)
	}

	fileCallsAfterWarmup := counting.getFileCalls
	nodeCallsAfterWarmup := counting.getNodeCalls
	listChildrenAfterWarmup := counting.listChildrenCalls
	listNodesAfterWarmup := counting.listNodeCalls
	chunkCallsAfterWarmup := counting.listChunkCalls

	if fileCallsAfterWarmup == 0 || nodeCallsAfterWarmup == 0 || listChildrenAfterWarmup == 0 || listNodesAfterWarmup == 0 || chunkCallsAfterWarmup == 0 {
		t.Fatalf("expected warmup to populate file/node/directory/download-plan caches, got file=%d node=%d dir=%d healthyNodes=%d chunks=%d",
			fileCallsAfterWarmup, nodeCallsAfterWarmup, listChildrenAfterWarmup, listNodesAfterWarmup, chunkCallsAfterWarmup)
	}

	if _, err := svc.GetFile(ctx, store.FileSelector{ID: "bundle-file"}); err != nil {
		t.Fatalf("get file after warmup: %v", err)
	}
	if _, err := svc.BuildDownloadPlan(ctx, "bundle-file"); err != nil {
		t.Fatalf("build download plan after warmup: %v", err)
	}
	if _, err := svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{}); err != nil {
		t.Fatalf("list children after warmup: %v", err)
	}
	if _, err := svc.GetNode(ctx, "node-a"); err != nil {
		t.Fatalf("get node after warmup: %v", err)
	}
	if _, err := svc.AllocateUploadTargets(ctx, AllocateUploadTargetsRequest{FileID: "bundle-file", ChunkIndex: 0}); err != nil {
		t.Fatalf("allocate upload targets after warmup: %v", err)
	}

	if counting.getFileCalls != fileCallsAfterWarmup {
		t.Fatalf("expected warmed file cache to avoid repo, got file calls %d -> %d", fileCallsAfterWarmup, counting.getFileCalls)
	}
	if counting.getNodeCalls != nodeCallsAfterWarmup {
		t.Fatalf("expected warmed node cache to avoid repo, got node calls %d -> %d", nodeCallsAfterWarmup, counting.getNodeCalls)
	}
	if counting.listChildrenCalls != listChildrenAfterWarmup {
		t.Fatalf("expected warmed directory cache to avoid repo, got list children calls %d -> %d", listChildrenAfterWarmup, counting.listChildrenCalls)
	}
	if counting.listNodeCalls != listNodesAfterWarmup {
		t.Fatalf("expected warmed healthy node cache to avoid repo, got list nodes calls %d -> %d", listNodesAfterWarmup, counting.listNodeCalls)
	}
	if counting.listChunkCalls != chunkCallsAfterWarmup {
		t.Fatalf("expected warmed download plan cache to avoid repo, got chunk calls %d -> %d", chunkCallsAfterWarmup, counting.listChunkCalls)
	}
}
