package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) UpsertChunks(_ context.Context, chunks []metadata.ChunkMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return upsertChunks(&r.state, chunks)
}

func (tx *memoryTx) UpsertChunks(_ context.Context, chunks []metadata.ChunkMetadata) error {
	return upsertChunks(&tx.state, chunks)
}

// upsertChunks 支持批量写入或覆盖 chunk 记录。
// 当前主要守住 chunk size 和 offset/index 关系这两条基础不变量。
func upsertChunks(state *memoryState, chunks []metadata.ChunkMetadata) error {
	pending := make(map[metadata.ChunkID]metadata.ChunkMetadata, len(chunks))
	for _, chunk := range chunks {
		if chunk.ID == "" || chunk.FileID == "" {
			return fmt.Errorf("%w: chunk id and file id are required", ErrInvalidArgument)
		}
		if _, ok := state.files[chunk.FileID]; !ok {
			return fmt.Errorf("%w: file %q", ErrNotFound, chunk.FileID)
		}
		if chunk.Size < 0 {
			return fmt.Errorf("%w: chunk size cannot be negative", ErrInvalidArgument)
		}
		if chunk.Size > metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk size cannot exceed %d", ErrInvalidArgument, metadata.FixedChunkSizeBytes)
		}
		if chunk.Offset != chunk.Index*metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk offset must equal index * chunk size", ErrInvalidArgument)
		}
		for _, existing := range state.chunks {
			if existing.FileID != chunk.FileID || existing.ID == chunk.ID {
				continue
			}
			if existing.Index == chunk.Index {
				return fmt.Errorf("%w: duplicate chunk index %d for file %q", ErrAlreadyExists, chunk.Index, chunk.FileID)
			}
			if existing.Offset == chunk.Offset {
				return fmt.Errorf("%w: duplicate chunk offset %d for file %q", ErrAlreadyExists, chunk.Offset, chunk.FileID)
			}
		}
		for _, existing := range pending {
			if existing.FileID != chunk.FileID || existing.ID == chunk.ID {
				continue
			}
			if existing.Index == chunk.Index {
				return fmt.Errorf("%w: duplicate chunk index %d for file %q", ErrAlreadyExists, chunk.Index, chunk.FileID)
			}
			if existing.Offset == chunk.Offset {
				return fmt.Errorf("%w: duplicate chunk offset %d for file %q", ErrAlreadyExists, chunk.Offset, chunk.FileID)
			}
		}
		pending[chunk.ID] = cloneChunk(chunk)
	}
	for id, chunk := range pending {
		copyChunk := chunk
		state.chunks[id] = &copyChunk
	}
	return nil
}

func (r *memoryRepository) GetChunk(_ context.Context, selector ChunkSelector) (*metadata.ChunkMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getChunk(r.state, selector)
}

func (tx *memoryTx) GetChunk(_ context.Context, selector ChunkSelector) (*metadata.ChunkMetadata, error) {
	return getChunk(tx.state, selector)
}

// getChunk 返回副本，避免调用方绕过仓储直接修改副本集合和校验结果。
func getChunk(state memoryState, selector ChunkSelector) (*metadata.ChunkMetadata, error) {
	chunk, ok := findChunk(state, selector)
	if !ok {
		return nil, fmt.Errorf("%w: chunk", ErrNotFound)
	}
	copyChunk := *chunk
	copyChunk.Checksum = cloneChecksum(chunk.Checksum)
	copyChunk.ReplicaPolicy = chunk.ReplicaPolicy
	copyChunk.Replicas = cloneReplicaSet(chunk.Replicas)
	return &copyChunk, nil
}

func (r *memoryRepository) ListChunksByFile(_ context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listChunksByFile(r.state, fileID)
}

func (tx *memoryTx) ListChunksByFile(_ context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return listChunksByFile(tx.state, fileID)
}

// listChunksByFile 会按 chunk index 排序，方便上层按文件顺序恢复分片列表。
func listChunksByFile(state memoryState, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	chunks := make([]metadata.ChunkMetadata, 0)
	for _, chunk := range state.chunks {
		if chunk.FileID != fileID {
			continue
		}
		chunks = append(chunks, cloneChunk(*chunk))
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })
	return chunks, nil
}

func (r *memoryRepository) ListChunksByNode(_ context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listChunksByNode(r.state, nodeID), nil
}

func (tx *memoryTx) ListChunksByNode(_ context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	return listChunksByNode(tx.state, nodeID), nil
}

func listChunksByNode(state memoryState, nodeID metadata.NodeID) []metadata.ChunkMetadata {
	chunks := make([]metadata.ChunkMetadata, 0)
	for _, chunk := range state.chunks {
		if _, ok := chunk.Replicas[nodeID]; !ok {
			continue
		}
		chunks = append(chunks, cloneChunk(*chunk))
	}
	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].FileID != chunks[j].FileID {
			return chunks[i].FileID < chunks[j].FileID
		}
		return chunks[i].Index < chunks[j].Index
	})
	return chunks
}

func (r *memoryRepository) UpdateChunkStatus(_ context.Context, patch ChunkStatusPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateChunkStatus(&r.state, patch)
}

func (tx *memoryTx) UpdateChunkStatus(_ context.Context, patch ChunkStatusPatch) error {
	return updateChunkStatus(&tx.state, patch)
}

// updateChunkStatus 主要更新 chunk 状态、校验结果和最近错误码。
func updateChunkStatus(state *memoryState, patch ChunkStatusPatch) error {
	chunk, ok := findChunk(*state, patch.Selector)
	if !ok {
		return fmt.Errorf("%w: chunk", ErrNotFound)
	}
	chunk.Status = patch.Status
	chunk.LastErrorCode = patch.LastErrorCode
	if patch.Checksum != nil {
		chunk.Checksum = cloneChecksum(*patch.Checksum)
	}
	if patch.VerifiedAt != nil {
		t := *patch.VerifiedAt
		chunk.VerifiedAt = &t
	}
	chunk.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) UpdateChunkReplicas(_ context.Context, patch ChunkReplicaPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateChunkReplicas(&r.state, patch)
}

func (tx *memoryTx) UpdateChunkReplicas(_ context.Context, patch ChunkReplicaPatch) error {
	return updateChunkReplicas(&tx.state, patch)
}

// updateChunkReplicas 负责维护 chunk 级副本集合。
// 如果没有显式传入 ReplicaCount，则按当前副本条目数自动回填。
func updateChunkReplicas(state *memoryState, patch ChunkReplicaPatch) error {
	chunk, ok := findChunk(*state, patch.Selector)
	if !ok {
		return fmt.Errorf("%w: chunk", ErrNotFound)
	}
	if chunk.Replicas == nil {
		chunk.Replicas = make(metadata.ReplicaSet)
	}
	for nodeID, replica := range patch.Upserts {
		chunk.Replicas[nodeID] = cloneReplica(replica)
	}
	for _, nodeID := range patch.RemoveNodeIDs {
		delete(chunk.Replicas, nodeID)
	}
	if patch.ReplicaCount != nil {
		chunk.ReplicaCount = *patch.ReplicaCount
	} else {
		chunk.ReplicaCount = len(chunk.Replicas)
	}
	if patch.ReplicaPolicy != nil {
		chunk.ReplicaPolicy = *patch.ReplicaPolicy
	}
	chunk.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) RemoveChunkReplica(_ context.Context, selector ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return removeChunkReplica(&r.state, selector, nodeID, updatedAt)
}

func (tx *memoryTx) RemoveChunkReplica(_ context.Context, selector ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	return removeChunkReplica(&tx.state, selector, nodeID, updatedAt)
}

func removeChunkReplica(state *memoryState, selector ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	chunk, ok := findChunk(*state, selector)
	if !ok {
		return fmt.Errorf("%w: chunk", ErrNotFound)
	}
	if chunk.Replicas == nil {
		return fmt.Errorf("%w: replica on node %q", ErrNotFound, nodeID)
	}
	if _, exists := chunk.Replicas[nodeID]; !exists {
		return fmt.Errorf("%w: replica on node %q", ErrNotFound, nodeID)
	}
	delete(chunk.Replicas, nodeID)
	chunk.ReplicaCount = len(chunk.Replicas)
	chunk.UpdatedAt = updatedAt
	return nil
}

func (r *memoryRepository) DeleteChunk(_ context.Context, selector ChunkSelector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deleteChunk(&r.state, selector)
}

func (tx *memoryTx) DeleteChunk(_ context.Context, selector ChunkSelector) error {
	return deleteChunk(&tx.state, selector)
}

func deleteChunk(state *memoryState, selector ChunkSelector) error {
	chunk, ok := findChunk(*state, selector)
	if !ok {
		return fmt.Errorf("%w: chunk", ErrNotFound)
	}
	delete(state.chunks, chunk.ID)
	return nil
}
