package mds

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	rediscache "AstraStorage/internal/platform/redis/cache"
	redis "github.com/redis/go-redis/v9"
)

func TestServiceGetFile_UsesReadCacheAfterFirstLoad(t *testing.T) {
	repo := store.NewMemoryRepository()
	counting := &countingRepository{Repository: repo}
	svc, err := NewService(counting)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	backend := newFakeReadCacheBackend()
	svc.SetReadCache(newServiceReadCache(backend, backend, backend, rediscache.Policy{
		FileMetaTTL:       time.Minute,
		FileMetaTTLJitter: 5 * time.Second,
		DownloadPlanTTL:   time.Minute,
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)
	if _, err := svc.CreateFile(ctx, CreateFileRequest{
		InodeID:   "cached-inode",
		FileID:    "cached-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "cached.txt",
		Size:      128,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	file, err := svc.GetFile(ctx, store.FileSelector{ID: "cached-file"})
	if err != nil {
		t.Fatalf("first get file: %v", err)
	}
	if file.ID != "cached-file" {
		t.Fatalf("unexpected file id %q", file.ID)
	}
	if counting.getFileCalls != 1 {
		t.Fatalf("expected one repo get file call, got %d", counting.getFileCalls)
	}

	file, err = svc.GetFile(ctx, store.FileSelector{ID: "cached-file"})
	if err != nil {
		t.Fatalf("second get file: %v", err)
	}
	if file.ID != "cached-file" {
		t.Fatalf("unexpected file id on cached read %q", file.ID)
	}
	if counting.getFileCalls != 1 {
		t.Fatalf("expected cached second read to avoid repo, got %d repo calls", counting.getFileCalls)
	}
}

func TestServiceGetFile_CachesNotFoundEntries(t *testing.T) {
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
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
	}, time.Second))

	ctx := context.Background()
	_, err = svc.GetFile(ctx, store.FileSelector{ID: "missing-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	_, err = svc.GetFile(ctx, store.FileSelector{ID: "missing-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected cached not found, got %v", err)
	}
	if counting.getFileCalls != 1 {
		t.Fatalf("expected missing file to be cached after first miss, got %d repo calls", counting.getFileCalls)
	}
}

func TestServiceBuildDownloadPlan_UsesReadCacheAfterFirstLoad(t *testing.T) {
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
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)
	mustSeedDownloadPlanFixture(t, ctx, repo, now)

	plan, err := svc.BuildDownloadPlan(ctx, "bundle-file")
	if err != nil {
		t.Fatalf("first build download plan: %v", err)
	}
	if plan.FileID != "bundle-file" {
		t.Fatalf("unexpected file id %q", plan.FileID)
	}
	if counting.getFileCalls != 1 || counting.listChunkCalls != 1 {
		t.Fatalf("expected one repo read for file and chunks, got file=%d chunks=%d", counting.getFileCalls, counting.listChunkCalls)
	}

	plan, err = svc.BuildDownloadPlan(ctx, "bundle-file")
	if err != nil {
		t.Fatalf("second build download plan: %v", err)
	}
	if plan.FileID != "bundle-file" {
		t.Fatalf("unexpected cached plan file id %q", plan.FileID)
	}
	if counting.getFileCalls != 1 || counting.listChunkCalls != 1 {
		t.Fatalf("expected cached second plan read, got file=%d chunks=%d", counting.getFileCalls, counting.listChunkCalls)
	}
}

func TestServiceListChildren_UsesReadCacheAfterFirstLoad(t *testing.T) {
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
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)
	if _, err := svc.CreateDirectory(ctx, CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	children, err := svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{})
	if err != nil {
		t.Fatalf("first list children: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one child, got %d", len(children))
	}
	if counting.listChildrenCalls != 1 {
		t.Fatalf("expected one repo list children call, got %d", counting.listChildrenCalls)
	}

	children, err = svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{})
	if err != nil {
		t.Fatalf("second list children: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one cached child, got %d", len(children))
	}
	if counting.listChildrenCalls != 1 {
		t.Fatalf("expected cached second list to avoid repo, got %d repo calls", counting.listChildrenCalls)
	}
}

func TestServiceCreateFile_InvalidatesParentDirectoryCache(t *testing.T) {
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
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)

	children, err := svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{})
	if err != nil {
		t.Fatalf("warm directory cache: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected empty root before create, got %d children", len(children))
	}

	if _, err := svc.CreateFile(ctx, CreateFileRequest{
		InodeID:   "dir-file-inode",
		FileID:    "dir-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "dir-file.txt",
		Size:      64,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	children, err = svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{})
	if err != nil {
		t.Fatalf("list children after create file: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected invalidated directory cache to return one child, got %d", len(children))
	}
}

func TestServiceGetNode_CacheIsInvalidatedByHeartbeat(t *testing.T) {
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
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := svc.RegisterNode(ctx, RegisterNodeRequest{
		ID:        "node-a",
		Address:   "http://node-a.local",
		Capacity:  100,
		Used:      10,
		Healthy:   true,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}

	node, err := svc.GetNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("first get node: %v", err)
	}
	if node.Used != 10 {
		t.Fatalf("unexpected node used %d", node.Used)
	}
	if counting.getNodeCalls != 1 {
		t.Fatalf("expected one repo node read, got %d", counting.getNodeCalls)
	}

	if _, err := svc.HeartbeatNode(ctx, HeartbeatNodeRequest{
		NodeID:     "node-a",
		Healthy:    true,
		Capacity:   100,
		Used:       25,
		LastSeenAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("heartbeat node: %v", err)
	}

	node, err = svc.GetNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("second get node: %v", err)
	}
	if node.Used != 25 {
		t.Fatalf("expected invalidated node cache to return updated used bytes, got %d", node.Used)
	}
	if counting.getNodeCalls != 2 {
		t.Fatalf("expected second repo node read after invalidation, got %d", counting.getNodeCalls)
	}
}

func TestServiceStartUpload_InvalidatesCachedFileMetadata(t *testing.T) {
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
		NodeHealthTTL:     time.Minute,
		NullEntryTTL:      time.Minute,
	}, time.Second))

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRootInRepo(t, ctx, repo, now)
	if _, err := svc.CreateFile(ctx, CreateFileRequest{
		InodeID:   "upload-inode",
		FileID:    "upload-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "upload.bin",
		Size:      256,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	file, err := svc.GetFile(ctx, store.FileSelector{ID: "upload-file"})
	if err != nil {
		t.Fatalf("warm file cache: %v", err)
	}
	if file.Status != metadata.FileStatusPending {
		t.Fatalf("expected pending file before upload, got %q", file.Status)
	}

	if _, err := svc.StartUpload(ctx, StartUploadRequest{
		SessionID:    "session-upload",
		FileID:       "upload-file",
		ExpectedSize: 256,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}

	file, err = svc.GetFile(ctx, store.FileSelector{ID: "upload-file"})
	if err != nil {
		t.Fatalf("get file after upload start: %v", err)
	}
	if file.Status != metadata.FileStatusUploading {
		t.Fatalf("expected cache invalidation to expose uploading status, got %q", file.Status)
	}
}

func TestServiceAllocateUploadTargets_UsesHealthyNodeCacheAfterFirstLoad(t *testing.T) {
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
	if _, err := svc.CreateFile(ctx, CreateFileRequest{
		InodeID:   "alloc-inode",
		FileID:    "alloc-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "alloc.bin",
		Size:      64,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	for _, node := range []metadata.NodeInfo{
		{ID: "node-a", Address: "http://node-a.local", Healthy: true, Capacity: 1000, Used: 100, UpdatedAt: now},
		{ID: "node-b", Address: "http://node-b.local", Healthy: true, Capacity: 1000, Used: 200, UpdatedAt: now},
	} {
		if err := repo.UpsertNode(ctx, node); err != nil {
			t.Fatalf("upsert node %q: %v", node.ID, err)
		}
	}

	resp, err := svc.AllocateUploadTargets(ctx, AllocateUploadTargetsRequest{FileID: "alloc-file", ChunkIndex: 0})
	if err != nil {
		t.Fatalf("first allocate upload targets: %v", err)
	}
	if len(resp.Targets) != 2 {
		t.Fatalf("expected two targets, got %d", len(resp.Targets))
	}
	if counting.getFileCalls != 1 || counting.listNodeCalls != 1 {
		t.Fatalf("expected first allocation to hit repo once for file and nodes, got file=%d nodes=%d", counting.getFileCalls, counting.listNodeCalls)
	}

	resp, err = svc.AllocateUploadTargets(ctx, AllocateUploadTargetsRequest{FileID: "alloc-file", ChunkIndex: 1})
	if err != nil {
		t.Fatalf("second allocate upload targets: %v", err)
	}
	if len(resp.Targets) != 2 {
		t.Fatalf("expected cached allocation to keep two targets, got %d", len(resp.Targets))
	}
	if counting.getFileCalls != 1 || counting.listNodeCalls != 1 {
		t.Fatalf("expected cached allocation to avoid repo reads, got file=%d nodes=%d", counting.getFileCalls, counting.listNodeCalls)
	}
}

type countingRepository struct {
	store.Repository
	getFileCalls      int
	getNodeCalls      int
	listChunkCalls    int
	listChildrenCalls int
	listNodeCalls     int
}

func (r *countingRepository) GetFile(ctx context.Context, selector store.FileSelector) (*metadata.FileMetadata, error) {
	r.getFileCalls++
	return r.Repository.GetFile(ctx, selector)
}

func (r *countingRepository) GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	r.getNodeCalls++
	return r.Repository.GetNode(ctx, nodeID)
}

func (r *countingRepository) ListChunksByFile(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	r.listChunkCalls++
	return r.Repository.ListChunksByFile(ctx, fileID)
}

func (r *countingRepository) ListChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	r.listChildrenCalls++
	return r.Repository.ListChildren(ctx, parentID, opts)
}

func (r *countingRepository) ListNodes(ctx context.Context, filter store.NodeFilter) ([]metadata.NodeInfo, error) {
	r.listNodeCalls++
	return r.Repository.ListNodes(ctx, filter)
}

type fakeReadCacheBackend struct {
	values     map[string]string
	sortedSets map[string]map[string]float64
}

func newFakeReadCacheBackend() *fakeReadCacheBackend {
	return &fakeReadCacheBackend{
		values:     make(map[string]string),
		sortedSets: make(map[string]map[string]float64),
	}
}

func (b *fakeReadCacheBackend) Get(ctx context.Context, key string) *redis.StringCmd {
	if value, ok := b.values[key]; ok {
		return redis.NewStringResult(value, nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (b *fakeReadCacheBackend) Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd {
	switch v := value.(type) {
	case []byte:
		b.values[key] = string(v)
	case string:
		b.values[key] = v
	default:
		return redis.NewStatusResult("", errors.New("unsupported fake cache value type"))
	}
	return redis.NewStatusResult("OK", nil)
}

func (b *fakeReadCacheBackend) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	var deleted int64
	for _, key := range keys {
		if _, ok := b.values[key]; ok {
			delete(b.values, key)
			deleted++
		}
	}
	return redis.NewIntResult(deleted, nil)
}

func (b *fakeReadCacheBackend) Incr(ctx context.Context, key string) *redis.IntCmd {
	current := int64(0)
	if value, ok := b.values[key]; ok {
		var err error
		current, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return redis.NewIntResult(0, err)
		}
	}
	current++
	b.values[key] = strconv.FormatInt(current, 10)
	return redis.NewIntResult(current, nil)
}

func (b *fakeReadCacheBackend) Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(true, nil)
}

func (b *fakeReadCacheBackend) SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd {
	if _, exists := b.values[key]; exists {
		return redis.NewBoolResult(false, nil)
	}
	s, ok := value.(string)
	if !ok {
		return redis.NewBoolResult(false, errors.New("unsupported fake lock value type"))
	}
	b.values[key] = s
	return redis.NewBoolResult(true, nil)
}

func (b *fakeReadCacheBackend) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	if len(keys) != 1 || len(args) != 1 {
		return redis.NewCmdResult(nil, errors.New("unexpected eval arguments"))
	}
	key := keys[0]
	owner, ok := args[0].(string)
	if !ok {
		return redis.NewCmdResult(nil, errors.New("unexpected owner token"))
	}
	if current, exists := b.values[key]; exists && current == owner {
		delete(b.values, key)
		return redis.NewCmdResult(int64(1), nil)
	}
	return redis.NewCmdResult(int64(0), nil)
}

func (b *fakeReadCacheBackend) ZIncrBy(ctx context.Context, key string, increment float64, member string) *redis.FloatCmd {
	if _, ok := b.sortedSets[key]; !ok {
		b.sortedSets[key] = make(map[string]float64)
	}
	b.sortedSets[key][member] += increment
	return redis.NewFloatResult(b.sortedSets[key][member], nil)
}

func (b *fakeReadCacheBackend) ZRevRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd {
	set := b.sortedSets[key]
	if len(set) == 0 {
		return redis.NewStringSliceResult(nil, nil)
	}
	type memberScore struct {
		member string
		score  float64
	}
	ordered := make([]memberScore, 0, len(set))
	for member, score := range set {
		ordered = append(ordered, memberScore{member: member, score: score})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score == ordered[j].score {
			return ordered[i].member < ordered[j].member
		}
		return ordered[i].score > ordered[j].score
	})
	if start < 0 {
		start = 0
	}
	if stop >= int64(len(ordered)) {
		stop = int64(len(ordered) - 1)
	}
	if start > stop || start >= int64(len(ordered)) {
		return redis.NewStringSliceResult(nil, nil)
	}
	result := make([]string, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		result = append(result, ordered[i].member)
	}
	return redis.NewStringSliceResult(result, nil)
}

func mustCreateRootInRepo(t *testing.T, ctx context.Context, repo store.Repository, now time.Time) {
	t.Helper()
	if err := repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:         metadata.InodeID(metadata.RootInodeID),
		Path:       "/",
		Type:       metadata.InodeTypeDirectory,
		Status:     metadata.InodeStatusActive,
		LinkCount:  1,
		Generation: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil && !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("create root inode: %v", err)
	}
}

func mustSeedDownloadPlanFixture(t *testing.T, ctx context.Context, repo store.Repository, now time.Time) {
	t.Helper()
	if err := repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:         "bundle-inode",
		ParentID:   metadata.InodeID(metadata.RootInodeID),
		FileID:     "bundle-file",
		Name:       "bundle.bin",
		Path:       "/bundle.bin",
		Type:       metadata.InodeTypeFile,
		Status:     metadata.InodeStatusActive,
		LinkCount:  1,
		Generation: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("create bundle inode: %v", err)
	}
	if err := repo.CreateFile(ctx, &metadata.FileMetadata{
		ID:            "bundle-file",
		InodeID:       "bundle-inode",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Name:          "bundle.bin",
		Path:          "/bundle.bin",
		Size:          metadata.FixedChunkSizeBytes + 64,
		StoredSize:    metadata.FixedChunkSizeBytes + 64,
		ChunkSize:     metadata.FixedChunkSizeBytes,
		Status:        metadata.FileStatusAvailable,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create bundle file: %v", err)
	}
	if err := repo.UpsertChunks(ctx, []metadata.ChunkMetadata{
		{
			ID:     "bundle-chunk-0",
			FileID: "bundle-file",
			Index:  0,
			Offset: 0,
			Size:   metadata.FixedChunkSizeBytes,
			Status: metadata.ChunkStatusAvailable,
			Replicas: metadata.ReplicaSet{
				"node-a": {NodeID: "node-a", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:     "bundle-chunk-1",
			FileID: "bundle-file",
			Index:  1,
			Offset: metadata.FixedChunkSizeBytes,
			Size:   64,
			Status: metadata.ChunkStatusAvailable,
			Replicas: metadata.ReplicaSet{
				"node-b": {NodeID: "node-b", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}); err != nil {
		t.Fatalf("upsert bundle chunks: %v", err)
	}
}
