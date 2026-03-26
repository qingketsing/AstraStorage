// replica.go
// Metadata Service 中副本元信息模型定义文件。
// 该文件预留用于定义数据副本的描述结构，
// 包括副本编号、所在节点、同步状态、健康状态以及主从角色等信息。

package metadata

import "time"

// ReplicaRole 描述副本在一组拷贝中的职责。
type ReplicaRole string

const (
	ReplicaRolePrimary   ReplicaRole = "primary"
	ReplicaRoleSecondary ReplicaRole = "secondary"
	ReplicaRoleWitness   ReplicaRole = "witness"
)

// ReplicaState 描述副本当前的同步与健康状态。
type ReplicaState string

const (
	ReplicaStatePending   ReplicaState = "pending"
	ReplicaStateWriting   ReplicaState = "writing"
	ReplicaStateReady     ReplicaState = "ready"
	ReplicaStateLagging   ReplicaState = "lagging"
	ReplicaStateCorrupted ReplicaState = "corrupted"
	ReplicaStateLost      ReplicaState = "lost"
)

// NodeInfo 描述一个存储节点的基础信息。
type NodeInfo struct {
	ID         NodeID
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

// ReplicaMetadata 描述 chunk 在某个节点上的一份副本。
type ReplicaMetadata struct {
	ID         string
	FileID     FileID
	ChunkID    ChunkID
	NodeID     NodeID
	Role       ReplicaRole
	State      ReplicaState
	Checksum   Checksum
	StoredSize int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	VerifiedAt *time.Time
}

// ReplicaSet 使用节点 ID 建立副本集合，便于按节点快速定位。
type ReplicaSet map[NodeID]ReplicaMetadata

// NodePlacement 描述一个文件在单个节点上的整体放置信息。
type NodePlacement struct {
	Node          NodeInfo
	ReplicaRole   ReplicaRole
	ReplicaState  ReplicaState
	IsPrimary     bool
	ChunkIDs      []ChunkID
	StoredSize    int64
	ChecksumState string
	LastSyncAt    *time.Time
}
