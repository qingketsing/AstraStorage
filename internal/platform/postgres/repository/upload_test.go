package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
)

func TestCreateUploadSessionRejectsMissingFile(t *testing.T) {
	err := createUploadSession(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{err: pgx.ErrNoRows}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
	}, &metadata.UploadSession{ID: "s1", FileID: "f1"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateUploadProgressRejectsOffsetBeyondExpectedSize(t *testing.T) {
	now := time.Now().UTC()
	rowCall := 0
	err := updateUploadProgress(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			rowCall++
			switch rowCall {
			case 1:
				return fakeRow{values: uploadValues("session-1", "file-1", metadata.UploadStatusActive, metadata.FixedChunkSizeBytes+512, metadata.FixedChunkSizeBytes, now)}
			default:
				return fakeRow{err: errors.New("unexpected query row")}
			}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
	}, store.UploadProgressPatch{
		SessionID:       "session-1",
		Status:          metadata.UploadStatusActive,
		ConfirmedOffset: metadata.FixedChunkSizeBytes,
		NextOffset:      metadata.FixedChunkSizeBytes + 1024,
		UpdatedAt:       now.Add(time.Minute),
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCompleteUploadSessionRejectsIncompleteConfirmedOffset(t *testing.T) {
	now := time.Now().UTC()
	err := completeUploadSession(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: uploadValues("session-1", "file-1", metadata.UploadStatusActive, metadata.FixedChunkSizeBytes+256, metadata.FixedChunkSizeBytes, now)}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
	}, "session-1", now.Add(time.Minute))
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func uploadValues(id metadata.UploadSessionID, fileID metadata.FileID, status metadata.UploadStatus, expectedSize, confirmedOffset int64, now time.Time) []any {
	return []any{
		string(id),
		string(fileID),
		"upload-key",
		string(status),
		expectedSize,
		metadata.FixedChunkSizeBytes,
		confirmedOffset,
		confirmedOffset,
		"",
		"",
		"",
		false,
		sql.NullTime{},
		"",
		"",
		false,
		sql.NullTime{},
		0,
		0,
		false,
		"",
		"",
		int64(0),
		"",
		sql.NullTime{},
		sql.NullTime{},
		now,
		now,
		sql.NullTime{},
		sql.NullTime{},
		mustJSON(map[string]string{}),
		mustJSON(map[string]string{}),
	}
}
