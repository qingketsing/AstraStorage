# 文件上传测试 - 快速参考

## 🚀 一键测试

```powershell
# 方式1：使用快速测试脚本（推荐）
.\quick_test.ps1

# 方式2：使用完整脚本
.\scripts\run_integration_test.ps1
```

---

## 📋 分步测试

### 1️⃣ 启动集群
```powershell
.\scripts\start_docker_cluster.ps1
```

### 2️⃣ 等待就绪（30秒）
```powershell
Start-Sleep -Seconds 30
```

### 3️⃣ 运行测试
```powershell
go test -v ./tests -run TestFileUploadIntegration
```

### 4️⃣ 停止集群
```powershell
.\scripts\stop_docker_cluster.ps1
```

---

## 🔍 查看日志

```powershell
# 所有节点
.\scripts\view_docker_logs.ps1

# 特定节点
docker-compose logs -f node-0

# RabbitMQ
docker-compose logs -f rabbitmq

# PostgreSQL
docker-compose logs -f postgres-0
```

---

## 🛠️ 常用命令

```powershell
# 检查容器状态
docker-compose ps

# 查看资源使用
docker stats

# 进入容器
docker exec -it multi-driver-node-0 sh

# 连接数据库
docker exec -it postgres-0 psql -U postgres -d driver

# 重启某个节点
docker-compose restart node-0
```

---

## ⚠️ 故障排查

### 问题：集群未启动
```powershell
# 解决：检查 Docker 并启动
docker ps
.\scripts\start_docker_cluster.ps1
```

### 问题：端口被占用
```powershell
# 解决：检查端口
netstat -ano | findstr "5432"
netstat -ano | findstr "5672"
```

### 问题：测试超时
```powershell
# 解决：检查 Leader 选举
.\scripts\test_cluster.ps1

# 重启集群
.\scripts\stop_docker_cluster.ps1
.\scripts\start_docker_cluster.ps1
```

---

## 📊 测试验证点

✅ **连接性测试**
- RabbitMQ 连接正常
- PostgreSQL 连接正常
- 节点通信正常

✅ **功能测试**
- 文件上传成功
- Token 验证通过
- TCP 传输完整

✅ **复制测试**
- 文件复制到多个节点
- storage_nodes 字段更新
- 节点选择策略正确

✅ **数据库测试**
- 记录插入成功
- 字段值正确
- 时间戳正常

✅ **完整性测试**
- 文件大小匹配
- MD5 校验通过
- 内容无损

---

## 🎯 预期结果

```
=== 测试结果汇总 ===
✓ 文件上传成功
✓ 文件已复制到 3 个节点
✓ 数据库记录正确
✓ 文件内容完整

🎉 所有测试通过！
--- PASS: TestFileUploadIntegration (12.34s)
PASS
```

---

## 📚 详细文档

- 完整测试指南：[tests/README.md](./README.md)
- Docker 部署：[docker/README.md](../docker/README.md)
- 功能说明：[docs/REPLICATION_AND_DIRECTORY.md](../docs/REPLICATION_AND_DIRECTORY.md)
