package mds

import (
	"context"
	"fmt"
	"sort"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// DownloadChunkPlan 描述单个 chunk 的读取计划。
type DownloadChunkPlan struct {
	ChunkID          metadata.ChunkID
	Index            int64
	Offset           int64
	Size             int64
	Status           metadata.ChunkStatus
	PreferredNodeID  metadata.NodeID
	CandidateNodeIDs []metadata.NodeID
	Checksum         metadata.Checksum
	ReplicaCount     int
}

// DownloadPlan 描述一个文件的顺序读取方案。
type DownloadPlan struct {
	FileID     metadata.FileID
	InodeID    metadata.InodeID
	Path       string
	Size       int64
	StoredSize int64
	ChunkSize  int64
	FileStatus metadata.FileStatus
	ChunkCount int
	Chunks     []DownloadChunkPlan
}

// ListChildren 列出指定目录下的直接子项。
func (s *Service) ListChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	if s.readCache != nil && parentID != "" && !opts.Recursive {
		return s.readCache.GetChildren(ctx, parentID, opts, func(ctx context.Context) ([]metadata.DirectoryEntry, error) {
			return s.repo.ListChildren(ctx, parentID, opts)
		})
	}
	return s.repo.ListChildren(ctx, parentID, opts)
}

// ListFileChunks 按 chunk index 顺序列出文件分片。
func (s *Service) ListFileChunks(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return s.repo.ListChunksByFile(ctx, fileID)
}

// GetUploadSession 查询单个上传会话。
func (s *Service) GetUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return s.repo.GetUploadSession(ctx, sessionID)
}

// BuildDownloadPlan 组装文件下载所需的顺序 chunk 计划。
func (s *Service) BuildDownloadPlan(ctx context.Context, fileID metadata.FileID) (*DownloadPlan, error) {
	if s.readCache != nil && fileID != "" {
		return s.readCache.GetDownloadPlan(ctx, fileID, func(ctx context.Context) (*DownloadPlan, error) {
			return s.buildDownloadPlan(ctx, fileID)
		})
	}
	return s.buildDownloadPlan(ctx, fileID)
}

func (s *Service) buildDownloadPlan(ctx context.Context, fileID metadata.FileID) (*DownloadPlan, error) {
	file, err := s.repo.GetFile(ctx, store.FileSelector{ID: fileID})
	if err != nil {
		return nil, err
	}
	chunks, err := s.repo.ListChunksByFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("%w: file %q has no chunks", store.ErrNotFound, fileID)
	}

	planChunks := make([]DownloadChunkPlan, 0, len(chunks))
	for _, chunk := range chunks {
		candidateNodeIDs, preferredNodeID := orderedReplicaCandidates(chunk.Replicas)
		planChunks = append(planChunks, DownloadChunkPlan{
			ChunkID:          chunk.ID,
			Index:            chunk.Index,
			Offset:           chunk.Offset,
			Size:             chunk.Size,
			Status:           chunk.Status,
			PreferredNodeID:  preferredNodeID,
			CandidateNodeIDs: candidateNodeIDs,
			Checksum:         chunk.Checksum,
			ReplicaCount:     chunk.ReplicaCount,
		})
	}

	return &DownloadPlan{
		FileID:     file.ID,
		InodeID:    file.InodeID,
		Path:       file.Path,
		Size:       file.Size,
		StoredSize: file.StoredSize,
		ChunkSize:  file.ChunkSize,
		FileStatus: file.Status,
		ChunkCount: len(planChunks),
		Chunks:     planChunks,
	}, nil
}

func orderedReplicaCandidates(replicas metadata.ReplicaSet) ([]metadata.NodeID, metadata.NodeID) {
	if len(replicas) == 0 {
		return nil, ""
	}

	type candidate struct {
		nodeID  metadata.NodeID
		role    metadata.ReplicaRole
		healthy bool
	}

	candidates := make([]candidate, 0, len(replicas))
	for nodeID, replica := range replicas {
		healthy := replica.State == metadata.ReplicaStateReady || replica.State == metadata.ReplicaStateWriting
		candidates = append(candidates, candidate{
			nodeID:  nodeID,
			role:    replica.Role,
			healthy: healthy,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].healthy != candidates[j].healthy {
			return candidates[i].healthy
		}
		if candidates[i].role != candidates[j].role {
			return replicaRoleRank(candidates[i].role) < replicaRoleRank(candidates[j].role)
		}
		return candidates[i].nodeID < candidates[j].nodeID
	})

	nodeIDs := make([]metadata.NodeID, 0, len(candidates))
	for _, candidate := range candidates {
		nodeIDs = append(nodeIDs, candidate.nodeID)
	}
	return nodeIDs, nodeIDs[0]
}

func replicaRoleRank(role metadata.ReplicaRole) int {
	switch role {
	case metadata.ReplicaRolePrimary:
		return 0
	case metadata.ReplicaRoleSecondary:
		return 1
	case metadata.ReplicaRoleWitness:
		return 2
	default:
		return 3
	}
}
