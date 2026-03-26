package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

func TestCreateInode_RejectsSecondRoot(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	root := &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.CreateInode(ctx, root); err != nil {
		t.Fatalf("create first root: %v", err)
	}

	secondRoot := &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := repo.CreateInode(ctx, secondRoot)
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestCreateInode_RejectsDuplicateSiblingName(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)

	first := &metadata.InodeMetadata{
		ID:        "dir-a",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		Path:      "/docs",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.CreateInode(ctx, first); err != nil {
		t.Fatalf("create first child: %v", err)
	}

	duplicate := &metadata.InodeMetadata{
		ID:        "dir-b",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		Path:      "/docs-2",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := repo.CreateInode(ctx, duplicate)
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestRenameDirectory_UpdatesSubtreePaths(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		Path:      "/docs",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        "nested",
		ParentID:  "docs",
		Name:      "nested",
		Path:      "/docs/nested",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        "file",
		ParentID:  "nested",
		Name:      "readme.txt",
		Path:      "/docs/nested/readme.txt",
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})

	updatedAt := now.Add(time.Minute)
	if err := repo.RenameInode(ctx, store.RenameInodeOperation{
		Selector:  store.InodeSelector{ID: "docs"},
		NewName:   "manuals",
		UpdatedAt: updatedAt,
	}); err != nil {
		t.Fatalf("rename inode: %v", err)
	}
	if err := repo.UpdateSubtreePaths(ctx, store.UpdateSubtreePathsOperation{
		RootID:    "docs",
		OldPrefix: "/docs",
		NewPrefix: "/manuals",
		UpdatedAt: updatedAt,
	}); err != nil {
		t.Fatalf("update subtree paths: %v", err)
	}

	docs, err := repo.GetInode(ctx, store.InodeSelector{ID: "docs"})
	if err != nil {
		t.Fatalf("get renamed dir: %v", err)
	}
	if docs.Path != "/manuals" {
		t.Fatalf("expected renamed dir path /manuals, got %q", docs.Path)
	}

	nested, err := repo.GetInode(ctx, store.InodeSelector{ID: "nested"})
	if err != nil {
		t.Fatalf("get nested dir: %v", err)
	}
	if nested.Path != "/manuals/nested" {
		t.Fatalf("expected nested path /manuals/nested, got %q", nested.Path)
	}

	file, err := repo.GetInode(ctx, store.InodeSelector{ID: "file"})
	if err != nil {
		t.Fatalf("get nested file: %v", err)
	}
	if file.Path != "/manuals/nested/readme.txt" {
		t.Fatalf("expected nested file path /manuals/nested/readme.txt, got %q", file.Path)
	}
}

func TestInTx_RollsBackOnError(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)

	expectedErr := errors.New("boom")
	err := repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateInode(ctx, &metadata.InodeMetadata{
			ID:        "dir-a",
			ParentID:  metadata.InodeID(metadata.RootInodeID),
			Name:      "docs",
			Path:      "/docs",
			Type:      metadata.InodeTypeDirectory,
			Status:    metadata.InodeStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected transaction error %v, got %v", expectedErr, err)
	}

	_, err = repo.GetInode(ctx, store.InodeSelector{ID: "dir-a"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after rollback, got %v", err)
	}
}

func TestCreateFile_RejectsDirectoryInode(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	mustCreateRoot(t, ctx, repo, now)
	mustCreateInode(t, ctx, repo, &metadata.InodeMetadata{
		ID:        "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		Path:      "/docs",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})

	err := repo.CreateFile(ctx, &metadata.FileMetadata{
		ID:            "file-1",
		InodeID:       "docs",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Path:          "/docs",
		Name:          "docs",
		Status:        metadata.FileStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCreateFile_RejectsSecondFileForSameInode(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	fileInode := mustCreateFileInode(t, ctx, repo, now, "readme", metadata.InodeID(metadata.RootInodeID), "readme.txt", "/readme.txt")
	mustCreateFileRecord(t, ctx, repo, now, "file-1", fileInode.ID, fileInode.ParentID, fileInode.Name, fileInode.Path)

	err := repo.CreateFile(ctx, &metadata.FileMetadata{
		ID:            "file-2",
		InodeID:       fileInode.ID,
		ParentInodeID: fileInode.ParentID,
		Path:          fileInode.Path,
		Name:          fileInode.Name,
		Status:        metadata.FileStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestUpsertChunks_RejectsDuplicateIndexForSameFile(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	file := mustCreateFileFixture(t, ctx, repo, now, "notes", "/notes.txt", "file-1")

	err := repo.UpsertChunks(ctx, []metadata.ChunkMetadata{
		{
			ID:        "chunk-1",
			FileID:    file.ID,
			Index:     0,
			Offset:    0,
			Size:      metadata.FixedChunkSizeBytes,
			Status:    metadata.ChunkStatusPersisted,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "chunk-2",
			FileID:    file.ID,
			Index:     0,
			Offset:    0,
			Size:      128,
			Status:    metadata.ChunkStatusPersisted,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestUpsertChunks_RejectsOffsetMismatch(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	file := mustCreateFileFixture(t, ctx, repo, now, "video", "/video.mp4", "file-1")

	err := repo.UpsertChunks(ctx, []metadata.ChunkMetadata{
		{
			ID:        "chunk-1",
			FileID:    file.ID,
			Index:     1,
			Offset:    0,
			Size:      1024,
			Status:    metadata.ChunkStatusPersisted,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestUpdateUploadProgress_RejectsOffsetBeyondExpectedSize(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	file := mustCreateFileFixture(t, ctx, repo, now, "archive", "/archive.tar", "file-1")
	session := mustCreateUploadSessionFixture(t, ctx, repo, now, file.ID, metadata.FixedChunkSizeBytes+512)

	err := repo.UpdateUploadProgress(ctx, store.UploadProgressPatch{
		SessionID:       session.ID,
		Status:          metadata.UploadStatusActive,
		ConfirmedOffset: metadata.FixedChunkSizeBytes,
		NextOffset:      metadata.FixedChunkSizeBytes + 1024,
		UpdatedAt:       now.Add(time.Minute),
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCompleteUploadSession_RejectsIncompleteConfirmedOffset(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now()

	file := mustCreateFileFixture(t, ctx, repo, now, "dataset", "/dataset.bin", "file-1")
	session := mustCreateUploadSessionFixture(t, ctx, repo, now, file.ID, metadata.FixedChunkSizeBytes+256)

	if err := repo.UpdateUploadProgress(ctx, store.UploadProgressPatch{
		SessionID:       session.ID,
		Status:          metadata.UploadStatusActive,
		ConfirmedOffset: metadata.FixedChunkSizeBytes,
		NextOffset:      metadata.FixedChunkSizeBytes,
		UpdatedAt:       now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("update upload progress: %v", err)
	}

	err := repo.CompleteUploadSession(ctx, session.ID, now.Add(2*time.Minute))
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestMemoryRepository_ReplicaPlanCreateAndList(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	plan := &metadata.ReplicaPlan{
		ID:            "plan-1",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		RequiredBytes: 4096,
		State:         metadata.ReplicaPlanStatePlanned,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.CreateReplicaPlan(ctx, plan); err != nil {
		t.Fatalf("create replica plan: %v", err)
	}

	plans, err := repo.ListReplicaPlans(ctx, store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeFailover},
	})
	if err != nil {
		t.Fatalf("list replica plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ID != plan.ID {
		t.Fatalf("expected plan id %q, got %q", plan.ID, plans[0].ID)
	}
}

func TestMemoryRepository_ReplicaPlanRejectsDuplicateActiveTarget(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	first := &metadata.ReplicaPlan{
		ID:            "plan-1",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		RequiredBytes: 4096,
		State:         metadata.ReplicaPlanStatePlanned,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.CreateReplicaPlan(ctx, first); err != nil {
		t.Fatalf("create first plan: %v", err)
	}

	duplicate := &metadata.ReplicaPlan{
		ID:            "plan-2",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		RequiredBytes: 4096,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now.Add(time.Minute),
		UpdatedAt:     now.Add(time.Minute),
	}
	err := repo.CreateReplicaPlan(ctx, duplicate)
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestMemoryRepository_ReplicaPlanStateMovesToDone(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	plan := &metadata.ReplicaPlan{
		ID:            "plan-1",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		RequiredBytes: 4096,
		State:         metadata.ReplicaPlanStatePlanned,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.CreateReplicaPlan(ctx, plan); err != nil {
		t.Fatalf("create replica plan: %v", err)
	}

	completedAt := now.Add(2 * time.Minute)
	err := repo.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
		ID:          plan.ID,
		State:       ptrReplicaPlanState(metadata.ReplicaPlanStateDone),
		CompletedAt: &completedAt,
		UpdatedAt:   completedAt,
	})
	if err != nil {
		t.Fatalf("update replica plan: %v", err)
	}

	stored, err := repo.GetReplicaPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get replica plan: %v", err)
	}
	if stored.State != metadata.ReplicaPlanStateDone {
		t.Fatalf("expected done state, got %q", stored.State)
	}
	if stored.CompletedAt == nil || !stored.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed at %v, got %#v", completedAt, stored.CompletedAt)
	}
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

func mustCreateFileInode(t *testing.T, ctx context.Context, repo store.Repository, now time.Time, id metadata.InodeID, parentID metadata.InodeID, name, path string) *metadata.InodeMetadata {
	t.Helper()
	mustCreateRoot(t, ctx, repo, now)
	inode := &metadata.InodeMetadata{
		ID:        id,
		ParentID:  parentID,
		Name:      name,
		Path:      path,
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	mustCreateInode(t, ctx, repo, inode)
	return inode
}

func mustCreateFileRecord(t *testing.T, ctx context.Context, repo store.Repository, now time.Time, fileID metadata.FileID, inodeID metadata.InodeID, parentID metadata.InodeID, name, path string) *metadata.FileMetadata {
	t.Helper()
	file := &metadata.FileMetadata{
		ID:            fileID,
		InodeID:       inodeID,
		ParentInodeID: parentID,
		Name:          name,
		Path:          path,
		Status:        metadata.FileStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.CreateFile(ctx, file); err != nil {
		t.Fatalf("create file %q: %v", file.ID, err)
	}
	return file
}

func mustCreateFileFixture(t *testing.T, ctx context.Context, repo store.Repository, now time.Time, inodeID metadata.InodeID, path string, fileID metadata.FileID) *metadata.FileMetadata {
	t.Helper()
	name := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 && idx+1 < len(path) {
		name = path[idx+1:]
	}
	inode := mustCreateFileInode(t, ctx, repo, now, inodeID, metadata.InodeID(metadata.RootInodeID), name, path)
	return mustCreateFileRecord(t, ctx, repo, now, fileID, inode.ID, inode.ParentID, inode.Name, inode.Path)
}

func mustCreateUploadSessionFixture(t *testing.T, ctx context.Context, repo store.Repository, now time.Time, fileID metadata.FileID, expectedSize int64) *metadata.UploadSession {
	t.Helper()
	session := &metadata.UploadSession{
		ID:           metadata.UploadSessionID("session-" + string(fileID)),
		FileID:       fileID,
		Status:       metadata.UploadStatusPending,
		ExpectedSize: expectedSize,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateUploadSession(ctx, session); err != nil {
		t.Fatalf("create upload session %q: %v", session.ID, err)
	}
	return session
}

func ptrReplicaPlanState(state metadata.ReplicaPlanState) *metadata.ReplicaPlanState {
	return &state
}
