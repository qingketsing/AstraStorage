package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"AstraStorage/internal/datanode"
	"AstraStorage/internal/mds/metadata"
	mdsrpc "AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	"github.com/felixge/httpsnoop"
	"log/slog"
)

type httpHandler struct {
	client   *UpstreamClient
	registry *metrics.Registry
	obs      *gatewayObservability
	logger   *slog.Logger
	mux      *http.ServeMux
}

var newRequestLogger = func(service, component string) *slog.Logger {
	return logging.NewLogger(os.Stderr, service, component)
}

type healthResponse struct {
	Status   string         `json:"status"`
	Upstream []HealthStatus `json:"upstream"`
}

type errorResponse struct {
	Error httpError `json:"error"`
}

type httpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type uploadRequest struct {
	InodeID       metadata.InodeID         `json:"inode_id"`
	FileID        metadata.FileID          `json:"file_id"`
	ParentID      metadata.InodeID         `json:"parent_id"`
	Name          string                   `json:"name"`
	ContentBase64 string                   `json:"content_base64"`
	ContentType   string                   `json:"content_type"`
	StorageClass  string                   `json:"storage_class"`
	SessionID     metadata.UploadSessionID `json:"session_id"`
	ChunkID       metadata.ChunkID         `json:"chunk_id"`
}

type uploadResponse struct {
	FileID      metadata.FileID          `json:"file_id"`
	SessionID   metadata.UploadSessionID `json:"session_id"`
	ChunkID     metadata.ChunkID         `json:"chunk_id"`
	NodeID      metadata.NodeID          `json:"node_id"`
	NodeAddress string                   `json:"node_address"`
	Size        int64                    `json:"size"`
	Checksum    metadata.Checksum        `json:"checksum"`
	ChunkCount  int                      `json:"chunk_count"`
	Chunks      []uploadedChunk          `json:"chunks"`
}

type uploadedChunk struct {
	ChunkID     metadata.ChunkID  `json:"chunk_id"`
	Index       int64             `json:"index"`
	Offset      int64             `json:"offset"`
	Size        int64             `json:"size"`
	NodeID      metadata.NodeID   `json:"node_id"`
	NodeAddress string            `json:"node_address"`
	Checksum    metadata.Checksum `json:"checksum"`
}

type downloadedChunk struct {
	index int64
	data  []byte
}

// NewHTTPHandler 构建 gateway 的最小 HTTP 入口。
func NewHTTPHandler(client *UpstreamClient, registry *metrics.Registry) (http.Handler, error) {
	if client == nil {
		return nil, errors.New("gateway http: upstream client is nil")
	}
	if registry == nil {
		return nil, errors.New("gateway http: metrics registry is nil")
	}
	handler := &httpHandler{
		client:   client,
		registry: registry,
		logger:   newRequestLogger("gateway", "http"),
	}
	obs, err := newGatewayObservability(registry)
	if err != nil {
		return nil, err
	}
	handler.obs = obs
	if client.obs == nil {
		client.obs = obs
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.handleHealth)
	mux.Handle("/metrics", registry.MetricsHandler())
	mux.HandleFunc("/uploads", handler.handleUploads)
	mux.HandleFunc("/downloads/", handler.handleDownloads)
	mux.HandleFunc("/files/", handler.handleFiles)
	handler.mux = mux
	return handler, nil
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.mux == nil || h.registry == nil {
		http.NotFound(w, r)
		return
	}
	route := gatewayRouteLabel(r.URL.Path)
	requestID := logging.RequestIDFromHeader(r.Header)
	if requestID == "" {
		requestID = generateRequestID()
	}
	logging.SetRequestIDHeader(w.Header(), requestID)
	r = r.WithContext(logging.WithRequestID(r.Context(), requestID))

	metrics := httpsnoop.CaptureMetricsFn(w, func(ww http.ResponseWriter) {
		h.registry.Middleware("gateway", route, h.mux).ServeHTTP(ww, r)
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

func gatewayRouteLabel(path string) string {
	switch {
	case path == "/healthz":
		return "/healthz"
	case path == "/metrics":
		return "/metrics"
	case path == "/uploads":
		return "/uploads"
	case hasSingleSegmentPath(path, "/downloads/"):
		return "/downloads/:fileID"
	case hasSingleSegmentPath(path, "/files/"):
		return "/files/:fileID"
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

	upstreams := make([]HealthStatus, 0, 2)
	healthy := true

	if status, err := h.client.CheckMDSHealth(r.Context()); err != nil {
		healthy = false
		if status != nil {
			upstreams = append(upstreams, *status)
		}
	} else if status != nil {
		upstreams = append(upstreams, *status)
	}
	if status, err := h.client.CheckDataNodeHealth(r.Context()); err != nil {
		healthy = false
		if status != nil {
			upstreams = append(upstreams, *status)
		}
	} else if status != nil {
		upstreams = append(upstreams, *status)
	}

	response := healthResponse{
		Status:   "ok",
		Upstream: upstreams,
	}
	if !healthy {
		response.Status = "degraded"
		writeJSON(w, http.StatusServiceUnavailable, response)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *httpHandler) handleUploads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "uploads endpoint only supports POST")
		return
	}
	defer r.Body.Close()

	requestID := logging.RequestIDFromContext(r.Context())
	result := "failure"
	var uploadedBytes int64
	var uploadedChunkCount int
	var fileID metadata.FileID
	var sessionID metadata.UploadSessionID
	defer func() {
		if result == "success" {
			h.obs.recordUploadBytes(uploadedBytes)
		}
		h.obs.recordUploadChunks(result, uploadedChunkCount)
		h.obs.recordUploadRequest(result)
		h.logger.Info("gateway upload request",
			"request_id", requestID,
			"file_id", fileID,
			"session_id", sessionID,
			"result", result,
			"chunks", uploadedChunkCount,
			"bytes", uploadedBytes,
		)
	}()

	var req uploadRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("decode upload request: %v", err))
		return
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.ContentBase64))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("decode content_base64: %v", err))
		return
	}
	if len(content) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "content_base64 is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "file name is required")
		return
	}

	now := time.Now().UTC()
	if req.ParentID == "" {
		req.ParentID = metadata.InodeID(metadata.RootInodeID)
	}
	if req.InodeID == "" {
		req.InodeID = metadata.InodeID(generateID("inode", now))
	}
	if req.FileID == "" {
		req.FileID = metadata.FileID(generateID("file", now))
	}
	if req.SessionID == "" {
		req.SessionID = metadata.UploadSessionID(generateID("session", now))
	}
	chunkBaseID := req.ChunkID
	if chunkBaseID == "" {
		chunkBaseID = metadata.ChunkID(generateID("chunk", now))
	}

	fileChecksum := checksumForBytes(content, now)
	fileID = req.FileID
	sessionID = req.SessionID
	uploadedBytes = int64(len(content))

	if _, err := h.client.CreateFile(r.Context(), mdsrpc.CreateFileRequest{
		InodeID:      req.InodeID,
		FileID:       req.FileID,
		ParentID:     req.ParentID,
		Name:         req.Name,
		Size:         int64(len(content)),
		ContentType:  req.ContentType,
		StorageClass: req.StorageClass,
		CreatedAt:    now,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	if _, err := h.client.StartUpload(r.Context(), mdsrpc.StartUploadRequest{
		SessionID:        req.SessionID,
		FileID:           req.FileID,
		ExpectedSize:     int64(len(content)),
		ExpectedChecksum: &fileChecksum,
		CreatedAt:        now,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	contentSize := int64(len(content))
	totalChunks := chunkCount(contentSize)
	uploadedChunks := make([]uploadedChunk, 0, totalChunks)
	for chunkIndex, offset := int64(0), int64(0); offset < contentSize; chunkIndex, offset = chunkIndex+1, offset+metadata.FixedChunkSizeBytes {
		chunkEnd := offset + metadata.FixedChunkSizeBytes
		if chunkEnd > contentSize {
			chunkEnd = contentSize
		}
		chunkData := content[int(offset):int(chunkEnd)]
		chunkID := chunkIDForIndex(chunkBaseID, chunkIndex, totalChunks)
		chunkChecksum := checksumForBytes(chunkData, now)

		targets, err := h.client.AllocateUploadTargets(r.Context(), mdsrpc.AllocateUploadTargetsRequest{
			FileID:     req.FileID,
			ChunkIndex: chunkIndex,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, "mds_error", err.Error())
			return
		}
		if len(targets.Targets) == 0 {
			writeError(w, http.StatusBadGateway, "mds_error", "mds returned no upload targets")
			return
		}
		target := targets.Targets[0]
		replicaTargets := targets.Targets[1:]

		if err := h.client.PutChunk(r.Context(), target.Address, string(chunkID), req.FileID, chunkChecksum, chunkData); err != nil {
			writeError(w, http.StatusBadGateway, "datanode_error", err.Error())
			return
		}
		replicaResults := make([]replicaWriteResult, 0, len(replicaTargets))
		if len(replicaTargets) > 0 {
			replicateResp, err := h.client.ReplicateChunk(r.Context(), target.Address, datanode.ReplicateChunkRequest{
				ChunkID: string(chunkID),
				Targets: toDataNodeReplicaTargets(replicaTargets),
			})
			if err != nil {
				writeError(w, http.StatusBadGateway, "datanode_error", err.Error())
				return
			}
			replicaResults = replicateResp
		}
		replicas := buildChunkReplicas(target, replicaTargets, replicaResults, chunkChecksum, int64(len(chunkData)), now)
		if _, err := h.client.CommitChunk(r.Context(), mdsrpc.CommitChunkRequest{
			SessionID:   req.SessionID,
			ChunkID:     chunkID,
			Index:       chunkIndex,
			Offset:      offset,
			Size:        int64(len(chunkData)),
			Checksum:    &chunkChecksum,
			Replicas:    replicas,
			CommittedAt: now,
		}); err != nil {
			writeError(w, http.StatusBadGateway, "mds_error", err.Error())
			return
		}
		uploadedChunkCount++
		h.logger.Info("gateway upload chunk committed",
			"request_id", requestID,
			"file_id", req.FileID,
			"session_id", req.SessionID,
			"chunk_id", chunkID,
			"chunk_index", chunkIndex,
			"size", len(chunkData),
		)

		uploadedChunks = append(uploadedChunks, uploadedChunk{
			ChunkID:     chunkID,
			Index:       chunkIndex,
			Offset:      offset,
			Size:        int64(len(chunkData)),
			NodeID:      target.NodeID,
			NodeAddress: target.Address,
			Checksum:    chunkChecksum,
		})
	}
	if _, err := h.client.CompleteUpload(r.Context(), mdsrpc.CompleteUploadRequest{
		SessionID:        req.SessionID,
		FinalChecksum:    &fileChecksum,
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusUploading},
		CompletedAt:      now,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	if _, err := h.client.VerifyUpload(r.Context(), mdsrpc.VerifyUploadRequest{
		SessionID:        req.SessionID,
		VerifiedChecksum: &fileChecksum,
		ExpectedStatuses: []metadata.FileStatus{metadata.FileStatusVerifying},
		VerifiedAt:       now,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}

	response := uploadResponse{
		FileID:     req.FileID,
		SessionID:  req.SessionID,
		Size:       int64(len(content)),
		Checksum:   fileChecksum,
		ChunkCount: len(uploadedChunks),
		Chunks:     uploadedChunks,
	}
	if len(uploadedChunks) > 0 {
		response.ChunkID = uploadedChunks[0].ChunkID
		response.NodeID = uploadedChunks[0].NodeID
		response.NodeAddress = uploadedChunks[0].NodeAddress
	}
	result = "success"
	writeJSON(w, http.StatusCreated, response)
}

func (h *httpHandler) handleDownloads(w http.ResponseWriter, r *http.Request) {
	fileID := metadata.FileID(strings.TrimPrefix(r.URL.Path, "/downloads/"))
	if strings.TrimSpace(string(fileID)) == "" || strings.Contains(string(fileID), "/") {
		writeError(w, http.StatusNotFound, "not_found", "download endpoint not found")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "downloads endpoint only supports GET")
		return
	}

	requestID := logging.RequestIDFromContext(r.Context())
	result := "failure"
	var returnedBytes int64
	defer func() {
		if result == "success" {
			h.obs.recordDownloadBytes(returnedBytes)
		}
		h.obs.recordDownloadRequest(result)
		h.logger.Info("gateway download request",
			"request_id", requestID,
			"file_id", fileID,
			"result", result,
			"bytes", returnedBytes,
		)
	}()

	planResp, err := h.client.BuildDownloadPlan(r.Context(), mdsrpc.BuildDownloadPlanRequest{FileID: fileID})
	if err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	if planResp.Plan == nil || len(planResp.Plan.Chunks) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "download plan has no chunks")
		return
	}

	chunks := make([]downloadedChunk, 0, len(planResp.Plan.Chunks))
	for _, chunk := range planResp.Plan.Chunks {
		data, err := h.fetchChunkFromCandidates(r.Context(), chunk)
		if err != nil {
			writeError(w, http.StatusBadGateway, "datanode_error", err.Error())
			return
		}
		chunks = append(chunks, downloadedChunk{
			index: chunk.Index,
			data:  data,
		})
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	if planResp.Plan.Size > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", planResp.Plan.Size))
	}
	w.WriteHeader(http.StatusOK)
	for _, chunk := range chunks {
		if _, err := w.Write(chunk.data); err != nil {
			return
		}
		returnedBytes += int64(len(chunk.data))
	}
	result = "success"
}

func (h *httpHandler) handleFiles(w http.ResponseWriter, r *http.Request) {
	fileID := metadata.FileID(strings.TrimPrefix(r.URL.Path, "/files/"))
	if strings.TrimSpace(string(fileID)) == "" || strings.Contains(string(fileID), "/") {
		writeError(w, http.StatusNotFound, "not_found", "file endpoint not found")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "files endpoint only supports DELETE")
		return
	}

	requestID := logging.RequestIDFromContext(r.Context())
	result := "failure"
	defer func() {
		h.obs.recordDeleteRequest(result)
		h.logger.Info("gateway delete request",
			"request_id", requestID,
			"file_id", fileID,
			"result", result,
		)
	}()

	chunksResp, err := h.client.ListFileChunks(r.Context(), mdsrpc.ListFileChunksRequest{FileID: fileID})
	if err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	for _, chunk := range chunksResp.Chunks {
		for nodeID := range chunk.Replicas {
			nodeResp, err := h.client.GetNode(r.Context(), mdsrpc.GetNodeRequest{ID: nodeID})
			if err != nil {
				writeError(w, http.StatusBadGateway, "mds_error", err.Error())
				return
			}
			if nodeResp.Node == nil || strings.TrimSpace(nodeResp.Node.Address) == "" {
				writeError(w, http.StatusBadGateway, "mds_error", fmt.Sprintf("node %q has no address", nodeID))
				return
			}
			if err := h.client.DeleteChunk(r.Context(), nodeResp.Node.Address, string(chunk.ID)); err != nil {
				writeError(w, http.StatusBadGateway, "datanode_error", err.Error())
				return
			}
		}
	}

	if _, err := h.client.DeleteFile(r.Context(), mdsrpc.DeleteFileRequest{
		FileID:    fileID,
		DeletedAt: time.Now().UTC(),
	}); err != nil {
		writeError(w, http.StatusBadGateway, "mds_error", err.Error())
		return
	}
	result = "success"
	w.WriteHeader(http.StatusNoContent)
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

func generateID(prefix string, now time.Time) string {
	return fmt.Sprintf("%s-%d", prefix, now.UnixNano())
}

func checksumForBytes(data []byte, now time.Time) metadata.Checksum {
	sum := sha256.Sum256(data)
	return metadata.Checksum{
		Algorithm:  "sha256",
		Value:      hex.EncodeToString(sum[:]),
		Verified:   true,
		VerifiedAt: &now,
	}
}

func chunkCount(size int64) int {
	if size <= 0 {
		return 0
	}
	count := size / metadata.FixedChunkSizeBytes
	if size%metadata.FixedChunkSizeBytes != 0 {
		count++
	}
	return int(count)
}

func chunkIDForIndex(baseID metadata.ChunkID, index int64, totalChunks int) metadata.ChunkID {
	if totalChunks <= 1 {
		return baseID
	}
	return metadata.ChunkID(fmt.Sprintf("%s-%d", baseID, index))
}

func buildChunkReplicas(primary mdsrpc.UploadTarget, secondaryTargets []mdsrpc.UploadTarget, results []replicaWriteResult, checksum metadata.Checksum, size int64, now time.Time) metadata.ReplicaSet {
	replicas := metadata.ReplicaSet{
		primary.NodeID: {
			NodeID:     primary.NodeID,
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			Checksum:   checksum,
			StoredSize: size,
			CreatedAt:  now,
			UpdatedAt:  now,
			VerifiedAt: &now,
		},
	}
	resultByNode := make(map[metadata.NodeID]replicaWriteResult, len(results))
	for _, result := range results {
		resultByNode[result.NodeID] = result
	}
	for _, target := range secondaryTargets {
		state := metadata.ReplicaStatePending
		storedSize := int64(0)
		var verifiedAt *time.Time
		if result, ok := resultByNode[target.NodeID]; ok && result.State != "" {
			state = result.State
		}
		if state == metadata.ReplicaStateReady {
			storedSize = size
			verifiedAt = &now
		}
		replicas[target.NodeID] = metadata.ReplicaMetadata{
			NodeID:     target.NodeID,
			Role:       metadata.ReplicaRoleSecondary,
			State:      state,
			Checksum:   checksum,
			StoredSize: storedSize,
			CreatedAt:  now,
			UpdatedAt:  now,
			VerifiedAt: verifiedAt,
		}
	}
	return replicas
}

func toDataNodeReplicaTargets(targets []mdsrpc.UploadTarget) []datanode.ReplicaTarget {
	if len(targets) == 0 {
		return nil
	}
	replicaTargets := make([]datanode.ReplicaTarget, 0, len(targets))
	for _, target := range targets {
		replicaTargets = append(replicaTargets, datanode.ReplicaTarget{
			NodeID:  string(target.NodeID),
			Address: target.Address,
		})
	}
	return replicaTargets
}

func (h *httpHandler) fetchChunkFromCandidates(ctx context.Context, chunk mdsrpc.DownloadChunkPlan) ([]byte, error) {
	candidateNodeIDs := orderedCandidateNodeIDs(chunk)
	if len(candidateNodeIDs) == 0 {
		return nil, fmt.Errorf("download chunk %q has no candidate nodes", chunk.ChunkID)
	}

	var lastErr error
	for _, nodeID := range candidateNodeIDs {
		nodeResp, err := h.client.GetNode(ctx, mdsrpc.GetNodeRequest{ID: nodeID})
		if err != nil {
			lastErr = err
			continue
		}
		if nodeResp.Node == nil || strings.TrimSpace(nodeResp.Node.Address) == "" {
			lastErr = fmt.Errorf("node %q has no address", nodeID)
			continue
		}
		data, err := h.client.GetChunk(ctx, nodeResp.Node.Address, string(chunk.ChunkID))
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all candidate nodes failed for chunk %q", chunk.ChunkID)
	}
	return nil, lastErr
}

func orderedCandidateNodeIDs(chunk mdsrpc.DownloadChunkPlan) []metadata.NodeID {
	candidates := make([]metadata.NodeID, 0, len(chunk.CandidateNodeIDs)+1)
	seen := make(map[metadata.NodeID]struct{}, len(chunk.CandidateNodeIDs)+1)
	if chunk.PreferredNodeID != "" {
		candidates = append(candidates, chunk.PreferredNodeID)
		seen[chunk.PreferredNodeID] = struct{}{}
	}
	for _, nodeID := range chunk.CandidateNodeIDs {
		if _, ok := seen[nodeID]; ok {
			continue
		}
		candidates = append(candidates, nodeID)
		seen[nodeID] = struct{}{}
	}
	return candidates
}

func generateRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
