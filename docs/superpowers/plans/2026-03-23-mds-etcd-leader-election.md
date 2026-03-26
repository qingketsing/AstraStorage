# MDS Etcd Leader Election Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add etcd-backed leader election for `MDS` so multiple instances can serve traffic while only the elected leader runs coordinator loops such as the pending replica repairer.

**Architecture:** Keep PostgreSQL as the persistent metadata source of truth and introduce a thin etcd coordination layer under `internal/platform/etcd`. `cmd/mds` will assemble an elector and a leader-scoped coordinator supervisor so leadership only gates background control loops, not HTTP/gRPC serving.

**Tech Stack:** Go 1.25, `go.etcd.io/etcd/client/v3`, `go.etcd.io/etcd/server/v3/embed` for tests, existing `testing` package, current `cmd/mds` bootstrap and `internal/mds/coordinator`

---

## File Map

### New files

- `internal/platform/etcd/client/client.go`
- `internal/platform/etcd/client/client_test.go`
- `internal/platform/etcd/leader/elector.go`
- `internal/platform/etcd/leader/elector_test.go`
- `internal/mds/coordinator/supervisor.go`
- `internal/mds/coordinator/supervisor_test.go`

### Modified files

- `go.mod`
- `cmd/mds/app.go`
- `cmd/mds/app_test.go`
- `internal/mds/config/config.go`
- `internal/mds/observability.go`
- `internal/mds/observability_test.go`
- `internal/mds/coordinator/repairer.go`
- `internal/mds/coordinator/repairer_test.go`
- `docs/architecture/technical-debt-roadmap.md`
- `docs/architecture/observability-demo.md`
- `docs/architecture/manual-testing.md`

### Responsibility split

- `internal/platform/etcd/client/*`: etcd client construction from config, endpoint parsing, dial timeout validation
- `internal/platform/etcd/leader/*`: session lease, campaign loop, leadership callbacks, term tracking, clean resignation
- `internal/mds/coordinator/supervisor.go`: start/stop leader-scoped loops such as `repairer` without leaking etcd details into business logic
- `cmd/mds/app.go`: bootstrap etcd only when leader election is enabled, keep HTTP/gRPC always on, hand leadership events to supervisor
- `internal/mds/observability.go`: MDS leadership metrics only, keeping etcd runtime metrics out of the generic platform layer

## Global Constraints

- PostgreSQL remains the only source of truth for business metadata.
- etcd stores only leadership and coordination state, never file/chunk/replica metadata.
- HTTP and gRPC serving must remain available on follower MDS instances.
- Only leader-scoped coordinator loops are gated by election.
- First iteration protects `repairer` only; do not add rebalance/failover work in the same change.
- All new metrics must stay low-cardinality.

## Task 1: Add Etcd Config and Client Plumbing

**Files:**
- Modify: `go.mod`
- Modify: `internal/mds/config/config.go`
- Create: `internal/platform/etcd/client/client.go`
- Create: `internal/platform/etcd/client/client_test.go`

- [ ] **Step 1: Write failing config and client tests**

Add tests that prove:

- `MDS_LEADER_ELECTION_ENABLED=false` keeps config valid without etcd endpoints
- enabling leader election without endpoints fails validation
- etcd client config trims and splits endpoints correctly
- nil or empty endpoints are rejected

Suggested test skeletons:

```go
func TestConfigValidate_LeaderElectionRequiresEndpoints(t *testing.T) {}

func TestNewClientConfig_RejectsEmptyEndpoints(t *testing.T) {}

func TestNewClientConfig_TrimsEndpoints(t *testing.T) {}
```

- [ ] **Step 2: Run the targeted tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/config ./internal/platform/etcd/client -v
```

Expected:

- FAIL because the etcd config and client package do not exist yet

- [ ] **Step 3: Add minimal config and client code**

Implementation requirements:

- add etcd dependencies to `go.mod`
- extend `internal/mds/config.Config` with a nested leader election config
- support:
  - `MDS_LEADER_ELECTION_ENABLED`
  - `MDS_ETCD_ENDPOINTS`
  - `MDS_ETCD_DIAL_TIMEOUT`
  - `MDS_LEADER_ELECTION_PREFIX`
  - `MDS_LEADER_LEASE_TTL`
  - `MDS_INSTANCE_ID`
- create a thin etcd client constructor that accepts parsed config and returns `*clientv3.Client`

Suggested implementation shape:

```go
type LeaderElectionConfig struct {
    Enabled     bool
    InstanceID  string
    Prefix      string
    LeaseTTL    time.Duration
    EtcdEndpoints []string
    DialTimeout time.Duration
}

func New(cfg Config) (*clientv3.Client, error)
```

- [ ] **Step 4: Re-run the targeted tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/config ./internal/platform/etcd/client -v
```

Expected:

- PASS for all new config/client tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add go.mod internal/mds/config/config.go internal/platform/etcd/client
git commit -m "mds: add etcd leader election config"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 2: Add Etcd Leader Elector

**Files:**
- Create: `internal/platform/etcd/leader/elector.go`
- Create: `internal/platform/etcd/leader/elector_test.go`

- [ ] **Step 1: Write failing elector tests with embedded etcd**

Add tests that prove:

- one elector can become leader against embedded etcd
- two electors on the same election prefix do not become leader simultaneously
- when the first leader stops, the second can take over
- `OnStartedLeading` and `OnStoppedLeading` callbacks are fired once per transition

Suggested test skeletons:

```go
func TestElector_BecomesLeader(t *testing.T) {}

func TestElector_OnlyOneLeaderAtATime(t *testing.T) {}

func TestElector_FailoverTriggersNewLeader(t *testing.T) {}
```

- [ ] **Step 2: Run the elector tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/etcd/leader -v
```

Expected:

- FAIL because the elector package does not exist yet

- [ ] **Step 3: Implement the minimal elector**

Implementation requirements:

- use `clientv3/concurrency.Session`
- use a single election prefix such as `/astrastorage/controlplane/mds/leader`
- expose leadership callbacks with a leader-scoped context
- stop the leader-scoped context when the session is lost or the process shuts down
- surface a stable term value using the leader key revision

Suggested implementation shape:

```go
type Callbacks struct {
    OnStartedLeading func(ctx context.Context, term int64)
    OnStoppedLeading func(term int64)
}

type Elector struct { ... }

func New(client *clientv3.Client, cfg Config) (*Elector, error)
func (e *Elector) Run(ctx context.Context, callbacks Callbacks) error
```

- [ ] **Step 4: Re-run the elector tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/etcd/leader -v
```

Expected:

- PASS for the embedded etcd election tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/platform/etcd/leader
git commit -m "mds: add etcd leader elector"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 3: Add Coordinator Supervisor and Repairer Leadership Gate

**Files:**
- Create: `internal/mds/coordinator/supervisor.go`
- Create: `internal/mds/coordinator/supervisor_test.go`
- Modify: `internal/mds/coordinator/repairer.go`
- Modify: `internal/mds/coordinator/repairer_test.go`

- [ ] **Step 1: Write failing supervisor tests**

Add tests that prove:

- `OnStartedLeading` starts the repair loop exactly once
- a second `OnStartedLeading` while already leader is ignored
- `OnStoppedLeading` cancels the running loop
- losing leadership prevents further loop ticks

Suggested test skeletons:

```go
func TestSupervisor_StartsRepairerWhenLeading(t *testing.T) {}

func TestSupervisor_StopsRepairerWhenLeadershipLost(t *testing.T) {}
```

- [ ] **Step 2: Run the coordinator tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'Test(Supervisor_|PendingReplicaRepairer_)' -v
```

Expected:

- FAIL because the supervisor does not exist yet

- [ ] **Step 3: Implement the supervisor and wire repairer lifecycle**

Implementation requirements:

- add a small supervisor that owns a leader-scoped context
- start `repairer.Run(leaderCtx)` only while leader
- stop it on `OnStoppedLeading`
- keep `repairer` business logic unchanged beyond optional term-aware logging helpers

Suggested implementation shape:

```go
type Loop interface {
    Run(ctx context.Context)
}

type Supervisor struct { ... }

func NewSupervisor(repairer Loop) *Supervisor
func (s *Supervisor) OnStartedLeading(parent context.Context, term int64)
func (s *Supervisor) OnStoppedLeading(term int64)
```

- [ ] **Step 4: Re-run the coordinator tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds/coordinator -run 'Test(Supervisor_|PendingReplicaRepairer_)' -v
```

Expected:

- PASS for the new supervisor tests and the existing repairer tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/coordinator/supervisor.go internal/mds/coordinator/supervisor_test.go internal/mds/coordinator/repairer.go internal/mds/coordinator/repairer_test.go
git commit -m "mds: gate repairer behind leader supervisor"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 4: Wire Leader Election Into `cmd/mds`

**Files:**
- Modify: `cmd/mds/app.go`
- Modify: `cmd/mds/app_test.go`

- [ ] **Step 1: Write failing app tests for election-gated repairer**

Add tests that prove:

- with leader election disabled, bootstrap remains valid and repairer still runs locally
- with leader election enabled but invalid config, bootstrap fails
- with leader election enabled, app creates elector and does not directly launch repairer from `Run`

Suggested test skeletons:

```go
func TestNewApplicationWithConfig_LeaderElectionRequiresEtcd(t *testing.T) {}

func TestApplicationRun_DoesNotDirectlyStartRepairerWhenElectionEnabled(t *testing.T) {}
```

- [ ] **Step 2: Run the app tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/mds -v
```

Expected:

- FAIL because bootstrap has no etcd/elector integration yet

- [ ] **Step 3: Implement app wiring**

Implementation requirements:

- when leader election is disabled, preserve current single-instance behavior
- when enabled:
  - create etcd client
  - create elector
  - create coordinator supervisor
  - keep HTTP/gRPC serving on every instance
  - run supervisor callbacks from elector transitions
- ensure app shutdown resigns cleanly and closes etcd resources

Suggested implementation shape:

```go
type application struct {
    ...
    leaderElector *leader.Elector
    supervisor    *coordinator.Supervisor
    etcdCloseFn   func() error
}
```

- [ ] **Step 4: Re-run the app tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./cmd/mds -v
```

Expected:

- PASS for new election-aware bootstrap tests and existing app tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add cmd/mds/app.go cmd/mds/app_test.go
git commit -m "mds: wire etcd leader election into bootstrap"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 5: Add MDS Leadership Observability

**Files:**
- Modify: `internal/mds/observability.go`
- Modify: `internal/mds/observability_test.go`
- Modify: `cmd/mds/app.go`

- [ ] **Step 1: Write failing tests for leadership metrics**

Add tests that prove:

- `is_leader` gauge flips between `0` and `1`
- leadership transition counters increment on acquisition/loss
- keepalive failures can be counted without adding high-cardinality labels

Suggested test skeletons:

```go
func TestObservability_RecordsLeadershipState(t *testing.T) {}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds ./cmd/mds -run 'TestObservability_RecordsLeadershipState|TestNewApplication' -v
```

Expected:

- FAIL because leadership metrics do not exist yet

- [ ] **Step 3: Implement leadership metrics and logs**

Implementation requirements:

- add:
  - `astrastorage_mds_leader_is_leader`
  - `astrastorage_mds_leader_transitions_total{result}`
  - `astrastorage_mds_leader_keepalive_failures_total`
  - `astrastorage_mds_leader_term`
- log `instance_id`, `prefix`, and `term` on leadership changes in `cmd/mds`

- [ ] **Step 4: Re-run the focused tests and verify they pass**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/mds ./cmd/mds -run 'TestObservability_RecordsLeadershipState|TestNewApplication' -v
```

Expected:

- PASS for leadership metrics tests

- [ ] **Step 5: Commit**

If VCS metadata is available:

```bash
git add internal/mds/observability.go internal/mds/observability_test.go cmd/mds/app.go
git commit -m "mds: add leadership observability"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Task 6: Documentation and Full Verification

**Files:**
- Modify: `docs/architecture/manual-testing.md`
- Modify: `docs/architecture/observability-demo.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

- [ ] **Step 1: Write or update docs for the new control-plane shape**

Document:

- etcd responsibilities vs PostgreSQL responsibilities
- how to configure leader election locally
- how to verify only one `repairer` is active
- what leadership metrics to inspect
- what remains out of scope

- [ ] **Step 2: Run focused multi-instance verification**

At minimum, run:

```bash
GOCACHE=/tmp/go-cache go test ./internal/platform/etcd/... ./internal/mds/coordinator ./cmd/mds -v
```

Expected:

- PASS for etcd election, supervisor, and bootstrap wiring

- [ ] **Step 3: Run full repository verification**

Run:

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
```

Expected:

- PASS across the full repository

- [ ] **Step 4: Commit**

If VCS metadata is available:

```bash
git add docs/architecture/manual-testing.md docs/architecture/observability-demo.md docs/architecture/technical-debt-roadmap.md
git commit -m "mds: document etcd leader election"
```

If `.git` is unavailable in this workspace snapshot, skip the commit and continue.

## Notes for the Implementer

- Prefer embedded etcd for package-level tests; do not depend on an external etcd binary in unit tests.
- Keep etcd concerns out of `internal/mds/service*` and `internal/mds/handler.go`.
- Do not let leadership gate HTTP/gRPC startup.
- Do not introduce business metadata writes to etcd.
- If app tests become awkward because `repairer` is concrete, introduce a narrow loop interface instead of broad refactoring.
- If current config tests are missing, add them in the same package instead of inventing a new config test harness.
