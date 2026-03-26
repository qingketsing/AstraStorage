# AstraStorage 新会话接力文档

这份文档用于在重启 Codex CLI 或切换到新会话后，快速把当前项目状态交给下一个会话。

建议做法：

1. 先让新会话阅读这份文档
2. 再补一句你当前最想继续做的任务
3. 然后直接继续开发，不需要重新解释历史

---

## 可以直接贴给下一个会话的接力提示词

```text
请先阅读 /home/qingke/AstraStorage/docs/architecture/session-handoff.md，然后继续接手这个项目。当前仓库是 AstraStorage，一个面向 Kubernetes 的分布式存储面试原型。请基于文档里记录的当前实现状态、技术债和下一步优先级继续工作，不要重复做已经完成的部分。先总结你对当前状态的理解，再执行我接下来给你的任务。
```

---

## 项目定位

`AstraStorage` 当前不是完整生产级分布式存储系统，而是一个**面试可交付版最小分布式存储原型**。

当前系统已经具备这条最小闭环：

`client -> gateway -> mds -> datanode -> mds -> gateway -> client`

已经能做：

- 上传文件
- 下载文件
- 删除文件
- 元数据落 PostgreSQL
- 主节点接收后向副本节点分发
- `pending` 副本后台补齐

---

## 当前总体状态

### 已完成

- `MDS` 元数据服务已经成型
  - 目录、文件、chunk、副本、节点、上传会话
  - 上传状态机
  - 下载计划
  - 节点注册、心跳、上传目标分配
  - HTTP 和 gRPC 接口
  - `memory` 和 `PostgreSQL` backend

- `datanode` 已经可运行
  - chunk 本地落盘
  - `PUT/GET/DELETE /chunks/<chunkID>`
  - `POST /internal/replicate`
  - 启动时向 MDS 注册并定时心跳

- `gateway` 已经可运行
  - `POST /uploads`
  - `GET /downloads/<fileID>`
  - `DELETE /files/<fileID>`
  - 当前上传是串行多 chunk
  - 主节点先接收，再向副本节点分发

- `repair loop` 已经存在并做过一轮加固
  - MDS 定时扫描 `pending` 副本
  - 找一个 `ready` 源副本节点复制到目标节点
  - 已加入基础退避
  - 已加入单轮修复上限
  - 已避免短时间重复打同一个 pending 副本

- 工程配套已具备
  - `go test ./...`
  - `go build ./...`
  - PostgreSQL 集成测试
  - e2e 测试
  - GitHub Actions CI
  - 手工测试文档
  - 技术债文档

### 当前仍然是 MVP 的部分

- repair 还是**全量扫描**，不是事件驱动调度
- datanode 还是**本地文件系统原型**
- gateway 上传接口仍然是 `content_base64`
- 节点分配仍然是**简化版健康节点选择**
- 没有完整监控系统
- 没有 Redis / RabbitMQ
- 没有完整 Kubernetes 部署闭环

---

## 关键入口文件

### 服务入口

- [cmd/mds/main.go](/home/qingke/AstraStorage/cmd/mds/main.go)
- [cmd/mds/app.go](/home/qingke/AstraStorage/cmd/mds/app.go)
- [cmd/datanode/main.go](/home/qingke/AstraStorage/cmd/datanode/main.go)
- [cmd/datanode/app.go](/home/qingke/AstraStorage/cmd/datanode/app.go)
- [cmd/gateway/main.go](/home/qingke/AstraStorage/cmd/gateway/main.go)
- [cmd/gateway/app.go](/home/qingke/AstraStorage/cmd/gateway/app.go)

### 核心业务

- [internal/mds/service_upload.go](/home/qingke/AstraStorage/internal/mds/service_upload.go)
- [internal/mds/service_read.go](/home/qingke/AstraStorage/internal/mds/service_read.go)
- [internal/mds/service_mutation.go](/home/qingke/AstraStorage/internal/mds/service_mutation.go)
- [internal/mds/service_node.go](/home/qingke/AstraStorage/internal/mds/service_node.go)
- [internal/mds/coordinator/repairer.go](/home/qingke/AstraStorage/internal/mds/coordinator/repairer.go)
- [internal/datanode/http.go](/home/qingke/AstraStorage/internal/datanode/http.go)
- [internal/gateway/http.go](/home/qingke/AstraStorage/internal/gateway/http.go)
- [internal/gateway/client.go](/home/qingke/AstraStorage/internal/gateway/client.go)

### PostgreSQL

- [internal/platform/postgres/migrate/sql/001_init_mds.sql](/home/qingke/AstraStorage/internal/platform/postgres/migrate/sql/001_init_mds.sql)
- [internal/platform/postgres/repository](/home/qingke/AstraStorage/internal/platform/postgres/repository)

### 文档

- [README.md](/home/qingke/AstraStorage/README.md)
- [PROJECT_STRUCTURE.md](/home/qingke/AstraStorage/PROJECT_STRUCTURE.md)
- [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)
- [technical-debt-roadmap.md](/home/qingke/AstraStorage/docs/architecture/technical-debt-roadmap.md)

---

## 当前可运行能力

### MDS

- 目录管理
- 文件管理
- 上传状态机
- 下载计划
- 节点注册和心跳
- 上传目标分配
- PostgreSQL 持久化
- HTTP / gRPC 接口

### datanode

- 本地 chunk 落盘
- 读取 chunk
- 删除 chunk
- 主节点复制到副本节点

### gateway

- 上传
- 下载
- 删除

### 后台任务

- `repair loop` 自动补齐 `pending` 副本

---

## 最近一次关键实现

最近已经完成并验证通过的工作：

1. 文件删除闭环
   - `gateway -> MDS -> datanode`
   - 先删真实 chunk，再删 MDS 元数据

2. 副本复制协议正式化
   - 从 header 传参迁移成 `POST /internal/replicate`

3. `pending` 副本补齐
   - MDS 后台 repair loop 扫描并补齐

4. repair loop 加固
   - 失败退避
   - 单轮修复上限
   - 避免重复尝试同一副本

5. 手工测试文档
   - 已支持 PostgreSQL 手查 chunk 位置和节点地址

---

## 当前测试与验证状态

最近完成的验证口径：

```bash
GOCACHE=/tmp/go-cache go test ./...
GOCACHE=/tmp/go-cache go build ./...
```

上面两条最近一次都是通过的。

当前也有：

- PostgreSQL 集成测试
- e2e 测试
- gateway / datanode / repairer 层测试

---

## 手工联调文档

如果新会话需要快速人工验证系统，请优先看：

- [manual-testing.md](/home/qingke/AstraStorage/docs/architecture/manual-testing.md)

它已经包含：

- Docker 启 PostgreSQL
- 启动 MDS
- 启动 3 个 datanode
- 启动 gateway
- 手工上传 / 下载 / 删除
- PostgreSQL 查询 chunk 副本位置和节点地址

---

## 当前技术债要求

用户明确要求：

**以后只要为了先打通链路做了简化实现，就必须同步更新技术债文档。**

技术债文档位置：

- [technical-debt-roadmap.md](/home/qingke/AstraStorage/docs/architecture/technical-debt-roadmap.md)

记录格式必须包含：

- 当前为什么这样做
- 风险是什么
- 什么时候该还
- 还债顺序

这条要求是默认规则，不需要用户每次重复提醒。

---

## 下一步优先级

当前最推荐的后续顺序是：

1. 完整监控设计与最小埋点
   - 先定义业务指标、日志字段和 `/metrics`
   - 再考虑 Prometheus / Grafana

2. 完整 e2e
   - 上传 -> 副本失败 -> repair -> 下载 -> 删除

3. Docker / 本地一键联调
   - 把 `postgres + mds + gateway + 多 datanode` 串起来

4. Kubernetes
   - 不建议先做 k8s，再做监控
   - 当前结论是：**先监控，再 k8s**

---

## 关于监控的当前结论

当前已经讨论清楚但还没写代码的结论：

- 先做监控，再做 Kubernetes
- 监控不只是 Prometheus，还应至少包含：
  - metrics
  - structured logs
  - health/readiness
- 优先监控这些业务指标：
  - 上传请求数、成功率、耗时
  - 下载请求数、成功率、耗时
  - 删除请求数、成功率、耗时
  - `ready/pending` 副本数
  - repair loop 扫描数、修复成功数、修复失败数、backoff 跳过数
  - 节点总数、健康节点数

---

## 关于 repair loop 的当前结论

当前 repair loop 的结论已经明确：

- 现在是**全量扫描**
- 不是事件驱动
- 已有基础退避和单轮修复上限
- 仍然不等于完整调度系统

以后如果继续增强，优先演进方向是：

1. 从全量扫描变成事件驱动 / repair queue
2. 再做更成熟的调度与优先级

---

## 关于 superpowers 技能的说明

本机已经完成这些安装动作：

- 仓库已克隆到 `/home/qingke/.codex/superpowers`
- 已建立软链：
  - `/home/qingke/.agents/skills/superpowers`
  - `/home/qingke/.codex/skills/superpowers`

但当前对话里看不到这些技能，说明这个运行环境的技能发现可能不是纯磁盘热加载。

所以：

- 磁盘安装已完成
- 当前会话技能列表未刷新不代表安装失败
- 新会话如果仍看不到，需要继续排查平台侧技能发现机制

---

## 给新会话的工作原则

新会话接手后请遵守这些原则：

- 不要重复实现已经完成的功能
- 先阅读这份文档，再决定下一步
- 若引入新的简化方案，必须更新技术债文档
- 除非用户明确要求，不要为了“更优雅”去推翻当前可运行 MVP
- 当前项目目标是“面试可交付版完整原型”，不是生产级系统

---

## 文档更新时间

- 日期：`2026-03-22`
- 生成时项目状态：上传 / 下载 / 删除 / 副本复制 / repair loop / PostgreSQL / 手工测试文档都已具备
