package metadata

import "time"

type ReplicaPlanType string

const (
	ReplicaPlanTypeFailover  ReplicaPlanType = "failover"
	ReplicaPlanTypeRebalance ReplicaPlanType = "rebalance"
	ReplicaPlanTypeCleanup   ReplicaPlanType = "cleanup"
)

type ReplicaPlanState string

const (
	ReplicaPlanStatePlanned      ReplicaPlanState = "planned"
	ReplicaPlanStateMaterialized ReplicaPlanState = "materialized"
	ReplicaPlanStateCopyReady    ReplicaPlanState = "copy_ready"
	ReplicaPlanStateCleanupReady ReplicaPlanState = "cleanup_pending"
	ReplicaPlanStateDone         ReplicaPlanState = "done"
	ReplicaPlanStateFailed       ReplicaPlanState = "failed"
)

type ReplicaPlan struct {
	ID               string
	Type             ReplicaPlanType
	ChunkID          ChunkID
	FileID           FileID
	SourceNodeID     NodeID
	TargetNodeID     NodeID
	RequiredBytes    int64
	State            ReplicaPlanState
	Priority         int
	LastErrorCode    string
	LastErrorMessage string
	RetryCount       int
	NextRetryAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}
