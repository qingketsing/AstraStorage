package contracts

import (
	"encoding/json"
	"time"
)

type TaskType string

const (
	TaskReplicaRepair TaskType = "replica.repair"
	TaskCleanup       TaskType = "cleanup"
	TaskRebalance     TaskType = "rebalance"
	TaskFailover      TaskType = "failover"
)

type Envelope struct {
	MessageID  string          `json:"message_id"`
	EventID    string          `json:"event_id"`
	TaskType   TaskType        `json:"task_type"`
	TraceID    string          `json:"trace_id"`
	Attempt    int             `json:"attempt"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}
