package rpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/mds/store"
)

func TestRouterDispatch_UploadLifecycle(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	chunkVerifiedAt := now.Add(90 * time.Second)
	finalVerifiedAt := now.Add(150 * time.Second)
	mustCreateRoot(t, ctx, repo, now)

	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "file-inode",
		FileID:    "file-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "report.txt",
		Size:      64,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("dispatch create file: %v", err)
	}

	if _, err := router.Dispatch(ctx, rpc.MethodStartUpload, rpc.StartUploadRequest{
		SessionID:    "session-1",
		FileID:       "file-1",
		ExpectedSize: 64,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("dispatch start upload: %v", err)
	}

	result, err := router.Dispatch(ctx, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "session-1",
		ChunkID:   "chunk-1",
		Index:     0,
		Offset:    0,
		Size:      64,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "chunk-1",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID: "node-1",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch commit chunk: %v", err)
	}
	resp, ok := result.(*rpc.CommitChunkResponse)
	if !ok {
		t.Fatalf("expected CommitChunkResponse, got %T", result)
	}
	if resp.Chunk == nil || resp.Chunk.ID != "chunk-1" {
		t.Fatalf("expected chunk-1 in response, got %#v", resp.Chunk)
	}

	result, err = router.Dispatch(ctx, rpc.MethodCompleteUpload, rpc.CompleteUploadRequest{
		SessionID:        "session-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch complete upload: %v", err)
	}
	completeResp, ok := result.(*rpc.CompleteUploadResponse)
	if !ok {
		t.Fatalf("expected CompleteUploadResponse, got %T", result)
	}
	if completeResp.File == nil || completeResp.File.Status != metadata.FileStatusVerifying {
		t.Fatalf("expected verifying file, got %#v", completeResp.File)
	}

	result, err = router.Dispatch(ctx, rpc.MethodVerifyUpload, rpc.VerifyUploadRequest{
		SessionID: "session-1",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "file-1",
			Verified:   true,
			VerifiedAt: &finalVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch verify upload: %v", err)
	}
	verifyResp, ok := result.(*rpc.VerifyUploadResponse)
	if !ok {
		t.Fatalf("expected VerifyUploadResponse, got %T", result)
	}
	if verifyResp.File == nil || verifyResp.File.Status != metadata.FileStatusAvailable {
		t.Fatalf("expected available file, got %#v", verifyResp.File)
	}
}

func TestRouterDispatch_VerificationFailureAndRetryLifecycle(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	chunkVerifiedAt := now.Add(90 * time.Second)
	mustCreateRoot(t, ctx, repo, now)

	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "retry-inode",
		FileID:    "retry-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "retry.bin",
		Size:      metadata.FixedChunkSizeBytes + 64,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("dispatch create file: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodStartUpload, rpc.StartUploadRequest{
		SessionID:    "retry-session",
		FileID:       "retry-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 64,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("dispatch start upload: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "retry-session",
		ChunkID:   "retry-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "retry-chunk-0",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID: "node-1",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("dispatch first chunk: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "retry-session",
		ChunkID:   "retry-chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      64,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "retry-chunk-1",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-2": {
				NodeID: "node-2",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("dispatch second chunk: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCompleteUpload, rpc.CompleteUploadRequest{
		SessionID:        "retry-session",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("dispatch complete upload: %v", err)
	}

	result, err := router.Dispatch(ctx, rpc.MethodFailUploadVerification, rpc.FailUploadVerificationRequest{
		SessionID:        "retry-session",
		ChunkID:          "retry-chunk-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		ErrorCode:        "checksum_mismatch",
		ErrorMessage:     "chunk verification failed",
		Retryable:        true,
		Attempt:          1,
		MaxAttempts:      3,
		FailedAt:         now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch fail upload verification: %v", err)
	}
	failResp, ok := result.(*rpc.FailUploadVerificationResponse)
	if !ok {
		t.Fatalf("expected FailUploadVerificationResponse, got %T", result)
	}
	if failResp.File == nil || failResp.File.Status != metadata.FileStatusFailed {
		t.Fatalf("expected failed file after verification failure, got %#v", failResp.File)
	}

	result, err = router.Dispatch(ctx, rpc.MethodRetryUpload, rpc.RetryUploadRequest{
		SessionID:        "retry-session",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusFailed},
		RetriedAt:        now.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch retry upload: %v", err)
	}
	retryResp, ok := result.(*rpc.RetryUploadResponse)
	if !ok {
		t.Fatalf("expected RetryUploadResponse, got %T", result)
	}
	if retryResp.File == nil || retryResp.File.Status != metadata.FileStatusUploading {
		t.Fatalf("expected uploading file after retry, got %#v", retryResp.File)
	}

	session, err := repo.GetUploadSession(ctx, "retry-session")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusActive {
		t.Fatalf("expected session status active after retry, got %q", session.Status)
	}
	if session.ConfirmedOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected confirmed offset %d, got %d", metadata.FixedChunkSizeBytes, session.ConfirmedOffset)
	}
}

func TestRouterDispatch_ReadAndDownloadPlan(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	mustCreateRoot(t, ctx, repo, now)
	if _, err := router.Dispatch(ctx, rpc.MethodCreateDirectory, rpc.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("dispatch create directory: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "bundle-inode",
		FileID:    "bundle-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "bundle.bin",
		Size:      32,
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("dispatch create file: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodStartUpload, rpc.StartUploadRequest{
		SessionID:    "bundle-session",
		FileID:       "bundle-file",
		ExpectedSize: 32,
		CreatedAt:    now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("dispatch start upload: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCommitChunk, rpc.CommitChunkRequest{
		SessionID: "bundle-session",
		ChunkID:   "bundle-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      32,
		Replicas: metadata.ReplicaSet{
			"node-1": {
				NodeID: "node-1",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("dispatch commit chunk: %v", err)
	}

	result, err := router.Dispatch(ctx, rpc.MethodListChildren, rpc.ListChildrenRequest{
		ParentID: metadata.InodeID(metadata.RootInodeID),
	})
	if err != nil {
		t.Fatalf("dispatch list children: %v", err)
	}
	listResp, ok := result.(*rpc.ListChildrenResponse)
	if !ok {
		t.Fatalf("expected ListChildrenResponse, got %T", result)
	}
	if len(listResp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(listResp.Entries))
	}

	result, err = router.Dispatch(ctx, rpc.MethodBuildDownloadPlan, rpc.BuildDownloadPlanRequest{
		FileID: "bundle-file",
	})
	if err != nil {
		t.Fatalf("dispatch build download plan: %v", err)
	}
	planResp, ok := result.(*rpc.BuildDownloadPlanResponse)
	if !ok {
		t.Fatalf("expected BuildDownloadPlanResponse, got %T", result)
	}
	if planResp.Plan == nil || planResp.Plan.ChunkCount != 1 {
		t.Fatalf("expected single-chunk plan, got %#v", planResp.Plan)
	}
	if planResp.Plan.Chunks[0].PreferredNodeID != "node-1" {
		t.Fatalf("expected preferred node node-1, got %q", planResp.Plan.Chunks[0].PreferredNodeID)
	}
}

func TestRouterDispatch_NodeHeartbeatAndAllocateUploadTargets(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	mustCreateRoot(t, ctx, repo, now)

	result, err := router.Dispatch(ctx, rpc.MethodRegisterNode, rpc.RegisterNodeRequest{
		ID:        "node-1",
		Address:   "http://127.0.0.1:10080",
		Capacity:  1024,
		Used:      128,
		Healthy:   true,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("dispatch register node: %v", err)
	}
	registerResp, ok := result.(*rpc.RegisterNodeResponse)
	if !ok || registerResp.Node == nil {
		t.Fatalf("expected RegisterNodeResponse, got %#v", result)
	}

	result, err = router.Dispatch(ctx, rpc.MethodHeartbeatNode, rpc.HeartbeatNodeRequest{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       256,
		LastSeenAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("dispatch heartbeat node: %v", err)
	}
	heartbeatResp, ok := result.(*rpc.HeartbeatNodeResponse)
	if !ok || heartbeatResp.Node == nil || heartbeatResp.Node.Used != 256 {
		t.Fatalf("expected HeartbeatNodeResponse with used=256, got %#v", result)
	}

	result, err = router.Dispatch(ctx, rpc.MethodGetNode, rpc.GetNodeRequest{ID: "node-1"})
	if err != nil {
		t.Fatalf("dispatch get node: %v", err)
	}
	getNodeResp, ok := result.(*rpc.GetNodeResponse)
	if !ok || getNodeResp.Node == nil || getNodeResp.Node.Address != "http://127.0.0.1:10080" {
		t.Fatalf("expected GetNodeResponse, got %#v", result)
	}

	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "node-file-inode",
		FileID:    "node-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "node-file.txt",
		Size:      32,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("dispatch create file: %v", err)
	}

	result, err = router.Dispatch(ctx, rpc.MethodAllocateUploadTargets, rpc.AllocateUploadTargetsRequest{
		FileID:     "node-file",
		ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("dispatch allocate upload targets: %v", err)
	}
	allocateResp, ok := result.(*rpc.AllocateUploadTargetsResponse)
	if !ok {
		t.Fatalf("expected AllocateUploadTargetsResponse, got %T", result)
	}
	if len(allocateResp.Targets) != 1 || allocateResp.Targets[0].NodeID != "node-1" {
		t.Fatalf("unexpected allocation response: %#v", allocateResp)
	}
}

func TestRouterDispatch_RenameMoveAndDeleteFlows(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	mustCreateRoot(t, ctx, repo, now)

	if _, err := router.Dispatch(ctx, rpc.MethodCreateDirectory, rpc.CreateDirectoryRequest{
		InodeID:   "archive",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "archive",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "doc-inode",
		FileID:    "doc-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "doc.txt",
		Size:      12,
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}

	result, err := router.Dispatch(ctx, rpc.MethodRenameInode, rpc.RenameInodeRequest{
		InodeID:   "doc-inode",
		NewName:   "guide.txt",
		UpdatedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("rename inode: %v", err)
	}
	renameResp, ok := result.(*rpc.RenameInodeResponse)
	if !ok {
		t.Fatalf("expected RenameInodeResponse, got %T", result)
	}
	if renameResp.Inode == nil || renameResp.Inode.Path != "/guide.txt" {
		t.Fatalf("expected renamed path /guide.txt, got %#v", renameResp.Inode)
	}

	result, err = router.Dispatch(ctx, rpc.MethodMoveInode, rpc.MoveInodeRequest{
		InodeID:        "doc-inode",
		TargetParentID: "archive",
		UpdatedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("move inode: %v", err)
	}
	moveResp, ok := result.(*rpc.MoveInodeResponse)
	if !ok {
		t.Fatalf("expected MoveInodeResponse, got %T", result)
	}
	if moveResp.Inode == nil || moveResp.Inode.Path != "/archive/guide.txt" {
		t.Fatalf("expected moved path /archive/guide.txt, got %#v", moveResp.Inode)
	}

	if _, err := router.Dispatch(ctx, rpc.MethodDeleteFile, rpc.DeleteFileRequest{
		FileID: "doc-file",
	}); err != nil {
		t.Fatalf("delete file: %v", err)
	}

	_, err = repo.GetFile(ctx, store.FileSelector{ID: "doc-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted file to be removed, got %v", err)
	}
}

func TestRouterDispatch_DeleteDirectoryRecursive(t *testing.T) {
	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	mustCreateRoot(t, ctx, repo, now)

	if _, err := router.Dispatch(ctx, rpc.MethodCreateDirectory, rpc.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create docs: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCreateDirectory, rpc.CreateDirectoryRequest{
		InodeID:   "nested",
		ParentID:  "docs",
		Name:      "nested",
		CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create nested: %v", err)
	}
	if _, err := router.Dispatch(ctx, rpc.MethodCreateFile, rpc.CreateFileRequest{
		InodeID:   "leaf-inode",
		FileID:    "leaf-file",
		ParentID:  "nested",
		Name:      "leaf.txt",
		Size:      8,
		CreatedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("create leaf file: %v", err)
	}

	if _, err := router.Dispatch(ctx, rpc.MethodDeleteDirectory, rpc.DeleteDirectoryRequest{
		InodeID:   "docs",
		Recursive: true,
	}); err != nil {
		t.Fatalf("delete directory recursively: %v", err)
	}

	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "docs"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted docs inode, got %v", err)
	}
	_, err = repo.GetFile(ctx, store.FileSelector{ID: "leaf-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted leaf file, got %v", err)
	}
}

func mustCreateRoot(t *testing.T, ctx context.Context, repo store.Repository, now time.Time) {
	t.Helper()
	if err := repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root: %v", err)
	}
}
