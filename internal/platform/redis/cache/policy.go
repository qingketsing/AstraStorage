package cache

import (
	"math/rand"
	"time"

	redisclient "AstraStorage/internal/platform/redis/client"
)

type Policy struct {
	FileMetaTTL       time.Duration
	FileMetaTTLJitter time.Duration
	DownloadPlanTTL   time.Duration
	DirectoryListTTL  time.Duration
	NodeHealthTTL     time.Duration
	NullEntryTTL      time.Duration
	HotspotThreshold  int
	HotspotWindow     time.Duration
	StaleServeWindow  time.Duration
}

func NewPolicy(cfg redisclient.ReplicationGroupConfig) Policy {
	cfg = cfg.WithDefaults()
	return Policy{
		FileMetaTTL:       cfg.FileMetaTTL,
		FileMetaTTLJitter: cfg.FileMetaTTLJitter,
		DownloadPlanTTL:   cfg.DownloadPlanTTL,
		DirectoryListTTL:  cfg.DirectoryListTTL,
		NodeHealthTTL:     cfg.NodeHealthTTL,
		NullEntryTTL:      cfg.NullEntryTTL,
		HotspotThreshold:  cfg.HotspotThreshold,
		HotspotWindow:     cfg.HotspotWindow,
		StaleServeWindow:  cfg.StaleServeWindow,
	}
}

func ApplyTTLJitter(ttl, jitter time.Duration) time.Duration {
	if ttl <= 0 || jitter <= 0 {
		return ttl
	}
	window := int64(jitter) * 2
	delta := time.Duration(rand.Int63n(window+1)) - jitter
	return ttl + delta
}
