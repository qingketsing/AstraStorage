package mq

import (
	"fmt"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/platform/mq/contracts"
)

func NewReplicaRepairTask(chunkID metadata.ChunkID, fileID metadata.FileID, sourceNodeID, targetNodeID metadata.NodeID) contracts.ReplicaRepairTask {
	return contracts.ReplicaRepairTask{
		PlanID:       fmt.Sprintf("repair-%s-%s", chunkID, targetNodeID),
		FileID:       string(fileID),
		ChunkID:      string(chunkID),
		SourceNodeID: string(sourceNodeID),
		TargetNodeID: string(targetNodeID),
	}
}

func NewCleanupTask(plan metadata.ReplicaPlan) contracts.CleanupTask {
	return contracts.CleanupTask{
		PlanID: plan.ID,
		FileID: string(plan.FileID),
		NodeID: string(plan.SourceNodeID),
		Reason: string(plan.Type),
	}
}

func NewRebalanceTask(plan metadata.ReplicaPlan) contracts.RebalanceTask {
	return contracts.RebalanceTask{
		PlanID:       plan.ID,
		SourceNodeID: string(plan.SourceNodeID),
		TargetNodeID: string(plan.TargetNodeID),
		Reason:       "rebalance_plan_materialized",
	}
}

func NewFailoverTask(plan metadata.ReplicaPlan) contracts.FailoverTask {
	return contracts.FailoverTask{
		PlanID:       plan.ID,
		NodeID:       string(plan.SourceNodeID),
		TargetNodeID: string(plan.TargetNodeID),
		Reason:       "failover_plan_materialized",
	}
}
