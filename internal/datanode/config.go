package datanode

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 描述 datanode 的最小运行配置。
type Config struct {
	HTTPAddr          string
	DataDir           string
	NodeID            string
	AdvertiseURL      string
	MDSHTTPBaseURL    string
	CapacityBytes     int64
	HeartbeatInterval time.Duration
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
}

// WithDefaults 补齐 datanode 默认配置。
func (c Config) WithDefaults() Config {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		c.HTTPAddr = ":10080"
	}
	if strings.TrimSpace(c.DataDir) == "" {
		c.DataDir = "./data/datanode"
	}
	if strings.TrimSpace(c.AdvertiseURL) == "" {
		c.AdvertiseURL = defaultAdvertiseURL(c.HTTPAddr)
	}
	if strings.TrimSpace(c.NodeID) == "" {
		c.NodeID = defaultNodeID(c.AdvertiseURL, c.HTTPAddr)
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 15 * time.Second
	}
	if c.ReadHeaderTimeout <= 0 {
		c.ReadHeaderTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	return c
}

// Validate 校验配置是否合法。
func (c Config) Validate() error {
	c = c.WithDefaults()
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("datanode config: http addr is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("datanode config: data dir is required")
	}
	if c.CapacityBytes < 0 {
		return fmt.Errorf("datanode config: capacity bytes cannot be negative")
	}
	if strings.TrimSpace(c.MDSHTTPBaseURL) != "" {
		if strings.TrimSpace(c.NodeID) == "" {
			return fmt.Errorf("datanode config: node id is required when mds base url is configured")
		}
		if strings.TrimSpace(c.AdvertiseURL) == "" {
			return fmt.Errorf("datanode config: advertise url is required when mds base url is configured")
		}
		if c.HeartbeatInterval < 0 {
			return fmt.Errorf("datanode config: heartbeat interval cannot be negative")
		}
	}
	if c.ReadHeaderTimeout < 0 {
		return fmt.Errorf("datanode config: read header timeout cannot be negative")
	}
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("datanode config: shutdown timeout cannot be negative")
	}
	return nil
}

// LoadFromEnv 从环境变量读取 datanode 配置。
func LoadFromEnv() (Config, error) {
	capacityBytes, err := loadInt64FromEnv("DATANODE_CAPACITY_BYTES")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPAddr:          strings.TrimSpace(os.Getenv("DATANODE_HTTP_ADDR")),
		DataDir:           strings.TrimSpace(os.Getenv("DATANODE_DATA_DIR")),
		NodeID:            strings.TrimSpace(os.Getenv("DATANODE_NODE_ID")),
		AdvertiseURL:      strings.TrimSpace(os.Getenv("DATANODE_ADVERTISE_URL")),
		MDSHTTPBaseURL:    strings.TrimSpace(os.Getenv("DATANODE_MDS_HTTP_BASE_URL")),
		CapacityBytes:     capacityBytes,
		HeartbeatInterval: loadDurationFromEnv("DATANODE_HEARTBEAT_INTERVAL"),
	}
	return cfg.WithDefaults(), cfg.WithDefaults().Validate()
}

func defaultAdvertiseURL(httpAddr string) string {
	httpAddr = strings.TrimSpace(httpAddr)
	if httpAddr == "" {
		return "http://127.0.0.1:10080"
	}
	if strings.HasPrefix(httpAddr, "http://") || strings.HasPrefix(httpAddr, "https://") {
		return httpAddr
	}
	if strings.HasPrefix(httpAddr, ":") {
		return "http://127.0.0.1" + httpAddr
	}
	if strings.HasPrefix(httpAddr, "0.0.0.0:") {
		return "http://127.0.0.1:" + strings.TrimPrefix(httpAddr, "0.0.0.0:")
	}
	return "http://" + httpAddr
}

func defaultNodeID(advertiseURL, httpAddr string) string {
	base := strings.TrimSpace(advertiseURL)
	if base == "" {
		base = strings.TrimSpace(httpAddr)
	}
	base = strings.NewReplacer("http://", "", "https://", "", "/", "-", ":", "-", ".", "-").Replace(base)
	base = strings.Trim(base, "-")
	if base == "" {
		return "datanode"
	}
	return "datanode-" + base
}

func loadDurationFromEnv(key string) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return duration
}

func loadInt64FromEnv(key string) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("datanode config: parse %s: %w", key, err)
	}
	return parsed, nil
}
