package client

import redis "github.com/redis/go-redis/v9"

func newFailoverOptions(cfg Config, groupCfg ReplicationGroupConfig, replicaOnly bool) *redis.FailoverOptions {
	return &redis.FailoverOptions{
		MasterName:       groupCfg.MasterSetName,
		SentinelAddrs:    append([]string(nil), cfg.SentinelEndpoints...),
		Username:         cfg.Username,
		Password:         cfg.Password,
		SentinelUsername: cfg.Username,
		SentinelPassword: cfg.Password,
		DialTimeout:      cfg.DialTimeout,
		ReadTimeout:      cfg.ReadTimeout,
		WriteTimeout:     cfg.WriteTimeout,
		ReplicaOnly:      replicaOnly,
	}
}
