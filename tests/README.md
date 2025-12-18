# 文件上传集成测试指南

## 概述

这是一个完整的端到端集成测试，验证分布式文件存储系统的文件上传功能，包括：
- ✅ 文件上传到 Leader 节点
- ✅ 自动复制到其他节点
- ✅ 数据库记录存储节点列表
- ✅ 文件内容完整性验证

---

## 前置要求

### 1. 环境准备
- ✅ Docker Desktop 已安装并运行
- ✅ Go 1.21+ 已安装
- ✅ PowerShell 可用

### 2. 端口检查
确保以下端口未被占用：
- **5432-5436**: PostgreSQL (5个节点)
- **6379**: Redis
- **5672**: RabbitMQ AMQP
- **15672**: RabbitMQ 管理界面
- **29001-29005**: 节点通信端口
- **9081-9085**: 健康检查端口

---

## 快速开始

### 步骤1：启动 Docker 集群

```powershell
# 进入项目根目录
cd d:\IHaveADream\AstraStorage

# 启动集群（包含5个节点 + PostgreSQL + Redis + RabbitMQ）
.\scripts\start_docker_cluster.ps1
```

**预期输出：**
```
Starting Docker cluster...
Building images...
Starting containers...
✓ All containers started successfully!

Cluster Status:
Container               Status    Ports
-----------------------------------------
multi-driver-node-0     Up        29001, 9081
multi-driver-node-1     Up        29002, 9082
multi-driver-node-2     Up        29003, 9083
multi-driver-node-3     Up        29004, 9084
multi-driver-node-4     Up        29005, 9085
redis                   Up        6379
rabbitmq                Up        5672, 15672
postgres-0              Up        5432
postgres-1              Up        5433
postgres-2              Up        5434
postgres-3              Up        5435
postgres-4              Up        5436
```

### 步骤2：等待集群就绪

```powershell
# 等待30秒让集群完成初始化和 Leader 选举
Start-Sleep -Seconds 30

# 检查集群状态
.\scripts\test_cluster.ps1
```

**预期输出：**
```
Testing cluster health...
✓ Node 0: Healthy
✓ Node 1: Healthy
✓ Node 2: Healthy
✓ Leader elected: node-0
✓ Cluster is ready!
```

### 步骤3：运行集成测试

```powershell
# 运行文件上传测试
go test -v ./tests -run TestFileUploadIntegration -timeout 60s
```

---

## 测试流程详解

### 测试步骤

```
┌─────────────────────────────────────────────────────────┐
│         文件上传集成测试流程                              │
└─────────────────────────────────────────────────────────┘

[步骤1] 检查 Docker 集群状态
   ├─ 检查 RabbitMQ 连接
   ├─ 检查 PostgreSQL 连接
   └─ ✓ 集群就绪

[步骤2] 初始化数据库表结构
   ├─ 连接所有 PostgreSQL 节点
   ├─ 创建 files 表
   └─ ✓ 表结构已创建

[步骤3] 创建测试文件
   ├─ 文件名: test_upload.txt
   ├─ 文件大小: 77 字节
   └─ ✓ 测试文件已创建

[步骤4] 连接 RabbitMQ 并请求上传地址
   ├─ 发送上传元数据请求
   ├─ 接收 Leader 响应
   ├─ 获得上传地址: 172.28.0.2:xxxxx
   └─ ✓ 获得 Token: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

[步骤5] 通过 TCP 上传文件
   ├─ 连接到上传地址
   ├─ 发送 Token 验证
   ├─ 传输文件内容
   └─ ✓ 文件上传成功 (77 字节)

[步骤6] 等待文件复制完成
   └─ ⏳ 等待 5 秒...

[步骤7] 验证数据库记录
   ├─ 查询 files 表
   ├─ 文件ID: 1
   ├─ 文件名: test_upload.txt
   ├─ 文件大小: 77 字节
   ├─ 存储节点: node-0,node-1,node-2
   └─ ✓ 数据库记录正确

[步骤8] 验证文件内容完整性
   ├─ 计算 MD5: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
   ├─ 验证文件大小
   └─ ✓ 文件内容完整

═══════════════════════════════════════════════
✓ 文件上传成功
✓ 文件已复制到 3 个节点
✓ 数据库记录正确
✓ 文件内容完整

🎉 所有测试通过！
═══════════════════════════════════════════════
```

---

## 预期测试输出

### 成功场景

```
=== RUN   TestFileUploadIntegration
=== 文件上传集成测试 ===

[步骤1] 检查 Docker 集群状态...
✓ Docker 集群运行正常

[步骤2] 初始化数据库表结构...
✓ 数据库表结构已创建

[步骤3] 创建测试文件...
✓ 测试文件已创建: C:\Users\...\test_upload.txt (大小: 77 字节)

[步骤4] 连接 RabbitMQ 并请求上传地址...
✓ 获得上传地址: 172.28.0.2:45123, Token: 550e8400-e29b-41d4-a716-446655440000

[步骤5] 通过 TCP 上传文件...
  已发送 77 字节
✓ 文件上传成功

[步骤6] 等待文件复制到其他节点...
✓ 等待完成

[步骤7] 验证数据库记录...
  存储节点列表: [node-0 node-1 node-2]
✓ 数据库记录正确:
  - 文件ID: 1
  - 文件名: test_upload.txt
  - 文件大小: 77 字节
  - 存储节点: node-0,node-1,node-2

[步骤8] 验证文件内容完整性...
  原始文件 MD5: 8a3d9e5f7c1b2d4a6e8f0c3a5b7d9e1f
  文件大小匹配: 77 字节
✓ 文件内容完整，MD5校验通过

=== 测试结果汇总 ===
✓ 文件上传成功
✓ 文件已复制到 3 个节点
✓ 数据库记录正确
✓ 文件内容完整

🎉 所有测试通过！
--- PASS: TestFileUploadIntegration (12.34s)
PASS
ok      multi_driver/tests      12.456s
```

---

## 故障排查

### 问题1：Docker 集群未就绪

**错误信息：**
```
Docker 集群未就绪: 无法连接到 RabbitMQ
请先运行: .\scripts\start_docker_cluster.ps1
```

**解决方法：**
```powershell
# 启动集群
.\scripts\start_docker_cluster.ps1

# 检查容器状态
docker-compose ps

# 如果有容器未启动，查看日志
docker-compose logs rabbitmq
docker-compose logs node-0
```

### 问题2：端口被占用

**错误信息：**
```
Error: bind: address already in use
```

**解决方法：**
```powershell
# 检查端口占用
netstat -ano | findstr "5432"
netstat -ano | findstr "6379"
netstat -ano | findstr "5672"

# 停止占用端口的程序或修改 docker-compose.yml 中的端口映射
```

### 问题3：数据库表未创建

**错误信息：**
```
relation "files" does not exist
```

**解决方法：**
测试会自动创建表结构，但如果失败，可以手动执行：
```powershell
# 连接数据库
docker exec -it postgres-0 psql -U postgres -d driver

# 执行建表语句
\i /scripts/init_database.sql
```

### 问题4：测试超时

**错误信息：**
```
等待响应超时
```

**解决方法：**
```powershell
# 检查 Leader 是否选举成功
.\scripts\test_cluster.ps1

# 查看节点日志
.\scripts\view_docker_logs.ps1 -Node 0

# 重启集群
.\scripts\stop_docker_cluster.ps1
.\scripts\start_docker_cluster.ps1
```

---

## 高级用法

### 查看详细日志

```powershell
# 查看所有节点日志
.\scripts\view_docker_logs.ps1

# 查看特定节点
docker-compose logs -f node-0
docker-compose logs -f node-1

# 查看 RabbitMQ 日志
docker-compose logs -f rabbitmq

# 查看 PostgreSQL 日志
docker-compose logs -f postgres-0
```

### 手动测试上传

```powershell
# 构建客户端
go build -o bin/client.exe ./cmd/client

# 上传文件
.\bin\client.exe upload test.txt -amqp amqp://guest:guest@localhost:5672/ -queue file.upload
```

### 检查数据库

```powershell
# 连接到 PostgreSQL
docker exec -it postgres-0 psql -U postgres -d driver

# 查询文件记录
SELECT * FROM files;

# 查看存储节点分布
SELECT file_name, storage_nodes FROM files;
```

### 访问 RabbitMQ 管理界面

打开浏览器访问：http://localhost:15672
- 用户名：guest
- 密码：guest

---

## 清理测试环境

### 停止集群（保留数据）

```powershell
.\scripts\stop_docker_cluster.ps1
```

### 完全清理（删除数据）

```powershell
# 停止并删除所有容器和数据卷
docker-compose down -v

# 删除镜像
docker rmi multi_driver-node-0
```

---

## 测试配置

测试使用以下配置（可在测试代码中修改）：

| 配置项 | 默认值 |
|--------|--------|
| RabbitMQ URL | amqp://guest:guest@localhost:5672/ |
| 上传队列名 | file.upload |
| PostgreSQL (node-0) | localhost:5432 |
| PostgreSQL (node-1) | localhost:5433 |
| PostgreSQL (node-2) | localhost:5434 |
| 测试文件大小 | 77 字节 |
| 复制等待时间 | 5 秒 |
| 请求超时时间 | 10 秒 |

---

## 持续集成

### GitHub Actions 示例

```yaml
name: Integration Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Start Docker Cluster
        run: ./scripts/start_docker_cluster.ps1
      
      - name: Wait for cluster ready
        run: sleep 30
      
      - name: Run Integration Tests
        run: go test -v ./tests -run TestFileUploadIntegration
      
      - name: Cleanup
        if: always()
        run: ./scripts/stop_docker_cluster.ps1
```

---

## 参考文档

- [Docker 集群部署指南](../docker/README.md)
- [文件复制与目录树管理](./REPLICATION_AND_DIRECTORY.md)
- [文件上传数据库保存功能](./FILE_UPLOAD_DATABASE.md)
- [故障排查指南](../docker/TROUBLESHOOTING.md)

---

## 联系与支持

如遇到问题，请查看：
1. 日志文件：`docker-compose logs`
2. 故障排查文档：[TROUBLESHOOTING.md](../docker/TROUBLESHOOTING.md)
3. 测试成功记录：[TEST_SUCCESS.md](../docker/TEST_SUCCESS.md)
