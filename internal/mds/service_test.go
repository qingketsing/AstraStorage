package mds_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

func TestServiceCreateDirectory_CreatesChildUnderParent(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)

	dir, err := svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}

	if dir.Path != "/docs" {
		t.Fatalf("expected directory path /docs, got %q", dir.Path)
	}
	if dir.Type != metadata.InodeTypeDirectory {
		t.Fatalf("expected directory inode type, got %q", dir.Type)
	}
}

func TestServiceCreateFile_CreatesInodeAndFileAtomically(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)

	file, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "readme-inode",
		FileID:    "readme-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "readme.txt",
		Size:      128,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	if file.Path != "/readme.txt" {
		t.Fatalf("expected file path /readme.txt, got %q", file.Path)
	}
	if file.ChunkSize != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected chunk size %d, got %d", metadata.FixedChunkSizeBytes, file.ChunkSize)
	}

	inode, err := repo.GetInode(ctx, store.InodeSelector{ID: "readme-inode"})
	if err != nil {
		t.Fatalf("get inode: %v", err)
	}
	if inode.FileID != "readme-file" {
		t.Fatalf("expected inode file id readme-file, got %q", inode.FileID)
	}
}

func TestServiceCreateFile_RollsBackInodeWhenFileCreateFails(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        "existing-inode",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		FileID:    "existing-file",
		Name:      "existing.txt",
		Path:      "/existing.txt",
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err := repo.CreateFile(ctx, &metadata.FileMetadata{
		ID:            "existing-file",
		InodeID:       "existing-inode",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Name:          "existing.txt",
		Path:          "/existing.txt",
		Status:        metadata.FileStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "new-inode",
		FileID:    "existing-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "another.txt",
		CreatedAt: now,
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "new-inode"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected inode rollback with ErrNotFound, got %v", err)
	}
}

func TestServiceStartUpload_CreatesSessionAndUpdatesFileStatus(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "archive-inode",
		FileID:    "archive-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "archive.tar",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	session, err := svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-1",
		FileID:       "archive-file",
		UploadKey:    "upload/archive.tar",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}

	if session.Status != metadata.UploadStatusPending {
		t.Fatalf("expected pending session status, got %q", session.Status)
	}

	file, err := repo.GetFile(ctx, store.FileSelector{ID: "archive-file"})
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if file.Status != metadata.FileStatusUploading {
		t.Fatalf("expected file status uploading, got %q", file.Status)
	}
	if file.LatestUploadSessionID != "session-1" {
		t.Fatalf("expected latest upload session session-1, got %q", file.LatestUploadSessionID)
	}
}

func TestServiceStartUpload_RejectsConcurrentActiveSession(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "report-inode",
		FileID:    "report-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "report.txt",
		Size:      128,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-report-1",
		FileID:       "report-file",
		ExpectedSize: 128,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start first upload: %v", err)
	}

	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-report-2",
		FileID:       "report-file",
		ExpectedSize: 128,
		CreatedAt:    now.Add(2 * time.Minute),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for concurrent upload session, got %v", err)
	}
}

func TestServiceCommitChunk_WritesChunkAndAdvancesProgress(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "video-inode",
		FileID:    "video-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "video.mp4",
		Size:      metadata.FixedChunkSizeBytes + 512,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-video",
		FileID:       "video-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 512,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}

	chunk, err := svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID:   "session-video",
		ChunkID:     "chunk-0",
		Index:       0,
		Offset:      0,
		Size:        metadata.FixedChunkSizeBytes,
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit chunk: %v", err)
	}

	if chunk.Status != metadata.ChunkStatusPersisted {
		t.Fatalf("expected chunk status persisted, got %q", chunk.Status)
	}

	session, err := repo.GetUploadSession(ctx, "session-video")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.ConfirmedOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected confirmed offset %d, got %d", metadata.FixedChunkSizeBytes, session.ConfirmedOffset)
	}

	file, err := repo.GetFile(ctx, store.FileSelector{ID: "video-file"})
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if file.StoredSize != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected stored size %d, got %d", metadata.FixedChunkSizeBytes, file.StoredSize)
	}
}

func TestServiceCompleteUpload_MarksFileSessionAndChunksVerifying(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()
	firstVerifiedAt := now.Add(90 * time.Second)
	secondVerifiedAt := now.Add(150 * time.Second)

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "dataset-inode",
		FileID:    "dataset-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "dataset.bin",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-dataset",
		FileID:       "dataset-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}

	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-dataset",
		ChunkID:   "chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "chunk-0",
			Verified:   true,
			VerifiedAt: &firstVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-dataset",
		ChunkID:   "chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      256,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "chunk-1",
			Verified:   true,
			VerifiedAt: &secondVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-b": {
				NodeID: "node-b",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}

	file, err := svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID: "session-dataset",
		FinalChecksum: &metadata.Checksum{
			Algorithm: "sha256",
			Value:     "file-dataset",
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if file.Status != metadata.FileStatusVerifying {
		t.Fatalf("expected file status verifying, got %q", file.Status)
	}

	session, err := repo.GetUploadSession(ctx, "session-dataset")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusVerifying {
		t.Fatalf("expected session status verifying, got %q", session.Status)
	}

	chunks, err := repo.ListChunksByFile(ctx, "dataset-file")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	for _, chunk := range chunks {
		if chunk.Status != metadata.ChunkStatusVerifying {
			t.Fatalf("expected chunk %q to be verifying, got %q", chunk.ID, chunk.Status)
		}
	}
}

func TestServiceVerifyUpload_CompletesSessionAndMarksFileAvailable(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()
	firstVerifiedAt := now.Add(90 * time.Second)
	secondVerifiedAt := now.Add(150 * time.Second)
	finalVerifiedAt := now.Add(210 * time.Second)

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "verify-inode",
		FileID:    "verify-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "verify.bin",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-verify",
		FileID:       "verify-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-verify",
		ChunkID:   "verify-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "verify-chunk-0",
			Verified:   true,
			VerifiedAt: &firstVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-verify",
		ChunkID:   "verify-chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      256,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "verify-chunk-1",
			Verified:   true,
			VerifiedAt: &secondVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-b": {
				NodeID: "node-b",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}
	_, err = svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-verify",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	file, err := svc.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID: "session-verify",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "verify-file",
			Verified:   true,
			VerifiedAt: &finalVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("verify upload: %v", err)
	}
	if file.Status != metadata.FileStatusAvailable {
		t.Fatalf("expected file status available, got %q", file.Status)
	}
	if !file.Checksum.Verified {
		t.Fatalf("expected file checksum to be verified")
	}

	session, err := repo.GetUploadSession(ctx, "session-verify")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusCompleted {
		t.Fatalf("expected session status completed, got %q", session.Status)
	}

	chunks, err := repo.ListChunksByFile(ctx, "verify-file")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	for _, chunk := range chunks {
		if chunk.Status != metadata.ChunkStatusAvailable {
			t.Fatalf("expected chunk %q to be available, got %q", chunk.ID, chunk.Status)
		}
	}
}

func TestServiceVerifyUpload_RejectsChunkWithoutReadableReplica(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()
	finalVerifiedAt := now.Add(210 * time.Second)

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "unsafe-inode",
		FileID:    "unsafe-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "unsafe.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-unsafe",
		FileID:       "unsafe-file",
		ExpectedSize: 64,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-unsafe",
		ChunkID:   "unsafe-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Replicas: metadata.ReplicaSet{
			"node-x": {
				NodeID: "node-x",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateCorrupted,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit chunk: %v", err)
	}

	_, err = svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-unsafe",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	_, err = svc.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID: "session-unsafe",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "unsafe-file",
			Verified:   true,
			VerifiedAt: &finalVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(4 * time.Minute),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for unreadable chunk replicas, got %v", err)
	}
}

func TestServiceVerifyUpload_RejectsMissingVerifiedFileChecksum(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()
	chunkVerifiedAt := now.Add(90 * time.Second)

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "checksum-inode",
		FileID:    "checksum-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "checksum.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-checksum",
		FileID:       "checksum-file",
		ExpectedSize: 64,
		ExpectedChecksum: &metadata.Checksum{
			Algorithm: "sha256",
			Value:     "expected-only",
		},
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-checksum",
		ChunkID:   "checksum-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "chunk-ok",
			Verified:   true,
			VerifiedAt: &chunkVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit chunk: %v", err)
	}

	_, err = svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-checksum",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	_, err = svc.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID:        "session-checksum",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(4 * time.Minute),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for missing verified file checksum, got %v", err)
	}
}

func TestServiceVerifyUpload_RejectsUnverifiedChunkChecksum(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()
	finalVerifiedAt := now.Add(150 * time.Second)

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "chunk-checksum-inode",
		FileID:    "chunk-checksum-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "chunk-checksum.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-chunk-checksum",
		FileID:       "chunk-checksum-file",
		ExpectedSize: 64,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-chunk-checksum",
		ChunkID:   "chunk-checksum-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Checksum: &metadata.Checksum{
			Algorithm: "sha256",
			Value:     "not-verified",
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit chunk: %v", err)
	}

	_, err = svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-chunk-checksum",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	_, err = svc.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID: "session-chunk-checksum",
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "file-ok",
			Verified:   true,
			VerifiedAt: &finalVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now.Add(4 * time.Minute),
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for unverified chunk checksum, got %v", err)
	}
}

func TestServiceFailUploadVerification_MarksSessionRetryingAndFileFailed(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustPrepareVerifyingUpload(t, ctx, svc, "retry-inode", "retry-file", "session-retry", now)

	actualVerifiedAt := now.Add(5 * time.Minute)
	file, err := svc.FailUploadVerification(ctx, mds.FailUploadVerificationRequest{
		SessionID: "session-retry",
		ChunkID:   "session-retry-chunk-1",
		ActualChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "bad-chunk",
			Verified:   true,
			VerifiedAt: &actualVerifiedAt,
		},
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		ErrorCode:        "checksum_mismatch",
		ErrorMessage:     "chunk checksum mismatch",
		Retryable:        true,
		Attempt:          1,
		MaxAttempts:      3,
		FailedAt:         now.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("fail upload verification: %v", err)
	}
	if file.Status != metadata.FileStatusFailed {
		t.Fatalf("expected file status failed, got %q", file.Status)
	}

	session, err := repo.GetUploadSession(ctx, "session-retry")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusRetrying {
		t.Fatalf("expected session status retrying, got %q", session.Status)
	}
	if session.Retry.LastFailedOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected failed offset %d, got %d", metadata.FixedChunkSizeBytes, session.Retry.LastFailedOffset)
	}
	if session.Retry.LastFailedChunk != "session-retry-chunk-1" {
		t.Fatalf("expected failed chunk session-retry-chunk-1, got %q", session.Retry.LastFailedChunk)
	}

	chunks, err := repo.ListChunksByFile(ctx, "retry-file")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	for _, chunk := range chunks {
		if chunk.Status != metadata.ChunkStatusFailed {
			t.Fatalf("expected chunk %q to be failed, got %q", chunk.ID, chunk.Status)
		}
	}
}

func TestServiceRetryUpload_ReopensSessionAtFailedOffset(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustPrepareVerifyingUpload(t, ctx, svc, "resume-inode", "resume-file", "session-resume", now)

	_, err := svc.FailUploadVerification(ctx, mds.FailUploadVerificationRequest{
		SessionID:        "session-resume",
		ChunkID:          "session-resume-chunk-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		ErrorCode:        "replica_unhealthy",
		ErrorMessage:     "chunk lost quorum",
		Retryable:        true,
		Attempt:          1,
		MaxAttempts:      3,
		FailedAt:         now.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("fail upload verification: %v", err)
	}

	file, err := svc.RetryUpload(ctx, mds.RetryUploadRequest{
		SessionID:        "session-resume",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusFailed},
		RetriedAt:        now.Add(7 * time.Minute),
	})
	if err != nil {
		t.Fatalf("retry upload: %v", err)
	}
	if file.Status != metadata.FileStatusUploading {
		t.Fatalf("expected file status uploading, got %q", file.Status)
	}
	if file.StoredSize != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected stored size %d, got %d", metadata.FixedChunkSizeBytes, file.StoredSize)
	}

	session, err := repo.GetUploadSession(ctx, "session-resume")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusActive {
		t.Fatalf("expected session status active, got %q", session.Status)
	}
	if session.ConfirmedOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected confirmed offset %d, got %d", metadata.FixedChunkSizeBytes, session.ConfirmedOffset)
	}
	if session.NextOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected next offset %d, got %d", metadata.FixedChunkSizeBytes, session.NextOffset)
	}
	if session.LastPersistedChunk != "session-resume-chunk-0" {
		t.Fatalf("expected last persisted chunk session-resume-chunk-0, got %q", session.LastPersistedChunk)
	}
	if session.VerifiedChecksum != nil {
		t.Fatalf("expected verified checksum to be cleared on retry, got %#v", session.VerifiedChecksum)
	}

	chunks, err := repo.ListChunksByFile(ctx, "resume-file")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 persisted chunk after retry reset, got %d", len(chunks))
	}
	if chunks[0].ID != "session-resume-chunk-0" {
		t.Fatalf("expected remaining chunk session-resume-chunk-0, got %q", chunks[0].ID)
	}
	if chunks[0].Status != metadata.ChunkStatusPersisted {
		t.Fatalf("expected remaining chunk to be persisted, got %q", chunks[0].Status)
	}
}

func TestServiceFailUploadVerification_MarksSessionFailedWhenRetryExhausted(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustPrepareVerifyingUpload(t, ctx, svc, "terminal-inode", "terminal-file", "session-terminal", now)

	file, err := svc.FailUploadVerification(ctx, mds.FailUploadVerificationRequest{
		SessionID:        "session-terminal",
		ChunkID:          "session-terminal-chunk-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		ErrorCode:        "verification_failed",
		ErrorMessage:     "maximum retries reached",
		Retryable:        true,
		Attempt:          3,
		MaxAttempts:      3,
		FailedAt:         now.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("fail upload verification: %v", err)
	}
	if file.Status != metadata.FileStatusFailed {
		t.Fatalf("expected file status failed, got %q", file.Status)
	}

	session, err := repo.GetUploadSession(ctx, "session-terminal")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusFailed {
		t.Fatalf("expected session status failed, got %q", session.Status)
	}
}

func TestServiceRenameInode_UpdatesFileMetadata(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "rename-inode",
		FileID:    "rename-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "before.txt",
		Size:      32,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	inode, err := svc.RenameInode(ctx, mds.RenameInodeRequest{
		InodeID:   "rename-inode",
		NewName:   "after.txt",
		UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("rename inode: %v", err)
	}
	if inode.Path != "/after.txt" {
		t.Fatalf("expected renamed inode path /after.txt, got %q", inode.Path)
	}

	file, err := repo.GetFile(ctx, store.FileSelector{ID: "rename-file"})
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if file.Name != "after.txt" {
		t.Fatalf("expected file name after.txt, got %q", file.Name)
	}
	if file.Path != "/after.txt" {
		t.Fatalf("expected file path /after.txt, got %q", file.Path)
	}
}

func TestServiceMoveInode_UpdatesDirectorySubtreeAndFiles(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create docs: %v", err)
	}
	_, err = svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "archive",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "archive",
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	_, err = svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "notes-inode",
		FileID:    "notes-file",
		ParentID:  "docs",
		Name:      "notes.txt",
		Size:      16,
		CreatedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create file in docs: %v", err)
	}

	inode, err := svc.MoveInode(ctx, mds.MoveInodeRequest{
		InodeID:        "docs",
		TargetParentID: "archive",
		NewName:        "manuals",
		UpdatedAt:      now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("move inode: %v", err)
	}
	if inode.Path != "/archive/manuals" {
		t.Fatalf("expected moved directory path /archive/manuals, got %q", inode.Path)
	}

	child, err := repo.GetInode(ctx, store.InodeSelector{ID: "notes-inode"})
	if err != nil {
		t.Fatalf("get child inode: %v", err)
	}
	if child.Path != "/archive/manuals/notes.txt" {
		t.Fatalf("expected child inode path /archive/manuals/notes.txt, got %q", child.Path)
	}

	file, err := repo.GetFile(ctx, store.FileSelector{ID: "notes-file"})
	if err != nil {
		t.Fatalf("get moved file metadata: %v", err)
	}
	if file.Path != "/archive/manuals/notes.txt" {
		t.Fatalf("expected moved file path /archive/manuals/notes.txt, got %q", file.Path)
	}
}

func TestServiceDeleteFile_CascadesChunksAndUploadSessions(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "delete-inode",
		FileID:    "delete-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "delete.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-delete",
		FileID:       "delete-file",
		ExpectedSize: 64,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-delete",
		ChunkID:   "delete-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit chunk: %v", err)
	}

	if err := svc.DeleteFile(ctx, mds.DeleteFileRequest{
		FileID:    "delete-file",
		DeletedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("delete file: %v", err)
	}

	_, err = repo.GetFile(ctx, store.FileSelector{ID: "delete-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted file to be removed, got %v", err)
	}
	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "delete-inode"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted inode to be removed, got %v", err)
	}
	_, err = repo.GetChunk(ctx, store.ChunkSelector{ID: "delete-chunk-0"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted chunk to be removed, got %v", err)
	}
	_, err = repo.GetUploadSession(ctx, "session-delete")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted upload session to be removed, got %v", err)
	}
}

func TestServiceDeleteDirectory_RecursiveCascadesFilesAndChildren(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create docs: %v", err)
	}
	_, err = svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "nested",
		ParentID:  "docs",
		Name:      "nested",
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create nested: %v", err)
	}
	_, err = svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "deep-file-inode",
		FileID:    "deep-file",
		ParentID:  "nested",
		Name:      "deep.txt",
		Size:      8,
		CreatedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create deep file: %v", err)
	}

	if err := svc.DeleteDirectory(ctx, mds.DeleteDirectoryRequest{
		InodeID:   "docs",
		Recursive: true,
		DeletedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("delete directory recursively: %v", err)
	}

	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "docs"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected docs inode removed, got %v", err)
	}
	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "nested"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected nested inode removed, got %v", err)
	}
	_, err = repo.GetFile(ctx, store.FileSelector{ID: "deep-file"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deep file removed, got %v", err)
	}
}

func TestServiceListChildren_ReturnsDirectEntries(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:   "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create docs directory: %v", err)
	}
	_, err = svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "readme-inode",
		FileID:    "readme-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "readme.txt",
		Size:      32,
		CreatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create readme file: %v", err)
	}

	entries, err := svc.ListChildren(ctx, metadata.InodeID(metadata.RootInodeID), store.ListOptions{})
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "docs" || entries[1].Name != "readme.txt" {
		t.Fatalf("unexpected directory entries: %#v", entries)
	}
}

func TestServiceBuildDownloadPlan_OrdersChunksAndPrefersPrimaryReplica(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "bundle-inode",
		FileID:    "bundle-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "bundle.bin",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, err = svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-bundle",
		FileID:       "bundle-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}

	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-bundle",
		ChunkID:   "chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Replicas: metadata.ReplicaSet{
			"node-b": {
				NodeID: "node-b",
				Role:   metadata.ReplicaRoleSecondary,
				State:  metadata.ReplicaStateReady,
			},
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}
	_, err = svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-bundle",
		ChunkID:   "chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      256,
		Replicas: metadata.ReplicaSet{
			"node-c": {
				NodeID: "node-c",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}

	plan, err := svc.BuildDownloadPlan(ctx, "bundle-file")
	if err != nil {
		t.Fatalf("build download plan: %v", err)
	}
	if plan.ChunkCount != 2 {
		t.Fatalf("expected 2 chunks, got %d", plan.ChunkCount)
	}
	if plan.Chunks[0].ChunkID != "chunk-0" || plan.Chunks[1].ChunkID != "chunk-1" {
		t.Fatalf("unexpected chunk order: %#v", plan.Chunks)
	}
	if plan.Chunks[0].PreferredNodeID != "node-a" {
		t.Fatalf("expected preferred node node-a, got %q", plan.Chunks[0].PreferredNodeID)
	}
	if len(plan.Chunks[0].CandidateNodeIDs) != 2 {
		t.Fatalf("expected 2 candidate nodes, got %d", len(plan.Chunks[0].CandidateNodeIDs))
	}
}

func TestServiceAllocateUploadTargets_PrefersHigherAvailableCapacity(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "alloc-inode-1",
		FileID:    "alloc-file-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "alloc.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	for _, node := range []metadata.NodeInfo{
		{ID: "node-mid", Address: "http://node-mid.local", Healthy: true, Capacity: 1000, Used: 500, UpdatedAt: now},
		{ID: "node-high", Address: "http://node-high.local", Healthy: true, Capacity: 1000, Used: 100, UpdatedAt: now},
		{ID: "node-low", Address: "http://node-low.local", Healthy: true, Capacity: 1000, Used: 700, UpdatedAt: now},
	} {
		if err := repo.UpsertNode(ctx, node); err != nil {
			t.Fatalf("upsert node %q: %v", node.ID, err)
		}
	}

	resp, err := svc.AllocateUploadTargets(ctx, mds.AllocateUploadTargetsRequest{
		FileID:     "alloc-file-1",
		ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("allocate upload targets: %v", err)
	}
	if len(resp.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(resp.Targets))
	}
	if resp.Targets[0].NodeID != "node-high" {
		t.Fatalf("expected highest available node first, got %q", resp.Targets[0].NodeID)
	}
	if resp.Targets[1].NodeID != "node-mid" || resp.Targets[2].NodeID != "node-low" {
		t.Fatalf("unexpected remaining target order: %#v", resp.Targets)
	}
}

func TestServiceAllocateUploadTargets_SkipsFullNodes(t *testing.T) {
	repo := store.NewMemoryRepository()
	svc := mustNewService(t, repo)
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	_, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "alloc-inode-2",
		FileID:    "alloc-file-2",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "alloc-two.bin",
		Size:      64,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	for _, node := range []metadata.NodeInfo{
		{ID: "node-full", Address: "http://node-full.local", Healthy: true, Capacity: 1000, Used: 1000, UpdatedAt: now},
		{ID: "node-valid", Address: "http://node-valid.local", Healthy: true, Capacity: 1000, Used: 100, UpdatedAt: now},
	} {
		if err := repo.UpsertNode(ctx, node); err != nil {
			t.Fatalf("upsert node %q: %v", node.ID, err)
		}
	}

	resp, err := svc.AllocateUploadTargets(ctx, mds.AllocateUploadTargetsRequest{
		FileID:     "alloc-file-2",
		ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("allocate upload targets: %v", err)
	}
	if len(resp.Targets) != 1 {
		t.Fatalf("expected one non-full target, got %d", len(resp.Targets))
	}
	if resp.Targets[0].NodeID != "node-valid" {
		t.Fatalf("expected node-valid to remain, got %q", resp.Targets[0].NodeID)
	}
}

func mustNewService(t *testing.T, repo store.Repository) *mds.Service {
	t.Helper()
	svc, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

func mustCreateRoot(t *testing.T, ctx context.Context, repo store.Repository, now time.Time) {
	t.Helper()
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func mustCreateInode(t *testing.T, ctx context.Context, repo store.Repository, inode *metadata.InodeMetadata) {
	t.Helper()
	if err := repo.CreateInode(ctx, inode); err != nil {
		t.Fatalf("create inode %q: %v", inode.ID, err)
	}
}

func mustPrepareVerifyingUpload(t *testing.T, ctx context.Context, svc *mds.Service, inodeID metadata.InodeID, fileID metadata.FileID, sessionID metadata.UploadSessionID, now time.Time) {
	t.Helper()
	firstVerifiedAt := now.Add(90 * time.Second)
	secondVerifiedAt := now.Add(150 * time.Second)

	if _, err := svc.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   inodeID,
		FileID:    fileID,
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      string(fileID) + ".bin",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := svc.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    sessionID,
		FileID:       fileID,
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}
	if _, err := svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: sessionID,
		ChunkID:   metadata.ChunkID(string(sessionID) + "-chunk-0"),
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      string(sessionID) + "-chunk-0",
			Verified:   true,
			VerifiedAt: &firstVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID: "node-a",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}
	if _, err := svc.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: sessionID,
		ChunkID:   metadata.ChunkID(string(sessionID) + "-chunk-1"),
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      256,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      string(sessionID) + "-chunk-1",
			Verified:   true,
			VerifiedAt: &secondVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-b": {
				NodeID: "node-b",
				Role:   metadata.ReplicaRolePrimary,
				State:  metadata.ReplicaStateReady,
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}
	if _, err := svc.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        sessionID,
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("complete upload: %v", err)
	}
}
