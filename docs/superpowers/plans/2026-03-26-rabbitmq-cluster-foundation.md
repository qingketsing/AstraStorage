# RabbitMQ Cluster Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a production-style RabbitMQ cluster foundation to AstraStorage with quorum queues, retry/DLX support, and MDS task integration for repair, cleanup, rebalance, and failover workflows.

**Architecture:** Deploy a 3-node RabbitMQ cluster for local development, build a reusable RabbitMQ client and topology declaration layer under `internal/platform/mq/rabbitmq`, then integrate MDS producers and consumers incrementally. Use quorum queues for task durability, manual acknowledgements plus retry/DLX for reliability, and idempotency keys for safe at-least-once processing.

**Tech Stack:** Go, RabbitMQ 4.x, AMQP 0-9-1, Docker Compose, Go testing

---

### Task 1: RabbitMQ Cluster Deployment Skeleton

**Files:**
- Create: `deploy/docker/rabbitmq-cluster/docker-compose.yml`
- Create: `deploy/docker/rabbitmq-cluster/rabbitmq-1.conf`
- Create: `deploy/docker/rabbitmq-cluster/rabbitmq-2.conf`
- Create: `deploy/docker/rabbitmq-cluster/rabbitmq-3.conf`
- Create: `deploy/docker/rabbitmq-cluster/enabled_plugins`
- Create: `deploy/docker/rabbitmq-cluster/README.md`

- [ ] **Step 1: Write a failing deployment smoke test**

Create `test/integration/rabbitmq_cluster_smoke_test.go` that:
- skips unless `MDS_TEST_RABBITMQ_ENDPOINT` is set
- dials RabbitMQ using the future client config
- expects the cluster test to fail initially because the client layer does not exist yet

- [ ] **Step 2: Run the smoke test to verify it fails**

Run: `GOCACHE=/tmp/go-cache go test ./test/integration -run TestRabbitMQClusterSmoke -v`

Expected: FAIL because RabbitMQ config/client code does not exist yet.

- [ ] **Step 3: Add the Docker cluster skeleton**

Implement:
- a 3-node cluster in `docker-compose.yml`
- persistent volumes for all three nodes
- management and Prometheus plugins via `enabled_plugins`
- node-specific config files that share the same cluster cookie and permit peer discovery
- README commands for `up`, `down`, and status inspection

- [ ] **Step 4: Manually verify the cluster starts**

Run:
- `docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml up -d`
- `docker compose -f deploy/docker/rabbitmq-cluster/docker-compose.yml ps`
- `docker exec rabbitmq-cluster-rabbitmq-1-1 rabbitmqctl cluster_status`

Expected:
- all three nodes are `Up`
- cluster status shows all three nodes

### Task 2: RabbitMQ Client Foundation

**Files:**
- Create: `internal/platform/mq/rabbitmq/client/config.go`
- Create: `internal/platform/mq/rabbitmq/client/connection.go`
- Create: `internal/platform/mq/rabbitmq/client/channel.go`
- Create: `internal/platform/mq/rabbitmq/client/publisher.go`
- Create: `internal/platform/mq/rabbitmq/client/consumer.go`
- Create: `internal/platform/mq/rabbitmq/client/health.go`
- Create: `internal/platform/mq/rabbitmq/client/client_test.go`

- [ ] **Step 1: Write the failing client tests**

Cover:
- multiple endpoint parsing
- connection config validation
- publisher confirm mode setup
- consumer QoS setup
- health summary wiring

- [ ] **Step 2: Run the client tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/client -v`

Expected: FAIL because the client package is empty.

- [ ] **Step 3: Implement the minimal client foundation**

Implement:
- config parsing and defaults
- one connection manager that tries endpoints in order
- channel helper for publish/consume setup
- publisher confirm support
- health summary struct

- [ ] **Step 4: Re-run the client tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/client -v`

Expected: PASS

### Task 3: Topology Declaration Layer

**Files:**
- Create: `internal/platform/mq/rabbitmq/topology/exchanges.go`
- Create: `internal/platform/mq/rabbitmq/topology/queues.go`
- Create: `internal/platform/mq/rabbitmq/topology/bindings.go`
- Create: `internal/platform/mq/rabbitmq/topology/quorum.go`
- Create: `internal/platform/mq/rabbitmq/topology/declare.go`
- Create: `internal/platform/mq/rabbitmq/topology/topology_test.go`

- [ ] **Step 1: Write the failing topology tests**

Cover:
- exchange names
- quorum queue arguments
- retry queue arguments
- DLQ bindings
- task routing keys

- [ ] **Step 2: Run the topology tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/topology -v`

Expected: FAIL because topology declarations do not exist.

- [ ] **Step 3: Implement topology declarations**

Implement:
- `astra.tasks`
- `astra.events`
- `astra.retry`
- `astra.dlx`
- quorum queues for repair/cleanup/rebalance/failover
- retry queues and DLQs
- queue bindings

- [ ] **Step 4: Re-run the topology tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/topology -v`

Expected: PASS

### Task 4: Message Contract Layer

**Files:**
- Create: `internal/platform/mq/contracts/envelope.go`
- Create: `internal/platform/mq/contracts/codec.go`
- Create: `internal/platform/mq/contracts/headers.go`
- Create: `internal/platform/mq/contracts/task_events.go`
- Create: `internal/platform/mq/contracts/domain_events.go`
- Create: `internal/platform/mq/contracts/contracts_test.go`

- [ ] **Step 1: Write the failing contract tests**

Cover:
- envelope encode/decode
- `message_id`, `event_id`, `trace_id`, `attempt` fields
- repair/cleanup/rebalance/failover task payloads

- [ ] **Step 2: Run the contract tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/contracts -v`

Expected: FAIL because contracts do not exist.

- [ ] **Step 3: Implement the message contracts**

Implement:
- stable envelope type
- JSON codec
- task payload types
- helper constructors for common headers

- [ ] **Step 4: Re-run the contract tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/contracts -v`

Expected: PASS

### Task 5: MDS Producer Integration

**Files:**
- Create: `internal/mds/mq/producer.go`
- Create: `internal/mds/mq/events.go`
- Modify: `cmd/mds/app.go`
- Modify: `internal/mds/coordinator/repairer.go`
- Modify: `internal/mds/coordinator/cleanup.go`
- Modify: `internal/mds/coordinator/rebalance.go`
- Modify: `internal/mds/coordinator/failover.go`

- [ ] **Step 1: Write the failing MDS producer tests**

Cover:
- repair planner publishes repair tasks
- cleanup planner publishes cleanup tasks
- rebalance planner publishes rebalance tasks
- failover planner publishes failover tasks

- [ ] **Step 2: Run the targeted producer tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds ./cmd/mds -run 'Test.*Publish.*Task' -v`

Expected: FAIL because RabbitMQ producer wiring does not exist.

- [ ] **Step 3: Implement the producer integration**

Implement:
- leader-side producer bootstrap in `cmd/mds/app.go`
- coordinator-to-MQ publishing hooks
- clear observability around publish success/failure

- [ ] **Step 4: Re-run the targeted tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds ./cmd/mds -run 'Test.*Publish.*Task' -v`

Expected: PASS

### Task 6: MDS Consumer Integration

**Files:**
- Create: `internal/mds/mq/consumer_repair.go`
- Create: `internal/mds/mq/consumer_cleanup.go`
- Create: `internal/mds/mq/consumer_rebalance.go`
- Create: `internal/mds/mq/consumer_failover.go`
- Create: `internal/mds/mq/orchestrator.go`
- Modify: `cmd/mds/app.go`

- [ ] **Step 1: Write the failing consumer tests**

Cover:
- repair consumer acks on success
- cleanup consumer acks on success
- rebalance consumer acks on success
- failover consumer acks on success

- [ ] **Step 2: Run the consumer tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/mq -v`

Expected: FAIL because consumers do not exist.

- [ ] **Step 3: Implement the consumers**

Implement:
- consumer handlers that decode contracts
- calls into the existing MDS/coordinator logic
- manual ack only on successful execution

- [ ] **Step 4: Re-run the consumer tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/mds/mq -v`

Expected: PASS

### Task 7: Retry, Dead Letter, and Idempotency

**Files:**
- Create: `internal/platform/mq/rabbitmq/retry/policy.go`
- Create: `internal/platform/mq/rabbitmq/retry/dlx.go`
- Create: `internal/platform/mq/rabbitmq/retry/delay.go`
- Create: `internal/platform/mq/rabbitmq/retry/attempts.go`
- Create: `internal/platform/mq/rabbitmq/idempotency/key.go`
- Create: `internal/platform/mq/rabbitmq/idempotency/store.go`
- Create: `internal/platform/mq/rabbitmq/idempotency/handler.go`
- Create: `internal/platform/mq/rabbitmq/retry/retry_test.go`
- Create: `internal/platform/mq/rabbitmq/idempotency/idempotency_test.go`

- [ ] **Step 1: Write the failing reliability tests**

Cover:
- retry queue routing after a transient failure
- DLQ routing after maximum attempts
- duplicate task detection by idempotency key

- [ ] **Step 2: Run the reliability tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/retry ./internal/platform/mq/rabbitmq/idempotency -v`

Expected: FAIL because retry and idempotency code does not exist.

- [ ] **Step 3: Implement retry and idempotency**

Implement:
- retry policy helper
- DLQ helper
- attempt extraction and increment
- idempotency key builder and handler wrapper

- [ ] **Step 4: Re-run the reliability tests**

Run: `GOCACHE=/tmp/go-cache go test ./internal/platform/mq/rabbitmq/retry ./internal/platform/mq/rabbitmq/idempotency -v`

Expected: PASS

### Task 8: RabbitMQ Integration Tests

**Files:**
- Modify: `test/integration/rabbitmq_cluster_smoke_test.go`
- Create: `test/integration/rabbitmq_topology_integration_test.go`
- Create: `test/integration/rabbitmq_publish_consume_test.go`
- Create: `test/integration/rabbitmq_retry_dlq_test.go`
- Create: `test/integration/rabbitmq_cluster_failover_test.go`
- Modify: `deploy/docker/rabbitmq-cluster/README.md`

- [ ] **Step 1: Write the failing integration tests**

Cover:
- cluster smoke test
- topology declaration on a real cluster
- publish/consume round trip
- retry and DLQ behavior
- single-node broker interruption while quorum queues stay available

- [ ] **Step 2: Run the integration tests to verify they fail**

Run: `GOCACHE=/tmp/go-cache go test ./test/integration -run TestRabbitMQ -v`

Expected: FAIL because RabbitMQ integration code is incomplete.

- [ ] **Step 3: Implement integration wiring and docs**

Implement:
- env-driven test setup
- README commands for cluster start/stop
- any missing bootstrap hooks needed by the tests

- [ ] **Step 4: Re-run the integration tests**

Run: `GOCACHE=/tmp/go-cache go test ./test/integration -run TestRabbitMQ -v`

Expected: PASS when the Docker cluster is running.
