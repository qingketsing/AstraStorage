# 项目结构

```text
.
├── cmd/                 # 可执行程序入口
│   ├── client/          # 客户端 CLI，向 RabbitMQ 发送请求
│   └── node/            # 存储节点入口，运行 Raft、集群服务等
├── internal/
│   ├── core/            # 分布式核心逻辑
│   │   ├── cluster/     # 节点发现、心跳、成员管理、RPC
│   │   ├── consensus/   # 共识算法（Raft 等）、Leader 协调
│   │   ├── integration/ # 将 Raft + 集群 + DB + 中间件整合为 Node
│   │   └── replication/ # 数据复制相关逻辑
│   ├── db/              # 底层数据库访问（连接、仓储）
│   ├── middleware/      # Redis、RabbitMQ 等中间件封装与管理器
│   └── storage/         # 对外暴露的存储接口与业务封装
├── scripts/             # PowerShell 脚本（启动/停止集群、测试等）
├── docker/              # Docker 相关文档与辅助脚本
├── bin/                 # 编译产物输出目录
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── README.md
```