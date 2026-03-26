package datanode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
)

// MDSClient 负责 datanode 到 MDS 的注册和心跳回写。
type MDSClient struct {
	httpClient *http.Client
	baseURL    string
	obs        *datanodeObservability
	logger     *slog.Logger
}

// NodeRegistration 描述 datanode 对 MDS 的注册信息。
type NodeRegistration struct {
	NodeID     string
	Address    string
	Rack       string
	Zone       string
	Region     string
	Labels     map[string]string
	Capacity   int64
	Used       int64
	Healthy    bool
	LastSeenAt *time.Time
	UpdatedAt  time.Time
}

// NodeHeartbeat 描述 datanode 定期上报到 MDS 的节点状态。
type NodeHeartbeat struct {
	NodeID     string
	Healthy    bool
	Capacity   int64
	Used       int64
	LastSeenAt time.Time
}

var newMDSClientLogger = func(service, component string) *slog.Logger {
	return logging.NewLogger(os.Stderr, service, component)
}

// NewMDSClient 创建 datanode 使用的 MDS HTTP client。
func NewMDSClient(baseURL string) (*MDSClient, error) {
	return newMDSClient(baseURL, &http.Client{Timeout: 5 * time.Second})
}

func newMDSClient(baseURL string, httpClient *http.Client) (*MDSClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("datanode mds client: base url is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &MDSClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		logger:     newMDSClientLogger("datanode", "mds_client"),
	}, nil
}

func (c *MDSClient) AttachObservability(registry *metrics.Registry) error {
	if c == nil {
		return fmt.Errorf("datanode mds client: client is nil")
	}
	if registry == nil {
		c.obs = nil
		return nil
	}
	obs, err := newDatanodeObservability(registry)
	if err != nil {
		return err
	}
	c.obs = obs
	return nil
}

// RegisterNode 在 MDS 中创建或刷新节点记录。
func (c *MDSClient) RegisterNode(ctx context.Context, node NodeRegistration) error {
	_, err := callMDSRPC[mdsrpc.RegisterNodeResponse](ctx, c, mdsrpc.MethodRegisterNode, mdsrpc.RegisterNodeRequest{
		ID:         metadata.NodeID(node.NodeID),
		Address:    node.Address,
		Rack:       node.Rack,
		Zone:       node.Zone,
		Region:     node.Region,
		Labels:     cloneStringMap(node.Labels),
		Capacity:   node.Capacity,
		Used:       node.Used,
		Healthy:    node.Healthy,
		LastSeenAt: cloneTimePtr(node.LastSeenAt),
		UpdatedAt:  node.UpdatedAt,
	})
	if c != nil && c.obs != nil {
		c.obs.recordRegistration(lifecycleResult(err), node.UpdatedAt)
	}
	if c != nil && c.logger != nil {
		args := []any{
			"request_id", logging.RequestIDFromContext(ctx),
			"node_id", node.NodeID,
			"address", node.Address,
			"result", lifecycleResult(err),
		}
		if err != nil {
			args = append(args, "error", err.Error())
		}
		c.logger.Info("register node", args...)
	}
	return err
}

// HeartbeatNode 更新 MDS 中节点的最新健康状态。
func (c *MDSClient) HeartbeatNode(ctx context.Context, heartbeat NodeHeartbeat) error {
	_, err := callMDSRPC[mdsrpc.HeartbeatNodeResponse](ctx, c, mdsrpc.MethodHeartbeatNode, mdsrpc.HeartbeatNodeRequest{
		NodeID:     metadata.NodeID(heartbeat.NodeID),
		Healthy:    heartbeat.Healthy,
		Capacity:   heartbeat.Capacity,
		Used:       heartbeat.Used,
		LastSeenAt: heartbeat.LastSeenAt,
	})
	if c != nil && c.obs != nil {
		c.obs.recordHeartbeat(lifecycleResult(err), heartbeat.LastSeenAt)
	}
	if err != nil && c != nil && c.logger != nil {
		c.logger.Info("heartbeat node",
			"request_id", logging.RequestIDFromContext(ctx),
			"node_id", heartbeat.NodeID,
			"result", "failure",
			"error", err.Error(),
		)
	}
	return err
}

func callMDSRPC[Resp any](ctx context.Context, client *MDSClient, method string, request any) (*Resp, error) {
	result, done := client.observeUpstream("mds", method)
	defer done(&result)

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("datanode mds client: marshal %s request: %w", method, err)
	}
	req, err := client.newRequest(ctx, http.MethodPost, client.baseURL+"/rpc/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("datanode mds client: build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("datanode mds client: call %s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("datanode mds client: %s returned status %d", method, resp.StatusCode)
	}
	var payload Resp
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("datanode mds client: decode %s response: %w", method, err)
	}
	result = "success"
	return &payload, nil
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	t := *src
	return &t
}

func (c *MDSClient) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	logging.SetRequestIDHeader(req.Header, logging.RequestIDFromContext(ctx))
	return req, nil
}

func (c *MDSClient) observeUpstream(target, operation string) (string, func(*string)) {
	start := time.Now()
	result := "failure"
	return result, func(current *string) {
		if c == nil || c.obs == nil {
			return
		}
		recordedResult := result
		if current != nil && *current != "" {
			recordedResult = *current
		}
		c.obs.recordUpstreamRequest(target, operation, recordedResult, time.Since(start))
	}
}

func lifecycleResult(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}
