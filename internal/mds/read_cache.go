package mds

import (
	"context"
	"errors"
	"fmt"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
	rediscache "AstraStorage/internal/platform/redis/cache"
	redisclient "AstraStorage/internal/platform/redis/client"
	redislock "AstraStorage/internal/platform/redis/lock"
	redis "github.com/redis/go-redis/v9"
)

const (
	fileMetaLockPrefix      = "astra:lock:file:meta:"
	downloadPlanLockPrefix  = "astra:lock:download:plan:"
	directoryListLockPrefix = "astra:lock:dir:list:"
	nodeInfoLockPrefix      = "astra:lock:node:"
)

type readCacheBackend interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Incr(ctx context.Context, key string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	ZIncrBy(ctx context.Context, key string, increment float64, member string) *redis.FloatCmd
}

type ReadCache interface {
	GetFile(ctx context.Context, fileID metadata.FileID, loader func(context.Context) (*metadata.FileMetadata, error)) (*metadata.FileMetadata, error)
	GetDownloadPlan(ctx context.Context, fileID metadata.FileID, loader func(context.Context) (*DownloadPlan, error)) (*DownloadPlan, error)
	GetChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions, loader func(context.Context) ([]metadata.DirectoryEntry, error)) ([]metadata.DirectoryEntry, error)
	GetNode(ctx context.Context, nodeID metadata.NodeID, loader func(context.Context) (*metadata.NodeInfo, error)) (*metadata.NodeInfo, error)
	GetHealthyNodes(ctx context.Context, loader func(context.Context) ([]metadata.NodeInfo, error)) ([]metadata.NodeInfo, error)
	InvalidateFile(ctx context.Context, fileID metadata.FileID) error
	InvalidateDirectory(ctx context.Context, inodeID metadata.InodeID) error
	InvalidateNode(ctx context.Context, nodeID metadata.NodeID) error
	InvalidateHealthyNodes(ctx context.Context) error
}

type serviceReadCache struct {
	reader readCacheBackend
	writer readCacheBackend
	policy rediscache.Policy
	locker *redislock.Locker
}

func NewRedisReadCache(bundle *redisclient.Bundle, groupCfg redisclient.ReplicationGroupConfig, lockTTL time.Duration) ReadCache {
	if bundle == nil || bundle.Cache() == nil || bundle.Coord() == nil {
		return nil
	}
	return newServiceReadCache(
		bundle.Cache().ReadClient(),
		bundle.Cache().WriteClient(),
		bundle.Coord().WriteClient(),
		rediscache.NewPolicy(groupCfg),
		lockTTL,
	)
}

func newServiceReadCache(reader, writer, lockBackend readCacheBackend, policy rediscache.Policy, lockTTL time.Duration) *serviceReadCache {
	if reader == nil || writer == nil || lockBackend == nil {
		return nil
	}
	if lockTTL <= 0 {
		lockTTL = 5 * time.Second
	}
	return &serviceReadCache{
		reader: reader,
		writer: writer,
		policy: policy,
		locker: redislock.NewLocker(lockBackend, lockTTL),
	}
}

func (c *serviceReadCache) GetFile(ctx context.Context, fileID metadata.FileID, loader func(context.Context) (*metadata.FileMetadata, error)) (*metadata.FileMetadata, error) {
	if c == nil || fileID == "" {
		return loader(ctx)
	}
	key := rediscache.FileMetaKey(string(fileID))
	nullKey := rediscache.NullFileKey(string(fileID))

	var cached metadata.FileMetadata
	if hit, err := c.loadJSON(ctx, key, &cached); err != nil {
		return nil, err
	} else if hit {
		c.trackHotspot(ctx, rediscache.HotFileSetKey(), string(fileID))
		return cloneFile(&cached), nil
	}
	if hit, err := c.checkNull(ctx, nullKey); err != nil {
		return nil, err
	} else if hit {
		return nil, fmt.Errorf("%w: file %q", store.ErrNotFound, fileID)
	}

	payload, err := c.readThroughWithLock(ctx, fileMetaLockPrefix+string(fileID), key, nullKey, c.policy.FileMetaTTL, c.policy.FileMetaTTLJitter, func(ctx context.Context) ([]byte, error) {
		file, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return rediscache.Encode(file)
	})
	if err != nil {
		return nil, err
	}
	if err := rediscache.Decode(payload, &cached); err != nil {
		return nil, err
	}
	c.trackHotspot(ctx, rediscache.HotFileSetKey(), string(fileID))
	return cloneFile(&cached), nil
}

func (c *serviceReadCache) GetDownloadPlan(ctx context.Context, fileID metadata.FileID, loader func(context.Context) (*DownloadPlan, error)) (*DownloadPlan, error) {
	if c == nil || fileID == "" {
		return loader(ctx)
	}
	key := rediscache.DownloadPlanKey(string(fileID))
	nullKey := rediscache.NullFileKey(string(fileID))

	var cached DownloadPlan
	if hit, err := c.loadJSON(ctx, key, &cached); err != nil {
		return nil, err
	} else if hit {
		c.trackHotspot(ctx, rediscache.HotFileSetKey(), string(fileID))
		return cloneDownloadPlan(&cached), nil
	}
	if hit, err := c.checkNull(ctx, nullKey); err != nil {
		return nil, err
	} else if hit {
		return nil, fmt.Errorf("%w: file %q", store.ErrNotFound, fileID)
	}

	payload, err := c.readThroughWithLock(ctx, downloadPlanLockPrefix+string(fileID), key, nullKey, c.policy.DownloadPlanTTL, c.policy.FileMetaTTLJitter, func(ctx context.Context) ([]byte, error) {
		plan, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return rediscache.Encode(plan)
	})
	if err != nil {
		return nil, err
	}
	if err := rediscache.Decode(payload, &cached); err != nil {
		return nil, err
	}
	c.trackHotspot(ctx, rediscache.HotFileSetKey(), string(fileID))
	return cloneDownloadPlan(&cached), nil
}

func (c *serviceReadCache) GetNode(ctx context.Context, nodeID metadata.NodeID, loader func(context.Context) (*metadata.NodeInfo, error)) (*metadata.NodeInfo, error) {
	if c == nil || nodeID == "" {
		return loader(ctx)
	}
	key := rediscache.NodeHealthKey(string(nodeID))

	var cached metadata.NodeInfo
	if hit, err := c.loadJSON(ctx, key, &cached); err != nil {
		return nil, err
	} else if hit {
		c.trackHotspot(ctx, rediscache.HotNodeSetKey(), string(nodeID))
		return cloneNode(&cached), nil
	}

	payload, err := c.readThroughWithLock(ctx, nodeInfoLockPrefix+string(nodeID), key, "", c.policy.NodeHealthTTL, 0, func(ctx context.Context) ([]byte, error) {
		node, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return rediscache.Encode(node)
	})
	if err != nil {
		return nil, err
	}
	if err := rediscache.Decode(payload, &cached); err != nil {
		return nil, err
	}
	c.trackHotspot(ctx, rediscache.HotNodeSetKey(), string(nodeID))
	return cloneNode(&cached), nil
}

func (c *serviceReadCache) GetChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions, loader func(context.Context) ([]metadata.DirectoryEntry, error)) ([]metadata.DirectoryEntry, error) {
	if c == nil || parentID == "" || opts.Recursive {
		return loader(ctx)
	}
	version, err := c.directoryListVersion(ctx, parentID)
	if err != nil {
		return nil, err
	}
	key := rediscache.DirectoryListKey(fmt.Sprintf("%s:v%s", parentID, version), opts.Offset, opts.Limit)

	var cached []metadata.DirectoryEntry
	if hit, err := c.loadJSON(ctx, key, &cached); err != nil {
		return nil, err
	} else if hit {
		c.trackHotspot(ctx, rediscache.HotDirectorySetKey(), string(parentID))
		return cloneDirectoryEntries(cached), nil
	}

	payload, err := c.readThroughWithLock(ctx, directoryListLockPrefix+string(parentID), key, "", c.policy.DirectoryListTTL, c.policy.FileMetaTTLJitter, func(ctx context.Context) ([]byte, error) {
		children, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return rediscache.Encode(children)
	})
	if err != nil {
		return nil, err
	}
	if err := rediscache.Decode(payload, &cached); err != nil {
		return nil, err
	}
	c.trackHotspot(ctx, rediscache.HotDirectorySetKey(), string(parentID))
	return cloneDirectoryEntries(cached), nil
}

func (c *serviceReadCache) GetHealthyNodes(ctx context.Context, loader func(context.Context) ([]metadata.NodeInfo, error)) ([]metadata.NodeInfo, error) {
	if c == nil {
		return loader(ctx)
	}
	key := rediscache.HealthyNodesKey()

	var cached []metadata.NodeInfo
	if hit, err := c.loadJSON(ctx, key, &cached); err != nil {
		return nil, err
	} else if hit {
		return cloneNodes(cached), nil
	}

	payload, err := c.readThroughWithLock(ctx, nodeInfoLockPrefix+"healthy", key, "", c.policy.NodeHealthTTL, 0, func(ctx context.Context) ([]byte, error) {
		nodes, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return rediscache.Encode(nodes)
	})
	if err != nil {
		return nil, err
	}
	if err := rediscache.Decode(payload, &cached); err != nil {
		return nil, err
	}
	return cloneNodes(cached), nil
}

func (c *serviceReadCache) InvalidateFile(ctx context.Context, fileID metadata.FileID) error {
	if c == nil || fileID == "" {
		return nil
	}
	return c.writer.Del(
		ctx,
		rediscache.FileMetaKey(string(fileID)),
		rediscache.DownloadPlanKey(string(fileID)),
		rediscache.NullFileKey(string(fileID)),
	).Err()
}

func (c *serviceReadCache) InvalidateDirectory(ctx context.Context, inodeID metadata.InodeID) error {
	if c == nil || inodeID == "" {
		return nil
	}
	versionKey := rediscache.DirectoryListVersionKey(string(inodeID))
	return c.writer.Incr(ctx, versionKey).Err()
}

func (c *serviceReadCache) InvalidateNode(ctx context.Context, nodeID metadata.NodeID) error {
	if c == nil || nodeID == "" {
		return nil
	}
	return c.writer.Del(ctx, rediscache.NodeHealthKey(string(nodeID))).Err()
}

func (c *serviceReadCache) InvalidateHealthyNodes(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.writer.Del(ctx, rediscache.HealthyNodesKey()).Err()
}

func (c *serviceReadCache) directoryListVersion(ctx context.Context, inodeID metadata.InodeID) (string, error) {
	version, err := c.reader.Get(ctx, rediscache.DirectoryListVersionKey(string(inodeID))).Result()
	if err == nil {
		return version, nil
	}
	if errors.Is(err, redis.Nil) {
		return "0", nil
	}
	return "", err
}

func (c *serviceReadCache) trackHotspot(ctx context.Context, key, member string) {
	if c == nil || key == "" || member == "" {
		return
	}
	if _, err := c.writer.ZIncrBy(ctx, key, 1, member).Result(); err != nil {
		return
	}
	if c.policy.HotspotWindow > 0 {
		_, _ = c.writer.Expire(ctx, key, c.policy.HotspotWindow*2).Result()
	}
}

func (c *serviceReadCache) loadJSON(ctx context.Context, key string, target any) (bool, error) {
	payload, err := c.reader.Get(ctx, key).Bytes()
	if err == nil {
		return true, rediscache.Decode(payload, target)
	}
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return false, err
}

func (c *serviceReadCache) checkNull(ctx context.Context, key string) (bool, error) {
	_, err := c.reader.Get(ctx, key).Result()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return false, err
}

func (c *serviceReadCache) readThroughWithLock(
	ctx context.Context,
	lockKey string,
	key string,
	nullKey string,
	ttl time.Duration,
	jitter time.Duration,
	loader func(context.Context) ([]byte, error),
) ([]byte, error) {
	token, err := redislock.NewOwnerToken()
	if err != nil {
		return nil, err
	}
	acquired, err := c.locker.Acquire(ctx, lockKey, token, 0)
	if err != nil {
		return nil, err
	}
	if !acquired {
		for range 3 {
			time.Sleep(10 * time.Millisecond)
			payload, err := c.writer.Get(ctx, key).Bytes()
			if err == nil {
				return payload, nil
			}
			if err != nil && !errors.Is(err, redis.Nil) {
				return nil, err
			}
		}
		return loader(ctx)
	}
	defer func() {
		_, _ = c.locker.Release(ctx, lockKey, token)
	}()

	payload, err := loader(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) && nullKey != "" {
			if setErr := c.writer.Set(ctx, nullKey, "1", c.policy.NullEntryTTL).Err(); setErr != nil {
				return nil, setErr
			}
		}
		return nil, err
	}

	effectiveTTL := ttl
	if jitter > 0 {
		effectiveTTL = rediscache.ApplyTTLJitter(ttl, jitter)
	}
	if err := c.writer.Set(ctx, key, payload, effectiveTTL).Err(); err != nil {
		return nil, err
	}
	if nullKey != "" {
		if err := c.writer.Del(ctx, nullKey).Err(); err != nil {
			return nil, err
		}
	}
	return payload, nil
}
