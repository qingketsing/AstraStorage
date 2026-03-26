package gateway

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 描述 gateway 的最小运行配置。
type Config struct {
	HTTPAddr          string
	MDSHTTPBaseURL    string
	DataNodeBaseURL   string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
}

// WithDefaults 为 gateway 配置补齐默认值。
func (c Config) WithDefaults() Config {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		c.HTTPAddr = ":11080"
	}
	if c.ReadHeaderTimeout <= 0 {
		c.ReadHeaderTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	return c
}

// Validate 校验 gateway 配置是否合法。
func (c Config) Validate() error {
	c = c.WithDefaults()
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("gateway config: http addr is required")
	}
	if strings.TrimSpace(c.MDSHTTPBaseURL) == "" {
		return fmt.Errorf("gateway config: mds http base url is required")
	}
	if strings.TrimSpace(c.DataNodeBaseURL) == "" {
		return fmt.Errorf("gateway config: datanode base url is required")
	}
	if c.ReadHeaderTimeout < 0 {
		return fmt.Errorf("gateway config: read header timeout cannot be negative")
	}
	if c.ShutdownTimeout < 0 {
		return fmt.Errorf("gateway config: shutdown timeout cannot be negative")
	}
	return nil
}

// LoadFromEnv 从环境变量加载 gateway 配置。
func LoadFromEnv() (Config, error) {
	cfg := Config{
		HTTPAddr:        strings.TrimSpace(os.Getenv("GATEWAY_HTTP_ADDR")),
		MDSHTTPBaseURL:  strings.TrimSpace(os.Getenv("GATEWAY_MDS_HTTP_BASE_URL")),
		DataNodeBaseURL: strings.TrimSpace(os.Getenv("GATEWAY_DATANODE_BASE_URL")),
	}
	cfg = cfg.WithDefaults()
	return cfg, cfg.Validate()
}
