package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/observability/logging"
	"AstraStorage/internal/platform/observability/metrics"
	"github.com/felixge/httpsnoop"
	"log/slog"
)

var errUnknownMethod = errors.New("mds/rpc/http: unknown method")

type httpHandler struct {
	router   *Router
	health   store.HealthChecker
	registry *metrics.Registry
	obs      *mds.Observability
	logger   *slog.Logger
	mux      *http.ServeMux
}

var newRequestLogger = func(service, component string) *slog.Logger {
	return logging.NewLogger(os.Stderr, service, component)
}

var requestLoggerFactory = newRequestLogger

type healthResponse struct {
	Status string `json:"status"`
}

type errorEnvelope struct {
	Error httpError `json:"error"`
}

type httpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewHTTPHandler 构建基于 JSON over HTTP 的网络入口。
// 目前复用现有 RPC method 常量和请求响应结构，避免引入额外协议层。
func NewHTTPHandler(router *Router, health store.HealthChecker, registry *metrics.Registry) (http.Handler, error) {
	if router == nil {
		return nil, errors.New("mds/rpc/http: router is nil")
	}
	if health == nil {
		return nil, errors.New("mds/rpc/http: health checker is nil")
	}
	if registry == nil {
		return nil, errors.New("mds/rpc/http: metrics registry is nil")
	}
	obs, err := mds.NewObservability(registry)
	if err != nil {
		return nil, fmt.Errorf("mds/rpc/http: init observability: %w", err)
	}

	handler := &httpHandler{
		router:   router,
		health:   health,
		registry: registry,
		obs:      obs,
		logger:   requestLoggerFactory("mds", "http"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.handleHealth)
	mux.Handle("/metrics", registry.MetricsHandler())
	mux.HandleFunc("/rpc/", handler.handleRPC)
	handler.mux = mux
	return handler, nil
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.mux == nil || h.registry == nil {
		http.NotFound(w, r)
		return
	}
	route := mdsRouteLabel(r.URL.Path)
	requestID := logging.RequestIDFromHeader(r.Header)
	if requestID == "" {
		requestID = generateRequestID()
	}
	logging.SetRequestIDHeader(w.Header(), requestID)
	r = r.WithContext(logging.WithRequestID(r.Context(), requestID))

	metrics := httpsnoop.CaptureMetricsFn(w, func(ww http.ResponseWriter) {
		h.registry.Middleware("mds", route, h.mux).ServeHTTP(ww, r)
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

func mdsRouteLabel(path string) string {
	switch {
	case path == "/healthz":
		return "/healthz"
	case path == "/metrics":
		return "/metrics"
	case hasSingleSegmentPath(path, "/rpc/"):
		return "/rpc/:method"
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
	if err := h.health.Ping(r.Context()); err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *httpHandler) handleRPC(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/rpc/")
	if method == "" || strings.Contains(method, "/") {
		writeError(w, http.StatusNotFound, "not_found", "rpc method not found")
		return
	}
	startedAt := time.Now()
	result := "success"
	defer func() {
		if h != nil && h.obs != nil {
			h.obs.RecordRPCRequest(method, result, time.Since(startedAt))
		}
	}()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		result = "method_not_allowed"
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "rpc endpoint only supports POST")
		return
	}

	request, err := decodeRequest(r.Context(), method, r.Body)
	if err != nil {
		result = rpcResult(err)
		writeMappedError(w, err)
		return
	}
	response, err := h.router.Dispatch(r.Context(), method, request)
	if err != nil {
		result = rpcResult(err)
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func rpcResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, errUnknownMethod):
		return "unknown_method"
	default:
		return mds.ClassifyResult(err)
	}
}

func decodeRequest(_ context.Context, method string, body io.ReadCloser) (any, error) {
	defer body.Close()

	target, err := newRequestPayload(method)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: request body is required", store.ErrInvalidArgument)
		}
		return nil, fmt.Errorf("%w: decode request body: %v", store.ErrInvalidArgument, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: request body must contain a single JSON object", store.ErrInvalidArgument)
	}

	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, fmt.Errorf("%w: request payload for %s", store.ErrInvalidArgument, method)
	}
	return value.Elem().Interface(), nil
}

func newRequestPayload(method string) (any, error) {
	switch method {
	case MethodCreateDirectory:
		return &CreateDirectoryRequest{}, nil
	case MethodCreateFile:
		return &CreateFileRequest{}, nil
	case MethodRegisterNode:
		return &RegisterNodeRequest{}, nil
	case MethodHeartbeatNode:
		return &HeartbeatNodeRequest{}, nil
	case MethodAllocateUploadTargets:
		return &AllocateUploadTargetsRequest{}, nil
	case MethodStartUpload:
		return &StartUploadRequest{}, nil
	case MethodCommitChunk:
		return &CommitChunkRequest{}, nil
	case MethodCompleteUpload:
		return &CompleteUploadRequest{}, nil
	case MethodVerifyUpload:
		return &VerifyUploadRequest{}, nil
	case MethodFailUploadVerification:
		return &FailUploadVerificationRequest{}, nil
	case MethodRetryUpload:
		return &RetryUploadRequest{}, nil
	case MethodRenameInode:
		return &RenameInodeRequest{}, nil
	case MethodMoveInode:
		return &MoveInodeRequest{}, nil
	case MethodDeleteFile:
		return &DeleteFileRequest{}, nil
	case MethodDeleteDirectory:
		return &DeleteDirectoryRequest{}, nil
	case MethodGetInode:
		return &GetInodeRequest{}, nil
	case MethodGetFile:
		return &GetFileRequest{}, nil
	case MethodGetNode:
		return &GetNodeRequest{}, nil
	case MethodListChildren:
		return &ListChildrenRequest{}, nil
	case MethodListFileChunks:
		return &ListFileChunksRequest{}, nil
	case MethodGetUploadSession:
		return &GetUploadSessionRequest{}, nil
	case MethodBuildDownloadPlan:
		return &BuildDownloadPlanRequest{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownMethod, method)
	}
}

func writeMappedError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal"
	switch {
	case errors.Is(err, errUnknownMethod):
		status = http.StatusNotFound
		code = "unknown_method"
	case errors.Is(err, store.ErrInvalidArgument):
		status = http.StatusBadRequest
		code = "invalid_argument"
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, store.ErrAlreadyExists):
		status = http.StatusConflict
		code = "already_exists"
	case errors.Is(err, store.ErrConflict):
		status = http.StatusConflict
		code = "conflict"
	}
	writeError(w, status, code, err.Error())
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{
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

func generateRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
