package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"AstraStorage/internal/datanode"
	"AstraStorage/internal/mds/metadata"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/platform/observability/logging"
)

// UpstreamClient 负责 gateway 对 MDS 和 datanode 的最小健康探测。
type UpstreamClient struct {
	httpClient      *http.Client
	mdsBaseURL      string
	dataNodeBaseURL string
	obs             *gatewayObservability
}

// HealthStatus 描述一次健康探测结果。
type HealthStatus struct {
	Name       string        `json:"name"`
	BaseURL    string        `json:"base_url"`
	StatusCode int           `json:"status_code"`
	Healthy    bool          `json:"healthy"`
	Duration   time.Duration `json:"duration"`
}

type replicaWriteResult struct {
	NodeID metadata.NodeID
	State  metadata.ReplicaState
	Error  string
}

// NewUpstreamClient 创建上游 HTTP client。
func NewUpstreamClient(cfg Config) (*UpstreamClient, error) {
	return newUpstreamClient(cfg, &http.Client{
		Timeout: 5 * time.Second,
	})
}

func newUpstreamClient(cfg Config, httpClient *http.Client) (*UpstreamClient, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &UpstreamClient{
		httpClient:      httpClient,
		mdsBaseURL:      strings.TrimRight(cfg.MDSHTTPBaseURL, "/"),
		dataNodeBaseURL: strings.TrimRight(cfg.DataNodeBaseURL, "/"),
	}, nil
}

// CheckMDSHealth 检查 MDS 健康。
func (c *UpstreamClient) CheckMDSHealth(ctx context.Context) (*HealthStatus, error) {
	return c.checkHealth(ctx, "mds", c.mdsBaseURL)
}

// CheckDataNodeHealth 检查 datanode 健康。
func (c *UpstreamClient) CheckDataNodeHealth(ctx context.Context) (*HealthStatus, error) {
	return c.checkHealth(ctx, "datanode", c.dataNodeBaseURL)
}

func (c *UpstreamClient) CreateDirectory(ctx context.Context, req mdsrpc.CreateDirectoryRequest) (*mdsrpc.CreateDirectoryResponse, error) {
	return callMDSRPC[mdsrpc.CreateDirectoryResponse](ctx, c, mdsrpc.MethodCreateDirectory, req)
}

func (c *UpstreamClient) CreateFile(ctx context.Context, req mdsrpc.CreateFileRequest) (*mdsrpc.CreateFileResponse, error) {
	return callMDSRPC[mdsrpc.CreateFileResponse](ctx, c, mdsrpc.MethodCreateFile, req)
}

func (c *UpstreamClient) StartUpload(ctx context.Context, req mdsrpc.StartUploadRequest) (*mdsrpc.StartUploadResponse, error) {
	return callMDSRPC[mdsrpc.StartUploadResponse](ctx, c, mdsrpc.MethodStartUpload, req)
}

func (c *UpstreamClient) AllocateUploadTargets(ctx context.Context, req mdsrpc.AllocateUploadTargetsRequest) (*mdsrpc.AllocateUploadTargetsResponse, error) {
	return callMDSRPC[mdsrpc.AllocateUploadTargetsResponse](ctx, c, mdsrpc.MethodAllocateUploadTargets, req)
}

func (c *UpstreamClient) CommitChunk(ctx context.Context, req mdsrpc.CommitChunkRequest) (*mdsrpc.CommitChunkResponse, error) {
	return callMDSRPC[mdsrpc.CommitChunkResponse](ctx, c, mdsrpc.MethodCommitChunk, req)
}

func (c *UpstreamClient) CompleteUpload(ctx context.Context, req mdsrpc.CompleteUploadRequest) (*mdsrpc.CompleteUploadResponse, error) {
	return callMDSRPC[mdsrpc.CompleteUploadResponse](ctx, c, mdsrpc.MethodCompleteUpload, req)
}

func (c *UpstreamClient) VerifyUpload(ctx context.Context, req mdsrpc.VerifyUploadRequest) (*mdsrpc.VerifyUploadResponse, error) {
	return callMDSRPC[mdsrpc.VerifyUploadResponse](ctx, c, mdsrpc.MethodVerifyUpload, req)
}

func (c *UpstreamClient) BuildDownloadPlan(ctx context.Context, req mdsrpc.BuildDownloadPlanRequest) (*mdsrpc.BuildDownloadPlanResponse, error) {
	return callMDSRPC[mdsrpc.BuildDownloadPlanResponse](ctx, c, mdsrpc.MethodBuildDownloadPlan, req)
}

func (c *UpstreamClient) GetFile(ctx context.Context, req mdsrpc.GetFileRequest) (*mdsrpc.GetFileResponse, error) {
	return callMDSRPC[mdsrpc.GetFileResponse](ctx, c, mdsrpc.MethodGetFile, req)
}

func (c *UpstreamClient) ListChildren(ctx context.Context, req mdsrpc.ListChildrenRequest) (*mdsrpc.ListChildrenResponse, error) {
	return callMDSRPC[mdsrpc.ListChildrenResponse](ctx, c, mdsrpc.MethodListChildren, req)
}

func (c *UpstreamClient) ListFileChunks(ctx context.Context, req mdsrpc.ListFileChunksRequest) (*mdsrpc.ListFileChunksResponse, error) {
	return callMDSRPC[mdsrpc.ListFileChunksResponse](ctx, c, mdsrpc.MethodListFileChunks, req)
}

func (c *UpstreamClient) GetNode(ctx context.Context, req mdsrpc.GetNodeRequest) (*mdsrpc.GetNodeResponse, error) {
	return callMDSRPC[mdsrpc.GetNodeResponse](ctx, c, mdsrpc.MethodGetNode, req)
}

func (c *UpstreamClient) DeleteFile(ctx context.Context, req mdsrpc.DeleteFileRequest) (*mdsrpc.DeleteFileResponse, error) {
	return callMDSRPC[mdsrpc.DeleteFileResponse](ctx, c, mdsrpc.MethodDeleteFile, req)
}

func (c *UpstreamClient) PutChunk(ctx context.Context, baseURL, chunkID string, fileID metadata.FileID, checksum metadata.Checksum, data []byte) error {
	result, done := c.observeUpstream("datanode", "datanode.put_chunk")
	defer done(&result)

	req, err := c.newRequest(ctx, http.MethodPut, strings.TrimRight(baseURL, "/")+"/chunks/"+chunkID, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gateway client: build put chunk request: %w", err)
	}
	req.Header.Set("X-File-ID", string(fileID))
	if checksum.Algorithm != "" && checksum.Value != "" {
		req.Header.Set("X-Checksum-Algorithm", checksum.Algorithm)
		req.Header.Set("X-Checksum-Value", checksum.Value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gateway client: put chunk to %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway client: put chunk returned status %d", resp.StatusCode)
	}
	var payload struct {
		Chunk any `json:"chunk"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("gateway client: decode put chunk response: %w", err)
	}
	result = "success"
	return nil
}

func (c *UpstreamClient) DeleteChunk(ctx context.Context, baseURL, chunkID string) error {
	result, done := c.observeUpstream("datanode", "datanode.delete_chunk")
	defer done(&result)

	req, err := c.newRequest(ctx, http.MethodDelete, strings.TrimRight(baseURL, "/")+"/chunks/"+chunkID, nil)
	if err != nil {
		return fmt.Errorf("gateway client: build delete chunk request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gateway client: delete chunk from %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		result = "success"
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway client: delete chunk returned status %d", resp.StatusCode)
	}
	result = "success"
	return nil
}

func (c *UpstreamClient) ReplicateChunk(ctx context.Context, baseURL string, req datanode.ReplicateChunkRequest) ([]replicaWriteResult, error) {
	result, done := c.observeUpstream("datanode", "datanode.replicate_chunk")
	defer done(&result)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("gateway client: marshal replicate chunk request: %w", err)
	}
	httpReq, err := c.newRequest(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/internal/replicate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway client: build replicate chunk request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway client: call replicate chunk on %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway client: replicate chunk returned status %d", resp.StatusCode)
	}
	var payload datanode.ReplicateChunkResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("gateway client: decode replicate chunk response: %w", err)
	}
	results := make([]replicaWriteResult, 0, len(payload.Replicas))
	for _, replica := range payload.Replicas {
		results = append(results, replicaWriteResult{
			NodeID: metadata.NodeID(replica.NodeID),
			State:  metadata.ReplicaState(replica.State),
			Error:  replica.Error,
		})
	}
	result = "success"
	return results, nil
}

func (c *UpstreamClient) GetChunk(ctx context.Context, baseURL, chunkID string) ([]byte, error) {
	result, done := c.observeUpstream("datanode", "datanode.get_chunk")
	defer done(&result)

	req, err := c.newRequest(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/chunks/"+chunkID, nil)
	if err != nil {
		return nil, fmt.Errorf("gateway client: build get chunk request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway client: get chunk from %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway client: get chunk returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gateway client: read chunk body: %w", err)
	}
	result = "success"
	return data, nil
}

func (c *UpstreamClient) checkHealth(ctx context.Context, name, baseURL string) (*HealthStatus, error) {
	target := name
	operation := "health." + name
	result, done := c.observeUpstream(target, operation)
	defer done(&result)

	start := time.Now()
	req, err := c.newRequest(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return nil, fmt.Errorf("gateway client: build %s health request: %w", name, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &HealthStatus{
			Name:     name,
			BaseURL:  baseURL,
			Healthy:  false,
			Duration: time.Since(start),
		}, fmt.Errorf("gateway client: call %s health: %w", name, err)
	}
	defer resp.Body.Close()

	status := &HealthStatus{
		Name:       name,
		BaseURL:    baseURL,
		StatusCode: resp.StatusCode,
		Healthy:    resp.StatusCode >= 200 && resp.StatusCode < 300,
		Duration:   time.Since(start),
	}
	if !status.Healthy {
		return status, fmt.Errorf("gateway client: %s health returned status %d", name, resp.StatusCode)
	}
	result = "success"
	return status, nil
}

func callMDSRPC[Resp any](ctx context.Context, client *UpstreamClient, method string, request any) (*Resp, error) {
	result, done := client.observeUpstream("mds", method)
	defer done(&result)

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("gateway client: marshal %s request: %w", method, err)
	}
	req, err := client.newRequest(ctx, http.MethodPost, client.mdsBaseURL+"/rpc/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway client: build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway client: call %s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway client: %s returned status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var payload Resp
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("gateway client: decode %s response: %w", method, err)
	}
	result = "success"
	return &payload, nil
}

func (c *UpstreamClient) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	logging.SetRequestIDHeader(req.Header, logging.RequestIDFromContext(ctx))
	return req, nil
}

func (c *UpstreamClient) observeUpstream(target, operation string) (string, func(*string)) {
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
