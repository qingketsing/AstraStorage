# 分布式网盘实现路线图

## 当前状态
✅ Raft共识算法（Leader选举、日志复制、心跳）
✅ 节点发现和健康监控
✅ 集群管理脚本（5节点）
✅ 基础文件树状态机定义

## 第一阶段：核心存储层（1-2天）

### 1.1 扩展Integration Node
文件：`internal/core/integration/storage_node.go`

```go
// 在Node中添加
type Node struct {
    // ... 现有字段
    FileTree    *storage.FileTree
    StorageDir  string  // 本地存储目录
}
```

**任务：**
- [x] 定义FileCommand和FileMeta数据结构
- [x] 实现FileTree状态机
- [ ] 在applyCh监听器中应用FileCommand
- [ ] 实现本地文件存储（分片写入/读取）

### 1.2 实现文件操作API
文件：`internal/core/storage/operations.go`

**端点：**
- `POST /api/upload/init` - 初始化上传，返回文件ID和目标节点
- `POST /api/upload/chunk` - 上传分片（1MB）
- `POST /api/upload/complete` - 完成上传
- `GET /api/download/{fileId}` - 下载文件（重定向到最快节点）
- `GET /api/files` - 列出文件
- `DELETE /api/files/{fileId}` - 删除文件

### 1.3 实现副本管理
文件：`internal/core/storage/replication.go`

**功能：**
- Leader选择3个目标节点存储副本
- 上传时并行写入3个节点
- 定期检查副本健康度
- 副本修复（当节点故障时）

## 第二阶段：数据库和缓存（1天）

### 2.1 PostgreSQL集成
文件：`internal/core/storage/database.go`

**表结构：**
```sql
CREATE TABLE files (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    path TEXT NOT NULL,
    size BIGINT NOT NULL,
    hash VARCHAR(64) NOT NULL,
    chunk_count INT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    is_directory BOOLEAN DEFAULT FALSE
);

CREATE TABLE replicas (
    id SERIAL PRIMARY KEY,
    file_id VARCHAR(64) REFERENCES files(id) ON DELETE CASCADE,
    node_id VARCHAR(64) NOT NULL,
    node_address VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE chunks (
    id SERIAL PRIMARY KEY,
    file_id VARCHAR(64) REFERENCES files(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    size INT NOT NULL,
    hash VARCHAR(64) NOT NULL,
    offset BIGINT NOT NULL
);
```

**任务：**
- [ ] 集成pgx驱动
- [ ] 实现文件元数据持久化
- [ ] 实现查询接口

### 2.2 Redis集成
文件：`internal/core/cache/redis.go`

**用途：**
- 缓存文件元数据（GET请求）
- 缓存下载任务状态
- 节点健康状态缓存

**任务：**
- [ ] 集成go-redis
- [ ] 实现缓存层
- [ ] 实现任务状态跟踪

### 2.3 RabbitMQ集成
文件：`internal/core/queue/rabbitmq.go`

**队列：**
- `upload_tasks` - 上传任务队列
- `download_tasks` - 下载任务队列
- `replication_tasks` - 副本同步任务

**任务：**
- [ ] 集成amqp库
- [ ] 实现任务发布/订阅
- [ ] 实现异步任务处理

## 第三阶段：智能路由和传输（1-2天）

### 3.1 节点选择算法
文件：`internal/core/routing/selector.go`

**功能：**
- 根据网络延迟选择最快节点
- 负载均衡（避免单点过载）
- 地理位置优先（如果可用）

**算法：**
```go
func SelectBestNode(fileId string, clientIP string, availableNodes []string) string {
    // 1. 过滤有该文件的节点
    // 2. ping测试延迟
    // 3. 检查节点负载
    // 4. 返回最优节点
}
```

### 3.2 TCP直连下载
文件：`internal/core/transfer/tcp_server.go`

**功能：**
- 在选定节点启动临时TCP服务器
- 支持断点续传
- 实时进度报告

### 3.3 WebSocket进度推送
文件：`internal/core/api/websocket.go`

**功能：**
- 客户端连接WebSocket获取实时进度
- 服务器推送进度百分比和速度

## 第四阶段：Docker部署（1天）

### 4.1 Docker Compose配置
文件：`docker-compose.yml`

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: netdisk
      POSTGRES_USER: admin
      POSTGRES_PASSWORD: password
    volumes:
      - postgres_data:/var/lib/postgresql/data
  
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
  
  rabbitmq:
    image: rabbitmq:3-management
    ports:
      - "5672:5672"
      - "15672:15672"
  
  node-0:
    build: .
    environment:
      NODE_ID: node-0
      NODE_ADDR: node-0:29001
      PEERS: node-1:29001,node-2:29001
      ME: 0
      POSTGRES_URL: postgres://admin:password@postgres:5432/netdisk
      REDIS_URL: redis:6379
      RABBITMQ_URL: amqp://guest:guest@rabbitmq:5672/
    volumes:
      - node0_data:/data
    depends_on:
      - postgres
      - redis
      - rabbitmq
  
  node-1:
    # 类似node-0...
  
  node-2:
    # 类似node-0...

volumes:
  postgres_data:
  node0_data:
  node1_data:
  node2_data:
```

### 4.2 Dockerfile
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /bin/node ./cmd/node

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/node /bin/node
EXPOSE 29001 9081 8080
CMD ["/bin/node"]
```

## 立即开始的步骤

### 步骤1：完成applyCh处理（30分钟）
修改 `cmd/node/main.go`:

```go
fileTree := storage.NewFileTree()

go func() {
    for msg := range applyCh {
        if msg.CommandValid {
            var cmd storage.FileCommand
            if err := json.Unmarshal(msg.Command.([]byte), &cmd); err == nil {
                fileTree.Apply(cmd)
            }
        }
    }
}()
```

### 步骤2：添加上传API（1小时）
创建 `internal/core/api/upload.go`:

```go
func (api *API) InitUpload(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求（文件名、大小）
    // 2. 生成fileID
    // 3. Leader选择3个目标节点
    // 4. 通过Raft提交CREATE_FILE命令
    // 5. 返回fileID和目标节点地址
}
```

### 步骤3：测试基本流程（30分钟）
```bash
# 1. 启动集群
.\scripts\start_nodes.ps1

# 2. 初始化上传
curl -X POST http://localhost:9081/api/upload/init \
  -d '{"name":"test.txt","size":1048576}'

# 3. 查看Raft日志是否复制
.\scripts\test_cluster.ps1
```

## 预计时间表

- **第一阶段**：2天（核心存储）
- **第二阶段**：1天（数据库集成）
- **第三阶段**：2天（传输优化）
- **第四阶段**：1天（Docker部署）

**总计：约6天完成核心功能**

## 现在就开始

建议从步骤1开始：完成applyCh处理和文件树集成。我可以帮你立即实现这部分代码？
