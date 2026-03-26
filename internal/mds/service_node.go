package mds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// RegisterNodeRequest 描述一次 data node 到 MDS 的注册请求。
type RegisterNodeRequest struct {
	ID         metadata.NodeID
	Address    string
	Rack       string
	Zone       string
	Region     string
	Labels     map[string]string
	Capacity   int64
	Used       int64
	Healthy    bool
	LastSeenAt *time.Time
	UpdatedAt  time.Time
}

// HeartbeatNodeRequest 描述一次节点心跳回写。
type HeartbeatNodeRequest struct {
	NodeID     metadata.NodeID
	Healthy    bool
	Capacity   int64
	Used       int64
	LastSeenAt time.Time
}

// UploadTarget 描述单个 chunk 当前可写入的目标节点。
type UploadTarget struct {
	NodeID  metadata.NodeID
	Address string
}

// AllocateUploadTargetsRequest 描述一次最小上传目标分配请求。
type AllocateUploadTargetsRequest struct {
	FileID     metadata.FileID
	ChunkIndex int64
}

// AllocateUploadTargetsResponse 返回当前 chunk 的候选上传目标。
type AllocateUploadTargetsResponse struct {
	FileID     metadata.FileID
	ChunkIndex int64
	Targets    []UploadTarget
}

// RegisterNode 创建或更新节点基础信息，并返回持久化后的节点视图。
func (s *Service) RegisterNode(ctx context.Context, req RegisterNodeRequest) (*metadata.NodeInfo, error) {
	if err := validateRegisterNodeRequest(req); err != nil {
		return nil, err
	}

	var registered *metadata.NodeInfo
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		updatedAt := requestTime(req.UpdatedAt)
		lastSeenAt := cloneTimePtr(req.LastSeenAt)
		if lastSeenAt == nil {
			lastSeenAt = cloneTimePtr(&updatedAt)
		}

		node := metadata.NodeInfo{
			ID:         req.ID,
			Address:    strings.TrimSpace(req.Address),
			Rack:       strings.TrimSpace(req.Rack),
			Zone:       strings.TrimSpace(req.Zone),
			Region:     strings.TrimSpace(req.Region),
			Labels:     cloneStringMap(req.Labels),
			Capacity:   req.Capacity,
			Used:       req.Used,
			Healthy:    req.Healthy,
			LastSeenAt: lastSeenAt,
			UpdatedAt:  updatedAt,
		}
		if err := tx.UpsertNode(ctx, node); err != nil {
			return err
		}

		storedNode, err := tx.GetNode(ctx, req.ID)
		if err != nil {
			return err
		}
		registered = cloneNode(storedNode)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateNodeReadModels(ctx, registered.ID)
	s.invalidateHealthyNodeReadModels(ctx)
	return registered, nil
}

// HeartbeatNode 更新节点健康状态、容量和最后一次心跳时间。
func (s *Service) HeartbeatNode(ctx context.Context, req HeartbeatNodeRequest) (*metadata.NodeInfo, error) {
	if err := validateHeartbeatNodeRequest(req); err != nil {
		return nil, err
	}

	var updated *metadata.NodeInfo
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		lastSeenAt := requestTime(req.LastSeenAt)
		if err := tx.UpdateNodeHeartbeat(ctx, store.NodeHeartbeatPatch{
			NodeID:     req.NodeID,
			Healthy:    req.Healthy,
			Capacity:   req.Capacity,
			Used:       req.Used,
			LastSeenAt: lastSeenAt,
		}); err != nil {
			return err
		}
		node, err := tx.GetNode(ctx, req.NodeID)
		if err != nil {
			return err
		}
		updated = cloneNode(node)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateNodeReadModels(ctx, updated.ID)
	s.invalidateHealthyNodeReadModels(ctx)
	return updated, nil
}

// AllocateUploadTargets 返回当前文件 chunk 可写入的健康节点。
func (s *Service) AllocateUploadTargets(ctx context.Context, req AllocateUploadTargetsRequest) (*AllocateUploadTargetsResponse, error) {
	if err := validateAllocateUploadTargetsRequest(req); err != nil {
		return nil, err
	}
	file, err := s.GetFile(ctx, store.FileSelector{ID: req.FileID})
	if err != nil {
		return nil, err
	}
	nodes, err := s.listHealthyNodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: no healthy storage nodes available for file %q", store.ErrConflict, file.ID)
	}

	targetCount := file.ReplicaPolicy.DesiredReplicaCount
	if targetCount <= 0 {
		targetCount = 1
	}
	selectedNodes := SelectCapacityAwareNodes(NodeSelectionInput{
		Candidates: nodes,
		Count:      targetCount,
	})
	if len(selectedNodes) == 0 {
		return nil, fmt.Errorf("%w: no capacity-valid storage nodes available for file %q", store.ErrConflict, file.ID)
	}

	resp := &AllocateUploadTargetsResponse{
		FileID:     file.ID,
		ChunkIndex: req.ChunkIndex,
		Targets:    make([]UploadTarget, 0, len(selectedNodes)),
	}
	for _, node := range selectedNodes {
		resp.Targets = append(resp.Targets, UploadTarget{
			NodeID:  node.ID,
			Address: node.Address,
		})
	}
	return resp, nil
}

func (s *Service) listHealthyNodes(ctx context.Context) ([]metadata.NodeInfo, error) {
	if s.readCache != nil {
		return s.readCache.GetHealthyNodes(ctx, func(ctx context.Context) ([]metadata.NodeInfo, error) {
			return s.repo.ListNodes(ctx, store.NodeFilter{HealthyOnly: true})
		})
	}
	return s.repo.ListNodes(ctx, store.NodeFilter{HealthyOnly: true})
}

func validateRegisterNodeRequest(req RegisterNodeRequest) error {
	if req.ID == "" {
		return fmt.Errorf("%w: node id is required", store.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Address) == "" {
		return fmt.Errorf("%w: node address is required", store.ErrInvalidArgument)
	}
	if req.Capacity < 0 || req.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", store.ErrInvalidArgument)
	}
	if req.Capacity > 0 && req.Used > req.Capacity {
		return fmt.Errorf("%w: node used space cannot exceed capacity", store.ErrInvalidArgument)
	}
	return nil
}

func validateHeartbeatNodeRequest(req HeartbeatNodeRequest) error {
	if req.NodeID == "" {
		return fmt.Errorf("%w: node id is required", store.ErrInvalidArgument)
	}
	if req.Capacity < 0 || req.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", store.ErrInvalidArgument)
	}
	if req.Capacity > 0 && req.Used > req.Capacity {
		return fmt.Errorf("%w: node used space cannot exceed capacity", store.ErrInvalidArgument)
	}
	return nil
}

func validateAllocateUploadTargetsRequest(req AllocateUploadTargetsRequest) error {
	if req.FileID == "" {
		return fmt.Errorf("%w: file id is required", store.ErrInvalidArgument)
	}
	if req.ChunkIndex < 0 {
		return fmt.Errorf("%w: chunk index cannot be negative", store.ErrInvalidArgument)
	}
	return nil
}

// GetNode 查询单个节点信息。
func (s *Service) GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	if s.readCache != nil && nodeID != "" {
		return s.readCache.GetNode(ctx, nodeID, func(ctx context.Context) (*metadata.NodeInfo, error) {
			return s.repo.GetNode(ctx, nodeID)
		})
	}
	return s.repo.GetNode(ctx, nodeID)
}
