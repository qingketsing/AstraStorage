package datanode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"
)

func TestStorePutGetDeleteChunk(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	now := time.Now().UTC()
	data := []byte("hello datanode")
	sum := sha256.Sum256(data)
	checksum := &Checksum{
		Algorithm:  "sha256",
		Value:      hex.EncodeToString(sum[:]),
		VerifiedAt: now,
	}

	meta, err := store.PutChunk(context.Background(), "chunk-1", "file-1", checksum, data, now)
	if err != nil {
		t.Fatalf("put chunk: %v", err)
	}
	if meta.Size != int64(len(data)) {
		t.Fatalf("expected stored size %d, got %d", len(data), meta.Size)
	}

	stored, err := store.GetChunk(context.Background(), "chunk-1")
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if string(stored.Data) != string(data) {
		t.Fatalf("expected chunk data %q, got %q", data, stored.Data)
	}
	if stored.Metadata.FileID != "file-1" {
		t.Fatalf("expected file id file-1, got %q", stored.Metadata.FileID)
	}

	if err := store.DeleteChunk(context.Background(), "chunk-1"); err != nil {
		t.Fatalf("delete chunk: %v", err)
	}
	_, err = store.GetChunk(context.Background(), "chunk-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	dataDir := t.TempDir()
	first, err := NewStore(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("new first store: %v", err)
	}
	now := time.Now().UTC()
	if _, err := first.PutChunk(context.Background(), "chunk-2", "file-2", nil, []byte("persisted"), now); err != nil {
		t.Fatalf("put chunk: %v", err)
	}

	second, err := NewStore(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("new second store: %v", err)
	}
	stored, err := second.GetChunk(context.Background(), "chunk-2")
	if err != nil {
		t.Fatalf("get chunk after reopen: %v", err)
	}
	if string(stored.Data) != "persisted" {
		t.Fatalf("expected persisted chunk data, got %q", stored.Data)
	}
}

func TestStore_UsageBytes_EmptyStore(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	used, err := store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() error = %v", err)
	}
	if used != 0 {
		t.Fatalf("expected empty store usage 0, got %d", used)
	}
}

func TestStore_UsageBytes_IncludesChunkDataAndMetadata(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	now := time.Now().UTC()
	data := []byte("usage bytes payload")
	if _, err := store.PutChunk(context.Background(), "chunk-usage-1", "file-usage-1", nil, data, now); err != nil {
		t.Fatalf("put chunk: %v", err)
	}

	dataInfo, err := os.Stat(store.dataPath("chunk-usage-1"))
	if err != nil {
		t.Fatalf("stat data file: %v", err)
	}
	metaInfo, err := os.Stat(store.metaPath("chunk-usage-1"))
	if err != nil {
		t.Fatalf("stat meta file: %v", err)
	}

	used, err := store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() error = %v", err)
	}
	want := dataInfo.Size() + metaInfo.Size()
	if used != want {
		t.Fatalf("expected usage %d, got %d", want, used)
	}
}

func TestStore_UsageBytes_DecreasesAfterDelete(t *testing.T) {
	store, err := NewStore(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	now := time.Now().UTC()
	if _, err := store.PutChunk(context.Background(), "chunk-usage-2", "file-usage-2", nil, []byte("before delete"), now); err != nil {
		t.Fatalf("put chunk: %v", err)
	}

	beforeDelete, err := store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() before delete error = %v", err)
	}
	if beforeDelete <= 0 {
		t.Fatalf("expected positive usage before delete, got %d", beforeDelete)
	}

	if err := store.DeleteChunk(context.Background(), "chunk-usage-2"); err != nil {
		t.Fatalf("delete chunk: %v", err)
	}

	afterDelete, err := store.UsageBytes()
	if err != nil {
		t.Fatalf("UsageBytes() after delete error = %v", err)
	}
	if afterDelete >= beforeDelete {
		t.Fatalf("expected usage to decrease after delete, before=%d after=%d", beforeDelete, afterDelete)
	}
	if afterDelete != 0 {
		t.Fatalf("expected empty store usage 0 after delete, got %d", afterDelete)
	}
}
