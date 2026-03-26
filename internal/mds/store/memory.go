package store

import (
	"errors"
	"sync"

	"AstraStorage/internal/mds/metadata"
)

var (
	// ErrNotFound 表示按主键或 selector 没有找到目标记录。
	ErrNotFound = errors.New("store: not found")
	// ErrAlreadyExists 表示创建操作命中了唯一性约束。
	ErrAlreadyExists = errors.New("store: already exists")
	// ErrInvalidArgument 表示调用方传入的参数不满足基本约束。
	ErrInvalidArgument = errors.New("store: invalid argument")
	// ErrConflict 表示对象当前状态与请求操作冲突。
	ErrConflict = errors.New("store: conflict")
)

// NewMemoryRepository builds an in-memory repository implementation for local development and tests.
func NewMemoryRepository() Repository {
	return &memoryRepository{
		state: memoryState{
			inodes:         make(map[metadata.InodeID]*metadata.InodeMetadata),
			files:          make(map[metadata.FileID]*metadata.FileMetadata),
			chunks:         make(map[metadata.ChunkID]*metadata.ChunkMetadata),
			uploadSessions: make(map[metadata.UploadSessionID]*metadata.UploadSession),
			nodes:          make(map[metadata.NodeID]*metadata.NodeInfo),
			replicaPlans:   make(map[string]*metadata.ReplicaPlan),
		},
	}
}

// memoryRepository 是 Repository 的内存实现。
// 它把所有数据都保存在内存里，并通过 RWMutex 提供并发安全。
type memoryRepository struct {
	mu    sync.RWMutex
	state memoryState
}

// memoryTx 表示一次基于快照的内存事务。
// BeginTx 时会复制一份完整状态，事务中的所有修改都发生在副本上；
// Commit 时再把整份副本覆盖回主仓库。
type memoryTx struct {
	repo   *memoryRepository
	state  memoryState
	closed bool
}

// memoryState 是内存仓储中的“当前真实状态”。
// 每个 map 都对应一种领域对象，作用上等价于一张逻辑表。
type memoryState struct {
	inodes         map[metadata.InodeID]*metadata.InodeMetadata
	files          map[metadata.FileID]*metadata.FileMetadata
	chunks         map[metadata.ChunkID]*metadata.ChunkMetadata
	uploadSessions map[metadata.UploadSessionID]*metadata.UploadSession
	nodes          map[metadata.NodeID]*metadata.NodeInfo
	replicaPlans   map[string]*metadata.ReplicaPlan
}
