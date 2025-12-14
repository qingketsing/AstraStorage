# Docker 镜像拉取问题解决方案

## 问题描述
Docker无法从Docker Hub下载镜像，显示连接超时错误。

## 解决方案

### 方案1：配置国内镜像加速（推荐）

1. 打开 Docker Desktop
2. 点击右上角设置图标 ⚙️
3. 选择 "Docker Engine"
4. 添加国内镜像源到配置：

```json
{
  "registry-mirrors": [
    "https://docker.m.daocloud.io",
    "https://docker.mirrors.sjtug.sjtu.edu.cn",
    "https://registry.docker-cn.com"
  ]
}
```

5. 点击 "Apply & Restart"
6. 等待Docker重启完成
7. 重新运行：`.\scripts\start_docker_cluster.ps1`

### 方案2：手动拉取镜像

```powershell
# 使用镜像加速手动拉取
docker pull docker.m.daocloud.io/library/golang:1.21-alpine
docker tag docker.m.daocloud.io/library/golang:1.21-alpine golang:1.21-alpine

docker pull docker.m.daocloud.io/library/alpine:latest
docker tag docker.m.daocloud.io/library/alpine:latest alpine:latest
```

### 方案3：使用本地已有镜像构建

如果有其他Go镜像，可以修改Dockerfile使用本地镜像。

### 方案4：离线方式（不推荐，仅供紧急情况）

暂时跳过Docker，继续使用本地PowerShell运行：
```powershell
.\scripts\stop_nodes.ps1
.\scripts\start_nodes.ps1
```

## 验证镜像加速配置

```powershell
# 测试镜像拉取速度
docker pull docker.m.daocloud.io/library/alpine:latest

# 成功后重新构建
.\scripts\start_docker_cluster.ps1
```

## 常见错误

### Error: "dial tcp: connectex: A connection attempt failed"
- **原因**: 无法连接到Docker Hub
- **解决**: 配置镜像加速（方案1）

### Error: "failed to fetch anonymous token"
- **原因**: 认证失败，通常是网络问题
- **解决**: 使用国内镜像源

### Error: "TLS handshake timeout"
- **原因**: 网络不稳定
- **解决**: 检查VPN/代理设置，或使用镜像加速
