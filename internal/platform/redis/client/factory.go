package client

import (
	"fmt"

	redis "github.com/redis/go-redis/v9"
)

// Bundle groups the logical Redis clients used by AstraStorage.
type Bundle struct {
	cache *GroupClients
	coord *GroupClients
}

// GroupClients owns the read and write clients for one Sentinel master set.
type GroupClients struct {
	group      Group
	write      *redis.Client
	read       *redis.Client
	writeOpts  *redis.FailoverOptions
	readOpts   *redis.FailoverOptions
	healthInfo HealthSummary
}

// NewBundle creates the cache and coordination Redis client groups.
func NewBundle(cfg Config) (*Bundle, error) {
	cfg = cfg.WithDefaults()
	if !cfg.Enabled {
		return nil, fmt.Errorf("redis client: config must be enabled")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cache, err := newGroupClients(GroupCache, cfg, cfg.Cache)
	if err != nil {
		return nil, err
	}
	coord, err := newGroupClients(GroupCoord, cfg, cfg.Coord)
	if err != nil {
		_ = cache.Close()
		return nil, err
	}
	return &Bundle{
		cache: cache,
		coord: coord,
	}, nil
}

func (b *Bundle) Cache() *GroupClients {
	if b == nil {
		return nil
	}
	return b.cache
}

func (b *Bundle) Coord() *GroupClients {
	if b == nil {
		return nil
	}
	return b.coord
}

func (b *Bundle) Close() error {
	if b == nil {
		return nil
	}
	var firstErr error
	if b.cache != nil {
		if err := b.cache.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.coord != nil {
		if err := b.coord.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b *Bundle) HealthSummaries() []HealthSummary {
	if b == nil {
		return nil
	}
	summaries := make([]HealthSummary, 0, 2)
	if b.cache != nil {
		summaries = append(summaries, b.cache.HealthSummary())
	}
	if b.coord != nil {
		summaries = append(summaries, b.coord.HealthSummary())
	}
	return summaries
}

func (g *GroupClients) Group() Group {
	if g == nil {
		return ""
	}
	return g.group
}

func (g *GroupClients) WriteClient() *redis.Client {
	if g == nil {
		return nil
	}
	return g.write
}

func (g *GroupClients) ReadClient() *redis.Client {
	if g == nil {
		return nil
	}
	return g.read
}

func (g *GroupClients) WriteOptions() redis.FailoverOptions {
	if g == nil || g.writeOpts == nil {
		return redis.FailoverOptions{}
	}
	return *g.writeOpts
}

func (g *GroupClients) ReadOptions() redis.FailoverOptions {
	if g == nil || g.readOpts == nil {
		return redis.FailoverOptions{}
	}
	return *g.readOpts
}

func (g *GroupClients) HealthSummary() HealthSummary {
	if g == nil {
		return HealthSummary{}
	}
	return g.healthInfo
}

func (g *GroupClients) Close() error {
	if g == nil {
		return nil
	}
	var firstErr error
	if g.write != nil {
		if err := g.write.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if g.read != nil {
		if err := g.read.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func newGroupClients(group Group, cfg Config, groupCfg ReplicationGroupConfig) (*GroupClients, error) {
	writeOpts := newFailoverOptions(cfg, groupCfg, false)
	readOpts := newFailoverOptions(cfg, groupCfg, true)
	return &GroupClients{
		group:      group,
		write:      redis.NewFailoverClient(writeOpts),
		read:       redis.NewFailoverClient(readOpts),
		writeOpts:  writeOpts,
		readOpts:   readOpts,
		healthInfo: newHealthSummary(group, groupCfg.MasterSetName, cfg.SentinelEndpoints),
	}, nil
}
