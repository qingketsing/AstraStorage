# Scheduling Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete first scheduling closure for AstraStorage by adding persistent replica plans, chunk-size-aware placement, failover planning, cleanup execution, rebalance planning, and leader-supervised controller lifecycle.

**Architecture:** Persist scheduling intent in PostgreSQL/memory store as `ReplicaPlan`, keep `etcd` limited to leader election, reuse the existing `repairer` as the replica copy executor, and add planner/cleanup loops around it. The control plane closes the loop by discovering work, materializing pending replicas, letting repair finish the copy, and then cleaning old replicas plus finalizing the plan.

**Tech Stack:** Go, PostgreSQL repository backend, in-memory repository backend, etcd leader election, existing MDS repairer, Go `testing`

---

## File Map

### Metadata / Store

- Create: `internal/mds/metadata/replica_plan.go`
- Modify: `internal/mds/store/store.go`
- Modify: `internal/mds/store/txn.go`
- Modify: `internal/mds/store/memory.go`
- Modify: `internal/mds/store/memory_helpers.go`
- Modify: `internal/mds/store/memory_chunk.go`
- Create: `internal/mds/store/memory_plan.go`
- Modify: `internal/mds/store/memory_test.go`
- Modify: `internal/platform/postgres/migrate/sql/001_init_mds.sql`
- Modify: `internal/platform/postgres/repository/repository.go`
- Create: `internal/platform/postgres/repository/plan.go`
- Create: `internal/platform/postgres/repository/plan_test.go`
- Modify: `internal/platform/postgres/repository/chunk.go`

### Placement / Controller

- Modify: `internal/mds/allocator.go`
- Create: `internal/mds/allocator_test.go` or extend existing file
- Modify: `internal/mds/coordinator/failover.go`
- Create: `internal/mds/coordinator/failover_test.go`
- Modify: `internal/mds/coordinator/rebalance.go`
- Create: `internal/mds/coordinator/rebalance_test.go`
- Create: `internal/mds/coordinator/cleanup.go`
- Create: `internal/mds/coordinator/cleanup_test.go`
- Modify: `internal/mds/coordinator/supervisor.go`
- Modify: `internal/mds/coordinator/supervisor_test.go`

### App Wiring / Observability / Docs

- Modify: `cmd/mds/app.go`
- Modify: `cmd/mds/app_test.go`
- Modify: `internal/mds/observability.go`
- Modify: `internal/mds/observability_test.go`
- Modify: `docs/architecture/manual-testing.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

## Task 1: Add `ReplicaPlan` Metadata and Store Interfaces

**Files:**
- Create: `internal/mds/metadata/replica_plan.go`
- Modify: `internal/mds/store/store.go`
- Modify: `internal/mds/store/txn.go`
- Test: `internal/mds/store/memory_test.go`

- [ ] **Step 1: Write failing metadata/store tests**

Add tests that expect:
- `ReplicaPlan` can be created and listed
- duplicate active plan for same `chunk + type + target` is rejected
- plan state can move from `planned` to `done`

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/store -run 'TestMemoryRepository_ReplicaPlan' -v`
Expected: FAIL because `ReplicaPlan` types/interfaces do not exist

- [ ] **Step 3: Add metadata model**

Implement:
- `ReplicaPlanType`
- `ReplicaPlanState`
- `ReplicaPlan`

Keep fields aligned with the spec and add minimal validation helpers if needed.

- [ ] **Step 4: Extend store interfaces**

Add:
- `ReplicaPlanRepository`
- `ReplicaPlanFilter`
- `ReplicaPlanPatch`
- `ListChunksByNode`
- `RemoveChunkReplica`

Wire `ReplicaPlanRepository` into `store.Repository`.

- [ ] **Step 5: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/store -run 'TestMemoryRepository_ReplicaPlan' -v`
Expected: compile may still fail because memory implementation is not written yet

## Task 2: Implement Memory Store Support for Plans and Replica Removal

**Files:**
- Modify: `internal/mds/store/memory.go`
- Modify: `internal/mds/store/memory_helpers.go`
- Modify: `internal/mds/store/memory_chunk.go`
- Create: `internal/mds/store/memory_plan.go`
- Test: `internal/mds/store/memory_test.go`

- [ ] **Step 1: Write failing memory-store tests**

Add tests for:
- create/list/update/delete `ReplicaPlan`
- `ListChunksByNode`
- `RemoveChunkReplica`

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/store -run 'TestMemoryRepository_(ReplicaPlan|ListChunksByNode|RemoveChunkReplica)' -v`
Expected: FAIL

- [ ] **Step 3: Implement minimal memory backing state**

Add `plans` collection to memory state and clone helpers.

- [ ] **Step 4: Implement repository methods**

Add CRUD/update/filter behavior for plans plus chunk listing/removal by node.

- [ ] **Step 5: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/store -run 'TestMemoryRepository_(ReplicaPlan|ListChunksByNode|RemoveChunkReplica)' -v`
Expected: PASS

## Task 3: Implement PostgreSQL Support for Plans and Replica Removal

**Files:**
- Modify: `internal/platform/postgres/migrate/sql/001_init_mds.sql`
- Modify: `internal/platform/postgres/repository/repository.go`
- Create: `internal/platform/postgres/repository/plan.go`
- Create: `internal/platform/postgres/repository/plan_test.go`
- Modify: `internal/platform/postgres/repository/chunk.go`
- Test: `internal/platform/postgres/repository/repository_test.go`

- [ ] **Step 1: Write failing postgres repository tests**

Add tests for:
- `ReplicaPlan` create/list/update lifecycle
- duplicate active-plan rejection
- `ListChunksByNode`
- `RemoveChunkReplica`

- [ ] **Step 2: Run focused tests to verify failure**

Run: `env MDS_TEST_POSTGRES_DSN=postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable GOCACHE=/tmp/go-cache go test ./internal/platform/postgres/repository -run 'TestRepository_(ReplicaPlan|ListChunksByNode|RemoveChunkReplica)' -v`
Expected: FAIL

- [ ] **Step 3: Add repository implementation**

Implement plan persistence and chunk replica removal/query logic in focused files.

- [ ] **Step 4: Run focused tests**

Run: `env MDS_TEST_POSTGRES_DSN=postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable GOCACHE=/tmp/go-cache go test ./internal/platform/postgres/repository -run 'TestRepository_(ReplicaPlan|ListChunksByNode|RemoveChunkReplica)' -v`
Expected: PASS

## Task 4: Upgrade Allocator to Chunk-Size-Aware Placement

**Files:**
- Modify: `internal/mds/allocator.go`
- Test: `internal/mds/allocator_test.go`

- [ ] **Step 1: Write failing allocator tests**

Add tests for:
- `RequiredPlacementBytes`
- selection rejects nodes where `available < required_bytes`
- effective ready replica counting only includes healthy ready replicas
- exclusion set covers all existing replica nodes

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds -run 'Test(RequiredPlacementBytes|SelectPlacementTargets|CountEffectiveReadyReplicas|BuildReplicaExclusionSet)' -v`
Expected: FAIL

- [ ] **Step 3: Implement allocator extensions**

Add:
- `PlacementRequest`
- `RequiredPlacementBytes`
- `SelectPlacementTargets`
- `CountEffectiveReadyReplicas`
- `BuildReplicaExclusionSet`

Keep upload placement and repair placement semantics compatible with the new allocator.

- [ ] **Step 4: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds -run 'Test(RequiredPlacementBytes|SelectPlacementTargets|CountEffectiveReadyReplicas|BuildReplicaExclusionSet)' -v`
Expected: PASS

## Task 5: Implement `FailoverPlanner`

**Files:**
- Modify: `internal/mds/coordinator/failover.go`
- Test: `internal/mds/coordinator/failover_test.go`

- [ ] **Step 1: Write failing planner tests**

Add tests for:
- unavailable node detection from heartbeat timeout
- plan generation when effective replica count drops below desired count
- no duplicate plan when an active plan already exists
- pending replica materialization on selected target

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestFailoverPlanner_' -v`
Expected: FAIL

- [ ] **Step 3: Implement planner**

Implement:
- `Run`
- `PlanOnce`
- `listUnavailableNodes`
- `planNodeFailover`
- `planChunkFailover`
- `materializePendingReplica`

The planner should create `failover` plans and pending replicas, but not perform data copy itself.

- [ ] **Step 4: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestFailoverPlanner_' -v`
Expected: PASS

## Task 6: Implement `CleanupController`

**Files:**
- Create: `internal/mds/coordinator/cleanup.go`
- Create: `internal/mds/coordinator/cleanup_test.go`

- [ ] **Step 1: Write failing cleanup tests**

Add tests for:
- failover plan finishes after target replica becomes `ready`
- rebalance cleanup removes source replica after copy completes
- cleanup retries on datanode delete failure
- long-dead lost replica metadata can be purged

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestCleanupController_' -v`
Expected: FAIL

- [ ] **Step 3: Implement cleanup controller**

Implement:
- `Run`
- `CleanupOnce`
- `finalizeCompletedPlans`
- `deleteSourceReplica`
- `purgeLostReplicaMetadata`
- `failOrRetryPlan`

- [ ] **Step 4: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestCleanupController_' -v`
Expected: PASS

## Task 7: Implement `RebalancePlanner`

**Files:**
- Modify: `internal/mds/coordinator/rebalance.go`
- Test: `internal/mds/coordinator/rebalance_test.go`

- [ ] **Step 1: Write failing rebalance tests**

Add tests for:
- node pressure classification by usage ratio
- selecting a movable replica from an overfull node
- creating a rebalance plan and materializing a pending replica
- skipping chunks that would reduce replica safety

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestRebalancePlanner_' -v`
Expected: FAIL

- [ ] **Step 3: Implement rebalance planner**

Implement:
- `Run`
- `PlanOnce`
- `classifyNodePressure`
- `selectReplicaToMove`
- `planReplicaMove`

Keep thresholds minimal and explicit; do not introduce a generic scoring framework.

- [ ] **Step 4: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'TestRebalancePlanner_' -v`
Expected: PASS

## Task 8: Expand Supervisor, App Wiring, and Observability

**Files:**
- Modify: `internal/mds/coordinator/supervisor.go`
- Modify: `internal/mds/coordinator/supervisor_test.go`
- Modify: `cmd/mds/app.go`
- Modify: `cmd/mds/app_test.go`
- Modify: `internal/mds/observability.go`
- Modify: `internal/mds/observability_test.go`

- [ ] **Step 1: Write failing lifecycle / metrics tests**

Add tests for:
- supervisor starts and stops all leader loops
- `cmd/mds` wiring attaches failover/rebalance/cleanup under leader election
- new scheduler metrics register and update correctly

- [ ] **Step 2: Run focused tests to verify failure**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator ./cmd/mds ./internal/mds -run 'Test(Supervisor|NewApplication|Observability)_' -v`
Expected: FAIL

- [ ] **Step 3: Implement leader-scoped loop management and metrics**

Extend supervisor to manage:
- repairer
- failover
- cleanup
- rebalance

Wire them in `cmd/mds/app.go` and add metrics/logging for plans and controller runs.

- [ ] **Step 4: Run focused tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator ./cmd/mds ./internal/mds -run 'Test(Supervisor|NewApplication|Observability)_' -v`
Expected: PASS

## Task 9: Documentation and Full Verification

**Files:**
- Modify: `docs/architecture/manual-testing.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

- [ ] **Step 1: Update docs**

Document:
- failover / rebalance / cleanup controller behavior
- plan lifecycle
- new metrics
- current limits and remaining debt

- [ ] **Step 2: Run package-level verification**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/... ./cmd/mds -v`
Expected: PASS

- [ ] **Step 3: Run full repository verification**

Run: `GOCACHE=/tmp/go-cache go test ./...`
Expected: PASS

- [ ] **Step 4: Run build verification**

Run: `GOCACHE=/tmp/go-cache go build ./...`
Expected: PASS

- [ ] **Step 5: Summarize residual risks**

Capture any remaining gaps, especially:
- no event-driven scheduling
- no fault-domain-aware placement
- no task ownership beyond leader-scoped loops
