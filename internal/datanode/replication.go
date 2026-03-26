package datanode

// ReplicaTarget 描述一次副本复制的目标节点。
type ReplicaTarget struct {
	NodeID  string `json:"node_id"`
	Address string `json:"address"`
}

// ReplicaWriteResult 描述一次副本复制尝试的结果。
type ReplicaWriteResult struct {
	NodeID  string `json:"node_id"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
	Address string `json:"address,omitempty"`
}

// ReplicateChunkRequest 描述 datanode 内部复制 RPC 的请求体。
type ReplicateChunkRequest struct {
	ChunkID string          `json:"chunk_id"`
	Targets []ReplicaTarget `json:"targets"`
}

// ReplicateChunkResponse 描述 datanode 内部复制 RPC 的返回结果。
type ReplicateChunkResponse struct {
	Chunk    *ChunkMetadata       `json:"chunk,omitempty"`
	Replicas []ReplicaWriteResult `json:"replicas,omitempty"`
}
