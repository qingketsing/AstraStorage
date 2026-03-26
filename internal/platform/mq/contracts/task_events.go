package contracts

type ReplicaRepairTask struct {
	PlanID       string `json:"plan_id"`
	FileID       string `json:"file_id"`
	ChunkID      string `json:"chunk_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
}

func (ReplicaRepairTask) Kind() TaskType { return TaskReplicaRepair }

type CleanupTask struct {
	PlanID string `json:"plan_id"`
	FileID string `json:"file_id"`
	NodeID string `json:"node_id"`
	Reason string `json:"reason"`
}

func (CleanupTask) Kind() TaskType { return TaskCleanup }

type RebalanceTask struct {
	PlanID       string `json:"plan_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	Reason       string `json:"reason"`
}

func (RebalanceTask) Kind() TaskType { return TaskRebalance }

type FailoverTask struct {
	PlanID       string `json:"plan_id"`
	NodeID       string `json:"node_id"`
	TargetNodeID string `json:"target_node_id"`
	Reason       string `json:"reason"`
}

func (FailoverTask) Kind() TaskType { return TaskFailover }
