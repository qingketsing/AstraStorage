# AstraStorage Project Structure

## Overview

`AstraStorage` 是一个面向 Kubernetes 场景的分布式云存储项目。
当前目录结构采用了“进程入口层 / 业务核心层 / 基础设施适配层 / 部署层 / 测试层”分离的方式，
目标是降低模块耦合度，提升后续对 PostgreSQL、Redis、RabbitMQ、Kubernetes 以及监控系统的扩展能力与可维护性。

---

## Top-Level Structure

```text
AstraStorage/
├── .github/
├── cmd/
├── deploy/
├── docs/
├── internal/
├── test/
├── README.md
├── go.mod
└── PROJECT_STRUCTURE.md
```

### Root Files And Directories

#### `.github/`

用于存放仓库自动化配置。
当前包含：

- `.github/workflows/ci.yml`
  GitHub Actions 流水线，负责启动临时 PostgreSQL、执行全量 `go test ./...` 和 `go build ./...`。

#### `cmd/`

用于存放各个可执行进程的启动入口。
每个子目录对应一个独立二进制程序，负责装配配置、依赖注入、服务启动和进程生命周期管理。

当前包含：

- `cmd/mds/`：元数据服务进程入口
- `cmd/datanode/`：最小数据节点进程入口
- `cmd/monitor/`：监控系统进程入口预留目录
- `cmd/gateway/`：网关或统一访问入口

其中 `cmd/mds/` 当前已经包含：

- `main.go`
  进程入口，负责启动和最小运行信息输出。
- `app.go`
  启动装配逻辑，负责把 `repo -> service -> handler -> router` 组装起来。
- `app_test.go`
  启动装配和 router 连通性测试。

其中 `cmd/datanode/` 当前已经包含：

- `main.go`
  数据节点进程入口，负责启动最小 chunk 存储 HTTP 服务。
- `app.go`
  启动装配逻辑，负责把 store 和 HTTP handler 组装起来。
- `app_test.go`
  数据节点启动装配和健康检查测试。

其中 `cmd/gateway/` 当前已经包含：

- `main.go`
  网关进程入口，负责启动最小 HTTP 服务。
- `app.go`
  启动装配逻辑，负责把上游 client 和 HTTP handler 组装起来。
- `app_test.go`
  网关启动装配和健康检查测试。

#### `deploy/`

用于存放部署相关资源。
该目录不承载业务逻辑，只负责开发、测试与生产环境的部署描述。

当前包含：

- `deploy/k8s/`：Kubernetes 部署清单
- `deploy/docker/`：本地开发或单机调试所需的容器配置

#### `docs/`

用于存放架构设计、部署说明、接口约定和运行文档。

当前包含：

- `docs/architecture/`：架构设计说明
- `docs/deploy/`：部署流程与环境说明

#### `README.md`

仓库入口说明文档。
当前用于描述项目范围、本地开发命令、CI 行为以及 PostgreSQL 集成测试的运行方式。

#### `internal/`

用于存放项目内部使用的核心代码。
按照 Go 的惯例，这里的代码不作为对外公共库暴露，重点用于组织业务模块和底层平台适配。

#### `test/`

用于存放测试资源。
测试代码与夹具、集成测试、端到端测试都统一放在该目录下，避免与业务目录混杂。

---

## Business Layer

### `internal/mds/`

该目录用于承载 Metadata Service，也就是元数据服务的核心业务逻辑。
它负责维护文件、对象、块、副本、节点等元数据信息，并作为后续元数据编排与调度的核心模块。

当前包含：

- `service.go`
  定义元数据服务的核心 `Service` 对象和依赖注入入口。

- `service_directory.go`
  目录相关用例，例如创建目录和查询 inode。

- `service_file.go`
  文件相关用例，例如创建文件和查询文件元数据。

- `service_upload.go`
  上传相关用例，例如启动上传、提交 chunk 和完成上传。

- `service_read.go`
  读路径用例，例如列目录、列 chunk、查上传会话和构建下载计划。

- `service_helpers.go`
  service 层共用的路径、时间、clone 和辅助函数。

- `handler.go`
  对上层协议的薄适配层，当前负责把请求转发到 service。

- `allocator.go`
  预留资源分配逻辑，例如副本选择、节点分配和容量感知策略。

- `errors.go`
  统一定义元数据服务中的错误类型和错误分类。

- `config/`
  放置元数据服务自身的配置模型和配置加载定义。

- `metadata/`
  放置元数据领域模型，例如 inode、chunk、object、replica 等核心元信息结构。

- `store/`
  放置元数据持久化抽象接口，不直接绑定某个数据库实现。

- `rpc/`
  放置进程内 RPC method、请求响应类型和 router。

- `service_test.go`
  service 层核心行为测试。

- `placement/`
  放置放置策略与调度逻辑，例如副本分布、故障域约束和节点选择策略。

- `discovery/`
  放置节点注册、服务发现、心跳等集群感知逻辑。

- `coordinator/`
  放置故障转移、重平衡、协调控制等高级控制逻辑。

### `internal/datanode/`

该目录用于承载最小数据节点原型。
当前职责是提供 chunk 的本地落盘、读取、删除和健康检查接口，为后续 `gateway -> mds -> datanode` 闭环做准备。

当前包含：

- `config.go`
  数据节点配置加载和默认值定义。
- `store.go`
  基于本地文件系统的 chunk 存储实现。
- `http.go`
  数据节点最小 HTTP 接口。
- `store_test.go`
  本地持久化和重启后可读测试。
- `http_test.go`
  HTTP 接口行为测试。

### `internal/gateway/`

该目录用于承载最小网关原型。
当前职责是提供对外 HTTP 入口，并探测上游 `MDS` 和 `datanode` 的健康状态，为后续真实上传/下载编排做准备。

当前包含：

- `config.go`
  网关配置加载和默认值定义。
- `client.go`
  对 MDS 和 datanode 的最小 HTTP health client。
- `http.go`
  网关最小 HTTP 接口和占位上传/下载路由。
- `http_test.go`
  网关 HTTP 行为测试。

### `internal/monitor/`

该目录用于承载自建监控系统相关业务逻辑。
它与底层采集、指标输出和告警逻辑解耦，后续可以扩展为独立的监控服务。

当前包含：

- `collector/`
  采集器相关逻辑目录，适合接入节点、服务和业务指标采集。

- `exporter/`
  指标暴露目录，适合对外输出监控数据。

- `metrics/`
  指标模型与指标注册目录。

- `api/`
  监控系统接口层预留目录。

- `ingest/`
  指标接收、写入和汇总入口预留目录。

- `rules/`
  告警规则、聚合规则和阈值规则预留目录。

- `storage/`
  监控数据存储接口或存储实现预留目录。

- `notifier/`
  告警通知与消息下发预留目录。

### `internal/shared/`

该目录用于放置多个服务之间可以共享的内部定义。
它的职责是复用通用配置、事件模型和基础类型，而不是承载具体业务实现。

当前包含：

- `config/`
  多服务共享的配置结构定义。

- `events/`
  跨模块共享的事件定义，例如异步任务事件、节点状态事件、告警事件等。

- `types/`
  多模块通用类型定义。

---

## Platform Layer

### `internal/platform/`

该目录用于承载所有基础设施适配代码。
这里的设计重点是将 PostgreSQL、Redis、RabbitMQ、Kubernetes 和观测系统的实现细节隔离出来，
避免业务层直接依赖具体中间件，从而提升系统可维护性和后续替换能力。

### `internal/platform/postgres/`

用于放置 PostgreSQL 相关实现。
业务层只依赖抽象接口，具体数据库连接、事务与仓储实现都应放在这里。

当前包含：

- `client/`
  数据库连接初始化、连接池管理与底层客户端封装。

- `repository/`
  基于 PostgreSQL 的仓储实现，例如元数据表读写、节点信息查询和事务性更新。

- `migrate/`
  数据库迁移脚本或迁移执行入口。

- `health/`
  数据库健康检查逻辑。

### `internal/platform/redis/`

用于放置 Redis 相关实现。
Redis 在该项目中通常适合作为缓存、分布式锁或轻量消息分发工具。

当前包含：

- `client/`
  Redis 客户端初始化与连接管理。

- `cache/`
  缓存读写实现，例如热点元数据缓存。

- `lock/`
  分布式锁实现，例如元数据操作互斥或领导者选举辅助。

- `pubsub/`
  基于 Redis Pub/Sub 的轻量消息订阅与广播逻辑。

### `internal/platform/mq/`

用于放置消息队列相关能力。
当前主要面向 RabbitMQ，但通过目录分层保留后续替换 Kafka 或其他 MQ 的空间。

当前包含：

- `contracts/`
  消息体契约、主题命名和投递约束定义。

- `rabbitmq/producer/`
  RabbitMQ 生产者实现。

- `rabbitmq/consumer/`
  RabbitMQ 消费者实现。

- `rabbitmq/topology/`
  Exchange、Queue、Binding 等拓扑初始化逻辑。

### `internal/platform/kube/`

用于放置与 Kubernetes 运行环境相关的适配代码。
适用于服务发现、领导者选举、控制器逻辑以及集群资源交互。

当前包含：

- `client/`
  Kubernetes API 客户端封装。

- `discovery/`
  Pod、Service、Node 等资源发现逻辑。

- `leader/`
  领导者选举逻辑。

- `controller/`
  与自定义控制器或资源协同相关的逻辑预留目录。

### `internal/platform/observability/`

用于放置观测性基础设施能力。
与 `internal/monitor/` 的区别是：这里偏向底层通用能力，`monitor/` 偏向业务化监控系统。

当前包含：

- `metrics/`
  通用指标采集与上报基础能力。

- `logging/`
  日志封装、日志规范与日志适配能力。

- `tracing/`
  链路追踪相关能力。

- `health/`
  健康检查、就绪检查和存活检查能力。

---

## Deployment Layer

### `deploy/k8s/`

用于存放 Kubernetes 环境部署清单。
建议后续按照基础资源、服务资源和中间件资源分层组织。

当前包含：

- `base/`
  公共基础配置，例如 namespace、configmap、secret 或通用模板。

- `mds/`
  元数据服务部署清单。

- `postgres/`
  PostgreSQL 部署清单。

- `redis/`
  Redis 部署清单。

- `rabbitmq/`
  RabbitMQ 部署清单。

- `monitor/`
  监控系统部署清单。

### `deploy/docker/`

用于存放本地开发或单机验证用的 Docker 资源。
通常用于快速拉起依赖环境，而不替代正式的 Kubernetes 部署。

当前包含：

- `postgres/`
- `redis/`
- `rabbitmq/`
- `monitor/`

---

## Test Layer

### `test/integration/`

用于存放集成测试。
这类测试通常会连接真实或近真实的 PostgreSQL、Redis、RabbitMQ 等组件，
用于验证模块之间的协作行为。

### `test/e2e/`

用于存放端到端测试。
这类测试更关注完整业务链路是否打通，例如从元数据请求入口到数据库持久化、消息投递与监控采集的整体流程。

### `test/fixtures/`

用于存放测试数据、样例配置、模拟输入和初始化资源。

---

## Decoupling Principles

当前结构遵循以下解耦原则：

1. 业务逻辑与基础设施实现分离
   `internal/mds` 和 `internal/monitor` 不应直接依赖具体 PostgreSQL、Redis、RabbitMQ 客户端细节。

2. 抽象优先，具体实现后置
   业务层优先面向接口设计，具体数据库或消息队列实现放在 `internal/platform`。

3. 部署资源与运行代码分离
   Kubernetes 与 Docker 的部署描述统一放在 `deploy/`，避免配置和业务代码混杂。

4. 通用能力与业务能力分层
   日志、指标、追踪、健康检查等通用基础能力放在 `platform/observability`，
   自建监控系统业务逻辑放在 `internal/monitor`。

5. 为替换中间件预留空间
   RabbitMQ 当前放在 `platform/mq/rabbitmq`，未来如需接入 Kafka，可以按同样模式新增适配目录，而无需大改业务模块。

---

## Recommended Next Steps

建议后续按以下顺序继续落地代码：

1. 在 `internal/mds/store/` 定义持久化抽象接口
2. 在 `internal/platform/postgres/repository/` 实现 PostgreSQL 版本的仓储逻辑
3. 在 `internal/shared/config/` 和 `internal/mds/config/` 定义配置结构
4. 在 `cmd/mds/` 组装配置、数据库连接和服务启动流程
5. 再逐步补充 Redis 缓存、RabbitMQ 异步任务、Kubernetes 发现与自建监控系统

这样可以先打通最核心的元数据主链路，再逐步补全平台能力。
