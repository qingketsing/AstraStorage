package datanode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	"github.com/felixge/httpsnoop"
	"log/slog"
)

type httpHandler struct {
	store      *Store
	httpClient *http.Client
	registry   *metrics.Registry
	obs        *datanodeObservability
	logger     *slog.Logger
	mux        *http.ServeMux
}

var newRequestLogger = func(service, component string) *slog.Logger {
	return logging.NewLogger(os.Stderr, service, component)
}

type healthResponse struct {
	Status  string `json:"status"`
	DataDir string `json:"data_dir"`
}

type putChunkResponse struct {
	Chunk *ChunkMetadata `json:"chunk"`
}

type errorResponse struct {
	Error httpError `json:"error"`
}

type httpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewHTTPHandler 构建 datanode 的最小 HTTP 接口。
func NewHTTPHandler(store *Store, registry *metrics.Registry) (http.Handler, error) {
	return newHTTPHandler(store, registry, &http.Client{Timeout: 5 * time.Second})
}

func newHTTPHandler(store *Store, registry *metrics.Registry, httpClient *http.Client) (http.Handler, error) {
	if store == nil {
		return nil, errors.New("datanode http: store is nil")
	}
	if registry == nil {
		return nil, errors.New("datanode http: metrics registry is nil")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	obs, err := newDatanodeObservability(registry)
	if err != nil {
		return nil, fmt.Errorf("datanode http: init observability: %w", err)
	}
	handler := &httpHandler{
		store:      store,
		httpClient: httpClient,
		registry:   registry,
		obs:        obs,
		logger:     newRequestLogger("datanode", "http"),
	}
	if count, err := store.CountChunks(); err == nil {
		handler.obs.setStoredChunks(count)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.handleHealth)
	mux.Handle("/metrics", registry.MetricsHandler())
	mux.HandleFunc("/chunks/", handler.handleChunk)
	mux.HandleFunc("/internal/replicate", handler.handleReplicate)
	handler.mux = mux
	return handler, nil
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.mux == nil || h.registry == nil {
		http.NotFound(w, r)
		return
	}
	route := datanodeRouteLabel(r.URL.Path)
	requestID := logging.RequestIDFromHeader(r.Header)
	if requestID == "" {
		requestID = generateRequestID()
	}
	logging.SetRequestIDHeader(w.Header(), requestID)
	r = r.WithContext(logging.WithRequestID(r.Context(), requestID))

	metrics := httpsnoop.CaptureMetricsFn(w, func(ww http.ResponseWriter) {
		h.registry.Middleware("datanode", route, h.mux).ServeHTTP(ww, r)
	})
	h.logger.Info("http request",
		"request_id", requestID,
		"method", r.Method,
		"route", route,
		"status", metrics.Code,
		"duration_ms", metrics.Duration.Milliseconds(),
		"bytes_written", metrics.Written,
	)
}

func datanodeRouteLabel(path string) string {
	switch {
	case path == "/healthz":
		return "/healthz"
	case path == "/metrics":
		return "/metrics"
	case path == "/internal/replicate":
		return "/internal/replicate"
	case hasSingleSegmentPath(path, "/chunks/"):
		return "/chunks/:chunkID"
	default:
		return "/unmatched"
	}
}

func hasSingleSegmentPath(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	tail := strings.TrimPrefix(path, prefix)
	return tail != "" && !strings.Contains(tail, "/")
}

func (h *httpHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "health endpoint only supports GET")
		return
	}
	if err := h.store.Ping(r.Context()); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		DataDir: h.store.dataDir,
	})
}

func (h *httpHandler) handleChunk(w http.ResponseWriter, r *http.Request) {
	chunkID := strings.TrimPrefix(r.URL.Path, "/chunks/")
	if chunkID == "" || strings.Contains(chunkID, "/") {
		writeError(w, http.StatusNotFound, "not_found", "chunk endpoint not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.putChunk(w, r, chunkID)
	case http.MethodGet:
		h.getChunk(w, r, chunkID)
	case http.MethodDelete:
		h.deleteChunk(w, r, chunkID)
	default:
		w.Header().Set("Allow", "PUT, GET, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "chunk endpoint supports PUT, GET and DELETE")
	}
}

func (h *httpHandler) putChunk(w http.ResponseWriter, r *http.Request, chunkID string) {
	defer r.Body.Close()

	data, err := io.ReadAll(r.Body)
	if err != nil {
		h.obs.recordChunkPut("invalid_argument")
		writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("read request body: %v", err))
		return
	}
	var checksum *Checksum
	algorithm := strings.TrimSpace(r.Header.Get("X-Checksum-Algorithm"))
	value := strings.TrimSpace(r.Header.Get("X-Checksum-Value"))
	if algorithm != "" || value != "" {
		checksum = &Checksum{
			Algorithm:  algorithm,
			Value:      value,
			VerifiedAt: time.Now().UTC(),
		}
	}

	meta, err := h.store.PutChunk(r.Context(), chunkID, strings.TrimSpace(r.Header.Get("X-File-ID")), checksum, data, time.Now().UTC())
	if err != nil {
		h.obs.recordChunkPut(datanodeResult(err))
		writeMappedError(w, err)
		return
	}
	h.obs.recordChunkPut("success")
	h.refreshStoredChunkGauge()
	writeJSON(w, http.StatusCreated, putChunkResponse{
		Chunk: meta,
	})
}

func (h *httpHandler) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "replicate endpoint only supports POST")
		return
	}
	defer r.Body.Close()

	var req ReplicateChunkRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.obs.recordReplicateRequest("failure")
		writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("decode replicate request: %v", err))
		return
	}
	if err := validateChunkID(req.ChunkID); err != nil {
		h.obs.recordReplicateRequest("failure")
		writeMappedError(w, err)
		return
	}
	for _, target := range req.Targets {
		if strings.TrimSpace(target.NodeID) == "" || strings.TrimSpace(target.Address) == "" {
			h.obs.recordReplicateRequest("failure")
			writeError(w, http.StatusBadRequest, "invalid_argument", "replicate target requires node id and address")
			return
		}
	}

	chunk, err := h.store.GetChunk(r.Context(), req.ChunkID)
	if err != nil {
		h.obs.recordReplicateRequest("failure")
		writeMappedError(w, err)
		return
	}

	replicas := make([]ReplicaWriteResult, 0, len(req.Targets))
	successCount := 0
	for _, target := range req.Targets {
		state := ReplicaWriteResult{
			NodeID:  target.NodeID,
			State:   "pending",
			Address: target.Address,
		}
		if err := h.forwardReplica(r.Context(), target, chunk.Metadata.ChunkID, chunk.Metadata.FileID, chunk.Metadata.Checksum, chunk.Data); err != nil {
			state.Error = err.Error()
			h.obs.recordReplicateTarget("failure")
		} else {
			state.State = "ready"
			successCount++
			h.obs.recordReplicateTarget("success")
		}
		replicas = append(replicas, state)
	}
	result := "success"
	switch {
	case len(req.Targets) == 0:
		result = "success"
	case successCount == 0:
		result = "failure"
	case successCount < len(req.Targets):
		result = "degraded"
	}
	h.obs.recordReplicateRequest(result)
	h.logger.Info("replicate request",
		"request_id", logging.RequestIDFromContext(r.Context()),
		"chunk_id", req.ChunkID,
		"targets", len(req.Targets),
		"succeeded", successCount,
		"failed", len(req.Targets)-successCount,
		"result", result,
	)
	writeJSON(w, http.StatusOK, ReplicateChunkResponse{
		Chunk:    &chunk.Metadata,
		Replicas: replicas,
	})
}

func (h *httpHandler) getChunk(w http.ResponseWriter, r *http.Request, chunkID string) {
	chunk, err := h.store.GetChunk(r.Context(), chunkID)
	if err != nil {
		h.obs.recordChunkGet(datanodeResult(err))
		writeMappedError(w, err)
		return
	}
	h.obs.recordChunkGet("success")
	if chunk.Metadata.FileID != "" {
		w.Header().Set("X-File-ID", chunk.Metadata.FileID)
	}
	if chunk.Metadata.Checksum != nil {
		w.Header().Set("X-Checksum-Algorithm", chunk.Metadata.Checksum.Algorithm)
		w.Header().Set("X-Checksum-Value", chunk.Metadata.Checksum.Value)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(chunk.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(chunk.Data)
}

func (h *httpHandler) deleteChunk(w http.ResponseWriter, r *http.Request, chunkID string) {
	if err := h.store.DeleteChunk(r.Context(), chunkID); err != nil {
		h.obs.recordChunkDelete(datanodeResult(err))
		writeMappedError(w, err)
		return
	}
	h.obs.recordChunkDelete("success")
	h.refreshStoredChunkGauge()
	w.WriteHeader(http.StatusNoContent)
}

func writeMappedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error: httpError{
			Code:    code,
			Message: message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *httpHandler) forwardReplica(ctx context.Context, target ReplicaTarget, chunkID, fileID string, checksum *Checksum, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(target.Address, "/")+"/chunks/"+chunkID, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("forward replica to %s: build request: %w", target.NodeID, err)
	}
	req.Header.Set("X-File-ID", fileID)
	if checksum != nil && checksum.Algorithm != "" && checksum.Value != "" {
		req.Header.Set("X-Checksum-Algorithm", checksum.Algorithm)
		req.Header.Set("X-Checksum-Value", checksum.Value)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("forward replica to %s: %w", target.NodeID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("forward replica to %s: status %d", target.NodeID, resp.StatusCode)
	}
	return nil
}

func generateRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func (h *httpHandler) refreshStoredChunkGauge() {
	if h == nil || h.obs == nil || h.store == nil {
		return
	}
	count, err := h.store.CountChunks()
	if err != nil {
		return
	}
	h.obs.setStoredChunks(count)
}

func datanodeResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	default:
		return "internal"
	}
}

// HealthChecker 用于 cmd/datanode 的健康探测适配。
type HealthChecker interface {
	Ping(context.Context) error
}
