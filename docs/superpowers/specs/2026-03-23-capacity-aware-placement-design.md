# Capacity-Aware Placement Design

## Goal

Introduce the next control-plane step after leader election:

- datanode reports real `used` bytes instead of a fixed placeholder
- MDS upload target allocation becomes capacity-aware
- MDS repair target selection applies the same minimum capacity rules

This phase is intentionally limited to the smallest useful scheduling improvement. It is not a full scheduler, and it does not include rebalance, failover, rack-awareness, or queue-driven orchestration.

## Scope

This change affects two existing decision paths:

1. `AllocateUploadTargets`
2. pending replica `repairer` target filtering

This change does not implement:

- rebalance
- failover
- zone / rack / region spread
- dynamic task ownership beyond the leader election already completed
- Redis / RabbitMQ integration

## Current State

### Datanode

`datanode` currently sends:

- configured `capacity`
- fixed `used=0` during heartbeat

That means node usage information stored in MDS is not trustworthy enough to drive placement.

### MDS

`AllocateUploadTargets` currently:

- lists healthy nodes
- takes the first `N`

`repairer` currently:

- identifies pending replicas
- checks source and target node metadata
- does not apply a real shared allocator policy

### Architecture consequence

The system has control-plane single leadership already, but node selection is still only health-based. That is enough for a chain demo, but not enough for a storage-engine-quality control plane.

## Design Summary

Use one shared minimal allocator policy for both upload target allocation and repair target filtering.

The policy is:

1. only consider healthy nodes
2. only consider nodes with non-empty advertised address
3. only consider nodes where `capacity >= used`
4. only consider nodes where `available = capacity - used` is greater than zero
5. sort by `available` descending
6. tie-break by `node_id` ascending for deterministic results

This is the smallest policy that:

- stops obviously bad placements
- uses real node resource signals
- stays easy to reason about
- gives upload and repair the same baseline semantics

## Component Design

### 1. Datanode real usage reporting

Add a real usage API to the filesystem-backed store:

- `UsageBytes() (int64, error)`

Behavior:

- scan the `chunks/` directory
- sum file sizes for persisted chunk payload files and sidecar metadata files
- return the total byte usage on disk

Rationale:

- no in-memory usage counter to drift from disk state
- restart-safe
- faithful to the current local-filesystem prototype
- simple enough for the current stage

Usage integration:

- node registration should include real `used`
- heartbeat should include real `used`

This means MDS will receive a truthful byte count from each datanode over time.

### 2. Shared allocator in MDS

Move node selection logic into `internal/mds/allocator.go`.

The allocator should remain intentionally small. It does not need to become a large subsystem in this iteration.

Suggested responsibilities:

- filter candidate nodes
- sort candidate nodes by available capacity
- apply optional exclusion set
- return first `N` selected nodes

Suggested shape:

```go
type NodeSelectionInput struct {
    Candidates []metadata.NodeInfo
    Excluded   map[metadata.NodeID]struct{}
    Count      int
}

func SelectCapacityAwareNodes(input NodeSelectionInput) []metadata.NodeInfo
```

This function should stay pure and deterministic so it is easy to test.

### 3. Upload target allocation

`Service.AllocateUploadTargets` should:

1. validate request
2. load file metadata
3. list healthy nodes
4. call the allocator
5. map selected nodes to `UploadTarget`

This removes the existing “first healthy nodes” behavior and centralizes selection logic.

### 4. Repair target filtering

This iteration should not redesign the repair loop into a general scheduler.

Instead:

- keep the existing pending-replica flow
- before invoking replication, filter pending targets through the same minimum-capacity rules

This keeps the repairer consistent with upload placement without turning this change into a broader control-plane rewrite.

If all pending targets fail the capacity filter:

- do not replicate
- keep the replica deferred / pending

## Data Flow

### Upload path

1. datanode writes chunks locally
2. datanode computes real `used` bytes from store state
3. datanode sends `capacity` and `used` in register / heartbeat
4. MDS persists node resource state
5. gateway asks MDS for upload targets
6. MDS allocator returns the highest-available valid nodes

### Repair path

1. leader-scoped repairer scans pending replicas
2. repairer identifies source and pending target nodes
3. repairer loads node resource metadata
4. repairer filters target nodes through the shared capacity rule
5. repair proceeds only for valid targets

## Error Handling

### Datanode usage calculation

If `UsageBytes()` fails:

- registration / heartbeat should fail rather than silently sending incorrect usage

Reason:

- fake capacity data is worse than a visible failure for this stage

### MDS allocation

If no capacity-valid nodes remain after filtering:

- return conflict-style allocation failure

### Repairer

If no pending targets remain after filtering:

- do not treat this as a successful repair
- keep work deferred for later retry

## Testing Strategy

### Unit tests

#### Datanode store

- `UsageBytes()` returns zero for empty store
- `UsageBytes()` includes both `.bin` and `.json`
- `UsageBytes()` changes after put/delete

#### MDS allocator

- excludes unhealthy nodes
- excludes nodes with empty address
- excludes nodes with invalid capacity state
- sorts by available capacity descending
- tie-breaks deterministically by node id
- respects excluded node set

### Service tests

- `AllocateUploadTargets` no longer returns first healthy nodes blindly
- it prefers nodes with more available capacity

### Repair tests

- repair skips pending targets that fail minimum capacity requirements
- repair still works when at least one pending target remains valid

## Observability

No new metrics are strictly required for the first iteration.

Existing node registration, heartbeat, allocation, and repair metrics are enough to validate the change.

If needed, a follow-up can add allocator-specific decision counters, but that is not required for this phase.

## Tradeoffs

### Why not just patch `service_node.go` and `repairer.go` independently?

Because upload and repair would immediately diverge in semantics, and the project would reintroduce control-plane inconsistency right after introducing leader election.

### Why not build full scheduler logic now?

Because that would blur the boundary between:

- resource truthfulness
- minimal placement policy
- full background orchestration

The right next step is to make node selection no longer naive, not to solve every scheduler problem in one iteration.

### Why not maintain a live in-memory usage counter on datanode?

Because the current storage backend is filesystem-based, and restart-safe truth is more valuable than fast but drift-prone counters.

## Out of Scope Follow-Ups

After this phase, the next scheduling-related steps should be:

1. chunk-size-aware minimum free space checks
2. zone / rack / region constraints
3. rebalance
4. failover
5. cleanup / orphan handling integration

## Acceptance Criteria

This phase is complete when:

1. datanode sends real `used` bytes on register and heartbeat
2. MDS upload target selection is capacity-aware
3. repair target filtering follows the same minimum-capacity rule
4. tests cover both allocator behavior and datanode usage reporting
5. `go test ./...` and `go build ./...` pass
