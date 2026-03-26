package datanode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound        = errors.New("datanode: not found")
	ErrInvalidArgument = errors.New("datanode: invalid argument")
)

// Checksum 描述 datanode 存储的 chunk 校验信息。
type Checksum struct {
	Algorithm  string    `json:"algorithm"`
	Value      string    `json:"value"`
	VerifiedAt time.Time `json:"verified_at"`
}

// ChunkMetadata 描述单个 chunk 的落盘元数据。
type ChunkMetadata struct {
	ChunkID   string    `json:"chunk_id"`
	FileID    string    `json:"file_id,omitempty"`
	Size      int64     `json:"size"`
	Checksum  *Checksum `json:"checksum,omitempty"`
	StoredAt  time.Time `json:"stored_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StoredChunk 描述完整的 chunk 数据与其元数据。
type StoredChunk struct {
	Metadata ChunkMetadata
	Data     []byte
}

// Store 负责 datanode 上 chunk 的最小落盘能力。
type Store struct {
	dataDir   string
	chunksDir string
	mu        sync.RWMutex
}

// NewStore 创建基于本地文件系统的 chunk store。
func NewStore(cfg Config) (*Store, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	store := &Store{
		dataDir:   cfg.DataDir,
		chunksDir: filepath.Join(cfg.DataDir, "chunks"),
	}
	if err := os.MkdirAll(store.chunksDir, 0o755); err != nil {
		return nil, fmt.Errorf("datanode store: create chunks dir: %w", err)
	}
	return store, nil
}

// Ping 用于健康检查。
func (s *Store) Ping(context.Context) error {
	if s == nil {
		return errors.New("datanode store: nil store")
	}
	if stat, err := os.Stat(s.chunksDir); err != nil {
		return fmt.Errorf("datanode store: stat chunks dir: %w", err)
	} else if !stat.IsDir() {
		return fmt.Errorf("datanode store: chunks path %q is not a directory", s.chunksDir)
	}
	return nil
}

// PutChunk 写入一个 chunk，并以 sidecar json 形式保存元数据。
func (s *Store) PutChunk(_ context.Context, chunkID, fileID string, checksum *Checksum, data []byte, now time.Time) (*ChunkMetadata, error) {
	if err := validateChunkID(chunkID); err != nil {
		return nil, err
	}
	if checksum != nil {
		if err := validateChecksum(*checksum, data); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	meta := ChunkMetadata{
		ChunkID:   chunkID,
		FileID:    strings.TrimSpace(fileID),
		Size:      int64(len(data)),
		Checksum:  cloneChecksum(checksum),
		StoredAt:  now.UTC(),
		UpdatedAt: now.UTC(),
	}
	if existing, err := s.readMetadata(chunkID); err == nil {
		meta.StoredAt = existing.StoredAt
	}

	if err := writeAtomically(s.dataPath(chunkID), data, 0o644); err != nil {
		return nil, err
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("datanode store: marshal metadata: %w", err)
	}
	if err := writeAtomically(s.metaPath(chunkID), metaBytes, 0o644); err != nil {
		return nil, err
	}
	return &meta, nil
}

// GetChunk 读取完整 chunk。
func (s *Store) GetChunk(_ context.Context, chunkID string) (*StoredChunk, error) {
	if err := validateChunkID(chunkID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, err := s.readMetadata(chunkID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.dataPath(chunkID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: chunk %q", ErrNotFound, chunkID)
		}
		return nil, fmt.Errorf("datanode store: read chunk data: %w", err)
	}
	return &StoredChunk{
		Metadata: *meta,
		Data:     data,
	}, nil
}

// DeleteChunk 删除 chunk 数据和元数据。
func (s *Store) DeleteChunk(_ context.Context, chunkID string) error {
	if err := validateChunkID(chunkID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	metaPath := s.metaPath(chunkID)
	dataPath := s.dataPath(chunkID)
	if _, err := os.Stat(metaPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: chunk %q", ErrNotFound, chunkID)
		}
		return fmt.Errorf("datanode store: stat chunk metadata: %w", err)
	}
	if err := os.Remove(metaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("datanode store: remove chunk metadata: %w", err)
	}
	if err := os.Remove(dataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("datanode store: remove chunk data: %w", err)
	}
	return nil
}

// CountChunks 返回当前已经持久化的 chunk 数量。
func (s *Store) CountChunks() (int, error) {
	if s == nil {
		return 0, errors.New("datanode store: nil store")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.chunksDir)
	if err != nil {
		return 0, fmt.Errorf("datanode store: read chunks dir: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count, nil
}

// UsageBytes 返回当前 chunk 目录的真实字节占用。
func (s *Store) UsageBytes() (int64, error) {
	if s == nil {
		return 0, errors.New("datanode store: nil store")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.chunksDir)
	if err != nil {
		return 0, fmt.Errorf("datanode store: read chunks dir for usage: %w", err)
	}

	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, fmt.Errorf("datanode store: stat chunk entry %q: %w", entry.Name(), err)
		}
		total += info.Size()
	}
	return total, nil
}

func (s *Store) dataPath(chunkID string) string {
	return filepath.Join(s.chunksDir, chunkID+".bin")
}

func (s *Store) metaPath(chunkID string) string {
	return filepath.Join(s.chunksDir, chunkID+".json")
}

func (s *Store) readMetadata(chunkID string) (*ChunkMetadata, error) {
	metaBytes, err := os.ReadFile(s.metaPath(chunkID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: chunk %q", ErrNotFound, chunkID)
		}
		return nil, fmt.Errorf("datanode store: read chunk metadata: %w", err)
	}
	var meta ChunkMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("datanode store: unmarshal chunk metadata: %w", err)
	}
	return &meta, nil
}

func validateChunkID(chunkID string) error {
	chunkID = strings.TrimSpace(chunkID)
	switch {
	case chunkID == "":
		return fmt.Errorf("%w: chunk id is required", ErrInvalidArgument)
	case strings.Contains(chunkID, "/"), strings.Contains(chunkID, `\`), strings.Contains(chunkID, ".."):
		return fmt.Errorf("%w: chunk id %q is invalid", ErrInvalidArgument, chunkID)
	default:
		return nil
	}
}

func validateChecksum(checksum Checksum, data []byte) error {
	algorithm := strings.ToLower(strings.TrimSpace(checksum.Algorithm))
	value := strings.TrimSpace(checksum.Value)
	if algorithm == "" || value == "" {
		return fmt.Errorf("%w: checksum algorithm and value must both be set", ErrInvalidArgument)
	}
	switch algorithm {
	case "sha256":
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		if actual != value {
			return fmt.Errorf("%w: sha256 checksum mismatch", ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: unsupported checksum algorithm %q", ErrInvalidArgument, checksum.Algorithm)
	}
	return nil
}

func writeAtomically(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "chunk-*")
	if err != nil {
		return fmt.Errorf("datanode store: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("datanode store: write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("datanode store: chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("datanode store: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("datanode store: rename temp file: %w", err)
	}
	return nil
}

func cloneChecksum(checksum *Checksum) *Checksum {
	if checksum == nil {
		return nil
	}
	copyChecksum := *checksum
	return &copyChecksum
}
