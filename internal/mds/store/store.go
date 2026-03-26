// store.go
// Metadata Service 元数据存储抽象定义文件。
// 该文件预留用于定义元数据持久化访问接口，
// 包括键值读写、对象查询、状态更新以及对不同后端存储实现的统一抽象。

package store

import (
	"context"
	"time"

	"AstraStorage/internal/mds/metadata"
)

// Repository 定义 MDS 持久化层对外暴露的完整能力集合。
type Repository interface {
	InodeRepository
	FileRepository
	ChunkRepository
	UploadSessionRepository
	NodeRepository
	ReplicaPlanRepository
	TransactionManager
	HealthChecker
}

// HealthChecker 用于探测持久化后端是否可用。
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// InodeRepository 描述目录树与 inode 元数据相关接口。
type InodeRepository interface {
	CreateInode(ctx context.Context, inode *metadata.InodeMetadata) error
	GetInode(ctx context.Context, selector InodeSelector) (*metadata.InodeMetadata, error)
	ListChildren(ctx context.Context, parentID metadata.InodeID, opts ListOptions) ([]metadata.DirectoryEntry, error)
	UpdateInode(ctx context.Context, patch InodePatch) error
	MoveInode(ctx context.Context, op MoveInodeOperation) error
	RenameInode(ctx context.Context, op RenameInodeOperation) error
	DeleteInode(ctx context.Context, selector InodeSelector) error
	UpdateSubtreePaths(ctx context.Context, op UpdateSubtreePathsOperation) error
}

// FileRepository 描述文件元数据相关读写接口。
type FileRepository interface {
	CreateFile(ctx context.Context, file *metadata.FileMetadata) error
	GetFile(ctx context.Context, selector FileSelector) (*metadata.FileMetadata, error)
	ListFiles(ctx context.Context, filter FileFilter) ([]*metadata.FileMetadata, error)
	UpdateFile(ctx context.Context, patch FilePatch) error
	UpdateFilePlacements(ctx context.Context, patch FilePlacementPatch) error
	DeleteFile(ctx context.Context, selector FileSelector) error
}

// ChunkRepository 描述 chunk 元数据相关读写接口。
type ChunkRepository interface {
	UpsertChunks(ctx context.Context, chunks []metadata.ChunkMetadata) error
	GetChunk(ctx context.Context, selector ChunkSelector) (*metadata.ChunkMetadata, error)
	ListChunksByFile(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error)
	ListChunksByNode(ctx context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error)
	UpdateChunkStatus(ctx context.Context, patch ChunkStatusPatch) error
	UpdateChunkReplicas(ctx context.Context, patch ChunkReplicaPatch) error
	RemoveChunkReplica(ctx context.Context, selector ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error
	DeleteChunk(ctx context.Context, selector ChunkSelector) error
}

// UploadSessionRepository 描述断点续传与校验重传会话存储接口。
type UploadSessionRepository interface {
	CreateUploadSession(ctx context.Context, session *metadata.UploadSession) error
	GetUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error)
	ListUploadSessionsByFile(ctx context.Context, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error)
	UpdateUploadProgress(ctx context.Context, progress UploadProgressPatch) error
	RecordUploadFailure(ctx context.Context, failure UploadFailureRecord) error
	CompleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID, completedAt time.Time) error
	DeleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) error
}

// NodeRepository 描述存储节点与心跳状态相关接口。
type NodeRepository interface {
	UpsertNode(ctx context.Context, node metadata.NodeInfo) error
	GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error)
	ListNodes(ctx context.Context, filter NodeFilter) ([]metadata.NodeInfo, error)
	UpdateNodeHeartbeat(ctx context.Context, heartbeat NodeHeartbeatPatch) error
}

// ReplicaPlanRepository 描述调度计划的持久化读写接口。
type ReplicaPlanRepository interface {
	CreateReplicaPlan(ctx context.Context, plan *metadata.ReplicaPlan) error
	GetReplicaPlan(ctx context.Context, id string) (*metadata.ReplicaPlan, error)
	ListReplicaPlans(ctx context.Context, filter ReplicaPlanFilter) ([]metadata.ReplicaPlan, error)
	UpdateReplicaPlan(ctx context.Context, patch ReplicaPlanPatch) error
	DeleteReplicaPlan(ctx context.Context, id string) error
}

// FileSelector 用于定位单个文件元数据记录。
type FileSelector struct {
	ID            metadata.FileID
	InodeID       metadata.InodeID
	ParentInodeID metadata.InodeID
	Namespace     string
	Path          string
	Name          string
	Version       *int64
}

// FileFilter 用于筛选文件元数据集合。
type FileFilter struct {
	Namespace     string
	ParentInodeID metadata.InodeID
	PathPrefix    string
	Status        []metadata.FileStatus
	NodeID        metadata.NodeID
	Limit         int
	Offset        int
}

// InodeSelector 用于定位单个 inode 或目录项。
type InodeSelector struct {
	ID        metadata.InodeID
	ParentID  metadata.InodeID
	Namespace string
	Path      string
	Name      string
	Type      *metadata.InodeType
}

// InodeFilter 用于筛选 inode 集合。
type InodeFilter struct {
	Namespace  string
	ParentID   metadata.InodeID
	PathPrefix string
	Type       []metadata.InodeType
	Status     []metadata.InodeStatus
	Owner      string
	Limit      int
	Offset     int
}

// ListOptions 用于目录列表查询控制。
type ListOptions struct {
	Recursive bool
	Limit     int
	Offset    int
}

// InodePatch 用于局部更新 inode 元数据。
type InodePatch struct {
	Selector    InodeSelector
	Path        *string
	Name        *string
	ParentID    *metadata.InodeID
	Status      *metadata.InodeStatus
	Size        *int64
	Permissions *uint32
	Owner       *string
	Group       *string
	LinkCount   *int64
	Generation  *int64
	AccessedAt  *time.Time
	UpdatedAt   time.Time
}

// MoveInodeOperation 描述将文件或目录移动到新父目录的操作。
type MoveInodeOperation struct {
	Selector         InodeSelector
	TargetParentID   metadata.InodeID
	TargetParentPath string
	NewName          string
	ExpectedType     *metadata.InodeType
	UpdatedAt        time.Time
}

// RenameInodeOperation 描述在同一父目录下的重命名操作。
type RenameInodeOperation struct {
	Selector     InodeSelector
	NewName      string
	ExpectedType *metadata.InodeType
	UpdatedAt    time.Time
}

// UpdateSubtreePathsOperation 用于在目录重命名或迁移后批量更新子树 path 缓存。
type UpdateSubtreePathsOperation struct {
	Namespace string
	RootID    metadata.InodeID
	OldPrefix string
	NewPrefix string
	UpdatedAt time.Time
}

// FilePatch 用于局部更新文件元数据。
type FilePatch struct {
	Selector              FileSelector
	ParentInodeID         *metadata.InodeID
	Path                  *string
	Name                  *string
	Size                  *int64
	StoredSize            *int64
	ChunkSize             *int64
	Version               *int64
	Status                *metadata.FileStatus
	PrimaryNodeID         *metadata.NodeID
	SecondaryNodeIDs      []metadata.NodeID
	LatestUploadSessionID *metadata.UploadSessionID
	Checksum              *metadata.Checksum
	ReplicaPolicy         *metadata.ReplicaPolicy
	UserMetadata          map[string]string
	Tags                  map[string]string
	CompletedAt           *time.Time
	UpdatedAt             time.Time
}

// FilePlacementPatch 用于更新文件的节点放置信息。
type FilePlacementPatch struct {
	Selector       FileSelector
	Upserts        metadata.NodePlacements
	RemoveNodeIDs  []metadata.NodeID
	ExpectedStatus []metadata.FileStatus
	UpdatedAt      time.Time
}

// ChunkSelector 用于定位单个 chunk。
type ChunkSelector struct {
	ID     metadata.ChunkID
	FileID metadata.FileID
	Index  *int64
}

// ChunkStatusPatch 用于更新 chunk 的状态与校验信息。
type ChunkStatusPatch struct {
	Selector      ChunkSelector
	Status        metadata.ChunkStatus
	Checksum      *metadata.Checksum
	LastErrorCode string
	VerifiedAt    *time.Time
	UpdatedAt     time.Time
}

// ChunkReplicaPatch 用于增量修改 chunk 的副本分布。
type ChunkReplicaPatch struct {
	Selector      ChunkSelector
	Upserts       metadata.ReplicaSet
	RemoveNodeIDs []metadata.NodeID
	ReplicaCount  *int
	ReplicaPolicy *metadata.ReplicaPolicy
	UpdatedAt     time.Time
}

type ReplicaPlanFilter struct {
	Types        []metadata.ReplicaPlanType
	States       []metadata.ReplicaPlanState
	ChunkID      metadata.ChunkID
	FileID       metadata.FileID
	SourceNodeID metadata.NodeID
	TargetNodeID metadata.NodeID
	Limit        int
	Offset       int
}

type ReplicaPlanPatch struct {
	ID               string
	State            *metadata.ReplicaPlanState
	LastErrorCode    *string
	LastErrorMessage *string
	RetryCount       *int
	NextRetryAt      *time.Time
	CompletedAt      *time.Time
	UpdatedAt        time.Time
}

// UploadProgressPatch 用于更新续传会话中的 offset 和校验进度。
type UploadProgressPatch struct {
	SessionID             metadata.UploadSessionID
	Status                metadata.UploadStatus
	ConfirmedOffset       int64
	NextOffset            int64
	LastPersistedChunkID  metadata.ChunkID
	ExpectedChecksum      *metadata.Checksum
	VerifiedChecksum      *metadata.Checksum
	ClearExpectedChecksum bool
	ClearVerifiedChecksum bool
	TransportAttributes   map[string]string
	UpdatedAt             time.Time
}

// UploadFailureRecord 用于记录一次失败写入或校验失败，以支持重传决策。
type UploadFailureRecord struct {
	SessionID        metadata.UploadSessionID
	FileID           metadata.FileID
	ChunkID          metadata.ChunkID
	FailedOffset     int64
	ExpectedChecksum *metadata.Checksum
	ActualChecksum   *metadata.Checksum
	ErrorCode        string
	ErrorMessage     string
	Retryable        bool
	Attempt          int
	MaxAttempts      int
	OccurredAt       time.Time
	NextRetryAt      *time.Time
}

// NodeFilter 用于筛选存储节点集合。
type NodeFilter struct {
	IDs         []metadata.NodeID
	HealthyOnly bool
	Zone        string
	Rack        string
	Labels      map[string]string
	Limit       int
	Offset      int
}

// NodeHeartbeatPatch 用于更新节点最后一次心跳及资源使用情况。
type NodeHeartbeatPatch struct {
	NodeID     metadata.NodeID
	Healthy    bool
	Capacity   int64
	Used       int64
	LastSeenAt time.Time
}
