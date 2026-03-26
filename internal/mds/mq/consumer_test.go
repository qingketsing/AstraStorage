package mq_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"AstraStorage/internal/mds/coordinator"
	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/idempotency"
)

func TestRepairConsumer_HandleAcksOnSuccess(t *testing.T) {
	repo := newFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStatePending},
	}, "node-1", "node-2")
	repairer, err := coordinator.NewPendingReplicaRepairer(repo, coordinator.PendingReplicaRepairerConfig{
		Interval:          time.Second,
		HTTPTimeout:       time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}
	replaceHTTPClient(t, repairer, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{{"node_id": "node-2", "state": "ready", "address": "http://node-2.local"}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	consumer := mdsmq.NewRepairConsumer(repairer)
	delivery := &fakeDelivery{payload: mustEnvelope(t, contracts.TaskReplicaRepair, contracts.ReplicaRepairTask{
		PlanID:       "repair-chunk-1-node-2",
		FileID:       "file-1",
		ChunkID:      "chunk-1",
		SourceNodeID: "node-1",
		TargetNodeID: "node-2",
	})}

	if err := consumer.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("handle repair: %v", err)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected delivery ack once, got %d", delivery.ackCount)
	}
	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if chunk.Replicas["node-2"].State != metadata.ReplicaStateReady {
		t.Fatalf("expected node-2 ready after repair, got %#v", chunk.Replicas["node-2"])
	}
}

func TestCleanupConsumer_HandleAcksOnSuccess(t *testing.T) {
	now := time.Now().UTC()
	repo := newFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-3": {NodeID: "node-3", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
	}, "node-1", "node-2", "node-3")
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "rebalance-plan",
		Type:          metadata.ReplicaPlanTypeRebalance,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateCleanupReady,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	controller := coordinator.NewCleanupController(repo, coordinator.CleanupControllerConfig{
		HTTPTimeout: time.Second,
	})
	replaceCleanupHTTPClient(t, controller, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	consumer := mdsmq.NewCleanupConsumer(controller)
	delivery := &fakeDelivery{payload: mustEnvelope(t, contracts.TaskCleanup, contracts.CleanupTask{
		PlanID: "rebalance-plan",
		FileID: "file-1",
		NodeID: "node-1",
		Reason: "rebalance",
	})}
	if err := consumer.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("handle cleanup: %v", err)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected delivery ack once, got %d", delivery.ackCount)
	}
	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if _, ok := chunk.Replicas["node-1"]; ok {
		t.Fatalf("expected source replica removed after cleanup")
	}
}

func TestRebalanceConsumer_HandleAcksOnSuccess(t *testing.T) {
	now := time.Now().UTC()
	repo := newFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-3": {NodeID: "node-3", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStatePending},
	}, "node-1", "node-2", "node-3")
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "rebalance-plan",
		Type:          metadata.ReplicaPlanTypeRebalance,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	repairer, err := coordinator.NewPendingReplicaRepairer(repo, coordinator.PendingReplicaRepairerConfig{
		Interval:          time.Second,
		HTTPTimeout:       time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}
	replaceHTTPClient(t, repairer, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{{"node_id": "node-3", "state": "ready", "address": "http://node-3.local"}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	consumer := mdsmq.NewRebalanceConsumer(repairer)
	delivery := &fakeDelivery{payload: mustEnvelope(t, contracts.TaskRebalance, contracts.RebalanceTask{
		PlanID:       "rebalance-plan",
		SourceNodeID: "node-1",
		TargetNodeID: "node-3",
		Reason:       "rebalance_plan_materialized",
	})}
	if err := consumer.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("handle rebalance: %v", err)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected delivery ack once, got %d", delivery.ackCount)
	}
	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if chunk.Replicas["node-3"].State != metadata.ReplicaStateReady {
		t.Fatalf("expected rebalance target ready, got %#v", chunk.Replicas["node-3"])
	}
	plan, err := repo.GetReplicaPlan(context.Background(), "rebalance-plan")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if plan.State != metadata.ReplicaPlanStateCleanupReady {
		t.Fatalf("expected cleanup ready plan, got %q", plan.State)
	}
}

func TestFailoverConsumer_HandleAcksOnSuccess(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-10 * time.Minute)
	repo := newFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-3": {NodeID: "node-3", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStatePending},
	}, "node-1", "node-2", "node-3")
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-1",
		Healthy:    false,
		Capacity:   1024,
		Used:       0,
		LastSeenAt: stale,
	}); err != nil {
		t.Fatalf("update stale node: %v", err)
	}
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "failover-plan",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	repairer, err := coordinator.NewPendingReplicaRepairer(repo, coordinator.PendingReplicaRepairerConfig{
		Interval:          time.Second,
		HTTPTimeout:       time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	})
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}
	replaceHTTPClient(t, repairer, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://node-2.local/internal/replicate" {
			t.Fatalf("unexpected source request url: %s", req.URL.String())
		}
		payload, _ := json.Marshal(map[string]any{
			"replicas": []map[string]any{{"node_id": "node-3", "state": "ready", "address": "http://node-3.local"}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	consumer := mdsmq.NewFailoverConsumer(repairer)
	delivery := &fakeDelivery{payload: mustEnvelope(t, contracts.TaskFailover, contracts.FailoverTask{
		PlanID:       "failover-plan",
		NodeID:       "node-1",
		TargetNodeID: "node-3",
		Reason:       "failover_plan_materialized",
	})}
	if err := consumer.Handle(context.Background(), delivery); err != nil {
		t.Fatalf("handle failover: %v", err)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected delivery ack once, got %d", delivery.ackCount)
	}
	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if chunk.Replicas["node-3"].State != metadata.ReplicaStateReady {
		t.Fatalf("expected failover target ready, got %#v", chunk.Replicas["node-3"])
	}
}

func TestRepairConsumer_HandleSkipsDuplicateTaskWithIdempotency(t *testing.T) {
	executor := &countingRepairExecutor{}
	consumer := mdsmq.NewRepairConsumer(executor)
	consumer.SetIdempotencyHandler(idempotency.NewHandler(idempotency.NewMemoryStore(), time.Minute))
	body := mustEnvelope(t, contracts.TaskReplicaRepair, contracts.ReplicaRepairTask{
		PlanID:       "plan-1",
		FileID:       "file-1",
		ChunkID:      "chunk-1",
		SourceNodeID: "node-1",
		TargetNodeID: "node-2",
	})

	first := &fakeDelivery{payload: body}
	if err := consumer.Handle(context.Background(), first); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	second := &fakeDelivery{payload: body}
	if err := consumer.Handle(context.Background(), second); err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("expected executor to run once, got %d", executor.calls)
	}
	if first.ackCount != 1 || second.ackCount != 1 {
		t.Fatalf("expected both deliveries acked, got first=%d second=%d", first.ackCount, second.ackCount)
	}
}

type fakeDelivery struct {
	payload  []byte
	ackCount int
}

type countingRepairExecutor struct {
	calls int
}

func (c *countingRepairExecutor) ExecuteReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error {
	c.calls++
	return nil
}

func (f *fakeDelivery) Body() []byte {
	return f.payload
}

func (f *fakeDelivery) Ack(multiple bool) error {
	f.ackCount++
	return nil
}

func (f *fakeDelivery) Nack(multiple, requeue bool) error {
	return nil
}

func mustEnvelope(t *testing.T, kind contracts.TaskType, payload any) []byte {
	t.Helper()
	body, err := contracts.EncodeEnvelope(contracts.Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   kind,
		TraceID:    "trace-1",
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload:    contracts.MustPayload(payload),
	})
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return body
}

func newFixtureRepository(t *testing.T, replicas metadata.ReplicaSet, nodeIDs ...metadata.NodeID) store.Repository {
	t.Helper()
	repo := store.NewMemoryRepository()
	now := time.Now().UTC()

	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root inode: %v", err)
	}
	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        "inode-1",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "demo.bin",
		Path:      "/demo.bin",
		Type:      metadata.InodeTypeFile,
		Status:    metadata.InodeStatusActive,
		FileID:    "file-1",
		LinkCount: 1,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file inode: %v", err)
	}
	if err := repo.CreateFile(context.Background(), &metadata.FileMetadata{
		ID:            "file-1",
		InodeID:       "inode-1",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Path:          "/demo.bin",
		Name:          "demo.bin",
		Size:          16,
		StoredSize:    16,
		ChunkSize:     metadata.FixedChunkSizeBytes,
		Status:        metadata.FileStatusAvailable,
		ReplicaPolicy: metadata.ReplicaPolicy{
			DesiredReplicaCount: metadata.DefaultReplicaCount,
			MinimumReplicaCount: metadata.MinimumReadableReplicaCount,
			CurrentReplicaCount: len(replicas),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	checksum := metadata.Checksum{Algorithm: "sha256", Value: "abc", Verified: true, VerifiedAt: &now}
	for nodeID, replica := range replicas {
		replica.NodeID = nodeID
		replica.FileID = "file-1"
		replica.ChunkID = "chunk-1"
		replica.Checksum = checksum
		replica.UpdatedAt = now
		if replica.CreatedAt.IsZero() {
			replica.CreatedAt = now
		}
		replicas[nodeID] = replica
	}
	if err := repo.UpsertChunks(context.Background(), []metadata.ChunkMetadata{{
		ID:           "chunk-1",
		FileID:       "file-1",
		Index:        0,
		Offset:       0,
		Size:         16,
		Status:       metadata.ChunkStatusPersisted,
		Checksum:     checksum,
		Replicas:     replicas,
		ReplicaCount: len(replicas),
		CreatedAt:    now,
		UpdatedAt:    now,
	}}); err != nil {
		t.Fatalf("upsert chunk: %v", err)
	}
	for _, nodeID := range nodeIDs {
		lastSeen := now
		if err := repo.UpsertNode(context.Background(), metadata.NodeInfo{
			ID:         nodeID,
			Address:    "http://" + string(nodeID) + ".local",
			Capacity:   1024,
			Used:       0,
			Healthy:    true,
			LastSeenAt: &lastSeen,
			UpdatedAt:  now,
		}); err != nil {
			t.Fatalf("upsert node %s: %v", nodeID, err)
		}
	}
	return repo
}

func replaceHTTPClient(t *testing.T, repairer *coordinator.PendingReplicaRepairer, client *http.Client) {
	t.Helper()
	setUnexportedField(t, repairer, "httpClient", client)
}

func replaceCleanupHTTPClient(t *testing.T, controller *coordinator.CleanupController, client *http.Client) {
	t.Helper()
	setUnexportedField(t, controller, "httpClient", client)
}

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		t.Fatalf("target must be a non-nil pointer")
	}
	field := rv.Elem().FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("field %s not found", fieldName)
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
