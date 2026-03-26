# AstraStorage 手工测试指南

这份文档用于手工验证当前最小闭环：

- `gateway` 上传文件
- `gateway` 下载文件
- `gateway` 删除文件
- `MDS` 元数据落到 PostgreSQL
- 用 PostgreSQL 直接查询 chunk 所在节点和副本状态

## 1. 前置条件

建议本地准备这些工具：

- `go`
- `curl`
- `docker`
- `psql`
- `base64`
- `etcdctl`（可选，仅在验证 leader election 时使用）

下面示例默认使用这些地址：

- `MDS`: `http://127.0.0.1:8080`
- `gateway`: `http://127.0.0.1:11080`
- `datanode-1`: `http://127.0.0.1:10081`
- `datanode-2`: `http://127.0.0.1:10082`
- `datanode-3`: `http://127.0.0.1:10083`
- `PostgreSQL`: `127.0.0.1:55432`
- `etcd`: `127.0.0.1:2379`

## 2. 启动 PostgreSQL

先起一个临时 PostgreSQL：

```bash
docker run -d --rm \
  --name astra-pg-manual \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=astra_test \
  -p 127.0.0.1:55432:5432 \
  postgres:16-alpine
```

连接串：

```bash
export MDS_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable'
```

## 3. 可选：启动 etcd 并验证 leader election

如果你只验证单实例链路，这一步可以跳过。  
如果你要验证“多 MDS 实例都能服务，但只有 leader 跑 repairer”，先起一个单节点 etcd：

```bash
docker run -d --rm \
  --name astra-etcd-manual \
  -p 127.0.0.1:2379:2379 \
  -p 127.0.0.1:2380:2380 \
  quay.io/coreos/etcd:v3.5.18 \
  /usr/local/bin/etcd \
  --name astra-etcd \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://127.0.0.1:2379 \
  --listen-peer-urls http://0.0.0.0:2380 \
  --initial-advertise-peer-urls http://127.0.0.1:2380 \
  --initial-cluster astra-etcd=http://127.0.0.1:2380 \
  --initial-cluster-state new
```

可选检查：

```bash
etcdctl --endpoints=http://127.0.0.1:2379 endpoint health
```

## 4. 启动 MDS

用 PostgreSQL backend 启动 `MDS`：

```bash
MDS_STORE_BACKEND=postgres \
MDS_POSTGRES_DSN="$MDS_POSTGRES_DSN" \
MDS_HTTP_ADDR=:8080 \
MDS_REPAIR_INTERVAL=15s \
MDS_REPAIR_HTTP_TIMEOUT=5s \
MDS_REPAIR_RETRY_BACKOFF=30s \
MDS_REPAIR_MAX_REPLICAS_PER_RUN=32 \
go run ./cmd/mds
```

如果要验证 leader election，可以起两个 `MDS`，都接同一个 PostgreSQL 和 etcd，但使用不同 HTTP/gRPC 地址：

终端 1：

```bash
MDS_STORE_BACKEND=postgres \
MDS_POSTGRES_DSN="$MDS_POSTGRES_DSN" \
MDS_HTTP_ADDR=:8080 \
MDS_GRPC_ADDR=:9090 \
MDS_LEADER_ELECTION_ENABLED=true \
MDS_ETCD_ENDPOINTS=http://127.0.0.1:2379 \
MDS_INSTANCE_ID=mds-1 \
MDS_REPAIR_INTERVAL=15s \
go run ./cmd/mds
```

终端 2：

```bash
MDS_STORE_BACKEND=postgres \
MDS_POSTGRES_DSN="$MDS_POSTGRES_DSN" \
MDS_HTTP_ADDR=:8081 \
MDS_GRPC_ADDR=:9091 \
MDS_LEADER_ELECTION_ENABLED=true \
MDS_ETCD_ENDPOINTS=http://127.0.0.1:2379 \
MDS_INSTANCE_ID=mds-2 \
MDS_REPAIR_INTERVAL=15s \
go run ./cmd/mds
```

另开一个终端检查：

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metrics | rg 'astrastorage_(http|mds)_'
```

如果 leader election 已开启，再看：

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_mds_leader_'
curl -s http://127.0.0.1:8081/metrics | rg 'astrastorage_mds_leader_'
```

预期：

- 只有一个实例的 `astrastorage_mds_leader_is_leader` 为 `1`
- 当前 leader 的 `astrastorage_mds_leader_term` 为正数
- 两边都还能返回 `healthz`

## 5. 启动 3 个 datanode

为了看到副本分布，建议至少起 3 个 datanode。

终端 1：

```bash
DATANODE_HTTP_ADDR=:10081 \
DATANODE_DATA_DIR=./data/datanode-1 \
DATANODE_NODE_ID=node-1 \
DATANODE_ADVERTISE_URL=http://127.0.0.1:10081 \
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
DATANODE_CAPACITY_BYTES=1073741824 \
go run ./cmd/datanode
```

终端 2：

```bash
DATANODE_HTTP_ADDR=:10082 \
DATANODE_DATA_DIR=./data/datanode-2 \
DATANODE_NODE_ID=node-2 \
DATANODE_ADVERTISE_URL=http://127.0.0.1:10082 \
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
DATANODE_CAPACITY_BYTES=1073741824 \
go run ./cmd/datanode
```

终端 3：

```bash
DATANODE_HTTP_ADDR=:10083 \
DATANODE_DATA_DIR=./data/datanode-3 \
DATANODE_NODE_ID=node-3 \
DATANODE_ADVERTISE_URL=http://127.0.0.1:10083 \
DATANODE_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
DATANODE_CAPACITY_BYTES=1073741824 \
go run ./cmd/datanode
```

分别检查：

```bash
curl http://127.0.0.1:10081/healthz
curl http://127.0.0.1:10082/healthz
curl http://127.0.0.1:10083/healthz
curl http://127.0.0.1:10081/metrics | rg 'astrastorage_(http|datanode)_'
```

## 6. 启动 gateway

`gateway` 的健康检查需要一个 datanode 基准地址，这里先指向 `node-1`：

```bash
GATEWAY_HTTP_ADDR=:11080 \
GATEWAY_MDS_HTTP_BASE_URL=http://127.0.0.1:8080 \
GATEWAY_DATANODE_BASE_URL=http://127.0.0.1:10081 \
go run ./cmd/gateway
```

检查：

```bash
curl http://127.0.0.1:11080/healthz
curl http://127.0.0.1:11080/metrics | rg 'astrastorage_(http|gateway)_'
```

## 7. 上传文件

先准备一个测试文件：

```bash
cat > /tmp/astra-demo.txt <<'EOF'
hello astra storage
this file is used for manual upload and download verification
EOF
```

Linux 常见做法：

```bash
CONTENT_BASE64=$(base64 -w 0 /tmp/astra-demo.txt)
```

如果你的 `base64` 不支持 `-w`，可以用：

```bash
CONTENT_BASE64=$(base64 /tmp/astra-demo.txt | tr -d '\n')
```

上传：

```bash
curl -X POST http://127.0.0.1:11080/uploads \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"astra-demo.txt\",
    \"parent_id\": \"root\",
    \"content_type\": \"text/plain\",
    \"content_base64\": \"${CONTENT_BASE64}\"
  }"
```

你会拿到一段 JSON，重点关注这些字段：

- `file_id`
- `session_id`
- `chunk_count`
- `chunks`

如果本机有 `jq`，可以直接提取 `file_id`：

```bash
FILE_ID=$(curl -s -X POST http://127.0.0.1:11080/uploads \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"astra-demo.txt\",
    \"parent_id\": \"root\",
    \"content_type\": \"text/plain\",
    \"content_base64\": \"${CONTENT_BASE64}\"
  }" | jq -r '.file_id')

echo "$FILE_ID"
```

如果没有 `jq`，就从返回 JSON 里手工记下 `file_id`。

## 8. 查询 MDS 元数据

查文件：

```bash
curl -X POST http://127.0.0.1:8080/rpc/mds.get_file \
  -H 'Content-Type: application/json' \
  -d "{
    \"ID\": \"${FILE_ID}\"
  }"
```

查 chunk 列表：

```bash
curl -X POST http://127.0.0.1:8080/rpc/mds.list_file_chunks \
  -H 'Content-Type: application/json' \
  -d "{
    \"FileID\": \"${FILE_ID}\"
  }"
```

查下载计划：

```bash
curl -X POST http://127.0.0.1:8080/rpc/mds.build_download_plan \
  -H 'Content-Type: application/json' \
  -d "{
    \"FileID\": \"${FILE_ID}\"
  }"
```

## 9. 下载文件

直接下载：

```bash
curl http://127.0.0.1:11080/downloads/${FILE_ID}
```

保存到本地并比较：

```bash
curl http://127.0.0.1:11080/downloads/${FILE_ID} -o /tmp/astra-downloaded.txt
diff -u /tmp/astra-demo.txt /tmp/astra-downloaded.txt
```

如果 `diff` 没输出，说明下载内容一致。

## 9. 用 PostgreSQL 查分片位置和节点

连接数据库：

```bash
psql 'postgres://postgres:postgres@127.0.0.1:55432/astra_test?sslmode=disable'
```

### 9.1 看有哪些节点

```sql
SELECT
  id,
  address,
  healthy,
  capacity,
  used,
  last_seen_at
FROM mds_nodes
ORDER BY id;
```

### 9.2 看文件基本信息

把下面的 `file-xxx` 换成你的真实 `FILE_ID`：

```sql
SELECT
  id,
  path,
  name,
  size,
  stored_size,
  status,
  primary_node_id,
  secondary_node_ids,
  latest_upload_session_id,
  created_at,
  updated_at
FROM mds_files
WHERE id = 'file-xxx';
```

### 9.3 查 chunk 列表

```sql
SELECT
  id,
  file_id,
  chunk_index,
  chunk_offset,
  size,
  status,
  replica_count,
  created_at,
  updated_at
FROM mds_chunks
WHERE file_id = 'file-xxx'
ORDER BY chunk_index;
```

### 9.4 查每个 chunk 的副本位置和节点地址

这是最直接的“分片位置和所在节点”查询：

```sql
SELECT
  c.file_id,
  c.id AS chunk_id,
  c.chunk_index,
  c.chunk_offset,
  c.size AS chunk_size,
  r.node_id,
  n.address AS node_address,
  r.role AS replica_role,
  r.state AS replica_state,
  r.stored_size,
  r.updated_at
FROM mds_chunks c
JOIN mds_chunk_replicas r
  ON r.chunk_id = c.id
JOIN mds_nodes n
  ON n.id = r.node_id
WHERE c.file_id = 'file-xxx'
ORDER BY c.chunk_index, r.node_id;
```

### 9.5 查文件级 placement 视图

```sql
SELECT
  file_id,
  node_id,
  replica_role,
  replica_state,
  is_primary,
  chunk_ids,
  stored_size,
  checksum_state,
  last_sync_at
FROM mds_file_placements
WHERE file_id = 'file-xxx'
ORDER BY node_id;
```

### 9.6 查上传会话

```sql
SELECT
  id,
  file_id,
  status,
  expected_size,
  confirmed_offset,
  next_offset,
  last_persisted_chunk_id,
  retry_attempt,
  retryable,
  last_error_code,
  created_at,
  updated_at,
  completed_at
FROM mds_upload_sessions
WHERE file_id = 'file-xxx'
ORDER BY created_at DESC;
```

## 10. 删除文件

通过 `gateway` 删除：

```bash
curl -X DELETE http://127.0.0.1:11080/files/${FILE_ID}
```

再查一次 PostgreSQL，确认元数据已清理：

```sql
SELECT * FROM mds_files WHERE id = 'file-xxx';
SELECT * FROM mds_chunks WHERE file_id = 'file-xxx';
SELECT * FROM mds_chunk_replicas WHERE file_id = 'file-xxx';
SELECT * FROM mds_upload_sessions WHERE file_id = 'file-xxx';
SELECT * FROM mds_file_placements WHERE file_id = 'file-xxx';
```

这些查询应该返回空结果。

也可以检查 datanode 数据目录，看对应 chunk 文件是否已经删除。

## 11. 停止临时 PostgreSQL

## 11. 验证观测面

这一节用于确认当前 observability foundation 已经真正可用，而不是只有代码存在。

### 11.1 检查三个服务都暴露 `/metrics`

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_(http|mds)_'
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_(http|datanode)_'
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_(http|gateway)_'
```

至少应看到这些 metric family：

- `astrastorage_http_requests_total`
- `astrastorage_http_request_duration_seconds`
- `astrastorage_gateway_upload_requests_total`
- `astrastorage_mds_rpc_requests_total`
- `astrastorage_datanode_chunk_put_total`

### 11.2 用固定 `X-Request-ID` 做一次上传并串日志

为了让日志更容易 grep，建议显式指定一个 request id：

```bash
REQUEST_ID=demo-observability-001
```

```bash
curl -X POST http://127.0.0.1:11080/uploads \
  -H "X-Request-ID: ${REQUEST_ID}" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"astra-demo-observability.txt\",
    \"parent_id\": \"root\",
    \"content_type\": \"text/plain\",
    \"content_base64\": \"${CONTENT_BASE64}\"
  }"
```

如果你是分别在 3 个终端里启动 `gateway`、`mds`、`datanode`，现在可以直接 grep 日志：

```bash
rg "${REQUEST_ID}" gateway.log mds.log datanode-1.log
```

如果没有把日志重定向到文件，就直接在对应终端里搜索这个 request id。

你应该能看到：

- `gateway` 的入站请求日志
- `gateway` 的业务日志，例如 upload request / chunk committed
- `mds` 的 HTTP / RPC 请求日志
- `datanode` 的 chunk PUT 请求日志

这一步主要证明 `X-Request-ID` 已经从 `gateway` 透传到了下游。

### 11.3 观察 gateway 业务指标变化

上传成功后查看 `gateway` 指标：

```bash
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_gateway_(upload|download|delete|upstream)_'
```

至少应能看到这些标签组合：

- `astrastorage_gateway_upload_requests_total{result="success"}`
- `astrastorage_gateway_upload_chunks_total{result="success"}`
- `astrastorage_gateway_upstream_requests_total{target="mds",operation="mds.start_upload",result="success"}`

### 11.4 观察 MDS RPC 与 repair 指标

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_mds_(rpc|upload|chunks|download|allocate|repair|nodes|heartbeats)_'
```

上传和下载计划调用之后，至少应看到：

- `astrastorage_mds_rpc_requests_total{method="mds.start_upload",result="success"}`
- `astrastorage_mds_upload_sessions_started_total{result="success"}`
- `astrastorage_mds_chunks_committed_total{result="success"}`
- `astrastorage_mds_download_plans_built_total{result="success"}`

如果 repair loop 正在工作，还会看到：

- `astrastorage_mds_repair_runs_total`
- `astrastorage_mds_repair_replicas_attempted_total`

### 11.5 观察 datanode 业务与生命周期指标

```bash
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_datanode_(chunk|replicate|stored|upstream|nodes_registered|heartbeats|last_|lifecycle)_'
```

至少应看到：

- `astrastorage_datanode_chunk_put_total{result="success"}`
- `astrastorage_datanode_chunk_get_total{result="success"}`
- `astrastorage_datanode_replicate_requests_total`
- `astrastorage_datanode_nodes_registered_total{result="success"}`
- `astrastorage_datanode_heartbeats_total{result="success"}`
- `astrastorage_datanode_stored_chunks`
- `astrastorage_datanode_last_registration_timestamp_seconds`
- `astrastorage_datanode_last_heartbeat_timestamp_seconds`

### 11.6 删除文件后再次检查指标和数据面

```bash
curl -X DELETE http://127.0.0.1:11080/files/${FILE_ID}
```

然后查看：

```bash
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_gateway_delete_requests_total'
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_datanode_(chunk_delete_total|stored_chunks)'
```

这一步主要确认：

- gateway 删除业务指标在增长
- datanode 的 chunk delete 指标在增长
- `stored_chunks` 会随成功删除下降

### 11.7 检查调度闭环持久状态

当前版本已经把 failover / rebalance / cleanup 收成了持久化 `ReplicaPlan`。

如果你在本地用 PostgreSQL backend 运行 `mds`，可以直接进库查看：

```sql
SELECT id, plan_type, chunk_id, source_node_id, target_node_id, state, retry_count
FROM mds_replica_plans
ORDER BY created_at, id;
```

正常情况下你应能看到：

- failover 或 rebalance 触发时会创建 plan
- plan 在目标副本 ready 后进入 `done`
- cleanup 失败时 `retry_count` 和 `next_retry_at` 会更新

### 11.8 检查 leader-scoped controller loops

当前 `repairer`、`failover`、`cleanup`、`rebalance` 都已经挂到同一套 leader supervisor 下。

如果你起两个 `mds` 实例并共享同一个 `etcd + PostgreSQL`，可以验证：

- 两个 `mds` 都能响应 API
- 只有当前 leader 会推进后台 controller loops
- leader 切换后，新 leader 会继续基于 PG 里的 `ReplicaPlan` 推进调度

## 12. 停止临时 PostgreSQL

测试结束后停止容器：

```bash
docker stop astra-pg-manual
```

## 13. 当前已知限制

这份手工测试文档基于当前最小可运行版本，仍有这些现实限制：

- `gateway` 上传接口仍然使用 `content_base64`
- repair loop 仍然是全量扫描，不是事件驱动
- datanode 现在已经会上报真实 `used bytes`，但当前统计仍然只基于本地 chunk 目录，不包含更复杂的磁盘保留、文件系统开销和多盘抽象
- failover / cleanup / rebalance 已经形成第一版闭环，但当前仍然是 scan-based controller，不是事件驱动调度
- 调度计划已经持久化到 `ReplicaPlan`，但当前还没有更强的 task ownership / distributed fencing 语义
- 当前 observability 仍然是应用层基础能力，不包含 tracing backend、dashboard、alerting、Redis/RabbitMQ/PostgreSQL/Kubernetes 基础设施监控
- 当前更适合做功能验证，不适合拿来测真实吞吐
