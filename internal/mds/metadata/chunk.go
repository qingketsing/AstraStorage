// chunk.go
// Metadata Service 中数据块元信息模型定义文件。
// 该文件预留用于描述对象或文件被切分后的 chunk 元数据结构，
// 包括块标识、大小、版本、副本位置及其与 inode 或对象之间的映射关系。

package metadata

import "time"

// ChunkStatus 描述 chunk 在写入与校验流程中的状态。
type ChunkStatus string

const (
	ChunkStatusPending   ChunkStatus = "pending"
	ChunkStatusWriting   ChunkStatus = "writing"
	ChunkStatusPersisted ChunkStatus = "persisted"
	ChunkStatusVerifying ChunkStatus = "verifying"
	ChunkStatusAvailable ChunkStatus = "available"
	ChunkStatusCorrupted ChunkStatus = "corrupted"
	ChunkStatusFailed    ChunkStatus = "failed"
)

// ChunkMetadata 描述文件切片后的单个数据块元信息。
type ChunkMetadata struct {
	ID            ChunkID
	FileID        FileID
	Index         int64
	Offset        int64
	Size          int64
	Status        ChunkStatus
	Version       int64
	Checksum      Checksum
	ReplicaPolicy ReplicaPolicy
	ReplicaCount  int
	Replicas      ReplicaSet
	CreatedAt     time.Time
	UpdatedAt     time.Time
	VerifiedAt    *time.Time
	LastErrorCode string
}
