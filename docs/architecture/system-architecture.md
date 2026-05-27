# AstraStorage 系统架构说明

## 1. 架构定位

AstraStorage 当前是一个分布式存储控制面 PoC。它已经打通 `gateway -> MDS -> datanode -> PostgreSQL` 的最小链路，用来验证文件元数据管理、文件分片、chunk 落盘、副本元数据、上传下载删除和基础 Kubernetes 部署是否可行。

当前系统不直接声明为生产级分布式存储。它的重点是：

- 证明核心组件可以协同工作。
- 证明元数据和真实字节数据可以分离。
- 证明 datanode 可以通过 StatefulSet 和 PVC 模拟有状态存储节点。
- 证明核心流程可以通过 smoke test 自动验证。

## 2. 总体架构

当前核心组件包括：

```text
Client
  |
  v
Gateway
  |
  +--> MDS ------------------> PostgreSQL
  |
  +--> DataNode
```

核心职责划分如下：

- `Gateway`：对外 HTTP 入口，负责把用户请求编排成对 MDS 和 datanode 的调用。
- `MDS`：元数据服务，负责目录、文件、chunk、副本、上传会话和节点状态。
- `DataNode`：数据节点，负责保存真实 chunk 字节。
- `PostgreSQL`：MDS 的持久化元数据存储。

系统中的数据分为两类：

- 元数据：目录、文件、chunk、replica、node、upload session，保存在 MDS 的 store 中，Kubernetes 默认后端是 PostgreSQL。
- 数据内容：真实文件字节，按 chunk 落到 datanode 的本地数据目录中。

## 3. MDS 元数据服务

MDS 是系统的控制面核心。它不保存真实文件内容，而是保存“文件在哪里、被切成几片、每片有哪些副本、节点是否健康”等信息。

当前 MDS 已经支持：

- 目录和 inode 管理。
- 文件元数据管理。
- upload session 管理。
- chunk 元数据管理。
- replica 元数据管理。
- datanode 注册和心跳。
- 上传目标分配。
- 下载计划构建。
- 文件删除和元数据清理。
- HTTP RPC、gRPC、`/healthz`、`/metrics`。

MDS 的核心数据关系是：

```text
inode -> file -> chunk -> replica -> node
file -> upload session
```

这些关系的含义是：

- `inode` 描述目录树结构。
- `file` 描述文件对象。
- `chunk` 描述文件分片。
- `replica` 描述某个 chunk 在某个 datanode 上的副本。
- `node` 描述 datanode 节点状态。
- `upload session` 描述一次上传过程。

MDS 的目标是成为控制面事实来源。Gateway、datanode 和后台修复逻辑都围绕 MDS 中的元数据决策工作。

## 4. DataNode 数据节点

DataNode 是数据面组件，负责保存真实 chunk 字节。它的职责更接近“存储节点”，不是元数据协调者。

当前 DataNode 已经支持：

- `PUT /chunks/<chunkID>` 写入 chunk。
- `GET /chunks/<chunkID>` 读取 chunk。
- `DELETE /chunks/<chunkID>` 删除 chunk。
- `POST /internal/replicate` 触发内部副本复制。
- 启动时向 MDS 注册节点。
- 周期性向 MDS 发送心跳。
- 上报容量和已使用空间。
- 暴露 `/healthz` 和 `/metrics`。

DataNode 当前使用本地文件系统作为最小存储后端。每个 chunk 会写成两个文件：

```text
DATANODE_DATA_DIR/chunks/<chunkID>.bin
DATANODE_DATA_DIR/chunks/<chunkID>.json
```

其中：

- `.bin` 保存真实 chunk 字节。
- `.json` 保存 chunk 元数据，例如 `chunk_id`、`file_id`、`size`、`checksum`、`stored_at`。

在 Kubernetes 中，DataNode 当前以单副本 `StatefulSet + PVC` 运行，数据目录挂载到：

```text
/data/datanode
```

这样 Pod 重启后可以重新挂载原 PVC，保留已经写入的 chunk 文件。

## 5. PostgreSQL 元数据存储

PostgreSQL 是当前 Kubernetes 部署下 MDS 的默认元数据后端。MDS 启动时会读取 `MDS_POSTGRES_DSN`，连接集群内 PostgreSQL，并运行 schema migration。

PostgreSQL 当前保存：

- inode 表。
- file 表。
- upload session 表。
- chunk 表。
- chunk replica 表。
- datanode node 表。
- replica plan 表。
- schema migration 记录。

PostgreSQL 只保存元数据，不保存真实文件内容。真实文件内容仍然在 datanode 的 chunk 文件里。

当前 PostgreSQL 以单副本 StatefulSet 运行，适合 PoC，不是 HA 数据库部署。生产化时需要考虑：

- PostgreSQL 高可用。
- 备份恢复。
- 连接池和连接数限制。
- schema migration 兼容性。
- 元数据表索引和容量增长。

## 6. 文件分片

当前 gateway 上传时会把文件内容切成固定大小 chunk。当前固定 chunk 大小是：

```text
4 MiB
```

分片规则是：

- 文件大小小于等于 `4 MiB` 时，只有一个 chunk。
- 文件大小超过 `4 MiB` 时，按 `4 MiB` 递增切片。
- 最后一个 chunk 可以小于 `4 MiB`。
- 每个 chunk 有独立 `chunkID`、offset、size 和 checksum。

分片后的信息会分别进入两个地方：

- chunk 字节写入 datanode。
- chunk 元数据提交给 MDS，并最终持久化到 PostgreSQL。

当前上传接口仍然基于 `content_base64`，也就是说 gateway 会先把完整文件内容解码到内存，再进行分片。这适合 PoC 小文件演示，但不适合正式大文件上传。

正式大文件上传应演进为：

- 客户端或 gateway 流式分片。
- 每个 chunk 单独上传和校验。
- gateway 不一次性持有完整文件。
- MDS 只记录上传会话和 chunk 元数据状态。

## 7. 副本管理

副本管理用于描述一个 chunk 在哪些 datanode 上存在副本，以及这些副本是否可读。

当前 replica 元数据包含：

- replica ID。
- file ID。
- chunk ID。
- node ID。
- role，例如 `primary` 或 `secondary`。
- state，例如 `ready` 或 `pending`。
- checksum。
- stored size。
- created / updated / verified 时间。

当前写入时的副本流程是：

1. gateway 向 MDS 申请上传目标。
2. MDS 基于健康节点和容量信息返回候选 datanode。
3. gateway 选择第一个目标作为 primary。
4. gateway 先把 chunk 写入 primary datanode。
5. 如果还有 secondary 目标，gateway 请求 primary datanode 通过 `/internal/replicate` 复制到其他 datanode。
6. gateway 将 primary 和 secondary 的写入结果提交给 MDS。
7. MDS 持久化 chunk 和 replica 元数据。

当前副本语义仍是 PoC 级：

- 上传路径仍以串行处理为主。
- 多副本拓扑和 Pod 级 datanode identity 还需要继续完善。
- pending 副本可以被后台 repairer 扫描并尝试补齐。
- 完整生产级副本一致性、回滚和审计仍待建设。

## 8. 上传流程

当前上传入口是 gateway 的 `POST /uploads`。

流程如下：

```text
Client
  -> Gateway
  -> MDS CreateFile
  -> MDS StartUpload
  -> MDS AllocateUploadTargets
  -> DataNode PutChunk
  -> DataNode ReplicateChunk
  -> MDS CommitChunk
  -> MDS CompleteUpload
  -> MDS VerifyUpload
```

详细步骤：

1. 客户端提交文件名、父目录、content type 和 `content_base64`。
2. gateway 解码文件内容并计算文件 checksum。
3. gateway 调用 MDS 创建文件元数据。
4. gateway 调用 MDS 启动 upload session。
5. gateway 按 `4 MiB` 切分内容。
6. 每个 chunk 上传前，gateway 向 MDS 申请上传目标。
7. gateway 将 chunk 写入 primary datanode。
8. gateway 触发 datanode 内部复制到 secondary 目标。
9. gateway 将 chunk、checksum 和 replica 结果提交给 MDS。
10. 全部 chunk 提交后，gateway 调用 MDS 完成上传。
11. gateway 调用 MDS 校验上传，让文件进入可读状态。

当前上传流程已经能跑通 PoC，但还不是正式大文件上传设计。

## 9. 下载流程

当前下载入口是 gateway 的 `GET /downloads/<fileID>`。

流程如下：

```text
Client
  -> Gateway
  -> MDS BuildDownloadPlan
  -> DataNode GetChunk
  -> Gateway returns file bytes
```

详细步骤：

1. 客户端向 gateway 请求下载文件。
2. gateway 向 MDS 请求下载计划。
3. MDS 根据 file、chunk、replica 和 node 信息生成下载计划。
4. 下载计划中每个 chunk 都包含优先节点和候选节点。
5. gateway 按 chunk 顺序从 datanode 读取数据。
6. 如果优先节点失败，gateway 可以尝试候选节点。
7. gateway 按 chunk index 拼接内容并返回客户端。

当前下载路径仍会在 gateway 聚合 chunk 内容后返回，后续应演进为边读边写的流式下载。

## 10. 删除流程

当前删除入口是 gateway 的 `DELETE /files/<fileID>`。

流程如下：

```text
Client
  -> Gateway
  -> MDS ListFileChunks
  -> MDS GetNode
  -> DataNode DeleteChunk
  -> MDS DeleteFile
```

详细步骤：

1. 客户端请求删除文件。
2. gateway 向 MDS 查询文件的 chunk 列表。
3. gateway 遍历每个 chunk 的 replica。
4. gateway 通过 MDS 查询 replica 所在 datanode 地址。
5. gateway 调用 datanode 删除对应 chunk 数据和 sidecar metadata。
6. 所有 chunk 删除成功后，gateway 调用 MDS 删除文件元数据。

当前删除流程已经覆盖“先删数据面，再删元数据”的基础闭环。还需要继续增强的部分包括：

- 删除失败后的重试和补偿。
- 目录级递归删除时的数据面清理。
- 上传失败残留 orphan chunk 的后台清理。

## 11. 故障恢复思路

当前故障恢复主要围绕副本修复和启动稳定性展开。

### 已具备的基础能力

当前系统已经具备这些恢复基础：

- datanode 会注册和心跳，MDS 能记录节点健康状态。
- replica 有 `ready` 和 `pending` 等状态。
- 上传时 secondary 副本失败可以被记录为 pending。
- MDS 中有 `PendingReplicaRepairer`，可以扫描 pending 副本并尝试补齐。
- repairer 会选择已有 ready 副本作为源节点。
- repairer 通过 datanode `/internal/replicate` 将 chunk 复制到目标节点。
- repairer 带有重试退避和单轮修复上限，避免持续打爆同一类失败。
- Kubernetes 中 MDS 会等待 PostgreSQL ready，datanode 会等待 MDS ready，减少冷启动 CrashLoop。

### 当前恢复流程

pending 副本修复的大致流程是：

```text
MDS repairer scans chunks
  -> find pending replica
  -> choose ready source replica
  -> choose target node
  -> call source datanode /internal/replicate
  -> update replica state in MDS
```

如果复制成功：

- 目标 replica 状态更新为 `ready`。
- 写入 size、checksum 和 verified 时间。

如果复制失败：

- replica 保持 `pending`。
- repairer 记录失败并延后下一次尝试。

### 后续生产化方向

当前恢复能力仍是 PoC 级。后续需要补齐：

- 基于事件或任务队列的 repair 调度，减少全量扫描。
- 更明确的节点失效检测和 failover 流程。
- replica plan 的完整生命周期管理。
- orphan chunk 清理。
- 删除失败补偿。
- 多副本读写一致性策略。
- 修复任务幂等和审计。
- 多 datanode 拓扑下的容量、故障域和负载均衡策略。

## 12. 当前边界

这份架构说明描述的是当前 PoC 已具备和已规划的架构形态。当前系统还不能被视为生产级分布式存储，主要边界是：

- `content_base64` 上传只适合小文件演示。
- gateway 上传和下载仍有内存聚合问题。
- datanode 仍是本地文件系统原型。
- PostgreSQL 和 MDS 还不是高可用部署。
- 多 datanode 副本拓扑还未完整落地。
- 安全、鉴权、限流、配额和审计还未补齐。

当前最准确的定位是：

```text
可运行、可验证、可继续演进的分布式存储控制面 PoC。
```
