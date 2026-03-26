package rpc

import (
	"context"
	"errors"
	"fmt"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// Router 负责把 RPC method 和传输结构映射到 handler 调用。
// 当前保持为进程内路由，便于先稳定方法契约，再决定网络协议。
type Router struct {
	handler *mds.Handler
}

// NewRouter 创建一个绑定到 mds.Handler 的 RPC Router。
func NewRouter(handler *mds.Handler) (*Router, error) {
	if handler == nil {
		return nil, errors.New("mds/rpc: handler is nil")
	}
	return &Router{handler: handler}, nil
}

// Dispatch 根据 method 名称分发请求。
func (r *Router) Dispatch(ctx context.Context, method string, request any) (any, error) {
	switch method {
	case MethodCreateDirectory:
		req, ok := request.(CreateDirectoryRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.CreateDirectory(ctx, req)
	case MethodCreateFile:
		req, ok := request.(CreateFileRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.CreateFile(ctx, req)
	case MethodRegisterNode:
		req, ok := request.(RegisterNodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.RegisterNode(ctx, req)
	case MethodHeartbeatNode:
		req, ok := request.(HeartbeatNodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.HeartbeatNode(ctx, req)
	case MethodAllocateUploadTargets:
		req, ok := request.(AllocateUploadTargetsRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.AllocateUploadTargets(ctx, req)
	case MethodStartUpload:
		req, ok := request.(StartUploadRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.StartUpload(ctx, req)
	case MethodCommitChunk:
		req, ok := request.(CommitChunkRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.CommitChunk(ctx, req)
	case MethodCompleteUpload:
		req, ok := request.(CompleteUploadRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.CompleteUpload(ctx, req)
	case MethodVerifyUpload:
		req, ok := request.(VerifyUploadRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.VerifyUpload(ctx, req)
	case MethodFailUploadVerification:
		req, ok := request.(FailUploadVerificationRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.FailUploadVerification(ctx, req)
	case MethodRetryUpload:
		req, ok := request.(RetryUploadRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.RetryUpload(ctx, req)
	case MethodRenameInode:
		req, ok := request.(RenameInodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.RenameInode(ctx, req)
	case MethodMoveInode:
		req, ok := request.(MoveInodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.MoveInode(ctx, req)
	case MethodDeleteFile:
		req, ok := request.(DeleteFileRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.DeleteFile(ctx, req)
	case MethodDeleteDirectory:
		req, ok := request.(DeleteDirectoryRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.DeleteDirectory(ctx, req)
	case MethodGetInode:
		req, ok := request.(GetInodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.GetInode(ctx, req)
	case MethodGetFile:
		req, ok := request.(GetFileRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.GetFile(ctx, req)
	case MethodGetNode:
		req, ok := request.(GetNodeRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.GetNode(ctx, req)
	case MethodListChildren:
		req, ok := request.(ListChildrenRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.ListChildren(ctx, req)
	case MethodListFileChunks:
		req, ok := request.(ListFileChunksRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.ListFileChunks(ctx, req)
	case MethodGetUploadSession:
		req, ok := request.(GetUploadSessionRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.GetUploadSession(ctx, req)
	case MethodBuildDownloadPlan:
		req, ok := request.(BuildDownloadPlanRequest)
		if !ok {
			return nil, fmt.Errorf("%w: request type for %s", store.ErrInvalidArgument, method)
		}
		return r.BuildDownloadPlan(ctx, req)
	default:
		return nil, fmt.Errorf("%w: unknown method %q", store.ErrInvalidArgument, method)
	}
}

func (r *Router) CreateDirectory(ctx context.Context, req CreateDirectoryRequest) (*CreateDirectoryResponse, error) {
	inode, err := r.handler.CreateDirectory(ctx, mds.CreateDirectoryRequest{
		InodeID:     req.InodeID,
		ParentID:    req.ParentID,
		Name:        req.Name,
		Permissions: req.Permissions,
		Owner:       req.Owner,
		Group:       req.Group,
		CreatedAt:   req.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &CreateDirectoryResponse{Inode: inode}, nil
}

func (r *Router) CreateFile(ctx context.Context, req CreateFileRequest) (*CreateFileResponse, error) {
	file, err := r.handler.CreateFile(ctx, mds.CreateFileRequest{
		InodeID:       req.InodeID,
		FileID:        req.FileID,
		ParentID:      req.ParentID,
		Name:          req.Name,
		Size:          req.Size,
		ContentType:   req.ContentType,
		StorageClass:  req.StorageClass,
		UserMetadata:  req.UserMetadata,
		Tags:          req.Tags,
		ReplicaPolicy: req.ReplicaPolicy,
		CreatedAt:     req.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &CreateFileResponse{File: file}, nil
}

func (r *Router) RegisterNode(ctx context.Context, req RegisterNodeRequest) (*RegisterNodeResponse, error) {
	node, err := r.handler.RegisterNode(ctx, mds.RegisterNodeRequest{
		ID:         req.ID,
		Address:    req.Address,
		Rack:       req.Rack,
		Zone:       req.Zone,
		Region:     req.Region,
		Labels:     req.Labels,
		Capacity:   req.Capacity,
		Used:       req.Used,
		Healthy:    req.Healthy,
		LastSeenAt: req.LastSeenAt,
		UpdatedAt:  req.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &RegisterNodeResponse{Node: node}, nil
}

func (r *Router) HeartbeatNode(ctx context.Context, req HeartbeatNodeRequest) (*HeartbeatNodeResponse, error) {
	node, err := r.handler.HeartbeatNode(ctx, mds.HeartbeatNodeRequest{
		NodeID:     req.NodeID,
		Healthy:    req.Healthy,
		Capacity:   req.Capacity,
		Used:       req.Used,
		LastSeenAt: req.LastSeenAt,
	})
	if err != nil {
		return nil, err
	}
	return &HeartbeatNodeResponse{Node: node}, nil
}

func (r *Router) AllocateUploadTargets(ctx context.Context, req AllocateUploadTargetsRequest) (*AllocateUploadTargetsResponse, error) {
	resp, err := r.handler.AllocateUploadTargets(ctx, mds.AllocateUploadTargetsRequest{
		FileID:     req.FileID,
		ChunkIndex: req.ChunkIndex,
	})
	if err != nil {
		return nil, err
	}
	targets := make([]UploadTarget, 0, len(resp.Targets))
	for _, target := range resp.Targets {
		targets = append(targets, UploadTarget{
			NodeID:  target.NodeID,
			Address: target.Address,
		})
	}
	return &AllocateUploadTargetsResponse{
		FileID:     resp.FileID,
		ChunkIndex: resp.ChunkIndex,
		Targets:    targets,
	}, nil
}

func (r *Router) StartUpload(ctx context.Context, req StartUploadRequest) (*StartUploadResponse, error) {
	session, err := r.handler.StartUpload(ctx, mds.StartUploadRequest{
		SessionID:           req.SessionID,
		FileID:              req.FileID,
		UploadKey:           req.UploadKey,
		ExpectedSize:        req.ExpectedSize,
		ExpectedChecksum:    req.ExpectedChecksum,
		ClientMetadata:      req.ClientMetadata,
		TransportAttributes: req.TransportAttributes,
		ExpiresAt:           req.ExpiresAt,
		CreatedAt:           req.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &StartUploadResponse{Session: session}, nil
}

func (r *Router) CommitChunk(ctx context.Context, req CommitChunkRequest) (*CommitChunkResponse, error) {
	chunk, err := r.handler.CommitChunk(ctx, mds.CommitChunkRequest{
		SessionID:     req.SessionID,
		ChunkID:       req.ChunkID,
		Index:         req.Index,
		Offset:        req.Offset,
		Size:          req.Size,
		Status:        req.Status,
		Checksum:      req.Checksum,
		Replicas:      req.Replicas,
		ReplicaPolicy: req.ReplicaPolicy,
		CommittedAt:   req.CommittedAt,
	})
	if err != nil {
		return nil, err
	}
	return &CommitChunkResponse{Chunk: chunk}, nil
}

func (r *Router) CompleteUpload(ctx context.Context, req CompleteUploadRequest) (*CompleteUploadResponse, error) {
	file, err := r.handler.CompleteUpload(ctx, mds.CompleteUploadRequest{
		SessionID:        req.SessionID,
		FinalChecksum:    req.FinalChecksum,
		ExpectedStatuses: req.ExpectedStatuses,
		CompletedAt:      req.CompletedAt,
	})
	if err != nil {
		return nil, err
	}
	return &CompleteUploadResponse{File: file}, nil
}

func (r *Router) VerifyUpload(ctx context.Context, req VerifyUploadRequest) (*VerifyUploadResponse, error) {
	file, err := r.handler.VerifyUpload(ctx, mds.VerifyUploadRequest{
		SessionID:        req.SessionID,
		VerifiedChecksum: req.VerifiedChecksum,
		ExpectedStatuses: req.ExpectedStatuses,
		VerifiedAt:       req.VerifiedAt,
	})
	if err != nil {
		return nil, err
	}
	return &VerifyUploadResponse{File: file}, nil
}

func (r *Router) FailUploadVerification(ctx context.Context, req FailUploadVerificationRequest) (*FailUploadVerificationResponse, error) {
	file, err := r.handler.FailUploadVerification(ctx, mds.FailUploadVerificationRequest{
		SessionID:        req.SessionID,
		ChunkID:          req.ChunkID,
		ActualChecksum:   req.ActualChecksum,
		ExpectedStatuses: req.ExpectedStatuses,
		ErrorCode:        req.ErrorCode,
		ErrorMessage:     req.ErrorMessage,
		Retryable:        req.Retryable,
		Attempt:          req.Attempt,
		MaxAttempts:      req.MaxAttempts,
		FailedAt:         req.FailedAt,
		NextRetryAt:      req.NextRetryAt,
	})
	if err != nil {
		return nil, err
	}
	return &FailUploadVerificationResponse{File: file}, nil
}

func (r *Router) RetryUpload(ctx context.Context, req RetryUploadRequest) (*RetryUploadResponse, error) {
	file, err := r.handler.RetryUpload(ctx, mds.RetryUploadRequest{
		SessionID:        req.SessionID,
		ExpectedStatuses: req.ExpectedStatuses,
		RetriedAt:        req.RetriedAt,
	})
	if err != nil {
		return nil, err
	}
	return &RetryUploadResponse{File: file}, nil
}

func (r *Router) RenameInode(ctx context.Context, req RenameInodeRequest) (*RenameInodeResponse, error) {
	inode, err := r.handler.RenameInode(ctx, mds.RenameInodeRequest{
		InodeID:   req.InodeID,
		NewName:   req.NewName,
		UpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &RenameInodeResponse{Inode: inode}, nil
}

func (r *Router) MoveInode(ctx context.Context, req MoveInodeRequest) (*MoveInodeResponse, error) {
	inode, err := r.handler.MoveInode(ctx, mds.MoveInodeRequest{
		InodeID:        req.InodeID,
		TargetParentID: req.TargetParentID,
		NewName:        req.NewName,
		UpdatedAt:      req.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return &MoveInodeResponse{Inode: inode}, nil
}

func (r *Router) DeleteFile(ctx context.Context, req DeleteFileRequest) (*DeleteFileResponse, error) {
	if err := r.handler.DeleteFile(ctx, mds.DeleteFileRequest{
		FileID:    req.FileID,
		DeletedAt: req.DeletedAt,
	}); err != nil {
		return nil, err
	}
	return &DeleteFileResponse{}, nil
}

func (r *Router) DeleteDirectory(ctx context.Context, req DeleteDirectoryRequest) (*DeleteDirectoryResponse, error) {
	if err := r.handler.DeleteDirectory(ctx, mds.DeleteDirectoryRequest{
		InodeID:   req.InodeID,
		Recursive: req.Recursive,
		DeletedAt: req.DeletedAt,
	}); err != nil {
		return nil, err
	}
	return &DeleteDirectoryResponse{}, nil
}

func (r *Router) GetInode(ctx context.Context, req GetInodeRequest) (*GetInodeResponse, error) {
	inode, err := r.handler.GetInode(ctx, store.InodeSelector{ID: req.ID})
	if err != nil {
		return nil, err
	}
	return &GetInodeResponse{Inode: inode}, nil
}

func (r *Router) GetFile(ctx context.Context, req GetFileRequest) (*GetFileResponse, error) {
	file, err := r.handler.GetFile(ctx, store.FileSelector{ID: req.ID})
	if err != nil {
		return nil, err
	}
	return &GetFileResponse{File: file}, nil
}

func (r *Router) GetNode(ctx context.Context, req GetNodeRequest) (*GetNodeResponse, error) {
	node, err := r.handler.GetNode(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return &GetNodeResponse{Node: node}, nil
}

func (r *Router) ListChildren(ctx context.Context, req ListChildrenRequest) (*ListChildrenResponse, error) {
	entries, err := r.handler.ListChildren(ctx, req.ParentID, store.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}
	return &ListChildrenResponse{Entries: entries}, nil
}

func (r *Router) ListFileChunks(ctx context.Context, req ListFileChunksRequest) (*ListFileChunksResponse, error) {
	chunks, err := r.handler.ListFileChunks(ctx, req.FileID)
	if err != nil {
		return nil, err
	}
	return &ListFileChunksResponse{Chunks: chunks}, nil
}

func (r *Router) GetUploadSession(ctx context.Context, req GetUploadSessionRequest) (*GetUploadSessionResponse, error) {
	session, err := r.handler.GetUploadSession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	return &GetUploadSessionResponse{Session: session}, nil
}

func (r *Router) BuildDownloadPlan(ctx context.Context, req BuildDownloadPlanRequest) (*BuildDownloadPlanResponse, error) {
	plan, err := r.handler.BuildDownloadPlan(ctx, req.FileID)
	if err != nil {
		return nil, err
	}

	rpcPlan := &DownloadPlan{
		FileID:     plan.FileID,
		InodeID:    plan.InodeID,
		Path:       plan.Path,
		Size:       plan.Size,
		StoredSize: plan.StoredSize,
		ChunkSize:  plan.ChunkSize,
		FileStatus: plan.FileStatus,
		ChunkCount: plan.ChunkCount,
		Chunks:     make([]DownloadChunkPlan, 0, len(plan.Chunks)),
	}
	for _, chunk := range plan.Chunks {
		rpcPlan.Chunks = append(rpcPlan.Chunks, DownloadChunkPlan{
			ChunkID:          chunk.ChunkID,
			Index:            chunk.Index,
			Offset:           chunk.Offset,
			Size:             chunk.Size,
			Status:           chunk.Status,
			PreferredNodeID:  chunk.PreferredNodeID,
			CandidateNodeIDs: append([]metadata.NodeID(nil), chunk.CandidateNodeIDs...),
			Checksum:         chunk.Checksum,
			ReplicaCount:     chunk.ReplicaCount,
		})
	}
	return &BuildDownloadPlanResponse{Plan: rpcPlan}, nil
}
