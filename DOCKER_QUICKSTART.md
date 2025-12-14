# Docker 集群快速启动

## ⚠️ 启动前准备

### 1. 启动 Docker Desktop
- 在Windows搜索栏输入 "Docker Desktop"
- 点击启动Docker Desktop应用
- 等待Docker完全启动（任务栏图标变为正常状态）

### 2. 验证Docker运行
```powershell
docker --version
docker info
```

如果看到版本信息和系统信息，说明Docker已就绪。

## 🚀 启动集群

```powershell
# 停止本地节点（如果在运行）
.\scripts\stop_nodes.ps1

# 启动Docker集群
.\scripts\start_docker_cluster.ps1

# 等待选举完成并测试
Start-Sleep -Seconds 10
.\scripts\test_cluster.ps1
```

## 📊 查看运行状态

```powershell
# 查看容器状态
docker-compose ps

# 查看所有节点日志
.\scripts\view_docker_logs.ps1

# 查看特定节点
.\scripts\view_docker_logs.ps1 -Node 0
```

## 🛑 停止集群

```powershell
# 停止但保留数据
.\scripts\stop_docker_cluster.ps1

# 停止并删除所有数据
docker-compose down -v
```

## 🔧 故障排查

### Docker Desktop未启动
```
Error: Docker Desktop is not running!
```
**解决**: 启动Docker Desktop并等待完全启动

### 端口被占用
```
Error: port is already allocated
```
**解决**: 
```powershell
# 先停止本地节点
.\scripts\stop_nodes.ps1
Get-Process node -ErrorAction SilentlyContinue | Stop-Process
```

### 构建失败
```powershell
# 清理并重建
docker-compose down -v
docker system prune -f
.\scripts\start_docker_cluster.ps1
```

## 📖 详细文档

查看完整Docker部署文档：[docker/README.md](docker/README.md)
