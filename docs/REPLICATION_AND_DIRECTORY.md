# 文件复制与目录树管理功能实现说明

## 概述

本次实现完成了分布式文件存储系统的三大核心功能：
1. ✅ **文件自动复制到其他节点**（冗余备份）
2. ✅ **数据库记录存储节点列表**（storage_nodes字段）
3. ✅ **文件目录树管理**（支持文件夹结构）

---

## 一、文件复制功能

### 新增文件
- [internal/core/replication/replication.go](internal/core/replication/replication.go) - 文件复制模块

### 核心功能

#### 1. **ReplicationManager** - 复制管理器
```go
type ReplicationManager struct {
    nodeID        string                        // 当前节点ID
    membership    *cluster.MembershipManager    // 集群成员管理
    replicaCount  int                           // 副本数量（默认3）
    storageDir    string                        // 本地存储目录
}
```

#### 2. **ReplicateFile()** - 文件复制方法
- 自动选择最佳的目标节点（按磁盘空间排序）
- 通过TCP直连将文件传输到其他节点
- 使用MD5校验和验证文件完整性
- 返回成功复制的节点ID列表

#### 3. **复制协议**
```
发送方 -> 接收方:
REPLICATE\n
<文件名>\n
<文件大小>\n
<MD5校验和>\n
<文件数据...>

接收方 -> 发送方:
OK (成功) 或 ER (失败)
```

#### 4. **复制服务器**
- 每个节点启动复制接收服务（端口19001）
- 自动验证接收的文件
- 失败时自动删除不完整的文件

### 工作流程

```
1. Leader节点接收客户端上传的文件
2. 保存到本地 FileStorage/ 目录
3. 通过 Membership 选择 N-1 个最佳节点
4. 并发将文件复制到选定的节点
5. 返回成功存储的节点列表
```

---

## 二、数据库更新功能

### 修改的文件
- [internal/db/repository.go](internal/db/repository.go) - 添加新的数据库操作方法

### 新增方法

#### 1. **UpdateFileStorageNodes()** - 更新存储节点列表
```go
func (dbc *DBConnection) UpdateFileStorageNodes(fileID int64, storageNodes string) error
```
- 更新文件的 `storage_nodes` 字段
- 记录格式：`"node1,node2,node3"`（逗号分隔）

#### 2. **GetFileByID()** - 获取文件信息
```go
func (dbc *DBConnection) GetFileByID(fileID int64) (*FileInfo, error)
```
- 根据文件ID查询完整的文件信息

#### 3. **FileInfo** - 文件信息结构
```go
type FileInfo struct {
    ID           int64
    FileName     string
    FileSize     int64
    LocalPath    string
    StorageNodes string    // "node1,node2,node3"
    StorageAdd   string    // "1-2-3"
    OwnerID      string
    CreatedAt    time.Time
}
```

---

## 三、目录树管理功能

### 数据库表结构

#### **directory_tree** 表
```sql
CREATE TABLE directory_tree (
    id BIGSERIAL PRIMARY KEY,                  -- 节点ID
    name VARCHAR(255) NOT NULL,                -- 文件/目录名
    parent_id BIGINT,                          -- 父目录ID（NULL=根目录）
    is_dir BOOLEAN NOT NULL,                   -- 是否为目录
    file_id BIGINT,                            -- 关联的文件ID
    owner_id VARCHAR(100),                     -- 所有者ID
    created_at TIMESTAMP NOT NULL
);
```

### 新增方法

#### 1. **CreateDirectory()** - 创建目录
```go
func (dbc *DBConnection) CreateDirectory(name string, parentID *int64, ownerID string) (int64, error)
```

#### 2. **AddFileToDirectory()** - 添加文件到目录
```go
func (dbc *DBConnection) AddFileToDirectory(fileName string, fileID int64, parentID *int64, ownerID string) (int64, error)
```
- 自动构建并更新 `storage_add` 路径

#### 3. **ListDirectory()** - 列出目录内容
```go
func (dbc *DBConnection) ListDirectory(parentID *int64) ([]*DirectoryNode, error)
```
- 列出指定目录下的所有文件和子目录
- 目录优先，然后按名称排序

#### 4. **MoveNode()** - 移动文件/目录
```go
func (dbc *DBConnection) MoveNode(nodeID int64, newParentID *int64) error
```
- 支持移动文件或整个目录树

#### 5. **DeleteNode()** - 删除节点
```go
func (dbc *DBConnection) DeleteNode(nodeID int64) error
```
- 递归删除目录及其所有子节点

#### 6. **buildStoragePath()** - 构建目录路径
```go
func (dbc *DBConnection) buildStoragePath(nodeID int64) (string, error)
```
- 使用递归CTE查询生成路径：`"1-2-3"`
- 自动从叶子节点追溯到根节点

---

## 四、集成到上传流程

### 修改的文件
- [internal/storage/upload/tcp_server.go](internal/storage/upload/tcp_server.go)
- [internal/storage/upload/service.go](internal/storage/upload/service.go)
- [internal/core/integration/node.go](internal/core/integration/node.go)

### 新增结构

#### **UploadContext** - 上传上下文
```go
type UploadContext struct {
    NodeID          string
    DB              *db.DBConnection
    ReplicationMgr  *replication.ReplicationManager
}
```

### 上传流程更新

```
1. 客户端上传文件 → Leader节点
2. 保存到本地 FileStorage/
3. 保存文件元数据到数据库（storage_nodes = 当前节点）
4. 【新增】自动复制到其他节点
5. 【新增】更新数据库 storage_nodes 字段
6. 完成！
```

### 在 Node 初始化时
```go
// 9. 初始化文件复制管理器
node.ReplicationMgr = replication.NewReplicationManager(id, node.Membership, "FileStorage")
// 启动复制接收服务（端口19001）
node.ReplicationMgr.StartReplicationServer("19001")
```

---

## 五、完整的上传流程

### 流程图

```
┌──────────┐                  ┌──────────┐                  ┌──────────┐
│  客户端  │                  │  Leader  │                  │  Node2/3 │
└────┬─────┘                  └────┬─────┘                  └────┬─────┘
     │                             │                             │
     │ 1. 上传文件                  │                             │
     ├───────────────────────────>│                             │
     │                             │                             │
     │                        2. 保存本地                        │
     │                        3. 写入数据库                      │
     │                        (storage_nodes=node1)              │
     │                             │                             │
     │                        4. 开始复制                        │
     │                             ├──────────────────────────>│
     │                             │   TCP传输文件                │
     │                             │   (端口19001)                │
     │                             │                             │
     │                             │<──────────────────────────┤
     │                             │   返回OK                     │
     │                             │                             │
     │                        5. 更新数据库                      │
     │                        (storage_nodes=node1,node2,node3)  │
     │                             │                             │
     │ 6. 返回成功                  │                             │
     │<────────────────────────────┤                             │
     │                             │                             │
```

### 日志示例

```
[node1] file upload finished: path=FileStorage/test.txt, name=test.txt, size=1024
[node1] file saved to database: id=1, name=test.txt, size=1024
[node1] starting file replication for test.txt
[node1] replicating file test.txt to node node2 (192.168.1.102:8080)
[node2] receiving replicated file: test.txt, size=1024
[node2] successfully received replicated file: test.txt
[node1] successfully replicated to node node2
[node1] file replicated to 3 nodes: [node1 node2 node3]
[node1] updated storage_nodes in database: node1,node2,node3
```

---

## 六、使用示例

### 1. 初始化数据库
```bash
psql -U postgres -d astra_storage -f scripts/init_database.sql
```

### 2. 上传文件（自动复制）
```bash
# 客户端上传
client upload example.txt

# 系统自动：
# - 保存到Leader节点
# - 复制到2个其他节点
# - 更新数据库记录
```

### 3. 查询文件存储位置
```sql
SELECT file_name, storage_nodes FROM files WHERE id = 1;
-- 结果: example.txt | node1,node2,node3
```

### 4. 创建目录结构
```go
// 创建根目录 "Documents"
dirID, _ := db.CreateDirectory("Documents", nil, "user001")

// 在 Documents 下创建子目录 "Photos"
subDirID, _ := db.CreateDirectory("Photos", &dirID, "user001")

// 将文件添加到 Photos 目录
db.AddFileToDirectory("vacation.jpg", fileID, &subDirID, "user001")
```

### 5. 列出目录内容
```go
// 列出根目录
nodes, _ := db.ListDirectory(nil)

// 列出特定目录
nodes, _ := db.ListDirectory(&dirID)
```

---

## 七、关键特性

### 容错机制
1. **部分复制失败不影响上传**：至少保存到1个节点即成功
2. **校验和验证**：确保复制的文件完整性
3. **超时控制**：30秒复制超时，避免长时间阻塞

### 性能优化
1. **智能节点选择**：优先选择磁盘空间大的节点
2. **分块传输**：1MB分块，支持大文件
3. **并发复制**：同时向多个节点复制（可扩展）

### 扩展性
1. **可配置副本数**：`SetReplicaCount(n)`
2. **目录树深度无限制**：使用递归CTE查询
3. **支持文件移动**：自动更新路径信息

---

## 八、注意事项

1. **端口占用**：复制服务使用固定端口19001，确保未被占用
2. **存储空间**：每个文件会占用 N 份存储空间（N=副本数）
3. **数据一致性**：当前实现为最终一致性，复制失败需要手动修复
4. **网络要求**：节点间需要能够通过TCP直连（端口19001）

---

## 九、后续优化方向

1. **异步复制队列**：当前为同步复制，可改为异步提高响应速度
2. **增量复制**：支持断点续传和增量同步
3. **副本修复**：自动检测并修复缺失的副本
4. **负载均衡**：根据节点负载动态调整复制策略
5. **压缩传输**：减少网络带宽占用
