package rpc

import (
	"time"

	"AstraStorage/internal/mds/metadata"
)

const (
	MethodCreateDirectory        = "mds.create_directory"
	MethodCreateFile             = "mds.create_file"
	MethodRegisterNode           = "mds.register_node"
	MethodHeartbeatNode          = "mds.heartbeat_node"
	MethodAllocateUploadTargets  = "mds.allocate_upload_targets"
	MethodStartUpload            = "mds.start_upload"
	MethodCommitChunk            = "mds.commit_chunk"
	MethodCompleteUpload         = "mds.complete_upload"
	MethodVerifyUpload           = "mds.verify_upload"
	MethodFailUploadVerification = "mds.fail_upload_verification"
	MethodRetryUpload            = "mds.retry_upload"
	MethodRenameInode            = "mds.rename_inode"
	MethodMoveInode              = "mds.move_inode"
	MethodDeleteFile             = "mds.delete_file"
	MethodDeleteDirectory        = "mds.delete_directory"
	MethodGetInode               = "mds.get_inode"
	MethodGetFile                = "mds.get_file"
	MethodGetNode                = "mds.get_node"
	MethodListChildren           = "mds.list_children"
	MethodListFileChunks         = "mds.list_file_chunks"
	MethodGetUploadSession       = "mds.get_upload_session"
	MethodBuildDownloadPlan      = "mds.build_download_plan"
)

type CreateDirectoryRequest struct {
	InodeID     metadata.InodeID
	ParentID    metadata.InodeID
	Name        string
	Permissions uint32
	Owner       string
	Group       string
	CreatedAt   time.Time
}

type CreateDirectoryResponse struct {
	Inode *metadata.InodeMetadata
}

type CreateFileRequest struct {
	InodeID       metadata.InodeID
	FileID        metadata.FileID
	ParentID      metadata.InodeID
	Name          string
	Size          int64
	ContentType   string
	StorageClass  string
	UserMetadata  map[string]string
	Tags          map[string]string
	ReplicaPolicy *metadata.ReplicaPolicy
	CreatedAt     time.Time
}

type CreateFileResponse struct {
	File *metadata.FileMetadata
}

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

type RegisterNodeResponse struct {
	Node *metadata.NodeInfo
}

type HeartbeatNodeRequest struct {
	NodeID     metadata.NodeID
	Healthy    bool
	Capacity   int64
	Used       int64
	LastSeenAt time.Time
}

type HeartbeatNodeResponse struct {
	Node *metadata.NodeInfo
}

type UploadTarget struct {
	NodeID  metadata.NodeID
	Address string
}

type AllocateUploadTargetsRequest struct {
	FileID     metadata.FileID
	ChunkIndex int64
}

type AllocateUploadTargetsResponse struct {
	FileID     metadata.FileID
	ChunkIndex int64
	Targets    []UploadTarget
}

type StartUploadRequest struct {
	SessionID           metadata.UploadSessionID
	FileID              metadata.FileID
	UploadKey           string
	ExpectedSize        int64
	ExpectedChecksum    *metadata.Checksum
	ClientMetadata      map[string]string
	TransportAttributes map[string]string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
}

type StartUploadResponse struct {
	Session *metadata.UploadSession
}

type CommitChunkRequest struct {
	SessionID     metadata.UploadSessionID
	ChunkID       metadata.ChunkID
	Index         int64
	Offset        int64
	Size          int64
	Status        metadata.ChunkStatus
	Checksum      *metadata.Checksum
	Replicas      metadata.ReplicaSet
	ReplicaPolicy *metadata.ReplicaPolicy
	CommittedAt   time.Time
}

type CommitChunkResponse struct {
	Chunk *metadata.ChunkMetadata
}

type CompleteUploadRequest struct {
	SessionID        metadata.UploadSessionID
	FinalChecksum    *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	CompletedAt      time.Time
}

type CompleteUploadResponse struct {
	File *metadata.FileMetadata
}

type VerifyUploadRequest struct {
	SessionID        metadata.UploadSessionID
	VerifiedChecksum *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	VerifiedAt       time.Time
}

type VerifyUploadResponse struct {
	File *metadata.FileMetadata
}

type FailUploadVerificationRequest struct {
	SessionID        metadata.UploadSessionID
	ChunkID          metadata.ChunkID
	ActualChecksum   *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	ErrorCode        string
	ErrorMessage     string
	Retryable        bool
	Attempt          int
	MaxAttempts      int
	FailedAt         time.Time
	NextRetryAt      *time.Time
}

type FailUploadVerificationResponse struct {
	File *metadata.FileMetadata
}

type RetryUploadRequest struct {
	SessionID        metadata.UploadSessionID
	ExpectedStatuses []metadata.FileStatus
	RetriedAt        time.Time
}

type RetryUploadResponse struct {
	File *metadata.FileMetadata
}

type RenameInodeRequest struct {
	InodeID   metadata.InodeID
	NewName   string
	UpdatedAt time.Time
}

type RenameInodeResponse struct {
	Inode *metadata.InodeMetadata
}

type MoveInodeRequest struct {
	InodeID        metadata.InodeID
	TargetParentID metadata.InodeID
	NewName        string
	UpdatedAt      time.Time
}

type MoveInodeResponse struct {
	Inode *metadata.InodeMetadata
}

type DeleteFileRequest struct {
	FileID    metadata.FileID
	DeletedAt time.Time
}

type DeleteFileResponse struct{}

type DeleteDirectoryRequest struct {
	InodeID   metadata.InodeID
	Recursive bool
	DeletedAt time.Time
}

type DeleteDirectoryResponse struct{}

type GetInodeRequest struct {
	ID metadata.InodeID
}

type GetInodeResponse struct {
	Inode *metadata.InodeMetadata
}

type GetFileRequest struct {
	ID metadata.FileID
}

type GetFileResponse struct {
	File *metadata.FileMetadata
}

type GetNodeRequest struct {
	ID metadata.NodeID
}

type GetNodeResponse struct {
	Node *metadata.NodeInfo
}

type ListChildrenRequest struct {
	ParentID metadata.InodeID
	Limit    int
	Offset   int
}

type ListChildrenResponse struct {
	Entries []metadata.DirectoryEntry
}

type ListFileChunksRequest struct {
	FileID metadata.FileID
}

type ListFileChunksResponse struct {
	Chunks []metadata.ChunkMetadata
}

type GetUploadSessionRequest struct {
	SessionID metadata.UploadSessionID
}

type GetUploadSessionResponse struct {
	Session *metadata.UploadSession
}

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

type BuildDownloadPlanRequest struct {
	FileID metadata.FileID
}

type BuildDownloadPlanResponse struct {
	Plan *DownloadPlan
}
