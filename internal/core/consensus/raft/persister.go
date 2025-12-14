package raft

import "sync"

// InMemoryPersister 内存持久化器，用于测试，避免与测试用的 MemoryPersister 重名
type InMemoryPersister struct {
	mu        sync.Mutex
	raftstate []byte
	snapshot  []byte
}

// NewInMemoryPersister 创建一个新的内存持久化器
func NewInMemoryPersister() *InMemoryPersister {
	return &InMemoryPersister{}
}

func (p *InMemoryPersister) Copy() *InMemoryPersister {
	p.mu.Lock()
	defer p.mu.Unlock()
	np := NewInMemoryPersister()
	np.raftstate = copyBytes(p.raftstate)
	np.snapshot = copyBytes(p.snapshot)
	return np
}

func (p *InMemoryPersister) Save(raftstate []byte, snapshot []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raftstate = copyBytes(raftstate)
	p.snapshot = copyBytes(snapshot)
}

func (p *InMemoryPersister) ReadRaftState() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return copyBytes(p.raftstate)
}

func (p *InMemoryPersister) ReadSnapshot() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return copyBytes(p.snapshot)
}

func (p *InMemoryPersister) RaftStateSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.raftstate)
}

func copyBytes(orig []byte) []byte {
	if orig == nil {
		return nil
	}
	x := make([]byte, len(orig))
	copy(x, orig)
	return x
}
