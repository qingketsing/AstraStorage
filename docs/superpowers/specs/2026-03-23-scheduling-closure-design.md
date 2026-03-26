# Scheduling Closure Design

## Goals

把当前 `leader election + repairer + capacity-aware placement` 演进成一个可讲清楚、可持续运行的作品级调度闭环。

这一步要实现的不是“再多几个后台循环”，而是明确这条控制面链路：

1. 发现异常或失衡
2. 生成持久化调度意图
3. 物化目标副本
4. 复用现有 `repairer` 完成复制
5. 验证复制结果
6. 清理旧副本或残留元数据
7. 回到稳定状态

## Non-Goals

这一轮明确不做：

- 事件驱动或 watch 驱动调度
- Redis / RabbitMQ 参与调度
- zone / rack / region 故障域打分模型
- 复杂任务队列框架
- 完整 fencing / task ownership 系统
- K8s 侧调度集成

## Why This Design

当前代码已经有：

- `etcd`-backed leader election
- `repairer` 单活运行
- datanode 真实 `used bytes`
- 最小容量感知 allocator

但控制面还没有闭环：

- `repairer` 只能处理已经存在的 `pending replica`
- `failover.go` / `rebalance.go` 仍为空壳
- 没有统一表达“为什么要迁移、迁到哪里、做到哪一步”的持久化对象
- cleanup 还没有正式 controller

如果继续把逻辑分散到各个 loop 里，调度意图会不可见，`failover`、`rebalance`、`cleanup` 会彼此耦合，后面很难解释系统拓扑。

因此这一轮采用：

- `PG` 保存业务真相源和调度计划
- `etcd` 继续只负责 leader election
- `repairer` 继续承担复制执行
- `failover` / `rebalance` 负责生成计划
- `cleanup` 负责在目标副本 ready 后做收尾

## Approach Options

### Option A: Pure Scan-And-Act Loops

`failover`、`rebalance`、`cleanup` 都像 `repairer` 一样扫库后直接执行。

优点：

- 改动最小
- 不需要新增元数据模型

问题：

- 调度原因和状态不可见
- 多个 loop 容易重复或冲突
- 很难解释“当前系统在做什么”

### Option B: Persistent Replica Plans Around Existing Repairer

新增持久化 `ReplicaPlan`，planner 负责创建 plan 和 `pending replica`，`repairer` 负责复制，`cleanup` 负责收尾。

优点：

- 叙事清晰
- 不推翻现有 `repairer`
- 非常适合当前 `PG + etcd + repairer` 架构

缺点：

- 需要扩 metadata / store / postgres / memory backend

### Option C: General Task Engine

抽象统一 `SchedulerTask`，planner 只产任务，独立 executor 消费任务。

优点：

- 长期扩展性最强

问题：

- 当前阶段过大
- 容易做成框架工程

## Selected Approach

选择 **Option B**。

理由：

- 它能把“调度为什么发生、现在做到哪一步、什么时候算完成”说清楚
- 它复用现有 `repairer`，不会把整个控制面推倒重来
- 它让 `failover`、`rebalance`、`cleanup` 的职责边界自然分开

## Architecture Overview

### Control Plane Responsibilities

- `PostgreSQL`
  - file / chunk / replica / node 持久元数据
  - `ReplicaPlan` 持久调度意图
- `etcd`
  - leader election
- `MDS`
  - 发现异常与失衡
  - 创建 `ReplicaPlan`
  - 物化 `pending replica`
  - 启动和管理 controller loops
- `repairer`
  - 继续作为复制执行器，把 `pending -> ready`
- `cleanup`
  - 在目标副本 ready 后做旧副本删除、残留清理和 plan 完结

### Core Flow

#### Failover

1. `FailoverPlanner` 发现节点失联
2. 找出受影响的 chunk
3. 如果有效 ready 副本数低于目标副本数，创建 `failover ReplicaPlan`
4. 在目标节点物化新的 `pending replica`
5. `repairer` 把 `pending replica` 复制成 `ready`
6. `CleanupController` 清理旧失联副本元数据
7. `ReplicaPlan` 标记为 `done`

#### Rebalance

1. `RebalancePlanner` 发现高压节点与低压节点
2. 选出适合迁移的 replica
3. 创建 `rebalance ReplicaPlan`
4. 在目标节点物化新的 `pending replica`
5. `repairer` 完成复制
6. `CleanupController` 删除源节点旧副本
7. `ReplicaPlan` 标记为 `done`

## Metadata Additions

新增 `metadata.ReplicaPlan`，用来表达调度工单。

建议字段：

- `ID`
- `Type`
  - `failover`
  - `rebalance`
  - `cleanup`
- `ChunkID`
- `FileID`
- `SourceNodeID`
- `TargetNodeID`
- `RequiredBytes`
- `State`
  - `planned`
  - `materialized`
  - `copy_ready`
  - `cleanup_pending`
  - `done`
  - `failed`
- `Priority`
- `LastErrorCode`
- `LastErrorMessage`
- `RetryCount`
- `NextRetryAt`
- `CreatedAt`
- `UpdatedAt`
- `CompletedAt`

### ReplicaPlan Invariants

- 同一个 chunk 在同一种调度类型下，不允许存在两个未完成且目标相同的 plan
- `SourceNodeID` 和 `TargetNodeID` 不能相同
- `RequiredBytes` 不能为负数
- `done` plan 必须有最终状态时间
- `copy_ready` 只能在目标 replica 已经 `ready` 后出现

## Store Changes

扩 `store.Repository`，新增 `ReplicaPlanRepository`。

建议接口：

- `CreateReplicaPlan(ctx context.Context, plan *metadata.ReplicaPlan) error`
- `GetReplicaPlan(ctx context.Context, id string) (*metadata.ReplicaPlan, error)`
- `ListReplicaPlans(ctx context.Context, filter store.ReplicaPlanFilter) ([]metadata.ReplicaPlan, error)`
- `UpdateReplicaPlan(ctx context.Context, patch store.ReplicaPlanPatch) error`
- `DeleteReplicaPlan(ctx context.Context, id string) error`

为支持 planner / cleanup，还需要补这些查询或变更接口：

- `ListChunksByNode(ctx context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error)`
- `RemoveChunkReplica(ctx context.Context, selector store.ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error`

这些接口需要同时落到：

- `internal/mds/store/memory_*`
- `internal/platform/postgres/repository/*`

## Allocator Changes

当前 allocator 只做到“健康 + 地址可用 + 可用容量大于 0 + 稳定排序”。

这一轮继续沿 allocator 边界演进，新增：

- `RequiredPlacementBytes(chunk metadata.ChunkMetadata) int64`
- `SelectPlacementTargets(req PlacementRequest) []metadata.NodeInfo`
- `CountEffectiveReadyReplicas(chunk metadata.ChunkMetadata, nodeIndex map[metadata.NodeID]metadata.NodeInfo) int`
- `BuildReplicaExclusionSet(chunk metadata.ChunkMetadata) map[metadata.NodeID]struct{}`

### New Placement Rule

从“可用容量大于 0”提升到：

- `available >= required_bytes`

其中 `required_bytes` 优先取 ready replica 的 `StoredSize`，没有时退回 `chunk.Size`。

这会补上当前缺失的 `chunk-size-aware` 最小剩余空间约束。

## Controllers

### 1. FailoverPlanner

职责：

- 找出失联节点
- 找出受影响 chunk
- 判断是否需要补副本
- 选择新 target
- 创建 `failover plan`
- 物化 `pending replica`

建议函数：

- `Run(ctx context.Context)`
- `PlanOnce(ctx context.Context) error`
- `listUnavailableNodes(ctx context.Context, now time.Time) ([]metadata.NodeInfo, error)`
- `planNodeFailover(ctx context.Context, node metadata.NodeInfo, now time.Time) error`
- `planChunkFailover(ctx context.Context, chunk metadata.ChunkMetadata, failedNodeID metadata.NodeID, now time.Time) error`
- `materializePendingReplica(ctx context.Context, plan metadata.ReplicaPlan, target metadata.NodeInfo, now time.Time) error`

### 2. RebalancePlanner

职责：

- 识别高压节点和低压节点
- 选出可迁移副本
- 创建 `rebalance plan`
- 物化 `pending replica`

建议函数：

- `Run(ctx context.Context)`
- `PlanOnce(ctx context.Context) error`
- `classifyNodePressure(nodes []metadata.NodeInfo) (overfull []metadata.NodeInfo, underfull []metadata.NodeInfo)`
- `selectReplicaToMove(ctx context.Context, source metadata.NodeInfo, targets []metadata.NodeInfo) (*RebalanceMove, error)`
- `planReplicaMove(ctx context.Context, move RebalanceMove, now time.Time) error`

### 3. CleanupController

职责：

- 检查哪些 plan 已经完成复制
- 对 rebalance 删除旧副本
- 对 failover 清理长期失联的旧副本元数据
- 更新 plan 状态

建议函数：

- `Run(ctx context.Context)`
- `CleanupOnce(ctx context.Context) error`
- `finalizeCompletedPlans(ctx context.Context, now time.Time) error`
- `deleteSourceReplica(ctx context.Context, plan metadata.ReplicaPlan, now time.Time) error`
- `purgeLostReplicaMetadata(ctx context.Context, plan metadata.ReplicaPlan, now time.Time) error`
- `failOrRetryPlan(ctx context.Context, plan metadata.ReplicaPlan, err error, now time.Time) error`

### 4. Supervisor

把现有 supervisor 从“只管 repairer”扩成“统一管理 leader-scoped loops”。

建议顺序：

- `repairer`
- `failover`
- `cleanup`
- `rebalance`

这样能先保证容灾，再做主动迁移。

## Error Handling and Retry

### Failover / Rebalance Planning

- 规划失败不直接修改业务元数据
- 创建 plan 和物化 pending replica 尽量放在同一事务
- 已存在未完成 plan 时，不重复创建

### Cleanup

- datanode 删除失败时，不立即丢 plan
- 更新 `RetryCount` 和 `NextRetryAt`
- 达到阈值后 plan 进入 `failed`

### Repairer Reuse

- `repairer` 不理解 plan 类型
- 它只关心 `pending replica`
- plan 的状态推进由 planner / cleanup 完成

## Observability

新增调度指标：

- `astrastorage_mds_failover_plans_total{result}`
- `astrastorage_mds_rebalance_plans_total{result}`
- `astrastorage_mds_cleanup_runs_total{result}`
- `astrastorage_mds_replica_plans_total{type,state}`
- `astrastorage_mds_replica_plan_retries_total{type}`

新增日志字段：

- `plan_id`
- `plan_type`
- `chunk_id`
- `source_node_id`
- `target_node_id`
- `required_bytes`
- `term`
- `run_id`

## Testing Strategy

### Unit Tests

- allocator 的 `required_bytes` 和 `chunk-size-aware` 过滤
- failover 失联节点判断
- rebalance 压力分类
- cleanup plan 状态推进

### Repository Tests

- `ReplicaPlan` 在 memory / postgres 的 CRUD
- `ListChunksByNode`
- `RemoveChunkReplica`

### Integration Tests

- 单节点失联 -> failover plan -> repair -> cleanup -> plan done
- 高压节点 -> rebalance plan -> repair -> cleanup -> plan done
- leader 切换后 loops 能继续推进已有 plan

## Delivery Plan

建议分两阶段实现，但保持同一套模型：

### Phase 1

- `ReplicaPlan` 元模型和 store
- `chunk-size-aware` allocator
- `FailoverPlanner`
- `CleanupController`
- supervisor 扩展

### Phase 2

- `RebalancePlanner`
- 调度指标和手工验证文档完善
- 更强的 integration / e2e

## Open Tradeoff

本设计刻意不在第一轮引入通用任务引擎。

原因：

- 当前系统还没有多个异构执行器
- `repairer` 已经是稳定复制执行器
- 先把存储控制面语义闭起来，比先抽框架更重要

## Summary

这一步完成后，系统会从“有 leader、会 repair、会按容量挑节点”升级到：

- 会发现副本缺口
- 会持久化调度意图
- 会复用 repair 完成复制
- 会在复制完成后自动清理和完结

也就是形成真正可讲清楚的最小调度闭环。
