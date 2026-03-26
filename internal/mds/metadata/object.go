// object.go
// Metadata Service 中文件元信息模型定义文件。
// 该文件用于描述文件系统场景下的文件元数据结构，
// 包括路径、所属 inode、大小、校验值以及块映射关系。

package metadata

import "time"

const (
	// DefaultReplicaCount 表示普通文件写入后的默认副本数。
	DefaultReplicaCount = 3
	// MinimumReadableReplicaCount 表示在三副本场景下保证文件仍可读取的最小健康副本数。
	MinimumReadableReplicaCount = 1
	// FixedChunkSizeBytes 表示上传和下载统一使用的固定分片大小，单位为字节。
	FixedChunkSizeBytes int64 = 4 << 20
)

// FileID 是文件元数据的全局唯一标识。
type FileID string

// UploadSessionID 是断点续传会话的唯一标识。
type UploadSessionID string

// ChunkID 是单个数据分片的唯一标识。
type ChunkID string

// NodeID 是存储节点的唯一标识。
type NodeID string

// FileStatus 描述文件在元数据层面的生命周期状态。
type FileStatus string

const (
	FileStatusPending   FileStatus = "pending"
	FileStatusUploading FileStatus = "uploading"
	FileStatusVerifying FileStatus = "verifying"
	FileStatusAvailable FileStatus = "available"
	FileStatusCorrupted FileStatus = "corrupted"
	FileStatusDeleting  FileStatus = "deleting"
	FileStatusDeleted   FileStatus = "deleted"
	FileStatusFailed    FileStatus = "failed"
)

// UploadStatus 描述上传会话当前的处理状态。
type UploadStatus string

const (
	UploadStatusPending   UploadStatus = "pending"
	UploadStatusActive    UploadStatus = "active"
	UploadStatusPaused    UploadStatus = "paused"
	UploadStatusRetrying  UploadStatus = "retrying"
	UploadStatusVerifying UploadStatus = "verifying"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusFailed    UploadStatus = "failed"
	UploadStatusExpired   UploadStatus = "expired"
)

// Checksum 保存可扩展的校验摘要信息。
type Checksum struct {
	Algorithm  string
	Value      string
	Verified   bool
	VerifiedAt *time.Time
}

// RetryState 记录续传与重传所需的失败上下文。
type RetryState struct {
	Attempt          int
	MaxAttempts      int
	Retryable        bool
	LastErrorCode    string
	LastErrorMessage string
	LastFailedOffset int64
	LastFailedChunk  ChunkID
	LastFailureAt    *time.Time
	NextRetryAt      *time.Time
}

// ReplicaPolicy 描述文件或 chunk 期望达到的副本策略。
type ReplicaPolicy struct {
	DesiredReplicaCount int
	MinimumReplicaCount int
	CurrentReplicaCount int
}

// UploadSession 记录断点续传的会话状态。
type UploadSession struct {
	ID                  UploadSessionID
	FileID              FileID
	UploadKey           string
	Status              UploadStatus
	ExpectedSize        int64
	ChunkSize           int64
	ConfirmedOffset     int64
	NextOffset          int64
	LastPersistedChunk  ChunkID
	ExpectedChecksum    *Checksum
	VerifiedChecksum    *Checksum
	Retry               RetryState
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ExpiresAt           *time.Time
	CompletedAt         *time.Time
	ClientMetadata      map[string]string
	TransportAttributes map[string]string
}

// NodePlacements 使用节点 ID 到节点放置信息的映射来描述文件分布。
// 这种结构便于快速判断某个文件当前落在哪些节点上，并能在单个节点维度扩展更多状态信息。
type NodePlacements map[NodeID]NodePlacement

// FileMetadata 描述一个文件在 MDS 中的核心元数据。
type FileMetadata struct {
	ID                    FileID
	Namespace             string
	InodeID               InodeID
	ParentInodeID         InodeID
	Path                  string
	Name                  string
	Size                  int64
	StoredSize            int64
	ChunkSize             int64
	Version               int64
	Status                FileStatus
	ContentType           string
	StorageClass          string
	PrimaryNodeID         NodeID
	SecondaryNodeIDs      []NodeID
	LatestUploadSessionID UploadSessionID
	Checksum              Checksum
	ReplicaPolicy         ReplicaPolicy
	UserMetadata          map[string]string
	Tags                  map[string]string
	NodePlacements        NodePlacements
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}
