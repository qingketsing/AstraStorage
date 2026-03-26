package contracts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEncodeDecodeEnvelope_RoundTripsTaskPayload(t *testing.T) {
	occurredAt := time.Now().UTC().Truncate(time.Second)
	payload := ReplicaRepairTask{
		PlanID:       "plan-1",
		FileID:       "file-1",
		ChunkID:      "chunk-1",
		SourceNodeID: "node-a",
		TargetNodeID: "node-b",
	}
	body, err := EncodeEnvelope(Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   TaskReplicaRepair,
		TraceID:    "trace-1",
		Attempt:    2,
		OccurredAt: occurredAt,
		Payload:    MustPayload(payload),
	})
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}

	var decoded Envelope
	if err := DecodeEnvelope(body, &decoded); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if decoded.MessageID != "msg-1" || decoded.EventID != "evt-1" || decoded.TaskType != TaskReplicaRepair || decoded.TraceID != "trace-1" {
		t.Fatalf("unexpected decoded envelope %#v", decoded)
	}
	if decoded.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", decoded.Attempt)
	}
	if !decoded.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected occurred_at %s", decoded.OccurredAt)
	}

	var decodedPayload ReplicaRepairTask
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decodedPayload.PlanID != "plan-1" || decodedPayload.TargetNodeID != "node-b" {
		t.Fatalf("unexpected decoded payload %#v", decodedPayload)
	}
}

func TestTaskPayloadTypes_ExposeStableTaskKinds(t *testing.T) {
	if (ReplicaRepairTask{}).Kind() != TaskReplicaRepair {
		t.Fatalf("unexpected repair task kind %q", (ReplicaRepairTask{}).Kind())
	}
	if (CleanupTask{}).Kind() != TaskCleanup {
		t.Fatalf("unexpected cleanup task kind %q", (CleanupTask{}).Kind())
	}
	if (RebalanceTask{}).Kind() != TaskRebalance {
		t.Fatalf("unexpected rebalance task kind %q", (RebalanceTask{}).Kind())
	}
	if (FailoverTask{}).Kind() != TaskFailover {
		t.Fatalf("unexpected failover task kind %q", (FailoverTask{}).Kind())
	}
}

func TestMustPayload_EncodesTaskBodies(t *testing.T) {
	raw := MustPayload(CleanupTask{
		PlanID: "cleanup-1",
		FileID: "file-1",
		NodeID: "node-a",
		Reason: "dangling replica",
	})

	var decoded CleanupTask
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal cleanup payload: %v", err)
	}
	if decoded.PlanID != "cleanup-1" || decoded.Reason != "dangling replica" {
		t.Fatalf("unexpected cleanup payload %#v", decoded)
	}
}
