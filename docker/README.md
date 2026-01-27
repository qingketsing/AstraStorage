# Docker 环境配置说明

## 概述

本项目的Docker配置已针对新环境和不同机器进行了优化，以确保测试的稳定性和可靠性。

## 主要改进

### 1. 健康检查增强
- **Redis**: 增加重试次数至10次，启动等待期30秒
- **RabbitMQ**: 增加重试次数至15次，启动等待期40秒，使用更可靠的健康检查命令
- **PostgreSQL**: 增加重试次数至10次，启动等待期30秒，添加数据库名称验证

### 2. 启动顺序优化
- 先启动基础设施服务（Redis, RabbitMQ, PostgreSQL）
- 等待所有基础设施服务健康后再启动应用节点
- 增加应用节点的启动稳定时间（15秒）

### 3. 数据库初始化
- 自动挂载初始化SQL脚本
- 确保数据库schema在容器启动时自动创建
- 添加UTF-8编码支持

### 4. 容器稳定性
- 添加重启策略（失败时自动重启，最多3次）
- 添加环境变量配置（Go代理等）
- 优化Dockerfile构建过程

## 可用脚本

### 启动和停止

```powershell
# 启动集群
.\scripts\start_docker_cluster.ps1

# 停止集群
.\scripts\stop_docker_cluster.ps1

# 重启集群（完全清理）
.\scripts\restart_docker_cluster.ps1

# 快速重启（不清理）
.\scripts\restart_docker_cluster.ps1 -Quick
```

### 诊断和清理

```powershell
# 诊断Docker环境
.\scripts\diagnose_docker.ps1

# 完全清理Docker资源
.\scripts\cleanup_docker.ps1
```

### 测试

```powershell
# 运行集成测试
.\scripts\run_integration_test.ps1

# 使用现有集群测试
.\scripts\run_integration_test.ps1 -SkipClusterStart

# 测试后保持集群运行
.\scripts\run_integration_test.ps1 -KeepCluster
```

## 故障排除

### 问题1: 容器启动失败

**症状**: 容器无法启动或立即退出

**解决方案**:
```powershell
# 1. 运行诊断
.\scripts\diagnose_docker.ps1

# 2. 完全清理并重启
.\scripts\cleanup_docker.ps1
.\scripts\start_docker_cluster.ps1

# 3. 查看日志
.\scripts\view_docker_logs.ps1
```

### 问题2: 端口被占用

**症状**: 端口冲突错误

**解决方案**:
```powershell
# 1. 检查端口占用
.\scripts\diagnose_docker.ps1

# 2. 停止冲突的服务或更改docker-compose.yml中的端口映射
```

### 问题3: 数据库连接失败

**症状**: 应用节点无法连接到PostgreSQL

**解决方案**:
```powershell
# 1. 确保数据库完全启动
docker ps  # 检查postgres容器状态

# 2. 检查健康状态
docker inspect postgres-0 --format='{{.State.Health.Status}}'

# 3. 如果不健康，重启数据库
docker-compose restart postgres-0
```

### 问题4: 测试超时

**症状**: 测试运行超时或挂起

**解决方案**:
- 增加等待时间（已在run_integration_test.ps1中设置为45秒）
- 确保Docker有足够的资源分配（至少4GB RAM）
- 检查防火墙设置

## 配置要求

### 最低系统要求
- **Docker Desktop**: 最新稳定版
- **内存**: 至少4GB分配给Docker
- **磁盘空间**: 至少5GB可用空间
- **CPU**: 至少2核

### 推荐配置
- **内存**: 8GB分配给Docker
- **磁盘空间**: 10GB可用空间
- **CPU**: 4核或更多

## 端口使用

| 服务 | 端口 | 说明 |
|-----|------|------|
| Redis | 6379 | 缓存服务 |
| RabbitMQ | 5672 | 消息队列 |
| RabbitMQ管理界面 | 15672 | Web管理界面 |
| PostgreSQL-0 | 20000 | 数据库节点0 |
| PostgreSQL-1 | 20001 | 数据库节点1 |
| PostgreSQL-2 | 20002 | 数据库节点2 |
| PostgreSQL-3 | 20003 | 数据库节点3 |
| PostgreSQL-4 | 20004 | 数据库节点4 |
| Node-0 RPC | 29001 | Raft通信 |
| Node-1 RPC | 29002 | Raft通信 |
| Node-2 RPC | 29003 | Raft通信 |
| Node-3 RPC | 29004 | Raft通信 |
| Node-4 RPC | 29005 | Raft通信 |
| Node-0 健康检查 | 9081 | HTTP状态端点 |
| Node-1 健康检查 | 9082 | HTTP状态端点 |
| Node-2 健康检查 | 9083 | HTTP状态端点 |
| Node-3 健康检查 | 9084 | HTTP状态端点 |
| Node-4 健康检查 | 9085 | HTTP状态端点 |
| Node-0 上传 | 30001 | TCP上传端口 |
| Node-1 上传 | 30002 | TCP上传端口 |
| Node-2 上传 | 30003 | TCP上传端口 |
| Node-3 上传 | 30004 | TCP上传端口 |
| Node-4 上传 | 30005 | TCP上传端口 |
| Node-0 下载 | 31001 | TCP下载端口 |
| Node-1 下载 | 31002 | TCP下载端口 |
| Node-2 下载 | 31003 | TCP下载端口 |
| Node-3 下载 | 31004 | TCP下载端口 |
| Node-4 下载 | 31005 | TCP下载端口 |

## 环境变量

可以通过修改 docker-compose.yml 中的环境变量来自定义配置：

```yaml
environment:
  - GO111MODULE=on
  - GOPROXY=https://goproxy.cn,direct  # 国内Go代理
  - POSTGRES_INITDB_ARGS=--encoding=UTF8
```

## 日志查看

```powershell
# 查看所有容器日志
docker-compose logs -f

# 查看特定节点日志
docker-compose logs -f node-0

# 查看最近100行日志
docker-compose logs --tail=100 node-0
```

## 性能优化建议

1. **Docker Desktop设置**
   - 增加分配的CPU核心数
   - 增加分配的内存（推荐8GB）
   - 启用WSL2后端（Windows）

2. **磁盘I/O优化**
   - 使用SSD存储
   - 定期清理未使用的Docker资源

3. **网络优化**
   - 关闭VPN（可能影响容器间通信）
   - 检查防火墙规则

## 更新日志

### 2026-01-27
- 优化健康检查配置
- 增加启动等待时间
- 添加数据库自动初始化
- 创建诊断和清理脚本
- 添加容器重启策略
