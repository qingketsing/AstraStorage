# AstraStorage

AstraStorage 是一个面向 Kubernetes 场景的分布式云存储项目。
当前仓库的实现重点是 Metadata Service (`MDS`)，也就是文件系统目录树、文件元数据、chunk、副本、节点和上传会话的控制面。
仓库现在也已经有了一个最小 `data node` 原型，用于真正落盘和读取 chunk。
同时也有了一个最小 `gateway` 原型，用于承接后续上传/下载入口。

## Current Scope

当前活跃入口是 [cmd/mds](/home/qingke/AstraStorage/cmd/mds)。
新增的最小数据节点入口在 [cmd/datanode](/home/qingke/AstraStorage/cmd/datanode)。
新增的最小网关入口在 [cmd/gateway](/home/qingke/AstraStorage/cmd/gateway)。
MDS 已经打通了这些核心链路：

- 创建目录和文件
- 启动上传、提交 chunk、完成上传、最终校验
- 校验失败后的失败记录与重试恢复
- rename、move、delete
- 构建下载计划

更多架构背景可以从 [PROJECT_STRUCTURE.md](/home/qingke/AstraStorage/PROJECT_STRUCTURE.md) 和 [docs/architecture](/home/qingke/AstraStorage/docs/architecture) 开始读。
HTTP 接口契约和错误模型见 [mds-http.md](/home/qingke/AstraStorage/docs/architecture/mds-http.md)。
gRPC 契约和 proto 入口见 [mds-grpc.md](/home/qingke/AstraStorage/docs/architecture/mds-grpc.md)。
手工联调步骤、`curl` 示例和 PostgreSQL 分片查询见 [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)。
如果需要重启 CLI 后让新会话继续接手当前项目，请使用 [session-handoff.md](/home/qingke/AstraStorage/docs/architecture/session-handoff.md)。
当前已知增强项和技术债路线图见 [technical-debt-roadmap.md](/home/qingke/AstraStorage/docs/architecture/technical-debt-roadmap.md)。

## Development

在仓库根目录运行：

```bash
go test ./...
go build ./...
go run ./cmd/mds
go run ./cmd/datanode
go run ./cmd/gateway
```

`go run ./cmd/mds` 现在会启动一个真实 HTTP 服务，默认监听 `:8080`。

如果本地默认 Go cache 路径不可写，可以像下面这样执行：

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
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
GATEWAY_HTTP_ADDR=:11080
GATEWAY_MDS_HTTP_BASE_URL=http://127.0.0.1:8080
GATEWAY_DATANODE_BASE_URL=http://127.0.0.1:10080
```

## Data Node API

最小 data node 当前提供这些 HTTP 接口：

- `GET /healthz`
- `PUT /chunks/<chunkID>`
- `GET /chunks/<chunkID>`
- `DELETE /chunks/<chunkID>`

它的实现位于 [internal/datanode](/home/qingke/AstraStorage/internal/datanode)，默认会把 chunk 数据和 sidecar metadata 落到 `DATANODE_DATA_DIR`。

## Gateway API

最小 gateway 当前提供这些 HTTP 接口：

- `GET /healthz`
- `POST /uploads`
- `GET /downloads/<fileID>`
- `DELETE /files/<fileID>`

当前状态：

- `GET /healthz` 已实现，会同时探测 MDS 和 datanode
- `POST /uploads` 已实现，当前是 `content_base64` 驱动的串行多 chunk 上传 MVP，主节点落盘后通过内部复制 RPC 向副本节点扇出
- `GET /downloads/<fileID>` 已实现，会按下载计划顺序拉取 chunk，并在候选节点间回退
- `DELETE /files/<fileID>` 已实现，会先清理 datanode 上的 chunk，再删除 MDS 元数据

它的实现位于 [internal/gateway](/home/qingke/AstraStorage/internal/gateway)。

## HTTP API

当前对外入口是 JSON over HTTP：

- `GET /healthz`
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

例如查看健康状态：

```bash
curl http://127.0.0.1:8080/healthz
```

## gRPC API

第二套正式接口是 gRPC，对应 proto 在 [mds.proto](/home/qingke/AstraStorage/internal/mds/grpcpb/mds.proto)。

启用方式：

```bash
MDS_HTTP_ADDR=:8080 \
MDS_GRPC_ADDR=:9090 \
go run ./cmd/mds
```

如果你只想验证 proto 和 server 实现，不需要真实端口，仓库里已经有基于 `bufconn` 的测试，位置在 [grpc_test.go](/home/qingke/AstraStorage/internal/mds/rpc/grpc_test.go)。

## CI

仓库已经接入 GitHub Actions，配置文件在 [ci.yml](/home/qingke/AstraStorage/.github/workflows/ci.yml)。

当前 CI 会在 `push`、`pull_request` 和手动触发时执行：

- 启动 `postgres:16-alpine` service
- 注入 `MDS_TEST_POSTGRES_DSN`
- 运行 `go test ./...`
- 运行 `go build ./...`

这意味着 [postgres_mds_integration_test.go](/home/qingke/AstraStorage/test/integration/postgres_mds_integration_test.go) 在 CI 中不会被跳过，而是会真实连接 PostgreSQL 跑上传主链路和失败恢复链路。

## Local PostgreSQL Integration Test

如果你想在本地手工复现 CI 里的 PostgreSQL 集成测试，可以直接起一个临时容器：

```bash
docker run -d --rm \
  --name astra-pg-it \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=astra_test \
  -p 127.0.0.1:55432:5432 \
  postgres:16-alpine
```

然后运行：

```bash
MDS_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable' \
GOCACHE=/tmp/go-cache \
go test ./test/integration -v
```

测试完成后停止容器：

```bash
docker stop astra-pg-it
```
