package integration_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	pgclient "AstraStorage/internal/platform/postgres/client"
	pgmigrate "AstraStorage/internal/platform/postgres/migrate"
	pgrepository "AstraStorage/internal/platform/postgres/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresIntegration_UploadVerifyAndBuildDownloadPlan(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	file, err := fixture.service.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "video-inode",
		FileID:    "video-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "video.mp4",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	if _, err := fixture.service.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-video",
		FileID:       file.ID,
		UploadKey:    "upload/video.mp4",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}

	firstVerifiedAt := now.Add(90 * time.Second)
	if _, err := fixture.service.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-video",
		ChunkID:   "session-video-chunk-0",
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
				ID:         "replica-0a",
				FileID:     file.ID,
				ChunkID:    "session-video-chunk-0",
				NodeID:     "node-a",
				Role:       metadata.ReplicaRolePrimary,
				State:      metadata.ReplicaStateReady,
				StoredSize: metadata.FixedChunkSizeBytes,
				CreatedAt:  now.Add(2 * time.Minute),
				UpdatedAt:  now.Add(2 * time.Minute),
			},
			"node-b": {
				ID:         "replica-0b",
				FileID:     file.ID,
				ChunkID:    "session-video-chunk-0",
				NodeID:     "node-b",
				Role:       metadata.ReplicaRoleSecondary,
				State:      metadata.ReplicaStateReady,
				StoredSize: metadata.FixedChunkSizeBytes,
				CreatedAt:  now.Add(2 * time.Minute),
				UpdatedAt:  now.Add(2 * time.Minute),
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}

	secondVerifiedAt := now.Add(150 * time.Second)
	if _, err := fixture.service.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-video",
		ChunkID:   "session-video-chunk-1",
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
				ID:         "replica-1b",
				FileID:     file.ID,
				ChunkID:    "session-video-chunk-1",
				NodeID:     "node-b",
				Role:       metadata.ReplicaRolePrimary,
				State:      metadata.ReplicaStateReady,
				StoredSize: 256,
				CreatedAt:  now.Add(3 * time.Minute),
				UpdatedAt:  now.Add(3 * time.Minute),
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}

	if _, err := fixture.service.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-video",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	fileVerifiedAt := now.Add(5 * time.Minute)
	verifiedFile, err := fixture.service.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID:        "session-video",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedChecksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "video-file-checksum",
			Verified:   true,
			VerifiedAt: &fileVerifiedAt,
		},
		VerifiedAt: fileVerifiedAt,
	})
	if err != nil {
		t.Fatalf("verify upload: %v", err)
	}

	if verifiedFile.Status != metadata.FileStatusAvailable {
		t.Fatalf("expected file status available, got %q", verifiedFile.Status)
	}
	if !verifiedFile.Checksum.Verified {
		t.Fatalf("expected verified file checksum, got %#v", verifiedFile.Checksum)
	}

	session, err := fixture.repo.GetUploadSession(ctx, "session-video")
	if err != nil {
		t.Fatalf("get upload session: %v", err)
	}
	if session.Status != metadata.UploadStatusCompleted {
		t.Fatalf("expected completed upload session, got %q", session.Status)
	}

	chunks, err := fixture.repo.ListChunksByFile(ctx, file.ID)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Status != metadata.ChunkStatusAvailable {
			t.Fatalf("expected available chunk status, got %q for %q", chunk.Status, chunk.ID)
		}
	}

	plan, err := fixture.service.BuildDownloadPlan(ctx, file.ID)
	if err != nil {
		t.Fatalf("build download plan: %v", err)
	}
	if plan.ChunkCount != 2 {
		t.Fatalf("expected chunk count 2, got %d", plan.ChunkCount)
	}
	if plan.Chunks[0].PreferredNodeID != "node-a" {
		t.Fatalf("expected first preferred node node-a, got %q", plan.Chunks[0].PreferredNodeID)
	}

	if _, err := fixture.repo.GetNode(ctx, "node-a"); err != nil {
		t.Fatalf("expected placeholder node to exist: %v", err)
	}
}

func TestPostgresIntegration_VerificationFailureRetryAndResume(t *testing.T) {
	fixture := newPostgresFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := fixture.service.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:   "retry-inode",
		FileID:    "retry-file",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "retry.bin",
		Size:      metadata.FixedChunkSizeBytes + 256,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := fixture.service.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:    "session-retry",
		FileID:       "retry-file",
		ExpectedSize: metadata.FixedChunkSizeBytes + 256,
		CreatedAt:    now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}

	firstVerifiedAt := now.Add(90 * time.Second)
	if _, err := fixture.service.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-retry",
		ChunkID:   "session-retry-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      metadata.FixedChunkSizeBytes,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "retry-0",
			Verified:   true,
			VerifiedAt: &firstVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-a": {
				ID:         "retry-0a",
				FileID:     "retry-file",
				ChunkID:    "session-retry-chunk-0",
				NodeID:     "node-a",
				Role:       metadata.ReplicaRolePrimary,
				State:      metadata.ReplicaStateReady,
				StoredSize: metadata.FixedChunkSizeBytes,
				CreatedAt:  now.Add(2 * time.Minute),
				UpdatedAt:  now.Add(2 * time.Minute),
			},
		},
		CommittedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("commit first chunk: %v", err)
	}

	secondVerifiedAt := now.Add(150 * time.Second)
	if _, err := fixture.service.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID: "session-retry",
		ChunkID:   "session-retry-chunk-1",
		Index:     1,
		Offset:    metadata.FixedChunkSizeBytes,
		Size:      256,
		Checksum: &metadata.Checksum{
			Algorithm:  "sha256",
			Value:      "retry-1",
			Verified:   true,
			VerifiedAt: &secondVerifiedAt,
		},
		Replicas: metadata.ReplicaSet{
			"node-b": {
				ID:         "retry-1b",
				FileID:     "retry-file",
				ChunkID:    "session-retry-chunk-1",
				NodeID:     "node-b",
				Role:       metadata.ReplicaRolePrimary,
				State:      metadata.ReplicaStateReady,
				StoredSize: 256,
				CreatedAt:  now.Add(3 * time.Minute),
				UpdatedAt:  now.Add(3 * time.Minute),
			},
		},
		CommittedAt: now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("commit second chunk: %v", err)
	}

	if _, err := fixture.service.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        "session-retry",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	nextRetryAt := now.Add(6 * time.Minute)
	failedFile, err := fixture.service.FailUploadVerification(ctx, mds.FailUploadVerificationRequest{
		SessionID:        "session-retry",
		ChunkID:          "session-retry-chunk-1",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		ErrorCode:        "checksum_mismatch",
		ErrorMessage:     "second chunk checksum mismatch",
		Retryable:        true,
		Attempt:          1,
		MaxAttempts:      2,
		FailedAt:         now.Add(5 * time.Minute),
		NextRetryAt:      &nextRetryAt,
	})
	if err != nil {
		t.Fatalf("fail upload verification: %v", err)
	}
	if failedFile.Status != metadata.FileStatusFailed {
		t.Fatalf("expected failed file status, got %q", failedFile.Status)
	}

	retriedFile, err := fixture.service.RetryUpload(ctx, mds.RetryUploadRequest{
		SessionID:        "session-retry",
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusFailed},
		RetriedAt:        now.Add(7 * time.Minute),
	})
	if err != nil {
		t.Fatalf("retry upload: %v", err)
	}
	if retriedFile.Status != metadata.FileStatusUploading {
		t.Fatalf("expected uploading file status after retry, got %q", retriedFile.Status)
	}

	session, err := fixture.repo.GetUploadSession(ctx, "session-retry")
	if err != nil {
		t.Fatalf("get retried session: %v", err)
	}
	if session.Status != metadata.UploadStatusActive {
		t.Fatalf("expected active session after retry, got %q", session.Status)
	}
	if session.NextOffset != metadata.FixedChunkSizeBytes {
		t.Fatalf("expected next offset to restart at failed chunk, got %d", session.NextOffset)
	}

	chunks, err := fixture.repo.ListChunksByFile(ctx, "retry-file")
	if err != nil {
		t.Fatalf("list chunks after retry: %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != "session-retry-chunk-0" {
		t.Fatalf("expected only first chunk to remain after retry, got %#v", chunks)
	}
}

type postgresFixture struct {
	repo    store.Repository
	service *mds.Service
}

func TestResetPostgresState_TruncatesForeignKeyDependents(t *testing.T) {
	db := &captureResetDB{}
	if err := resetPostgresState(context.Background(), db); err != nil {
		t.Fatalf("reset postgres state: %v", err)
	}
	if !strings.Contains(db.query, "mds_replica_plans") {
		t.Fatalf("expected reset query to truncate mds_replica_plans, got %q", db.query)
	}
	if !strings.Contains(db.query, "CASCADE") {
		t.Fatalf("expected reset query to use CASCADE for foreign key dependents, got %q", db.query)
	}
}

type captureResetDB struct {
	query string
}

func (db *captureResetDB) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.query = query
	return pgconn.CommandTag{}, nil
}

func newPostgresFixture(t *testing.T) postgresFixture {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("MDS_TEST_POSTGRES_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("MDS_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set MDS_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := pgclient.NewPool(ctx, pgclient.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("create postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)

	migrator, err := pgmigrate.New()
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	if err := migrator.Up(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := resetPostgresState(ctx, pool); err != nil {
		t.Fatalf("reset postgres state: %v", err)
	}

	repo, err := pgrepository.New(pool)
	if err != nil {
		t.Fatalf("new postgres repository: %v", err)
	}
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := ensureRoot(ctx, repo, time.Now().UTC()); err != nil {
		t.Fatalf("ensure root inode: %v", err)
	}

	return postgresFixture{
		repo:    repo,
		service: service,
	}
}

func resetPostgresState(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	_, err := db.Exec(ctx, `
TRUNCATE TABLE
	mds_replica_plans,
	mds_file_placements,
	mds_chunk_replicas,
	mds_upload_sessions,
	mds_chunks,
	mds_files,
	mds_inodes,
	mds_nodes
CASCADE
`)
	return err
}

func ensureRoot(ctx context.Context, repo store.Repository, now time.Time) error {
	_, err := repo.GetInode(ctx, store.InodeSelector{ID: metadata.InodeID(metadata.RootInodeID)})
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return repo.CreateInode(ctx, &metadata.InodeMetadata{
		ID:         metadata.InodeID(metadata.RootInodeID),
		Path:       "/",
		Type:       metadata.InodeTypeDirectory,
		Status:     metadata.InodeStatusActive,
		LinkCount:  1,
		Generation: 1,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
}
