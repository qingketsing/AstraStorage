package mq

import (
	"context"

	"AstraStorage/internal/platform/mq/contracts"
)

// TaskProducer publishes coordinator work into the RabbitMQ task topology.
type TaskProducer interface {
	PublishReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error
	PublishCleanup(ctx context.Context, task contracts.CleanupTask) error
	PublishRebalance(ctx context.Context, task contracts.RebalanceTask) error
	PublishFailover(ctx context.Context, task contracts.FailoverTask) error
}
