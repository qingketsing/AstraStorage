package mds

import (
	"context"
	"errors"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// Handler 负责把外部请求转发到 service 层。
// 当前实现保持很薄，后续可在这里补权限、审计和请求级校验。
type Handler struct {
	service       *Service
	observability *Observability
}

// NewHandler 创建一个绑定到 Service 的请求处理器。
func NewHandler(service *Service) (*Handler, error) {
	if service == nil {
		return nil, errors.New("mds: service is nil")
	}
	return &Handler{service: service}, nil
}

func (h *Handler) SetObservability(obs *Observability) {
	if h == nil {
		return
	}
	h.observability = obs
}

func (h *Handler) CreateDirectory(ctx context.Context, req CreateDirectoryRequest) (*metadata.InodeMetadata, error) {
	return h.service.CreateDirectory(ctx, req)
}

func (h *Handler) CreateFile(ctx context.Context, req CreateFileRequest) (*metadata.FileMetadata, error) {
	return h.service.CreateFile(ctx, req)
}

func (h *Handler) StartUpload(ctx context.Context, req StartUploadRequest) (*metadata.UploadSession, error) {
	session, err := h.service.StartUpload(ctx, req)
	h.recordResult(err, h.observability.RecordStartUpload)
	return session, err
}

func (h *Handler) CommitChunk(ctx context.Context, req CommitChunkRequest) (*metadata.ChunkMetadata, error) {
	chunk, err := h.service.CommitChunk(ctx, req)
	h.recordResult(err, h.observability.RecordCommitChunk)
	return chunk, err
}

func (h *Handler) CompleteUpload(ctx context.Context, req CompleteUploadRequest) (*metadata.FileMetadata, error) {
	file, err := h.service.CompleteUpload(ctx, req)
	h.recordResult(err, h.observability.RecordCompleteUpload)
	return file, err
}

func (h *Handler) VerifyUpload(ctx context.Context, req VerifyUploadRequest) (*metadata.FileMetadata, error) {
	return h.service.VerifyUpload(ctx, req)
}

func (h *Handler) FailUploadVerification(ctx context.Context, req FailUploadVerificationRequest) (*metadata.FileMetadata, error) {
	return h.service.FailUploadVerification(ctx, req)
}

func (h *Handler) RetryUpload(ctx context.Context, req RetryUploadRequest) (*metadata.FileMetadata, error) {
	return h.service.RetryUpload(ctx, req)
}

func (h *Handler) RenameInode(ctx context.Context, req RenameInodeRequest) (*metadata.InodeMetadata, error) {
	return h.service.RenameInode(ctx, req)
}

func (h *Handler) MoveInode(ctx context.Context, req MoveInodeRequest) (*metadata.InodeMetadata, error) {
	return h.service.MoveInode(ctx, req)
}

func (h *Handler) DeleteFile(ctx context.Context, req DeleteFileRequest) error {
	return h.service.DeleteFile(ctx, req)
}

func (h *Handler) DeleteDirectory(ctx context.Context, req DeleteDirectoryRequest) error {
	return h.service.DeleteDirectory(ctx, req)
}

func (h *Handler) GetInode(ctx context.Context, selector store.InodeSelector) (*metadata.InodeMetadata, error) {
	return h.service.GetInode(ctx, selector)
}

func (h *Handler) GetFile(ctx context.Context, selector store.FileSelector) (*metadata.FileMetadata, error) {
	return h.service.GetFile(ctx, selector)
}

func (h *Handler) ListChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	return h.service.ListChildren(ctx, parentID, opts)
}

func (h *Handler) ListFileChunks(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return h.service.ListFileChunks(ctx, fileID)
}

func (h *Handler) GetUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return h.service.GetUploadSession(ctx, sessionID)
}

func (h *Handler) BuildDownloadPlan(ctx context.Context, fileID metadata.FileID) (*DownloadPlan, error) {
	plan, err := h.service.BuildDownloadPlan(ctx, fileID)
	h.recordResult(err, h.observability.RecordBuildDownloadPlan)
	return plan, err
}

func (h *Handler) RegisterNode(ctx context.Context, req RegisterNodeRequest) (*metadata.NodeInfo, error) {
	node, err := h.service.RegisterNode(ctx, req)
	h.recordResult(err, h.observability.RecordRegisterNode)
	return node, err
}

func (h *Handler) HeartbeatNode(ctx context.Context, req HeartbeatNodeRequest) (*metadata.NodeInfo, error) {
	node, err := h.service.HeartbeatNode(ctx, req)
	h.recordResult(err, h.observability.RecordHeartbeatNode)
	return node, err
}

func (h *Handler) AllocateUploadTargets(ctx context.Context, req AllocateUploadTargetsRequest) (*AllocateUploadTargetsResponse, error) {
	resp, err := h.service.AllocateUploadTargets(ctx, req)
	h.recordResult(err, h.observability.RecordAllocateUploadTargets)
	return resp, err
}

func (h *Handler) GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	return h.service.GetNode(ctx, nodeID)
}

func (h *Handler) recordResult(err error, record func(string)) {
	if h == nil || record == nil {
		return
	}
	record(ClassifyResult(err))
}
