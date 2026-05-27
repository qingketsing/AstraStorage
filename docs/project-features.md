# AstraStorage 项目功能介绍

## 1. 项目定位

AstraStorage 是一个面向 Kubernetes 场景的分布式云存储工程原型。当前阶段的重点是验证分布式存储控制面的最小闭环，而不是直接交付生产级存储系统。

当前 PoC 已经能够证明：

- `gateway`、`MDS`、`datanode`、PostgreSQL 可以组成一条完整链路。
- 文件元数据可以持久化到 PostgreSQL。
- 文件内容可以被拆成 chunk 并落盘到 datanode。
- Kubernetes 中可以通过 `StatefulSet + PVC` 让 datanode 具备基础持久化能力。
- 核心上传、查询、下载、删除流程可以通过 smoke test 自动验证。

## 2. 系统组成

### Gateway

`gateway` 是对外 HTTP 入口。用户不会直接访问 MDS 或 datanode，而是通过 gateway 发起目录、上传、下载、查询和删除请求。

当前 gateway 负责：

- 提供 PoC 演示 API。
- 调用 MDS 创建文件、启动上传、获取上传目标和下载计划。
- 调用 datanode 写入、读取和删除 chunk。
- 对外屏蔽后端组件的调用细节。
- 暴露 `/healthz` 和 `/metrics`。

### MDS

`MDS` 是 Metadata Service，也就是元数据控制面。它负责维护目录、文件、chunk、副本和节点状态。

当前 MDS 负责：

- 目录和 inode 管理。
- 文件元数据管理。
- upload session 管理。
- chunk 和 replica 元数据管理。
- datanode 注册、心跳和容量信息记录。
- 上传目标分配。
- 下载计划构建。
- 删除文件和清理相关元数据。
- 暴露 HTTP RPC、gRPC、`/healthz` 和 `/metrics`。

### Datanode

`datanode` 是数据节点，负责保存真实 chunk 字节。

当前 datanode 负责：

- 接收 chunk 写入。
- 按 chunk ID 读取数据。
- 删除指定 chunk。
- 保存 chunk sidecar metadata。
- 向 MDS 注册节点并发送心跳。
- 上报容量和已使用字节数。
- 暴露 `/healthz` 和 `/metrics`。

### PostgreSQL

PostgreSQL 是当前 MDS 的默认持久化后端，用于保存元数据。

当前 PostgreSQL 保存：

- inode / file 元数据。
- upload session。
- chunk 元数据。
- replica 元数据。
- datanode 节点信息。
- schema migration 记录。

## 3. 已实现的核心功能

### 目录与文件元数据

项目已经支持基础目录和文件元数据能力：

- 创建目录。
- 查询目录子项。
- 创建文件元数据。
- 查询文件详情。
- 查询文件 chunk 列表。
- 构建文件下载计划。

### 小文件上传

当前 PoC 上传接口使用 `content_base64`。客户端把小文件内容编码成 base64 字符串，通过 JSON 请求提交给 gateway。

上传过程中，gateway 会：

1. 解码 `content_base64`。
2. 按固定大小拆分 chunk。
3. 向 MDS 创建文件并启动上传会话。
4. 向 MDS 申请上传目标 datanode。
5. 将 chunk 写入 datanode。
6. 向 MDS 提交 chunk 和 replica 元数据。
7. 完成上传并进行校验。

当前 chunk 大小为 `4 MiB`。datanode 会将每个 chunk 写成两个文件：

```text
DATANODE_DATA_DIR/chunks/<chunkID>.bin
DATANODE_DATA_DIR/chunks/<chunkID>.json
```

其中 `.bin` 保存真实字节，`.json` 保存 chunk 元数据。

### 文件下载

下载时，gateway 会先向 MDS 获取下载计划，再根据计划从 datanode 读取 chunk，最后返回给客户端。

当前下载流程包括：

1. 查询文件下载计划。
2. 获取每个 chunk 的候选 datanode。
3. 从 datanode 读取 chunk 内容。
4. 按 chunk index 拼接返回。

### 文件删除

当前文件删除已经覆盖元数据和数据面基础闭环：

1. gateway 查询文件 chunk 和 replica。
2. gateway 根据 replica 找到 datanode。
3. gateway 调用 datanode 删除 chunk。
4. gateway 调用 MDS 删除文件元数据。

这能避免只删除元数据、不清理真实 chunk 的问题。

### 节点注册与心跳

datanode 启动后会向 MDS 注册自己，并周期性发送心跳。

MDS 记录：

- 节点 ID。
- 节点访问地址。
- 节点容量。
- 节点已使用空间。
- 节点健康状态。
- 最近心跳时间。

这些信息是后续做容量感知放置、调度和副本恢复的基础。

## 4. Kubernetes 部署能力

当前项目已经具备基础 Kubernetes 部署能力，manifests 位于 `deploy/k8s/`。

当前包含：

- `base`：创建 `astrastorage` namespace。
- `postgres`：PostgreSQL `StatefulSet + Service + Secret + ConfigMap + PVC`。
- `mds`：MDS `Deployment + Service`。
- `datanode`：datanode `StatefulSet + PVC + Service + Headless Service`。
- `gateway`：gateway `Deployment + Service`。
- `monitor`：Prometheus Operator 使用的 `ServiceMonitor` 和 `PrometheusRule`。

datanode 当前已经从普通 Deployment 改为 StatefulSet，并通过 PVC 挂载数据目录：

```text
/data/datanode
```

这样 datanode Pod 重启后，可以重新挂载原 PVC，保留已经写入的 chunk 数据。

MDS 和 datanode 还增加了启动依赖等待：

- MDS 启动前等待 PostgreSQL ready。
- datanode 启动前等待 MDS `/healthz` ready。

这降低了集群冷启动时出现瞬时 `CrashLoopBackOff` 的概率。

## 5. 观测与监控能力

当前 `mds`、`datanode` 和 `gateway` 都暴露 Prometheus 格式的 `/metrics`。

已覆盖的基础指标包括：

- HTTP 请求量。
- RPC 请求量。
- 请求延迟。
- 错误统计。
- gateway 上传、下载、删除相关指标。
- datanode chunk put/get/delete 相关指标。
- datanode 当前存储 chunk 数量和使用字节数。

项目已经包含：

- 本地 Prometheus / Alertmanager Docker Compose 配置。
- Kubernetes `ServiceMonitor`。
- Kubernetes `PrometheusRule`。
- 监控 smoke 脚本。

需要注意的是，Kubernetes `monitor` 目录依赖 Prometheus Operator CRD。如果目标集群没有安装 Prometheus Operator，监控 manifests 不能直接 apply。

## 6. PoC 验证能力

项目已经提供端到端 smoke 脚本：

```bash
bash scripts/poc-smoke.sh
```

该脚本用于验证已经运行中的 PoC 环境，覆盖：

- gateway 健康检查。
- 上传小文件。
- 查询文件元数据。
- 下载文件。
- 校验下载内容和上传内容一致。
- 删除文件。
- 删除后再次查询确认文件不可用。

这个脚本可以证明核心链路可用，但不能证明生产级性能、可用性和容灾能力。

## 7. 当前限制

当前项目仍处于 PoC / 工程原型阶段，主要限制包括：

- 上传接口仍基于 `content_base64`，只适合小文件演示。
- gateway 仍会把上传内容读入内存，不适合大文件和高并发。
- datanode 当前是单副本持久化拓扑，还不是多 datanode 副本调度拓扑。
- PostgreSQL 是单副本 PoC 部署，不是 HA 架构。
- MDS 还不是多副本高可用。
- 监控告警基础已经具备，但 dashboard 和告警通知链路还不完整。
- 鉴权、限流、审计、TLS、配额等生产能力尚未补齐。

## 8. 后续演进方向

后续更合理的演进顺序是：

1. 将上传从 `content_base64` 演进为流式分片上传。
2. 支持多 datanode 拓扑和 Pod 级 datanode identity。
3. 完善 placement / allocator，让 MDS 能做更真实的副本放置。
4. 补齐副本修复、失败补偿和 orphan chunk 清理。
5. 完善 Prometheus dashboard、Alertmanager 通知和运行手册。
6. 补齐鉴权、限流、配额和审计。
7. 推进 MDS 和 PostgreSQL 的高可用部署。

## 9. 一句话总结

AstraStorage 当前已经从单纯的元数据服务实现，推进到可以在 Kubernetes 中跑通 `gateway -> MDS -> datanode -> PostgreSQL` 核心链路的分布式存储控制面 PoC。它已经适合做功能演示、部署验证和后续架构演进，但还不能被描述为生产级分布式存储系统。
