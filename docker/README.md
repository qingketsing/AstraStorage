# Docker 集群部署指南

## 前置要求

- ✅ Docker Desktop 已安装并运行
- ✅ Docker Compose 已安装
- ✅ 确保端口 29001-29005 和 9081-9085 未被占用

## 快速开始

### 1. 启动Docker集群

```powershell
.\scripts\start_docker_cluster.ps1
```

这会：
- 构建Docker镜像
- 启动5个节点容器
- 创建集群网络
- 等待节点启动

### 2. 测试集群状态

```powershell
# 等待几秒让选举完成
Start-Sleep -Seconds 5

# 测试集群
.\scripts\test_cluster.ps1
```

### 3. 查看日志

```powershell
# 查看所有节点日志
.\scripts\view_docker_logs.ps1

# 查看特定节点日志
.\scripts\view_docker_logs.ps1 -Node 0
docker-compose logs -f node-0
```

### 4. 停止集群

```powershell
.\scripts\stop_docker_cluster.ps1

# 或完全清理（删除数据卷）
docker-compose down -v
```

## Docker vs 本地运行对比

| 特性 | 本地运行 | Docker运行 |
|------|---------|-----------|
| 启动方式 | `.\scripts\start_nodes.ps1` | `.\scripts\start_docker_cluster.ps1` |
| 资源隔离 | ❌ 共享系统资源 | ✅ 容器隔离 |
| 端口冲突 | 可能与其他程序冲突 | 容器内部使用固定端口 |
| 数据持久化 | `/data` 目录 | Docker volumes |
| 网络 | localhost | bridge网络 (172.28.0.0/16) |
| 日志查看 | PowerShell窗口 | `docker-compose logs` |
| 适用场景 | 开发调试 | 生产/多机部署 |

## 容器架构

```
┌─────────────────────────────────────────────────┐
│             Host: Windows/Linux                 │
│                                                 │
│  ┌──────────────────────────────────────────┐  │
│  │     Docker Bridge Network (172.28.0.0)   │  │
│  │                                          │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐ │  │
│  │  │ node-0  │  │ node-1  │  │ node-2  │ │  │
│  │  │:29001   │  │:29001   │  │:29001   │ │  │
│  │  │:9081    │  │:9081    │  │:9081    │ │  │
│  │  └────┬────┘  └────┬────┘  └────┬────┘ │  │
│  │       │            │            │       │  │
│  │  ┌────┴────┐  ┌───┴─────┐             │  │
│  │  │ node-3  │  │ node-4  │              │  │
│  │  │:29001   │  │:29001   │              │  │
│  │  │:9081    │  │:9081    │              │  │
│  │  └─────────┘  └─────────┘              │  │
│  └──────────────────────────────────────────┘  │
│         ↓ Port Mapping                         │
│    localhost:9081-9085                         │
└─────────────────────────────────────────────────┘
```

## 常用Docker命令

### 查看容器状态
```powershell
docker-compose ps
docker ps -a
```

### 查看资源使用
```powershell
docker stats
```

### 进入容器shell
```powershell
docker exec -it multi-driver-node-0 sh
```

### 重启单个节点（测试故障恢复）
```powershell
# 停止leader测试选举
docker-compose stop node-0
Start-Sleep -Seconds 5
.\scripts\test_cluster.ps1

# 重启节点
docker-compose start node-0
```

### 查看容器内文件
```powershell
docker exec multi-driver-node-0 ls -la /data
```

### 清理所有Docker资源
```powershell
docker-compose down -v
docker system prune -a
```

## 端口映射

| 容器 | 容器内端口 | 主机端口 | 用途 |
|------|-----------|---------|------|
| node-0 | 29001 | 29001 | Raft RPC |
| node-0 | 9081 | 9081 | Health API |
| node-1 | 29001 | 29002 | Raft RPC |
| node-1 | 9081 | 9082 | Health API |
| node-2 | 29001 | 29003 | Raft RPC |
| node-2 | 9081 | 9083 | Health API |
| node-3 | 29001 | 29004 | Raft RPC |
| node-3 | 9081 | 9084 | Health API |
| node-4 | 29001 | 29005 | Raft RPC |
| node-4 | 9081 | 9085 | Health API |

## 数据持久化

Docker volumes保存在：
- Windows: `C:\ProgramData\docker\volumes\`
- Linux: `/var/lib/docker/volumes/`

卷名：
- `multi_driver_node0_data`
- `multi_driver_node1_data`
- `multi_driver_node2_data`
- `multi_driver_node3_data`
- `multi_driver_node4_data`

## 故障测试场景

### 1. 测试Leader故障
```powershell
# 查看当前leader
.\scripts\test_cluster.ps1

# 停止leader
docker-compose stop node-2  # 假设node-2是leader

# 等待选举
Start-Sleep -Seconds 5

# 验证新leader
.\scripts\test_cluster.ps1
```

### 2. 测试网络分区
```powershell
# 断开node-0的网络
docker network disconnect multi_driver_cluster-network multi-driver-node-0

# 观察集群
.\scripts\test_cluster.ps1

# 恢复网络
docker network connect multi_driver_cluster-network multi-driver-node-0
```

### 3. 测试节点重启
```powershell
docker-compose restart node-1
Start-Sleep -Seconds 3
.\scripts\test_cluster.ps1
```

## 性能调优

### 修改资源限制
编辑 `docker-compose.yml`:

```yaml
services:
  node-0:
    # ... 其他配置
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 512M
        reservations:
          cpus: '0.25'
          memory: 256M
```

### 修改日志驱动
```yaml
services:
  node-0:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

## 生产环境建议

1. **使用外部存储卷**
   ```yaml
   volumes:
     node0_data:
       driver: local
       driver_opts:
         type: nfs
         o: addr=10.0.0.1,rw
         device: ":/data/node0"
   ```

2. **启用健康检查**（已配置）

3. **配置重启策略**（已配置为 `unless-stopped`）

4. **使用环境变量文件**
   创建 `.env` 文件：
   ```
   POSTGRES_PASSWORD=your_secure_password
   REDIS_PASSWORD=your_redis_password
   ```

5. **监控和日志收集**
   - 使用 Prometheus + Grafana
   - 集成 ELK Stack (Elasticsearch, Logstash, Kibana)

## 故障排查

### 容器无法启动
```powershell
# 查看详细错误
docker-compose logs node-0

# 检查镜像是否构建成功
docker images | Select-String "multi_driver"
```

### 端口冲突
```powershell
# 检查端口占用
netstat -ano | Select-String "908[1-5]"

# 停止占用端口的进程
Stop-Process -Id <PID>
```

### 网络问题
```powershell
# 检查网络
docker network ls
docker network inspect multi_driver_cluster-network

# 重建网络
docker-compose down
docker network prune
docker-compose up -d
```

## 下一步

- [ ] 集成PostgreSQL容器
- [ ] 集成Redis容器
- [ ] 集成RabbitMQ容器
- [ ] 实现文件上传/下载API
- [ ] 配置Nginx反向代理
- [ ] 设置SSL证书
