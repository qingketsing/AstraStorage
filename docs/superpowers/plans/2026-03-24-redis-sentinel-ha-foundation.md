# Redis Sentinel HA Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a strengthened Redis high-availability layer built around dual master/replica groups plus Sentinel, then integrate cache, distributed locking, warmup, and invalidation support into AstraStorage without changing PostgreSQL's role as the source of truth.

**Architecture:** Keep PostgreSQL authoritative for metadata and transactional state. Introduce two Redis Sentinel-managed replication groups: a `cache` group for hot read models and cache protection, and a `coord` group for locks, warmup coordination, and invalidation events. Wire Redis in through `internal/platform/redis`, then layer cache-aware reads into MDS and gateway before adding warmup workers and resilience controls.

**Tech Stack:** Go, Redis Sentinel, Redis master/replica, PostgreSQL, HTTP, Docker Compose, Go testing

---

### Task 1: Define Redis topology and configuration model

**Files:**
- Create: `internal/platform/redis/client/config.go`
- Create: `internal/platform/redis/client/groups.go`
- Create: `internal/platform/redis/client/health.go`
- Create: `internal/platform/redis/README.md`
- Modify: `internal/mds/config/config.go`
- Modify: `README.md`

- [ ] **Step 1: Write the failing config tests**

Add tests that cover:
- dual replication group config parsing (`cache` and `coord`)
- Sentinel address parsing
- validation that both master set names are required when Redis is enabled
- validation that `coord` and `cache` groups cannot share empty names accidentally

- [ ] **Step 2: Run the config tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/config -run 'Test(Redis|Config)' -v`

Expected: FAIL because Redis config types and parsing do not exist yet.

- [ ] **Step 3: Add minimal Redis config model**

Implement:
- enable flag
- Sentinel addresses
- auth placeholders
- per-group master set names
- read/write timeout knobs
- cache TTL policy defaults
- warmup interval defaults

- [ ] **Step 4: Re-run config tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/config -run 'Test(Redis|Config)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/client/config.go internal/platform/redis/client/groups.go internal/platform/redis/client/health.go internal/platform/redis/README.md internal/mds/config/config.go README.md
git commit -m "redis: add sentinel configuration model"
```

### Task 2: Add Sentinel-aware Redis client foundation

**Files:**
- Create: `internal/platform/redis/client/factory.go`
- Create: `internal/platform/redis/client/sentinel.go`
- Create: `internal/platform/redis/client/failover.go`
- Create: `internal/platform/redis/client/client_test.go`

- [ ] **Step 1: Write the failing client tests**

Cover:
- factory creates separate `cache` and `coord` clients
- failover client requires Sentinel endpoints
- read/write role routing is preserved per group
- health summary reports group name and Sentinel state

- [ ] **Step 2: Run the Redis client tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/client -v`

Expected: FAIL because the factory and Sentinel client types do not exist.

- [ ] **Step 3: Implement minimal Sentinel-aware client factory**

Implement:
- group-aware client bundle
- Sentinel failover client creation
- read/write client accessors
- health check hook points

- [ ] **Step 4: Re-run the client tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/client -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/client/factory.go internal/platform/redis/client/sentinel.go internal/platform/redis/client/failover.go internal/platform/redis/client/client_test.go
git commit -m "redis: add sentinel client foundation"
```

### Task 3: Add cache keyspace and policy layer

**Files:**
- Create: `internal/platform/redis/cache/keys.go`
- Create: `internal/platform/redis/cache/policy.go`
- Create: `internal/platform/redis/cache/codec.go`
- Create: `internal/platform/redis/cache/nulls.go`
- Create: `internal/platform/redis/cache/bloom.go`
- Create: `internal/platform/redis/cache/cache_test.go`

- [ ] **Step 1: Write the failing cache policy tests**

Cover:
- file metadata cache key generation
- directory listing cache key generation
- download plan cache key generation
- TTL jitter stays within expected range
- null cache keys and bloom filter keys are stable

- [ ] **Step 2: Run the cache tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache -v`

Expected: FAIL because cache key and policy helpers do not exist.

- [ ] **Step 3: Implement keyspace and policy helpers**

Implement:
- deterministic key builders
- TTL defaults and jitter helper
- null cache helper
- bloom filter namespace helper
- JSON codec helpers for cached read models

- [ ] **Step 4: Re-run the cache tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/cache/keys.go internal/platform/redis/cache/policy.go internal/platform/redis/cache/codec.go internal/platform/redis/cache/nulls.go internal/platform/redis/cache/bloom.go internal/platform/redis/cache/cache_test.go
git commit -m "redis: add cache keyspace and policy layer"
```

### Task 4: Add distributed lock primitives on the coordination group

**Files:**
- Create: `internal/platform/redis/lock/locker.go`
- Create: `internal/platform/redis/lock/token.go`
- Create: `internal/platform/redis/lock/rebuild_lock.go`
- Create: `internal/platform/redis/lock/warmup_lock.go`
- Create: `internal/platform/redis/lock/ownership.go`
- Create: `internal/platform/redis/lock/lock_test.go`

- [ ] **Step 1: Write the failing lock tests**

Cover:
- acquire succeeds on empty key
- acquire fails while another owner token holds the key
- release only succeeds for the original owner token
- TTL is applied to lock keys

- [ ] **Step 2: Run the lock tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/lock -v`

Expected: FAIL because lock primitives do not exist.

- [ ] **Step 3: Implement minimal locker**

Implement:
- owner-token generation
- acquire with TTL
- compare-and-delete release
- small abstractions for rebuild and warmup locks

- [ ] **Step 4: Re-run the lock tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/lock -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/lock/locker.go internal/platform/redis/lock/token.go internal/platform/redis/lock/rebuild_lock.go internal/platform/redis/lock/warmup_lock.go internal/platform/redis/lock/ownership.go internal/platform/redis/lock/lock_test.go
git commit -m "redis: add distributed locking primitives"
```

### Task 5: Add cache invalidation and pub/sub contracts

**Files:**
- Create: `internal/platform/redis/pubsub/channels.go`
- Create: `internal/platform/redis/pubsub/publisher.go`
- Create: `internal/platform/redis/pubsub/subscriber.go`
- Create: `internal/platform/redis/pubsub/invalidation_events.go`
- Create: `internal/platform/redis/pubsub/pubsub_test.go`

- [ ] **Step 1: Write the failing pub/sub tests**

Cover:
- stable invalidation channel names
- event payload serialization for file, directory, and node invalidation
- publisher rejects empty channels

- [ ] **Step 2: Run the pub/sub tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/pubsub -v`

Expected: FAIL because pub/sub contracts do not exist.

- [ ] **Step 3: Implement minimal pub/sub contract layer**

Implement:
- invalidation event types
- channel naming
- publisher/subscriber abstraction interfaces

- [ ] **Step 4: Re-run the pub/sub tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/pubsub -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/pubsub/channels.go internal/platform/redis/pubsub/publisher.go internal/platform/redis/pubsub/subscriber.go internal/platform/redis/pubsub/invalidation_events.go internal/platform/redis/pubsub/pubsub_test.go
git commit -m "redis: add invalidation pubsub contracts"
```

### Task 6: Add read-through cache service for file metadata and download plans

**Files:**
- Create: `internal/platform/redis/cache/file_meta.go`
- Create: `internal/platform/redis/cache/download_plan.go`
- Modify: `internal/mds/service.go`
- Modify: `internal/mds/service_read.go`
- Modify: `internal/gateway/client.go`
- Create: `internal/platform/redis/cache/file_meta_test.go`
- Create: `internal/platform/redis/cache/download_plan_test.go`

- [ ] **Step 1: Write the failing cache-backed read tests**

Cover:
- file metadata read hits Redis after first PostgreSQL load
- download plan read uses cache on repeated access
- not-found file IDs are cached as nulls
- stale entry rebuild uses a lock and does not issue duplicate PostgreSQL reads

- [ ] **Step 2: Run the targeted tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds ./internal/gateway -run 'Test(FileMeta|DownloadPlan|Cache)' -v`

Expected: FAIL because the services are not wired to Redis.

- [ ] **Step 3: Implement minimal read-through cache integration**

Implement:
- service-side cache collaborator on read paths
- Redis-backed file metadata cache
- Redis-backed download plan cache
- null cache for missing file IDs
- stale-while-revalidate lock protection for hot rebuilds

- [ ] **Step 4: Re-run the targeted tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds ./internal/gateway -run 'Test(FileMeta|DownloadPlan|Cache)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/cache/file_meta.go internal/platform/redis/cache/download_plan.go internal/mds/service.go internal/mds/service_read.go internal/gateway/client.go internal/platform/redis/cache/file_meta_test.go internal/platform/redis/cache/download_plan_test.go
git commit -m "redis: cache file metadata and download plans"
```

### Task 7: Add directory listing and node health caching

**Files:**
- Create: `internal/platform/redis/cache/dir_list.go`
- Create: `internal/platform/redis/cache/node_health.go`
- Modify: `internal/mds/service_read.go`
- Modify: `internal/mds/service_node.go`
- Create: `internal/platform/redis/cache/dir_list_test.go`
- Create: `internal/platform/redis/cache/node_health_test.go`

- [ ] **Step 1: Write the failing directory and node cache tests**

Cover:
- directory list queries cache by parent and window
- node health snapshot caches healthy node set
- cache invalidates after node heartbeat or registration updates

- [ ] **Step 2: Run the targeted tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds -run 'Test(Directory|NodeHealth|Cache)' -v`

Expected: FAIL because directory and node cache integrations do not exist.

- [ ] **Step 3: Implement directory and node cache integration**

Implement:
- cache-backed list children read path
- node health snapshot cache
- invalidation hook points on node mutation paths

- [ ] **Step 4: Re-run the targeted tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds -run 'Test(Directory|NodeHealth|Cache)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/cache/dir_list.go internal/platform/redis/cache/node_health.go internal/mds/service_read.go internal/mds/service_node.go internal/platform/redis/cache/dir_list_test.go internal/platform/redis/cache/node_health_test.go
git commit -m "redis: cache directory and node reads"
```

### Task 8: Add mutation-triggered cache invalidation

**Files:**
- Create: `internal/platform/redis/cache/invalidation.go`
- Modify: `internal/mds/service_mutation.go`
- Modify: `internal/mds/service_upload.go`
- Modify: `internal/mds/service_directory.go`
- Modify: `internal/mds/service_file.go`
- Create: `internal/platform/redis/cache/invalidation_test.go`

- [ ] **Step 1: Write the failing invalidation tests**

Cover:
- rename invalidates affected file and directory keys
- move invalidates old and new parent directory lists
- delete invalidates file metadata and download plan keys
- upload completion / verification invalidates cached read models

- [ ] **Step 2: Run the invalidation tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds -run 'Test(Invalidation|Rename|Move|Delete|Upload)' -v`

Expected: FAIL because mutation hooks are not wired.

- [ ] **Step 3: Implement mutation-side invalidation**

Implement:
- helper for direct key deletes
- pub/sub invalidation event publishing
- service hooks after successful writes and transactions

- [ ] **Step 4: Re-run the invalidation tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/cache ./internal/mds -run 'Test(Invalidation|Rename|Move|Delete|Upload)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/cache/invalidation.go internal/mds/service_mutation.go internal/mds/service_upload.go internal/mds/service_directory.go internal/mds/service_file.go internal/platform/redis/cache/invalidation_test.go
git commit -m "redis: invalidate cached read models on mutations"
```

### Task 9: Add warmup scheduler and hotspot tracking

**Files:**
- Create: `internal/platform/redis/warmup/bootstrap.go`
- Create: `internal/platform/redis/warmup/scheduler.go`
- Create: `internal/platform/redis/warmup/hotspot.go`
- Create: `internal/platform/redis/warmup/queue.go`
- Create: `internal/platform/redis/warmup/worker.go`
- Create: `internal/platform/redis/warmup/policy.go`
- Create: `internal/platform/redis/warmup/warmup_test.go`
- Modify: `cmd/mds/app.go`

- [ ] **Step 1: Write the failing warmup tests**

Cover:
- startup warmup enqueues configured hot resources
- scheduler prevents duplicate work with warmup locks
- hotspot tracking raises frequently accessed file IDs into the queue

- [ ] **Step 2: Run the warmup tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/warmup ./cmd/mds -run 'Test(Warmup|Hotspot)' -v`

Expected: FAIL because the warmup package does not exist.

- [ ] **Step 3: Implement minimal warmup scheduler**

Implement:
- startup seed list
- periodic refresh scheduler
- lock-protected worker execution
- simple hotspot counters for file and plan reads

- [ ] **Step 4: Re-run the warmup tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/redis/warmup ./cmd/mds -run 'Test(Warmup|Hotspot)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/redis/warmup/bootstrap.go internal/platform/redis/warmup/scheduler.go internal/platform/redis/warmup/hotspot.go internal/platform/redis/warmup/queue.go internal/platform/redis/warmup/worker.go internal/platform/redis/warmup/policy.go internal/platform/redis/warmup/warmup_test.go cmd/mds/app.go
git commit -m "redis: add warmup scheduler and hotspot tracking"
```

### Task 10: Add Redis Sentinel deployment assets

**Files:**
- Create: `deploy/docker/redis-sentinel/docker-compose.yml`
- Create: `deploy/docker/redis-sentinel/redis-cache-master.conf`
- Create: `deploy/docker/redis-sentinel/redis-cache-replica.conf`
- Create: `deploy/docker/redis-sentinel/redis-coord-master.conf`
- Create: `deploy/docker/redis-sentinel/redis-coord-replica.conf`
- Create: `deploy/docker/redis-sentinel/sentinel-1.conf`
- Create: `deploy/docker/redis-sentinel/sentinel-2.conf`
- Create: `deploy/docker/redis-sentinel/sentinel-3.conf`
- Create: `deploy/docker/redis-sentinel/sentinel-4.conf`
- Create: `deploy/docker/redis-sentinel/sentinel-5.conf`
- Modify: `docs/architecture/manual-testing.md`

- [ ] **Step 1: Write the deployment verification checklist**

Document checks for:
- both master groups boot
- replicas follow correct master
- Sentinel discovers both groups
- failover promotes a replica after master stop

- [ ] **Step 2: Add Docker Compose topology**

Implement:
- `cache` master + two replicas
- `coord` master + two replicas
- five Sentinel containers
- stable service names and exposed ports

- [ ] **Step 3: Add manual verification instructions**

Document:
- how to bring up the topology
- how to inspect Sentinel state
- how to test master failover

- [ ] **Step 4: Commit**

```bash
git add deploy/docker/redis-sentinel/docker-compose.yml deploy/docker/redis-sentinel/*.conf docs/architecture/manual-testing.md
git commit -m "deploy: add redis sentinel topology"
```

### Task 11: Add integration tests for cache, locks, warmup, and failover

**Files:**
- Create: `test/integration/redis_cache_integration_test.go`
- Create: `test/integration/redis_lock_integration_test.go`
- Create: `test/integration/redis_warmup_integration_test.go`
- Create: `test/integration/redis_failover_integration_test.go`

- [ ] **Step 1: Write integration tests against the Docker Redis topology**

Cover:
- file metadata cache hit after warmup
- lock ownership under concurrent rebuild attempts
- null cache and bloom filter behavior
- Sentinel-driven failover with resumed writes to the new master

- [ ] **Step 2: Run the integration tests to verify initial failures**

Run: `GOCACHE=/tmp/go-cache go test ./test/integration -run 'TestRedis' -v`

Expected: FAIL until Redis integration and fixtures are complete.

- [ ] **Step 3: Implement missing glue until tests pass**

Use targeted edits from earlier tasks only; do not invent new subsystems here.

- [ ] **Step 4: Re-run integration tests**

Run: `GOCACHE=/tmp/go-cache go test ./test/integration -run 'TestRedis' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add test/integration/redis_cache_integration_test.go test/integration/redis_lock_integration_test.go test/integration/redis_warmup_integration_test.go test/integration/redis_failover_integration_test.go
git commit -m "test: add redis sentinel integration coverage"
```

### Task 12: Full verification and documentation sweep

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture/system-overview.md`
- Modify: `docs/architecture/session-handoff.md`
- Modify: `docs/architecture/technical-debt-roadmap.md`

- [ ] **Step 1: Update docs to reflect Redis HA integration**

Document:
- Redis is a cache/coordination layer, not the source of truth
- Sentinel topology and environment variables
- current limitations and future RabbitMQ follow-up

- [ ] **Step 2: Run full verification**

Run:
- `GOCACHE=/tmp/go-cache go test ./...`
- `GOCACHE=/tmp/go-cache go build ./...`

Expected:
- all unit and integration tests PASS
- full repository build PASS

- [ ] **Step 3: Commit**

```bash
git add README.md docs/architecture/system-overview.md docs/architecture/session-handoff.md docs/architecture/technical-debt-roadmap.md
git commit -m "docs: describe redis sentinel integration"
```
