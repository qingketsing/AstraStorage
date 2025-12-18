# 文件上传数据库保存功能实现说明

## 概述

本次实现为 TCP 文件上传功能添加了数据库持久化支持，当文件上传完成后，会自动将文件的元数据信息保存到 PostgreSQL 数据库中。

## 修改的文件

### 1. `internal/storage/storage.go`
更新了文件元数据结构：

- **MetaData**: 添加了可导出的 `ID` 字段
- **FileObject**: 扩展了结构体，包含以下字段：
  - `ID`: 文件数据库主键
  - `Name`: 文件名称
  - `Capacity`: 文件大小（字节）
  - `StorageAdd`: 文件目录树路径（格式: "1-2-3"）
  - `StorageNodes`: 存储该文件的节点ID列表（逗号分隔）
  - `CreatedAt`: 文件创建时间
  - `LocalPath`: 本地存储路径

### 2. `internal/db/repository.go`
添加了文件上传信息保存功能：

#### 新增类型：`FileUploadInfo`
```go
type FileUploadInfo struct {
    FileName     string    // 文件名
    FileSize     int64     // 文件大小（字节）
    LocalPath    string    // 本地存储路径
    StorageNodes string    // 存储节点ID列表（逗号分隔）
    StorageAdd   string    // 文件目录树路径（格式: "1-2-3"）
    OwnerID      string    // 文件所有者ID（可选）
    CreatedAt    time.Time // 创建时间
}
```

#### 新增方法：`SaveFileUpload`
```go
func (dbc *DBConnection) SaveFileUpload(info FileUploadInfo) (int64, error)
```
- **功能**: 将文件上传信息保存到数据库
- **返回值**: 返回文件的数据库ID（自增主键）
- **参数**: `FileUploadInfo` 结构体，包含文件的所有元数据

### 3. `internal/storage/upload/tcp_server.go`
在 `handleSingleUpload` 函数中添加了数据库保存逻辑：

- 文件接收完成后，自动调用 `dbc.SaveFileUpload()` 保存文件信息
- 记录的信息包括：
  - 文件名
  - 文件大小
  - 本地存储路径
  - 所有者ID（使用客户端IP）
- 如果数据库保存失败，只记录日志不影响文件上传流程

## 数据库表结构

### files 表

执行 `scripts/init_database.sql` 脚本来创建表：

```sql
CREATE TABLE files (
    id BIGSERIAL PRIMARY KEY,              -- 文件ID，自增主键
    file_name VARCHAR(255) NOT NULL,       -- 文件名称
    file_size BIGINT NOT NULL,             -- 文件大小（字节）
    local_path VARCHAR(500) NOT NULL,      -- 本地存储路径
    storage_nodes TEXT,                    -- 存储节点ID列表，逗号分隔
    storage_add VARCHAR(500),              -- 文件目录树路径
    owner_id VARCHAR(100),                 -- 文件所有者ID
    created_at TIMESTAMP NOT NULL,         -- 创建时间
    updated_at TIMESTAMP DEFAULT NOW()     -- 更新时间
);
```

#### 索引
- `idx_files_file_name`: 文件名索引
- `idx_files_owner_id`: 所有者ID索引
- `idx_files_created_at`: 创建时间索引

## 使用方法

### 1. 初始化数据库

```bash
# 连接到 PostgreSQL 数据库
psql -U your_username -d your_database -f scripts/init_database.sql
```

### 2. 上传文件

文件上传流程不变，使用现有的 TCP 上传接口：

1. 客户端发送上传元数据请求到 RabbitMQ 队列
2. Leader 节点返回上传地址和 token
3. 客户端连接 TCP 服务器上传文件
4. **[新增]** 文件上传完成后，自动保存到数据库

### 3. 查询文件信息

```sql
-- 查询所有文件
SELECT * FROM files ORDER BY created_at DESC;

-- 根据文件名查询
SELECT * FROM files WHERE file_name = 'example.txt';

-- 根据所有者查询
SELECT * FROM files WHERE owner_id = 'user001';
```

## 后续扩展

当前实现了基础的文件信息保存，后续可以扩展：

1. **文件复制**: 实现文件复制到其他节点后更新 `storage_nodes` 字段
2. **目录树**: 实现完整的文件目录树管理，更新 `storage_add` 字段
3. **文件删除**: 实现文件删除时同步更新数据库记录
4. **文件查询**: 添加更多查询接口，如按目录查询、按大小过滤等
5. **权限管理**: 扩展 `owner_id` 实现更完善的用户权限系统

## 注意事项

1. **数据库连接**: 确保节点启动时已正确配置并连接到 PostgreSQL 数据库
2. **错误处理**: 数据库保存失败不会影响文件上传，但会记录错误日志
3. **性能**: 大量文件上传时，注意数据库连接池配置和索引优化
4. **一致性**: 当前实现中，如果数据库写入失败，文件已保存到本地，需要定期对账和修复
