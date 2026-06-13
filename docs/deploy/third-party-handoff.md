# AstraStorage 第三方部署与接手文档

## 文档目的

这份文档用于把 AstraStorage 交给第三方团队继续部署、验收、运维或二次开发。

当前仓库提供的是 Kubernetes 场景下的分布式存储 PoC / 工程原型，不是生产级高可用存储系统。第三方接手时应先完成本文的 PoC 验收，再评估是否进入准生产改造。

## 当前系统边界

当前已具备的最小闭环：

```text
client -> gateway -> mds -> datanode -> mds -> gateway -> client
```

核心组件：

- `gateway`：对外 HTTP 入口，处理上传、下载、删除。
- `mds`：元数据服务，管理目录、文件、chunk、副本、节点和上传会话。
- `datanode`：chunk 字节存储节点，本地 PVC 落盘。
- `postgres`：MDS 元数据持久化后端。
- `monitor`：可选 Prometheus Operator 资源。

当前 Kubernetes 拓扑：

- 单副本 PostgreSQL StatefulSet
- 单副本 MDS Deployment
- 单副本 datanode StatefulSet + PVC
- 单副本 gateway Deployment
- 可选 ServiceMonitor 和 PrometheusRule

当前不包含：

- 多副本 MDS 高可用
- 多 datanode StatefulSet 拓扑
- HA PostgreSQL
- 生产级 TLS / Auth / NetworkPolicy
- Ingress
- 跨节点真实容量调度和故障域调度
- Redis / RabbitMQ / etcd 的完整生产部署闭环

## 接手前置条件

第三方环境需要具备：

- Linux 或 macOS 开发机
- Go 1.24.x
- Docker
- kubectl
- kind 或 minikube
- curl
- python3

可选：

- Helm
- kube-prometheus-stack
- psql
- jq

建议先在本地 `kind` 或 `minikube` 完成一轮部署验收，再迁移到远端 Kubernetes 集群。

## 代码和文档入口

建议阅读顺序：

1. [README.md](/home/qingke/AstraStorage/README.md)
2. [PROJECT_STRUCTURE.md](/home/qingke/AstraStorage/PROJECT_STRUCTURE.md)
3. [docs/project-features.md](/home/qingke/AstraStorage/docs/project-features.md)
4. [docs/architecture/system-architecture.md](/home/qingke/AstraStorage/docs/architecture/system-architecture.md)
5. [docs/architecture/kubernetes-deployment.md](/home/qingke/AstraStorage/docs/architecture/kubernetes-deployment.md)
6. [docs/architecture/manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)
7. [docs/architecture/technical-debt-roadmap.md](/home/qingke/AstraStorage/docs/architecture/technical-debt-roadmap.md)

关键代码入口：

- [cmd/mds](/home/qingke/AstraStorage/cmd/mds)
- [cmd/datanode](/home/qingke/AstraStorage/cmd/datanode)
- [cmd/gateway](/home/qingke/AstraStorage/cmd/gateway)
- [internal/mds](/home/qingke/AstraStorage/internal/mds)
- [internal/datanode](/home/qingke/AstraStorage/internal/datanode)
- [internal/gateway](/home/qingke/AstraStorage/internal/gateway)
- [internal/platform/postgres](/home/qingke/AstraStorage/internal/platform/postgres)
- [deploy/k8s](/home/qingke/AstraStorage/deploy/k8s)
- [deploy/docker/app](/home/qingke/AstraStorage/deploy/docker/app)

## 快速部署方案

### 本地一键部署

在仓库根目录运行：

```bash
bash scripts/deploy-k8s.sh
```

脚本会执行：

```text
1. 构建 astrastorage/mds:local、astrastorage/datanode:local、astrastorage/gateway:local
2. 自动识别 kind 或 minikube
3. 将镜像加载进本地集群
4. 按顺序应用 deploy/k8s 下的 Kustomize manifests
5. 等待 postgres、mds、datanode、gateway rollout 完成
6. 输出 pods、svc、pvc 状态
```

部署后检查：

```bash
kubectl get pods,svc,pvc -n astrastorage
```

### 部署并自动验收

```bash
bash scripts/deploy-k8s.sh --smoke
```

`--smoke` 会自动 port-forward gateway，并运行：

```bash
bash scripts/poc-smoke.sh
```

验收覆盖：

- gateway 健康检查
- 小文件上传
- 多 chunk 文件上传
- 文件元数据查询
- chunk 列表查询
- 下载计划查询
- 文件下载和内容比对
- 文件删除
- 删除后不可查询确认

### 指定集群类型

```bash
bash scripts/deploy-k8s.sh --cluster kind
bash scripts/deploy-k8s.sh --cluster minikube
```

如果镜像已经构建并加载，可以跳过：

```bash
bash scripts/deploy-k8s.sh --skip-build --skip-load
```

### 部署监控资源

只有在集群已安装 Prometheus Operator CRD 时才执行：

```bash
bash scripts/deploy-k8s.sh --with-monitor
```

否则 `ServiceMonitor` 和 `PrometheusRule` 会因为 CRD 不存在而失败。

## 手动部署方案

手动构建镜像：

```bash
bash scripts/build-images.sh
```

kind 加载镜像：

```bash
kind load docker-image astrastorage/mds:local
kind load docker-image astrastorage/datanode:local
kind load docker-image astrastorage/gateway:local
```

minikube 加载镜像：

```bash
minikube image load astrastorage/mds:local
minikube image load astrastorage/datanode:local
minikube image load astrastorage/gateway:local
```

按顺序部署：

```bash
kubectl apply -k deploy/k8s/base
kubectl apply -k deploy/k8s/postgres
kubectl apply -k deploy/k8s/mds
kubectl apply -k deploy/k8s/datanode
kubectl apply -k deploy/k8s/gateway
```

等待就绪：

```bash
kubectl -n astrastorage rollout status statefulset/astra-postgres
kubectl -n astrastorage rollout status deployment/astra-mds
kubectl -n astrastorage rollout status statefulset/astra-datanode
kubectl -n astrastorage rollout status deployment/astra-gateway
```

可选监控：

```bash
kubectl apply -k deploy/k8s/monitor
```

## 访问方式

转发 gateway：

```bash
kubectl -n astrastorage port-forward svc/astra-gateway 11080:11080
```

检查：

```bash
curl http://127.0.0.1:11080/healthz
```

转发 MDS：

```bash
kubectl -n astrastorage port-forward svc/astra-mds 8080:8080
```

检查：

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metrics
```

## 验收标准

交接验收至少应通过：

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
bash scripts/deploy-k8s.sh --smoke
```

Kubernetes 侧预期：

```text
namespace/astrastorage 存在
statefulset/astra-postgres Ready
deployment/astra-mds Ready
statefulset/astra-datanode Ready
deployment/astra-gateway Ready
postgres PVC Bound
datanode PVC Bound
gateway /healthz 返回 ok
poc-smoke.sh 成功完成
```

## 远端集群部署方案

远端集群通常不能使用 `kind load` 或 `minikube image load`。推荐流程：

1. 构建镜像并推送到镜像仓库。
2. 修改 `deploy/k8s/*/deployment.yaml` 或使用 overlay 替换镜像地址。
3. 确认远端集群有默认 StorageClass。
4. 应用 manifests。
5. 执行 rollout 和 smoke 验收。

示例：

```bash
IMAGE_PREFIX=registry.example.com/astrastorage IMAGE_TAG=2026-06-13 \
  bash scripts/build-images.sh
```

推送镜像：

```bash
docker push registry.example.com/astrastorage/mds:2026-06-13
docker push registry.example.com/astrastorage/datanode:2026-06-13
docker push registry.example.com/astrastorage/gateway:2026-06-13
```

远端集群必须能拉取这些镜像。如果 registry 需要认证，需要提前创建 `imagePullSecrets` 并更新 manifests。

## 配置说明

主要环境变量：

```text
MDS_HTTP_ADDR=:8080
MDS_STORE_BACKEND=postgres
MDS_POSTGRES_DSN=<from Secret/astra-postgres>
MDS_REPAIR_INTERVAL=15s
MDS_REPAIR_HTTP_TIMEOUT=5s
MDS_REPAIR_RETRY_BACKOFF=30s
MDS_REPAIR_MAX_REPLICAS_PER_RUN=32

DATANODE_HTTP_ADDR=:10080
DATANODE_DATA_DIR=/data/datanode
DATANODE_MDS_HTTP_BASE_URL=http://astra-mds.astrastorage.svc.cluster.local:8080
DATANODE_ADVERTISE_URL=http://astra-datanode.astrastorage.svc.cluster.local:10080
DATANODE_CAPACITY_BYTES=10737418240

GATEWAY_HTTP_ADDR=:11080
GATEWAY_MDS_HTTP_BASE_URL=http://astra-mds.astrastorage.svc.cluster.local:8080
GATEWAY_DATANODE_BASE_URL=http://astra-datanode.astrastorage.svc.cluster.local:10080
```

Kubernetes 清单位置：

- PostgreSQL: [deploy/k8s/postgres](/home/qingke/AstraStorage/deploy/k8s/postgres)
- MDS: [deploy/k8s/mds](/home/qingke/AstraStorage/deploy/k8s/mds)
- Datanode: [deploy/k8s/datanode](/home/qingke/AstraStorage/deploy/k8s/datanode)
- Gateway: [deploy/k8s/gateway](/home/qingke/AstraStorage/deploy/k8s/gateway)

## 数据持久化

当前持久化点：

- PostgreSQL 使用 StatefulSet 的 PVC 保存 MDS 元数据。
- datanode 使用 StatefulSet 的 PVC 保存 chunk 字节和 sidecar metadata。

清理应用但保留数据：

```bash
kubectl delete -k deploy/k8s/gateway
kubectl delete -k deploy/k8s/datanode
kubectl delete -k deploy/k8s/mds
kubectl delete -k deploy/k8s/postgres
```

彻底清理数据需要额外删除 PVC：

```bash
kubectl delete pvc -n astrastorage --all
```

删除 PVC 会丢失 PostgreSQL 元数据和 datanode chunk 数据。

## 日常运维命令

查看状态：

```bash
kubectl get pods,svc,pvc -n astrastorage
```

查看日志：

```bash
kubectl -n astrastorage logs deployment/astra-mds
kubectl -n astrastorage logs statefulset/astra-datanode
kubectl -n astrastorage logs deployment/astra-gateway
kubectl -n astrastorage logs statefulset/astra-postgres
```

查看事件：

```bash
kubectl -n astrastorage get events --sort-by=.lastTimestamp
```

重启组件：

```bash
kubectl -n astrastorage rollout restart deployment/astra-mds
kubectl -n astrastorage rollout restart statefulset/astra-datanode
kubectl -n astrastorage rollout restart deployment/astra-gateway
```

进入 PostgreSQL：

```bash
kubectl -n astrastorage exec -it statefulset/astra-postgres -- psql -U astra -d astra
```

## 故障排查

### Pod 一直 ImagePullBackOff

常见原因：

- 本地集群没有加载 `astrastorage/*:local` 镜像。
- 远端集群无法访问镜像仓库。
- manifests 里的镜像地址和实际镜像 tag 不一致。

本地修复：

```bash
bash scripts/deploy-k8s.sh --skip-build
```

远端修复：

- 确认镜像已 push。
- 确认 `imagePullSecrets`。
- 确认 manifests 中 image 字段。

### MDS 起不来

重点检查 PostgreSQL：

```bash
kubectl get pods -n astrastorage
kubectl -n astrastorage logs statefulset/astra-postgres
kubectl -n astrastorage logs deployment/astra-mds
kubectl -n astrastorage describe pod -l app.kubernetes.io/name=astra-mds
```

MDS 启动时会运行 PostgreSQL migration。DSN、Secret 或数据库不可达会导致启动失败。

### datanode 起不来

重点检查：

- MDS `/healthz` 是否 ready
- PVC 是否 Bound
- datanode initContainer 是否一直等待 MDS

```bash
kubectl -n astrastorage describe pod -l app.kubernetes.io/name=astra-datanode
kubectl -n astrastorage logs statefulset/astra-datanode
```

### smoke 上传失败

先确认 gateway、MDS、datanode 都健康：

```bash
kubectl -n astrastorage port-forward svc/astra-gateway 11080:11080
curl http://127.0.0.1:11080/healthz
```

再看三类日志：

```bash
kubectl -n astrastorage logs deployment/astra-gateway
kubectl -n astrastorage logs deployment/astra-mds
kubectl -n astrastorage logs statefulset/astra-datanode
```

## 升级与回滚

开发环境升级：

```bash
bash scripts/deploy-k8s.sh
```

远端环境升级建议：

1. 使用不可变镜像 tag，例如日期或 git SHA。
2. 更新 manifests 或 overlay。
3. `kubectl apply -k ...`
4. 等待 rollout。
5. 跑 smoke。

回滚 Deployment：

```bash
kubectl -n astrastorage rollout undo deployment/astra-mds
kubectl -n astrastorage rollout undo deployment/astra-gateway
```

datanode 是 StatefulSet，回滚前应确认 PVC 数据兼容性。当前 PoC 没有数据库 migration 回滚机制，涉及 PostgreSQL schema 的变更必须先评估。

## 安全与生产化改造清单

进入生产或准生产前，至少需要补齐：

- 替换默认 PostgreSQL 用户名、密码和 DSN Secret。
- 增加 TLS 和认证授权。
- 增加 Ingress 或 API Gateway 接入策略。
- 增加 NetworkPolicy。
- 增加 ResourceQuota、LimitRange、PodDisruptionBudget。
- 使用 HA PostgreSQL 或托管数据库。
- 增加多 datanode 拓扑和真实副本调度。
- 增加备份和恢复流程。
- 增加日志采集、指标面板和告警值班流程。
- 把 `content_base64` 上传路径替换为正式流式上传。

## 交接清单

第三方接手时应确认：

- 能在本地或测试集群成功执行 `bash scripts/deploy-k8s.sh --smoke`。
- 已明确当前版本是 PoC，不承诺生产 SLA。
- 已拿到镜像仓库地址和推送权限，或确认使用本地镜像加载。
- 已确认目标集群 StorageClass 可创建 PVC。
- 已确认是否部署 Prometheus Operator。
- 已确认是否需要保留 PVC 数据。
- 已阅读技术债文档。
- 已记录后续改造优先级和负责人。

## 推荐后续路线

建议第三方接手后的迭代顺序：

1. 固化远端镜像仓库和 Kustomize overlay。
2. 做多 datanode Kubernetes 拓扑。
3. 增加 Ingress、认证和 TLS。
4. 补齐监控面板、日志采集和告警。
5. 引入备份恢复方案。
6. 将 gateway 上传路径升级为流式上传。
7. 评估 PostgreSQL HA 和 MDS 多副本。
