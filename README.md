# AstraStorage

AstraStorage 是一个面向 Kubernetes 场景的分布式云存储项目。

当前项目已经实现了一个可运行的工程原型：`MDS` 负责元数据控制面，`gateway` 负责对外上传/下载/删除入口，`datanode` 负责真实 chunk 字节落盘。项目还包含 PostgreSQL、Redis、RabbitMQ、etcd、Prometheus 和 Kubernetes 相关适配能力。

当前定位更准确地说是：

```text
分布式存储 PoC 前夜 / 工程原型
```

核心链路已经存在，但要变成可交付 PoC，还需要补齐镜像构建、Kubernetes 默认持久化路径、StatefulSet/PVC datanode 和一键验收脚本。

## Current Capabilities

### MDS metadata service

MDS 是当前最成熟的模块，入口在 [cmd/mds](/home/qingke/AstraStorage/cmd/mds)，核心实现位于 [internal/mds](/home/qingke/AstraStorage/internal/mds)。

已经支持：

- 目录和文件创建
- inode / file 元数据管理
- upload session 管理
- chunk 和 replica 元数据管理
- 节点注册、心跳和上传目标分配
- 上传启动、提交 chunk、完成上传和最终校验
- 校验失败记录与 retry upload
- rename、move、delete 和递归目录删除的元数据清理
- 下载计划构建
- JSON over HTTP API
- gRPC API
- `/healthz`
- `/metrics`

核心关系是：

```text
inode -> file -> chunk -> replica -> node
file -> upload session
```

### Gateway

Gateway 入口在 [cmd/gateway](/home/qingke/AstraStorage/cmd/gateway)，实现位于 [internal/gateway](/home/qingke/AstraStorage/internal/gateway)。

当前提供：

- `GET /healthz`
- `POST /directories`
- `GET /directories/<inodeID>/children?limit=<n>&offset=<n>`
- `POST /uploads`
- `GET /downloads/<fileID>`
- `GET /files/<fileID>`
- `GET /files/<fileID>/chunks`
- `GET /files/<fileID>/download-plan`
- `DELETE /files/<fileID>`
- `/metrics`

当前 `POST /uploads` 仍是 `content_base64` 驱动的串行多 chunk 上传路径，适合 smoke test、小文件演示和当前 PoC 验证。正式大文件路径后续应演进为流式上传。

### Datanode

Datanode 入口在 [cmd/datanode](/home/qingke/AstraStorage/cmd/datanode)，实现位于 [internal/datanode](/home/qingke/AstraStorage/internal/datanode)。

当前提供：

- `GET /healthz`
- `PUT /chunks/<chunkID>`
- `GET /chunks/<chunkID>`
- `DELETE /chunks/<chunkID>`
- `POST /internal/replicate`
- `/metrics`

Datanode 会把 chunk 数据和 sidecar metadata 落到本地目录，并支持向 MDS 注册、发送心跳、上报容量和 used bytes。

### Platform integrations

当前已经有这些基础设施适配：

- memory store，用于单元测试和本地快速开发
- PostgreSQL repository 和 migration
- Redis client/cache/lock/warmup
- RabbitMQ client/topology/retry/idempotency
- etcd leader election
- Prometheus metrics 和结构化日志

这些能力不是所有环境的默认必需项。当前推荐方向见 [scope-reduction-plan.md](/home/qingke/AstraStorage/docs/architecture/scope-reduction-plan.md)。

## Development

在仓库根目录运行：

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
```

本地启动最小三进程链路：

```bash
GOCACHE=/tmp/go-cache go run ./cmd/mds
```

```bash
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
  GOCACHE=/tmp/go-cache \
  go run ./cmd/datanode
```

```bash
GATEWAY_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
  GATEWAY_DATANODE_BASE_URL=http://127.0.0.1:10080 \
  GOCACHE=/tmp/go-cache \
  go run ./cmd/gateway
```

默认地址：

```text
MDS:      http://127.0.0.1:8080
Datanode: http://127.0.0.1:10080
Gateway:  http://127.0.0.1:11080
```

常用环境变量：

```bash
MDS_HTTP_ADDR=:8080
MDS_GRPC_ADDR=:9090
MDS_STORE_BACKEND=memory
MDS_POSTGRES_DSN=postgres://postgres:postgres@127.0.0.1:5432/astra_test?sslmode=disable
MDS_REPAIR_INTERVAL=15s
MDS_REPAIR_HTTP_TIMEOUT=5s
MDS_REPAIR_RETRY_BACKOFF=30s
MDS_REPAIR_MAX_REPLICAS_PER_RUN=32

DATANODE_HTTP_ADDR=:10080
DATANODE_DATA_DIR=./data/datanode
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080
DATANODE_CAPACITY_BYTES=10737418240

GATEWAY_HTTP_ADDR=:11080
GATEWAY_MDS_HTTP_BASE_URL=http://127.0.0.1:8080
GATEWAY_DATANODE_BASE_URL=http://127.0.0.1:10080
```

## HTTP API

MDS 当前对外入口是 JSON over HTTP：

- `GET /healthz`
- `GET /metrics`
- `POST /rpc/<method>`

例如创建文件：

```bash
curl -X POST http://127.0.0.1:8080/rpc/mds.create_file \
  -H 'Content-Type: application/json' \
  -d '{
    "InodeID": "demo-file-inode",
    "FileID": "demo-file",
    "ParentID": "root",
    "Name": "demo.txt",
    "Size": 128
  }'
```

Gateway 上传示例见 [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)。

Gateway 当前是 PoC 演示的主要入口：

- `POST /directories`：创建目录，缺省 `parent_id` 时创建在 `root` 下。
- `GET /directories/<inodeID>/children`：查看目录子项，支持 `limit` 和 `offset`。
- `POST /uploads`：上传小文件并完成 MDS 元数据、datanode chunk 写入和上传校验。
- `GET /files/<fileID>`：查看文件元信息。
- `GET /files/<fileID>/chunks`：查看文件 chunk 和副本分布。
- `GET /files/<fileID>/download-plan`：查看 MDS 为下载生成的读取计划。
- `GET /downloads/<fileID>`：按下载计划从 datanode 读取并返回文件内容。
- `DELETE /files/<fileID>`：删除 datanode chunk 后删除 MDS 文件元数据。

## gRPC API

MDS 也提供 gRPC server，对应 proto 在 [mds.proto](/home/qingke/AstraStorage/internal/mds/grpcpb/mds.proto)。

启用方式：

```bash
MDS_HTTP_ADDR=:8080 \
MDS_GRPC_ADDR=:9090 \
GOCACHE=/tmp/go-cache \
go run ./cmd/mds
```

当前 HTTP 是主要联调路径。gRPC 保留为第二协议入口，后续是否作为正式外部 API 继续推进，见 [scope-reduction-plan.md](/home/qingke/AstraStorage/docs/architecture/scope-reduction-plan.md)。

## Monitoring

三个服务都暴露 Prometheus 格式的 `/metrics`：

```text
MDS:      http://127.0.0.1:8080/metrics
Datanode: http://127.0.0.1:10080/metrics
Gateway:  http://127.0.0.1:11080/metrics
```

本地 Prometheus 和 Alertmanager 配置位于 [deploy/docker/monitor](/home/qingke/AstraStorage/deploy/docker/monitor)，部署目录总览见 [deploy/README.md](/home/qingke/AstraStorage/deploy/README.md)。

启动：

```bash
docker compose -f deploy/docker/monitor/docker-compose.yml up
```

访问：

```text
Prometheus:   http://127.0.0.1:9090
Alertmanager: http://127.0.0.1:9093
```

运行 smoke check：

```bash
bash scripts/smoke/monitoring-smoke.sh
```

更多说明见 [prometheus-monitoring.md](/home/qingke/AstraStorage/docs/architecture/prometheus-monitoring.md) 和 [observability-demo.md](/home/qingke/AstraStorage/docs/architecture/observability-demo.md)。

## Kubernetes

Kubernetes manifests 位于 [deploy/k8s](/home/qingke/AstraStorage/deploy/k8s)，细分目录说明见 [deploy/k8s/README.md](/home/qingke/AstraStorage/deploy/k8s/README.md)。

当前包含：

- `base`: `astrastorage` namespace
- `postgres`: PostgreSQL StatefulSet + Service + Secret + ConfigMap + PVC
- `mds`: MDS Deployment + Service
- `datanode`: Datanode Deployment + Service
- `gateway`: Gateway Deployment + Service
- `monitor`: ServiceMonitor + PrometheusRule

应用顺序：

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/postgres
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
kubectl apply -k deploy/k8s/monitor
```

当前 K8s manifests 使用本地开发镜像：

```text
astrastorage/mds:local
astrastorage/datanode:local
astrastorage/gateway:local
```

应用镜像 Dockerfile 位于 [deploy/docker/app](/home/qingke/AstraStorage/deploy/docker/app)。

构建：

```bash
bash scripts/build-images.sh
```

如果使用本地 `minikube` 集群，还需要先把镜像导入节点：

```bash
minikube image load astrastorage/mds:local
minikube image load astrastorage/datanode:local
minikube image load astrastorage/gateway:local
```

如果使用 `kind`，则改用：

```bash
kind load docker-image astrastorage/mds:local
kind load docker-image astrastorage/datanode:local
kind load docker-image astrastorage/gateway:local
```

当前 K8s 配置仍是开发拓扑：

- MDS 默认使用单副本 PostgreSQL StatefulSet 作为 metadata backend
- Datanode 默认使用单副本 `StatefulSet + PVC`
- 当前文档和脚本默认面向本地镜像装载，不是远端镜像仓库发布流

剩余最重要的 PoC 缺口是 datanode 多副本拓扑与统一 smoke 脚本。更多说明见 [kubernetes-deployment.md](/home/qingke/AstraStorage/docs/architecture/kubernetes-deployment.md)。

## PostgreSQL Integration Test

本地复现 PostgreSQL 集成测试：

```bash
docker run -d --rm \
  --name astra-pg-it \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=astra_test \
  -p 127.0.0.1:55432:5432 \
  postgres:16-alpine
```

```bash
MDS_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable' \
GOCACHE=/tmp/go-cache \
go test ./test/integration -v
```

清理：

```bash
docker stop astra-pg-it
```

## CI

GitHub Actions 配置位于 [.github/workflows/ci.yml](/home/qingke/AstraStorage/.github/workflows/ci.yml)。

当前 CI 会：

- 启动 `postgres:16-alpine`
- 注入 `MDS_TEST_POSTGRES_DSN`
- 运行 `go test ./...`
- 运行 `go build ./...`

## Documentation Map

文档目录说明见 [docs/README.md](/home/qingke/AstraStorage/docs/README.md)。

如果你要看一份面向演示和交付的简版说明，直接看 [poc.md](/home/qingke/AstraStorage/docs/poc.md)。

建议阅读顺序：

1. [system-overview.md](/home/qingke/AstraStorage/docs/architecture/system-overview.md)
2. [mds-overview.md](/home/qingke/AstraStorage/docs/architecture/mds-overview.md)
3. [mds-invariants.md](/home/qingke/AstraStorage/docs/architecture/mds-invariants.md)
4. [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)
5. [prometheus-monitoring.md](/home/qingke/AstraStorage/docs/architecture/prometheus-monitoring.md)
6. [kubernetes-deployment.md](/home/qingke/AstraStorage/docs/architecture/kubernetes-deployment.md)
7. [scope-reduction-plan.md](/home/qingke/AstraStorage/docs/architecture/scope-reduction-plan.md)
8. [technical-debt-roadmap.md](/home/qingke/AstraStorage/docs/architecture/technical-debt-roadmap.md)

如果需要重启 CLI 后让新会话继续接手当前项目，请使用 [session-handoff.md](/home/qingke/AstraStorage/docs/architecture/session-handoff.md)。

## PoC Gap

当前项目要成为可交付 PoC，优先补齐：

1. 统一镜像构建和 kind/minikube 加载脚本。
2. Datanode 默认切到 `StatefulSet + PVC`。
3. 端到端 PoC smoke 脚本，覆盖上传、下载、校验、删除和指标检查。
4. `content_base64` 上传路径降级为 smoke/small-file path，并设计正式流式上传。

这些方向与 [scope-reduction-plan.md](/home/qingke/AstraStorage/docs/architecture/scope-reduction-plan.md) 保持一致。
