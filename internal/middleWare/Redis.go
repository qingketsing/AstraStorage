package middleware

type Redis struct {
	// Redis 连接相关字段
}

func NewRedisConnection(address string) (*Redis, error) {
	// 初始化 Redis 连接的逻辑
	return &Redis{}, nil
}
