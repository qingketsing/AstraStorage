# Capacity-Aware Placement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make datanode report real disk usage and make MDS upload allocation plus repair target filtering use one shared capacity-aware selection rule.

**Architecture:** Keep the current filesystem-backed datanode store and add one restart-safe `UsageBytes()` API that scans real on-disk chunk files. Move node filtering and ordering into a small pure allocator in `internal/mds/allocator.go`, then route both upload target allocation and repair target filtering through that same minimum-capacity policy.

**Tech Stack:** Go 1.25, current filesystem datanode store, existing MDS service/repairer, Go `testing`, current observability and integration test setup

---

## File Map

### New files

- `internal/mds/allocator.go`
- `internal/mds/allocator_test.go`

### Modified files

- `cmd/datanode/app.go`
- `cmd/datanode/app_test.go`
- `internal/datanode/store.go`
- `internal/datanode/store_test.go`
- `internal/mds/service_node.go`
- `internal/mds/service_node_test.go`
- `internal/mds/coordinator/repairer.go`
- `internal/mds/coordinator/repairer_test.go`
- `docs/architecture/manual-testing.md`
- `docs/architecture/technical-debt-roadmap.md`

### Responsibility split

- `internal/datanode/store.go`: expose real byte usage from the filesystem-backed chunk store
- `cmd/datanode/app.go`: send real `used` bytes during register and heartbeat
- `internal/mds/allocator.go`: pure deterministic capacity-aware node selection
- `internal/mds/service_node.go`: use allocator for upload target allocation
- `internal/mds/coordinator/repairer.go`: reuse the same minimum-capacity rule when selecting pending repair targets

## Global Constraints

- Do not introduce a full scheduler, rebalance, or failover in this change.
- Do not add random heuristics or post-hoc fallback logic.
- Keep allocator behavior deterministic and fully testable.
- Use real on-disk byte usage, not an in-memory counter.
- Keep placement semantics aligned between upload allocation and repair filtering.

## Task 1: Add Real Datanode Usage Accounting

**Files:**
- Modify: `internal/datanode/store.go`
- Modify: `internal/datanode/store_test.go`

- [ ] **Step 1: Write failing store usage tests**

Add tests that prove:

- empty store reports `0`
- `UsageBytes()` includes both `.bin` and `.json` files
- `UsageBytes()` changes after `PutChunk`
- `UsageBytes()` decreases after `DeleteChunk`

Suggested test skeletons:

```go
func TestStore_UsageBytes_EmptyStore(t *testing.T) {}

func TestStore_UsageBytes_IncludesChunkDataAndMetadata(t *testing.T) {}

func TestStore_UsageBytes_DecreasesAfterDelete(t *testing.T) {}
```

- [ ] **Step 2: Run targeted store tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/datanode -run 'TestStore_UsageBytes' -v
```

Expected:

- FAIL because `UsageBytes()` does not exist yet

- [ ] **Step 3: Implement `UsageBytes()` minimally**

Implementation requirements:

- add `UsageBytes() (int64, error)` to `Store`
- scan the `chunks` directory
- sum sizes of persisted files only
- return restart-safe byte usage from real filesystem state

- [ ] **Step 4: Re-run targeted store tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/datanode -run 'TestStore_UsageBytes' -v
```

Expected:

- PASS for all new usage tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/datanode/store.go internal/datanode/store_test.go
git commit -m "datanode: add real usage accounting"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 2: Send Real Used Bytes in Register and Heartbeat

**Files:**
- Modify: `cmd/datanode/app.go`
- Modify: `cmd/datanode/app_test.go`

- [ ] **Step 1: Write failing datanode app tests**

Add tests that prove:

- registration sends real `used` bytes from the store
- heartbeat sends real `used` bytes from the store
- usage read failure causes register or heartbeat path to fail instead of silently sending `0`

Suggested test skeletons:

```go
func TestApplication_Run_RegisterNodeSendsRealUsedBytes(t *testing.T) {}

func TestApplication_RunHeartbeatLoop_SendsRealUsedBytes(t *testing.T) {}

func TestApplication_Run_RegisterFailsWhenUsageReadFails(t *testing.T) {}
```

- [ ] **Step 2: Run targeted app tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/datanode -run 'TestApplication_Run_(RegisterNodeSendsRealUsedBytes|HeartbeatLoop_SendsRealUsedBytes|RegisterFailsWhenUsageReadFails)' -v
```

Expected:

- FAIL because the app still sends configured capacity with `used=0`

- [ ] **Step 3: Implement real usage reporting in `cmd/datanode`**

Implementation requirements:

- read store usage before initial `RegisterNode`
- read store usage on each heartbeat tick
- fail registration when usage cannot be read
- skip silent fallback to placeholder values

- [ ] **Step 4: Re-run targeted app tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/datanode -run 'TestApplication_Run_(RegisterNodeSendsRealUsedBytes|HeartbeatLoop_SendsRealUsedBytes|RegisterFailsWhenUsageReadFails)' -v
```

Expected:

- PASS for new datanode usage-reporting tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add cmd/datanode/app.go cmd/datanode/app_test.go
git commit -m "datanode: report real used bytes"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 3: Introduce Shared Capacity-Aware Allocator

**Files:**
- Create: `internal/mds/allocator.go`
- Create: `internal/mds/allocator_test.go`

- [ ] **Step 1: Write failing allocator tests**

Add tests that prove:

- unhealthy nodes are excluded
- nodes with empty address are excluded
- nodes with invalid `capacity/used` are excluded
- nodes with zero available bytes are excluded
- remaining nodes are ordered by available capacity descending
- ties are ordered by `node_id`
- excluded node ids are respected

Suggested test skeletons:

```go
func TestSelectCapacityAwareNodes_FiltersInvalidCandidates(t *testing.T) {}

func TestSelectCapacityAwareNodes_SortsByAvailableCapacity(t *testing.T) {}

func TestSelectCapacityAwareNodes_RespectsExcludedNodes(t *testing.T) {}
```

- [ ] **Step 2: Run allocator tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds -run 'TestSelectCapacityAwareNodes' -v
```

Expected:

- FAIL because allocator implementation does not exist yet

- [ ] **Step 3: Implement minimal pure allocator**

Implementation requirements:

- define a small input struct for candidates, excluded nodes, and target count
- filter on:
  - `Healthy == true`
  - non-empty address
  - `capacity >= used`
  - `available > 0`
- sort by `available` descending, then `node_id` ascending
- return first `Count` candidates

- [ ] **Step 4: Re-run allocator tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds -run 'TestSelectCapacityAwareNodes' -v
```

Expected:

- PASS for allocator tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/allocator.go internal/mds/allocator_test.go
git commit -m "mds: add capacity-aware allocator"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 4: Route Upload Target Allocation Through the Allocator

**Files:**
- Modify: `internal/mds/service_node.go`
- Modify: `internal/mds/service_node_test.go`

- [ ] **Step 1: Write failing allocation tests**

Add tests that prove:

- `AllocateUploadTargets` prefers nodes with higher available capacity
- nodes with zero available capacity are not returned
- deterministic ordering is preserved when availability ties

Suggested test skeletons:

```go
func TestService_AllocateUploadTargets_PrefersHigherAvailableCapacity(t *testing.T) {}

func TestService_AllocateUploadTargets_SkipsFullNodes(t *testing.T) {}
```

- [ ] **Step 2: Run targeted service tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds -run 'TestService_AllocateUploadTargets' -v
```

Expected:

- FAIL because service still selects the first healthy nodes

- [ ] **Step 3: Replace naive node slicing with allocator usage**

Implementation requirements:

- keep existing request validation and file lookup
- list healthy nodes as before
- call the shared allocator
- map selected nodes to existing `UploadTarget` response shape
- preserve conflict error behavior when no valid target remains

- [ ] **Step 4: Re-run targeted service tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds -run 'TestService_AllocateUploadTargets' -v
```

Expected:

- PASS for new allocation tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/service_node.go internal/mds/service_node_test.go
git commit -m "mds: use allocator for upload targets"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 5: Apply Capacity Filtering to Repair Targets

**Files:**
- Modify: `internal/mds/coordinator/repairer.go`
- Modify: `internal/mds/coordinator/repairer_test.go`

- [ ] **Step 1: Write failing repairer tests**

Add tests that prove:

- repair skips pending target nodes whose available capacity is zero
- repair still proceeds when at least one valid pending target remains
- when all pending targets are filtered out, no replicate request is issued and work remains deferred

Suggested test skeletons:

```go
func TestPendingReplicaRepairer_RepairOnceSkipsFullTargets(t *testing.T) {}

func TestPendingReplicaRepairer_RepairOnceRepairsOnlyCapacityValidTargets(t *testing.T) {}
```

- [ ] **Step 2: Run targeted repairer tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestPendingReplicaRepairer_RepairOnce(SkipsFullTargets|RepairsOnlyCapacityValidTargets)' -v
```

Expected:

- FAIL because repair target selection does not apply the shared capacity rule yet

- [ ] **Step 3: Implement shared minimum-capacity filtering in the repairer**

Implementation requirements:

- keep the current pending-replica flow and source-node selection
- filter pending targets through the same minimum-capacity rule before replication
- do not redesign repair scheduling in this task
- if no valid targets remain, treat the work as deferred instead of successful

- [ ] **Step 4: Re-run targeted repairer tests**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestPendingReplicaRepairer_RepairOnce(SkipsFullTargets|RepairsOnlyCapacityValidTargets)' -v
```

Expected:

- PASS for new repairer capacity-filtering tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/coordinator/repairer.go internal/mds/coordinator/repairer_test.go
git commit -m "mds: filter repair targets by capacity"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 6: Documentation and Full Verification

**Files:**
- Modify: `docs/architecture/manual-testing.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

- [ ] **Step 1: Update docs**

Document:

- datanode now reports real `used` bytes
- upload allocation now uses capacity-aware placement
- repair now shares the same minimum-capacity rule
- what is still out of scope: chunk-size-aware thresholds, rack/zone, rebalance, failover

- [ ] **Step 2: Run focused package verification**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/datanode ./internal/datanode ./internal/mds ./internal/mds/coordinator -v
```

Expected:

- PASS for datanode usage, allocator, service, and repair tests

- [ ] **Step 3: Run full repository verification**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
```

Expected:

- PASS for all packages

- [ ] **Step 4: Commit**

If VCS metadata is available:

```bash
git add docs/architecture/manual-testing.md docs/architecture/technical-debt-roadmap.md
git commit -m "mds: document capacity-aware placement"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Notes

- Prefer pure allocator helpers over embedding policy logic in service methods.
- Keep selection deterministic; no random tie-breaking in this phase.
- Do not introduce chunk-size heuristics yet unless the request explicitly expands scope.
- Repair and upload must not diverge in minimum-capacity semantics after this change.
