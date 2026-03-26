# AstraStorage Observability 演示手册

这份文档面向两种场景：

- 面试或演示时，用 5 到 10 分钟快速证明系统“不是黑盒”
- 本地联调时，快速定位 upload / download / replicate / heartbeat 是否真的被观测到了

它不替代 [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)，而是把其中和 observability 相关的部分压缩成一条更适合演示的路径。

## 1. 当前已经具备的观测面

当前仓库已经实现的是“应用层 observability foundation”，覆盖：

- `gateway` 入站请求、上游调用、上传/下载/删除业务指标
- `mds` HTTP/RPC 请求、核心业务计数、repair loop 指标
- `mds` 的 etcd leader election 指标和 leadership 摘要日志
- `datanode` chunk put/get/delete、replicate、register/heartbeat、生命周期 gauge
- `X-Request-ID` 在 `gateway -> mds -> datanode` 之间的透传
- 结构化 JSON 日志

当前还没有的是：

- tracing backend
- Grafana dashboard
- alerting
- Redis / RabbitMQ / PostgreSQL / Kubernetes 基础设施监控

## 2. 三个服务的 `/metrics`

默认地址：

- `gateway`: `http://127.0.0.1:11080/metrics`
- `mds`: `http://127.0.0.1:8080/metrics`
- `datanode-1`: `http://127.0.0.1:10081/metrics`

快速检查：

```bash
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_(http|gateway)_'
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_(http|mds)_'
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_(http|datanode)_'
```

## 3. 演示脚本

### 3.1 准备 request id 和测试文件

```bash
REQUEST_ID=demo-observability-001
cat > /tmp/astra-observability-demo.txt <<'EOF'
hello observability
this file is used for gateway mds datanode tracing by request id
EOF
CONTENT_BASE64=$(base64 -w 0 /tmp/astra-observability-demo.txt 2>/dev/null || base64 /tmp/astra-observability-demo.txt | tr -d '\n')
```

### 3.2 发起一次上传

```bash
UPLOAD_RESPONSE=$(curl -s -X POST http://127.0.0.1:11080/uploads \
  -H "X-Request-ID: ${REQUEST_ID}" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"astra-observability-demo.txt\",
    \"parent_id\": \"root\",
    \"content_type\": \"text/plain\",
    \"content_base64\": \"${CONTENT_BASE64}\"
  }")

echo "${UPLOAD_RESPONSE}"
```

如果装了 `jq`，提取 `file_id`：

```bash
FILE_ID=$(printf '%s' "${UPLOAD_RESPONSE}" | jq -r '.file_id')
echo "${FILE_ID}"
```

### 3.3 用 request id 串日志

如果服务日志已经重定向到文件：

```bash
rg "${REQUEST_ID}" gateway.log mds.log datanode-1.log
```

应能看到：

- `gateway` 的 `http request`
- `gateway` 的 `gateway upload request`
- `mds` 的 `http request`
- `datanode` 的 `http request`

如果某个请求 ID 只出现在 `gateway`，说明下游透传有问题。

### 3.4 看 gateway 指标

```bash
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_gateway_(upload|upstream|download|delete)_'
```

重点关注：

- `astrastorage_gateway_upload_requests_total{result="success"}`
- `astrastorage_gateway_upload_chunks_total{result="success"}`
- `astrastorage_gateway_upload_bytes_total`
- `astrastorage_gateway_upstream_requests_total{target="mds",operation="mds.start_upload",result="success"}`

这一步能证明：

- gateway 不只是收到了请求
- 它还把业务行为和对下游的调用都记下来了

### 3.5 看 MDS 指标

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_mds_(rpc|upload|chunks|download|allocate|nodes|heartbeats|repair)_'
```

重点关注：

- `astrastorage_mds_rpc_requests_total{method="mds.start_upload",result="success"}`
- `astrastorage_mds_upload_sessions_started_total{result="success"}`
- `astrastorage_mds_chunks_committed_total{result="success"}`

如果 repair loop 正在运行，还应看到：

- `astrastorage_mds_repair_runs_total`
- `astrastorage_mds_repair_replicas_attempted_total`

### 3.6 看 MDS leadership 指标

如果你已经用 etcd 起了两个 `MDS` 实例，再看：

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'astrastorage_mds_leader_'
curl -s http://127.0.0.1:8081/metrics | rg 'astrastorage_mds_leader_'
```

重点关注：

- `astrastorage_mds_leader_is_leader`
- `astrastorage_mds_leader_term`
- `astrastorage_mds_leader_transitions_total{result="started"}`
- `astrastorage_mds_leader_transitions_total{result="stopped"}`
- `astrastorage_mds_leader_election_failures_total`

这一步能证明：

- 多个 `MDS` 实例可以同时对外服务
- 但只有一个实例当前持有 controller leadership
- leadership 变化会被指标和日志记录下来

### 3.7 看 datanode 指标

```bash
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_datanode_(chunk|replicate|stored|upstream|nodes_registered|heartbeats|last_|lifecycle)_'
```

重点关注：

- `astrastorage_datanode_chunk_put_total{result="success"}`
- `astrastorage_datanode_chunk_get_total`
- `astrastorage_datanode_replicate_requests_total`
- `astrastorage_datanode_nodes_registered_total{result="success"}`
- `astrastorage_datanode_heartbeats_total{result="success"}`
- `astrastorage_datanode_stored_chunks`
- `astrastorage_datanode_last_registration_timestamp_seconds`
- `astrastorage_datanode_last_heartbeat_timestamp_seconds`

### 3.8 触发一次下载和删除

```bash
curl -s http://127.0.0.1:11080/downloads/${FILE_ID} -o /tmp/astra-observability-downloaded.txt
curl -X DELETE http://127.0.0.1:11080/files/${FILE_ID}
```

然后再看：

```bash
curl -s http://127.0.0.1:11080/metrics | rg 'astrastorage_gateway_(download_requests_total|delete_requests_total)'
curl -s http://127.0.0.1:10081/metrics | rg 'astrastorage_datanode_(chunk_delete_total|stored_chunks)'
```

## 4. 演示时建议讲的重点

如果你要把它当成面试原型讲，我建议按这个顺序说：

1. 当前不是完整监控平台，而是先把应用层 observability foundation 打出来。
2. 三个服务都有 `/metrics`，且指标边界跟业务边界对应。
3. `gateway` 负责入口和上游调用指标，`mds` 负责控制面和 repair，`datanode` 负责 chunk/replicate/heartbeat。
4. `MDS` 现在已经有 etcd-backed leader election，controller 单活不再只是约定。
5. 高基数 ID 没放进 metrics label，只放进结构化日志。
6. 现在已经能靠 `request_id` 从 `gateway` 串到 `mds` 和 `datanode`。
7. 后续如果接 Redis / RabbitMQ / PostgreSQL / Kubernetes，现有这层不需要推翻，只需要继续往上叠依赖层和基础设施层。

## 5. 后续扩展入口

当前演示手册对应的只是应用层观测。下一步如果继续扩展，建议顺序是：

1. Docker / 本地一键联调，把这套 `/metrics` 和日志验证流程自动化。
2. Kubernetes 部署后接 Prometheus 抓取。
3. 再补 dashboard / alerting。
4. 最后把 Redis / RabbitMQ / PostgreSQL exporter 和应用侧 client metrics 接进来。
