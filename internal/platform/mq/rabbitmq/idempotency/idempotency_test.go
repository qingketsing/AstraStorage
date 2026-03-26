package idempotency

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
)

func TestHandlerExecute_DetectsDuplicateTaskByIdempotencyKey(t *testing.T) {
	handler := NewHandler(NewMemoryStore(), 5*time.Minute)
	envelope := contracts.Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   contracts.TaskReplicaRepair,
		TraceID:    "trace-1",
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.ReplicaRepairTask{
			PlanID:       "plan-1",
			FileID:       "file-1",
			ChunkID:      "chunk-1",
			SourceNodeID: "node-1",
			TargetNodeID: "node-2",
		}),
	}

	executions := 0
	duplicate, err := handler.Execute(context.Background(), envelope, func(context.Context) error {
		executions++
		return nil
	})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if duplicate {
		t.Fatal("expected first execution not to be duplicate")
	}

	duplicate, err = handler.Execute(context.Background(), envelope, func(context.Context) error {
		executions++
		return nil
	})
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if !duplicate {
		t.Fatal("expected second execution to be detected as duplicate")
	}
	if executions != 1 {
		t.Fatalf("expected underlying handler to run once, got %d", executions)
	}
}

func TestKeyForEnvelope_PrefersStableIdentifiers(t *testing.T) {
	envelope := contracts.Envelope{
		MessageID: "msg-1",
		EventID:   "evt-1",
		TaskType:  contracts.TaskFailover,
		Payload: contracts.MustPayload(contracts.FailoverTask{
			PlanID:       "plan-1",
			NodeID:       "node-1",
			TargetNodeID: "node-3",
			Reason:       "failover",
		}),
	}

	key, err := KeyForEnvelope(envelope)
	if err != nil {
		t.Fatalf("key for envelope: %v", err)
	}
	if key != "evt-1" {
		t.Fatalf("expected event id key, got %q", key)
	}
}
