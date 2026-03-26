package mds

import (
	"testing"

	"AstraStorage/internal/mds/metadata"
)

func TestSelectCapacityAwareNodes_FiltersInvalidCandidates(t *testing.T) {
	selected := SelectCapacityAwareNodes(NodeSelectionInput{
		Candidates: []metadata.NodeInfo{
			{ID: "node-unhealthy", Address: "http://node-unhealthy.local", Healthy: false, Capacity: 100, Used: 10},
			{ID: "node-no-address", Healthy: true, Capacity: 100, Used: 10},
			{ID: "node-overused", Address: "http://node-overused.local", Healthy: true, Capacity: 100, Used: 101},
			{ID: "node-full", Address: "http://node-full.local", Healthy: true, Capacity: 100, Used: 100},
			{ID: "node-valid", Address: "http://node-valid.local", Healthy: true, Capacity: 100, Used: 40},
		},
		Count: 3,
	})

	if len(selected) != 1 {
		t.Fatalf("expected exactly one valid candidate, got %d", len(selected))
	}
	if selected[0].ID != "node-valid" {
		t.Fatalf("expected node-valid to remain, got %q", selected[0].ID)
	}
}

func TestSelectCapacityAwareNodes_SortsByAvailableCapacity(t *testing.T) {
	selected := SelectCapacityAwareNodes(NodeSelectionInput{
		Candidates: []metadata.NodeInfo{
			{ID: "node-b", Address: "http://node-b.local", Healthy: true, Capacity: 1000, Used: 300},
			{ID: "node-a", Address: "http://node-a.local", Healthy: true, Capacity: 1000, Used: 100},
			{ID: "node-c", Address: "http://node-c.local", Healthy: true, Capacity: 1000, Used: 300},
		},
		Count: 3,
	})

	if len(selected) != 3 {
		t.Fatalf("expected 3 selected nodes, got %d", len(selected))
	}
	if selected[0].ID != "node-a" {
		t.Fatalf("expected highest-available node first, got %q", selected[0].ID)
	}
	if selected[1].ID != "node-b" || selected[2].ID != "node-c" {
		t.Fatalf("expected tie to break by node id, got [%q %q]", selected[1].ID, selected[2].ID)
	}
}

func TestSelectCapacityAwareNodes_RespectsExcludedNodes(t *testing.T) {
	selected := SelectCapacityAwareNodes(NodeSelectionInput{
		Candidates: []metadata.NodeInfo{
			{ID: "node-1", Address: "http://node-1.local", Healthy: true, Capacity: 1000, Used: 100},
			{ID: "node-2", Address: "http://node-2.local", Healthy: true, Capacity: 1000, Used: 200},
		},
		Excluded: map[metadata.NodeID]struct{}{
			"node-1": {},
		},
		Count: 2,
	})

	if len(selected) != 1 {
		t.Fatalf("expected one remaining node, got %d", len(selected))
	}
	if selected[0].ID != "node-2" {
		t.Fatalf("expected node-2 to remain after exclusion, got %q", selected[0].ID)
	}
}

func TestRequiredPlacementBytes_PrefersReadyReplicaStoredSize(t *testing.T) {
	chunk := metadata.ChunkMetadata{
		ID:   "chunk-1",
		Size: 1024,
		Replicas: metadata.ReplicaSet{
			"node-a": {
				NodeID:     "node-a",
				State:      metadata.ReplicaStatePending,
				StoredSize: 2048,
			},
			"node-b": {
				NodeID:     "node-b",
				State:      metadata.ReplicaStateReady,
				StoredSize: 4096,
			},
		},
	}

	if got := RequiredPlacementBytes(chunk); got != 4096 {
		t.Fatalf("expected required bytes 4096, got %d", got)
	}
}

func TestSelectPlacementTargets_RejectsNodesWithoutEnoughRequiredBytes(t *testing.T) {
	selected := SelectPlacementTargets(PlacementRequest{
		Candidates: []metadata.NodeInfo{
			{ID: "node-small", Address: "http://node-small.local", Healthy: true, Capacity: 1000, Used: 600},
			{ID: "node-fit", Address: "http://node-fit.local", Healthy: true, Capacity: 1000, Used: 300},
		},
		RequiredBytes: 500,
		Count:         2,
	})

	if len(selected) != 1 {
		t.Fatalf("expected one selected node, got %d", len(selected))
	}
	if selected[0].ID != "node-fit" {
		t.Fatalf("expected node-fit to remain, got %q", selected[0].ID)
	}
}

func TestCountEffectiveReadyReplicas_CountsOnlyHealthyReadyReplicas(t *testing.T) {
	chunk := metadata.ChunkMetadata{
		Replicas: metadata.ReplicaSet{
			"node-a": {NodeID: "node-a", State: metadata.ReplicaStateReady},
			"node-b": {NodeID: "node-b", State: metadata.ReplicaStatePending},
			"node-c": {NodeID: "node-c", State: metadata.ReplicaStateReady},
		},
	}
	nodeIndex := map[metadata.NodeID]metadata.NodeInfo{
		"node-a": {ID: "node-a", Healthy: true},
		"node-b": {ID: "node-b", Healthy: true},
		"node-c": {ID: "node-c", Healthy: false},
	}

	if got := CountEffectiveReadyReplicas(chunk, nodeIndex); got != 1 {
		t.Fatalf("expected 1 effective ready replica, got %d", got)
	}
}

func TestBuildReplicaExclusionSet_CoversExistingReplicaNodes(t *testing.T) {
	chunk := metadata.ChunkMetadata{
		Replicas: metadata.ReplicaSet{
			"node-a": {NodeID: "node-a"},
			"node-b": {NodeID: "node-b"},
		},
	}

	excluded := BuildReplicaExclusionSet(chunk)
	if len(excluded) != 2 {
		t.Fatalf("expected two excluded nodes, got %d", len(excluded))
	}
	if _, ok := excluded["node-a"]; !ok {
		t.Fatalf("expected node-a to be excluded")
	}
	if _, ok := excluded["node-b"]; !ok {
		t.Fatalf("expected node-b to be excluded")
	}
}
