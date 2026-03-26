package coordinator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"AstraStorage/internal/datanode"
	rootmds "AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/observability/logging"
)

// PendingReplicaRepairer 定期扫描并补齐 pending 副本。
type PendingReplicaRepairer struct {
	repo              store.Repository
	httpClient        *http.Client
	interval          time.Duration
	retryBackoff      time.Duration
	maxReplicasPerRun int
	observability     RepairerObservability
	logger            *slog.Logger
	taskProducer      mdsmq.TaskProducer
	mu                sync.Mutex
	nextAttemptByKey  map[string]time.Time
}

type RepairerObservability interface {
	RecordRepairRun(result string, duration time.Duration)
	RecordRepairReplicasAttempted(count int)
	RecordRepairReplicasSucceeded(count int)
	RecordRepairReplicasFailed(count int)
	RecordRepairTargetsDeferred(count int)
}

type repairOutcome struct {
	attempted int
	succeeded int
	failed    int
	deferred  int
}

var repairerLoggerFactory = func() io.Writer { return os.Stderr }

// PendingReplicaRepairerConfig 描述副本修复循环的最小运行配置。
type PendingReplicaRepairerConfig struct {
	Interval          time.Duration
	HTTPTimeout       time.Duration
	RetryBackoff      time.Duration
	MaxReplicasPerRun int
}

// NewPendingReplicaRepairer 创建一个最小但可长期运行的副本修复循环。
func NewPendingReplicaRepairer(repo store.Repository, cfg PendingReplicaRepairerConfig) (*PendingReplicaRepairer, error) {
	return newPendingReplicaRepairer(repo, cfg, &http.Client{Timeout: cfg.HTTPTimeout})
}

func newPendingReplicaRepairer(repo store.Repository, cfg PendingReplicaRepairerConfig, httpClient *http.Client) (*PendingReplicaRepairer, error) {
	if repo == nil {
		return nil, fmt.Errorf("mds repairer: repository is nil")
	}
	if cfg.Interval < 0 {
		return nil, fmt.Errorf("mds repairer: interval cannot be negative")
	}
	if cfg.RetryBackoff < 0 {
		return nil, fmt.Errorf("mds repairer: retry backoff cannot be negative")
	}
	if cfg.MaxReplicasPerRun < 0 {
		return nil, fmt.Errorf("mds repairer: max replicas per run cannot be negative")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 30 * time.Second
	}
	if cfg.MaxReplicasPerRun <= 0 {
		cfg.MaxReplicasPerRun = 32
	}
	return &PendingReplicaRepairer{
		repo:              repo,
		httpClient:        httpClient,
		interval:          cfg.Interval,
		retryBackoff:      cfg.RetryBackoff,
		maxReplicasPerRun: cfg.MaxReplicasPerRun,
		logger:            logging.NewLogger(repairerLoggerFactory(), "mds", "repairer"),
		nextAttemptByKey:  make(map[string]time.Time),
	}, nil
}

func (r *PendingReplicaRepairer) SetObservability(obs RepairerObservability) {
	if r == nil {
		return
	}
	r.observability = obs
}

func (r *PendingReplicaRepairer) SetTaskProducer(producer mdsmq.TaskProducer) {
	if r == nil {
		return
	}
	r.taskProducer = producer
}

func (r *PendingReplicaRepairer) ExecuteReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error {
	if r == nil || r.repo == nil {
		return fmt.Errorf("mds repairer: repository is nil")
	}
	chunk, err := r.repo.GetChunk(ctx, store.ChunkSelector{ID: metadata.ChunkID(task.ChunkID)})
	if err != nil {
		return err
	}
	_, err = r.executeReplicaCopy(ctx, time.Now().UTC(), *chunk, metadata.NodeID(task.SourceNodeID), []metadata.NodeID{metadata.NodeID(task.TargetNodeID)})
	return err
}

func (r *PendingReplicaRepairer) ExecuteReplicaPlanCopy(ctx context.Context, planID string) error {
	if r == nil || r.repo == nil {
		return fmt.Errorf("mds repairer: repository is nil")
	}
	plan, err := r.repo.GetReplicaPlan(ctx, planID)
	if err != nil {
		return err
	}
	chunk, err := r.repo.GetChunk(ctx, store.ChunkSelector{ID: plan.ChunkID})
	if err != nil {
		return err
	}
	nodes, err := r.repo.ListNodes(ctx, store.NodeFilter{})
	if err != nil {
		return err
	}
	sourceNodeID, err := selectReplicaCopySource(*plan, *chunk, nodes)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	outcome, err := r.executeReplicaCopy(ctx, now, *chunk, sourceNodeID, []metadata.NodeID{plan.TargetNodeID})
	if err != nil {
		return err
	}
	if outcome.succeeded == 0 {
		return fmt.Errorf("mds repairer: replica plan %q copied no targets", planID)
	}
	state := metadata.ReplicaPlanStateCleanupReady
	return r.repo.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
		ID:        plan.ID,
		State:     &state,
		UpdatedAt: now,
	})
}

// Run 启动后台修复循环。
func (r *PendingReplicaRepairer) Run(ctx context.Context) {
	if r == nil || r.interval <= 0 {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.RepairOnce(ctx)
		}
	}
}

// RepairOnce 扫描当前所有 chunk，把 pending 副本重新复制到目标节点。
func (r *PendingReplicaRepairer) RepairOnce(ctx context.Context) error {
	startedAt := time.Now()
	runID := generateRepairRunID()
	files, err := r.repo.ListFiles(ctx, store.FileFilter{})
	if err != nil {
		r.recordRun(runID, startedAt, repairOutcome{}, err)
		return err
	}

	remaining := r.maxReplicasPerRun
	now := time.Now().UTC()
	var firstErr error
	var total repairOutcome
	for _, file := range files {
		if remaining == 0 {
			break
		}
		chunks, err := r.repo.ListChunksByFile(ctx, file.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, chunk := range chunks {
			if remaining == 0 {
				break
			}
			outcome, err := r.repairChunk(ctx, now, chunk, remaining)
			remaining -= outcome.attempted
			total.attempted += outcome.attempted
			total.succeeded += outcome.succeeded
			total.failed += outcome.failed
			total.deferred += outcome.deferred
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	r.recordRun(runID, startedAt, total, firstErr)
	return firstErr
}

func (r *PendingReplicaRepairer) repairChunk(ctx context.Context, now time.Time, chunk metadata.ChunkMetadata, remaining int) (repairOutcome, error) {
	sourceNodeID, pendingNodeIDs := selectRepairTargets(chunk.Replicas)
	if sourceNodeID == "" || len(pendingNodeIDs) == 0 || remaining <= 0 {
		return repairOutcome{}, nil
	}

	targetNodeIDs := r.selectDueTargets(chunk.ID, pendingNodeIDs, remaining, now)
	if len(targetNodeIDs) == 0 {
		return repairOutcome{}, nil
	}
	outcome := repairOutcome{}
	if r.taskProducer != nil {
		for _, nodeID := range targetNodeIDs {
			task := mdsmq.NewReplicaRepairTask(chunk.ID, chunk.FileID, sourceNodeID, nodeID)
			if err := r.taskProducer.PublishReplicaRepair(ctx, task); err != nil {
				r.deferTargets(chunk.ID, []metadata.NodeID{nodeID}, now)
				outcome.failed++
				outcome.deferred++
				return outcome, err
			}
			r.deferTargets(chunk.ID, []metadata.NodeID{nodeID}, now)
			outcome.attempted++
		}
		return outcome, nil
	}

	return r.executeReplicaCopy(ctx, now, chunk, sourceNodeID, targetNodeIDs)
}

func (r *PendingReplicaRepairer) executeReplicaCopy(ctx context.Context, now time.Time, chunk metadata.ChunkMetadata, sourceNodeID metadata.NodeID, targetNodeIDs []metadata.NodeID) (repairOutcome, error) {
	outcome := repairOutcome{}
	sourceNode, err := r.repo.GetNode(ctx, sourceNodeID)
	if err != nil {
		r.deferTargets(chunk.ID, targetNodeIDs, now)
		outcome.failed = len(targetNodeIDs)
		outcome.deferred = len(targetNodeIDs)
		return outcome, err
	}
	if sourceNode == nil || strings.TrimSpace(sourceNode.Address) == "" {
		r.deferTargets(chunk.ID, targetNodeIDs, now)
		outcome.failed = len(targetNodeIDs)
		outcome.deferred = len(targetNodeIDs)
		return outcome, fmt.Errorf("mds repairer: source node %q has no address", sourceNodeID)
	}

	targets := make([]datanode.ReplicaTarget, 0, len(targetNodeIDs))
	skippedNodeIDs := make([]metadata.NodeID, 0)
	candidates := make([]metadata.NodeInfo, 0, len(targetNodeIDs))
	knownNodes := make(map[metadata.NodeID]metadata.NodeInfo, len(targetNodeIDs))
	for _, nodeID := range targetNodeIDs {
		node, err := r.repo.GetNode(ctx, nodeID)
		if err != nil {
			r.deferTargets(chunk.ID, targetNodeIDs, now)
			outcome.failed = len(targetNodeIDs)
			outcome.deferred = len(targetNodeIDs)
			return outcome, err
		}
		if node == nil || strings.TrimSpace(node.Address) == "" {
			skippedNodeIDs = append(skippedNodeIDs, nodeID)
			continue
		}
		knownNodes[nodeID] = *node
		candidates = append(candidates, *node)
	}

	selectedNodes := rootmds.SelectCapacityAwareNodes(rootmds.NodeSelectionInput{
		Candidates: candidates,
		Count:      len(targetNodeIDs),
	})
	selectedNodeIDs := make(map[metadata.NodeID]struct{}, len(selectedNodes))
	for _, node := range selectedNodes {
		selectedNodeIDs[node.ID] = struct{}{}
		targets = append(targets, datanode.ReplicaTarget{
			NodeID:  string(node.ID),
			Address: node.Address,
		})
	}
	for _, nodeID := range targetNodeIDs {
		if _, skipped := selectedNodeIDs[nodeID]; skipped {
			continue
		}
		if _, known := knownNodes[nodeID]; known {
			skippedNodeIDs = append(skippedNodeIDs, nodeID)
		}
	}
	outcome.attempted = len(targets)
	if len(targets) == 0 {
		r.deferTargets(chunk.ID, targetNodeIDs, now)
		outcome.failed = len(targetNodeIDs)
		outcome.deferred = len(targetNodeIDs)
		return outcome, nil
	}

	results, err := r.replicateChunk(ctx, sourceNode.Address, datanode.ReplicateChunkRequest{
		ChunkID: string(chunk.ID),
		Targets: targets,
	})
	if err != nil {
		r.deferTargets(chunk.ID, targetNodeIDs, now)
		outcome.failed = len(targetNodeIDs)
		outcome.deferred = len(targetNodeIDs)
		return outcome, err
	}

	upserts := make(metadata.ReplicaSet, len(results))
	resultByNode := make(map[metadata.NodeID]datanode.ReplicaWriteResult, len(results))
	for _, result := range results {
		resultByNode[metadata.NodeID(result.NodeID)] = result
	}
	for _, target := range targets {
		nodeID := metadata.NodeID(target.NodeID)
		replica := chunk.Replicas[nodeID]
		replica.NodeID = nodeID
		replica.FileID = chunk.FileID
		replica.ChunkID = chunk.ID
		replica.Role = metadata.ReplicaRoleSecondary
		replica.Checksum = chunk.Checksum
		replica.UpdatedAt = now
		if replica.CreatedAt.IsZero() {
			replica.CreatedAt = now
		}
		result, ok := resultByNode[nodeID]
		if ok && metadata.ReplicaState(result.State) == metadata.ReplicaStateReady {
			replica.State = metadata.ReplicaStateReady
			replica.StoredSize = chunk.Size
			replica.VerifiedAt = &now
			r.clearRetry(chunk.ID, nodeID)
			outcome.succeeded++
		} else {
			replica.State = metadata.ReplicaStatePending
			r.deferTargets(chunk.ID, []metadata.NodeID{nodeID}, now)
			outcome.failed++
			outcome.deferred++
		}
		upserts[nodeID] = replica
	}
	for _, nodeID := range skippedNodeIDs {
		replica := chunk.Replicas[nodeID]
		replica.NodeID = nodeID
		replica.FileID = chunk.FileID
		replica.ChunkID = chunk.ID
		replica.Role = metadata.ReplicaRoleSecondary
		replica.Checksum = chunk.Checksum
		replica.State = metadata.ReplicaStatePending
		replica.UpdatedAt = now
		if replica.CreatedAt.IsZero() {
			replica.CreatedAt = now
		}
		upserts[nodeID] = replica
		r.deferTargets(chunk.ID, []metadata.NodeID{nodeID}, now)
		outcome.failed++
		outcome.deferred++
	}

	replicaCount := len(chunk.Replicas)
	err = r.repo.UpdateChunkReplicas(ctx, store.ChunkReplicaPatch{
		Selector:     store.ChunkSelector{ID: chunk.ID},
		Upserts:      upserts,
		ReplicaCount: &replicaCount,
		UpdatedAt:    now,
	})
	if err != nil {
		return outcome, err
	}
	return outcome, nil
}

func (r *PendingReplicaRepairer) replicateChunk(ctx context.Context, sourceAddress string, req datanode.ReplicateChunkRequest) ([]datanode.ReplicaWriteResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mds repairer: marshal replicate request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(sourceAddress, "/")+"/internal/replicate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mds repairer: build replicate request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mds repairer: call replicate endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mds repairer: replicate endpoint returned status %d", resp.StatusCode)
	}
	var payload datanode.ReplicateChunkResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("mds repairer: decode replicate response: %w", err)
	}
	return payload.Replicas, nil
}

func selectRepairTargets(replicas metadata.ReplicaSet) (metadata.NodeID, []metadata.NodeID) {
	ready := make([]metadata.NodeID, 0)
	pending := make([]metadata.NodeID, 0)
	var primary metadata.NodeID
	for nodeID, replica := range replicas {
		if replica.State == metadata.ReplicaStateReady {
			ready = append(ready, nodeID)
			if replica.Role == metadata.ReplicaRolePrimary {
				primary = nodeID
			}
		}
		if replica.State == metadata.ReplicaStatePending {
			pending = append(pending, nodeID)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return string(ready[i]) < string(ready[j]) })
	sort.Slice(pending, func(i, j int) bool { return string(pending[i]) < string(pending[j]) })
	if primary != "" {
		return primary, pending
	}
	if len(ready) > 0 {
		return ready[0], pending
	}
	return "", pending
}

func selectReplicaCopySource(plan metadata.ReplicaPlan, chunk metadata.ChunkMetadata, nodes []metadata.NodeInfo) (metadata.NodeID, error) {
	nodeIndex := make(map[metadata.NodeID]metadata.NodeInfo, len(nodes))
	for _, node := range nodes {
		nodeIndex[node.ID] = node
	}
	if replica, ok := chunk.Replicas[plan.SourceNodeID]; ok && replica.State == metadata.ReplicaStateReady {
		if node, ok := nodeIndex[plan.SourceNodeID]; ok && node.Healthy && strings.TrimSpace(node.Address) != "" {
			return plan.SourceNodeID, nil
		}
	}
	ready := make([]metadata.NodeID, 0, len(chunk.Replicas))
	for nodeID, replica := range chunk.Replicas {
		if nodeID == plan.TargetNodeID {
			continue
		}
		if replica.State != metadata.ReplicaStateReady {
			continue
		}
		node, ok := nodeIndex[nodeID]
		if !ok || !node.Healthy || strings.TrimSpace(node.Address) == "" {
			continue
		}
		ready = append(ready, nodeID)
	}
	sort.Slice(ready, func(i, j int) bool { return string(ready[i]) < string(ready[j]) })
	if len(ready) == 0 {
		return "", fmt.Errorf("mds repairer: no healthy source replica available for plan %q", plan.ID)
	}
	return ready[0], nil
}

func (r *PendingReplicaRepairer) selectDueTargets(chunkID metadata.ChunkID, nodeIDs []metadata.NodeID, limit int, now time.Time) []metadata.NodeID {
	pending := make([]metadata.NodeID, 0)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, nodeID := range nodeIDs {
		if len(pending) == limit {
			break
		}
		nextAttempt, ok := r.nextAttemptByKey[repairTargetKey(chunkID, nodeID)]
		if ok && nextAttempt.After(now) {
			continue
		}
		pending = append(pending, nodeID)
	}
	return pending
}

func (r *PendingReplicaRepairer) deferTargets(chunkID metadata.ChunkID, nodeIDs []metadata.NodeID, now time.Time) {
	if len(nodeIDs) == 0 {
		return
	}
	retryAt := now.Add(r.retryBackoff)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, nodeID := range nodeIDs {
		r.nextAttemptByKey[repairTargetKey(chunkID, nodeID)] = retryAt
	}
}

func (r *PendingReplicaRepairer) clearRetry(chunkID metadata.ChunkID, nodeID metadata.NodeID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nextAttemptByKey, repairTargetKey(chunkID, nodeID))
}

func repairTargetKey(chunkID metadata.ChunkID, nodeID metadata.NodeID) string {
	return string(chunkID) + "::" + string(nodeID)
}

func (r *PendingReplicaRepairer) recordRun(runID string, startedAt time.Time, outcome repairOutcome, err error) {
	duration := time.Since(startedAt)
	result := "success"
	if err != nil {
		result = "error"
	}
	if r != nil && r.observability != nil {
		r.observability.RecordRepairRun(result, duration)
		r.observability.RecordRepairReplicasAttempted(outcome.attempted)
		r.observability.RecordRepairReplicasSucceeded(outcome.succeeded)
		r.observability.RecordRepairReplicasFailed(outcome.failed)
		r.observability.RecordRepairTargetsDeferred(outcome.deferred)
	}
	if r != nil && r.logger != nil {
		args := []any{
			"run_id", runID,
			"result", result,
			"duration_ms", duration.Milliseconds(),
			"attempted", outcome.attempted,
			"succeeded", outcome.succeeded,
			"failed", outcome.failed,
			"deferred", outcome.deferred,
		}
		if err != nil {
			args = append(args, "error", err.Error())
		}
		r.logger.Info("repair run", args...)
	}
}

func generateRepairRunID() string {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return fmt.Sprintf("repair-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(token)
}
