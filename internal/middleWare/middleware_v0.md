# 技术栈中间件介绍
本项目中间件有两个，分别为RabbitMQ和Redis

## RabbitMQ

RabbitMQ用作集群中的异步任务队列，主动连接到leader节点，承载上传、下载、查询、删除、更新等存储任务的消息分发与处理。本服务会先判定本节点为 Leader，再通过 RabbitMQManager 获取 AMQP 连接、声明队列并消费对应任务消息。

### 连接细节

#### 初始化阶段

在节点初始化时，intergration.NewNode会创建RabbitMQ Manager实例，并传入本节点的 Raft 实例作为 LeaderChecker。这样管理器能读取 GetState() 判断是否为 Leader。

#### 定期检查

RabbitMQ Manager会在后台协程中每500ms轮流检测leader是否发生变化，如果发生变化，失去leader身份的节点使用disconnected，新的leader节点使用connect，与RabbitMQ Manager连接。

#### 消息传递

各个服务先获得RabbitMQ Manager中RabbitMQ实例的Channel，调用 channel的Publish 发送任务消息，然后调用Consume，处理上传的任务。业务侧把任务作为消息发布到对应队列。

## Redis

Redis 用作集群的高速缓存与轻量状态存储，承载文件元数据、分片/块索引、短期会话/令牌等高频读写数据，提升读写性能并减轻底层存储与数据库压力。所有节点均可访问 Redis，访问不依赖 Raft Leader 身份。

### 连接细节

#### 初始化阶段

节点启动时读取配置创建 Redis 客户端（例如通过地址、密码、DB 索引等参数），建立长连接池，供业务侧统一复用。

#### 定期检查

和RabbitMQ相同，通过每500ms进行一次检查进行连接。

#### 数据读写

只在读取数据时优先从 Redis 获取元数据/索引等热点数据，未命中再回源并回填缓存。
