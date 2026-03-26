package mq

import (
	"context"
	"fmt"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/idempotency"
)

type ReplicaPlanCopyExecutor interface {
	ExecuteReplicaPlanCopy(ctx context.Context, planID string) error
}

type RebalanceConsumer struct {
	executor    ReplicaPlanCopyExecutor
	idempotency *idempotency.Handler
}

func NewRebalanceConsumer(executor ReplicaPlanCopyExecutor) *RebalanceConsumer {
	return &RebalanceConsumer{executor: executor}
}

func (c *RebalanceConsumer) SetIdempotencyHandler(handler *idempotency.Handler) {
	if c == nil {
		return
	}
	c.idempotency = handler
}

func (c *RebalanceConsumer) Handle(ctx context.Context, delivery Delivery) error {
	var task contracts.RebalanceTask
	envelope, err := decodeTaskDelivery(delivery, contracts.TaskRebalance, &task)
	if err != nil {
		return err
	}
	if c == nil || c.executor == nil {
		return fmt.Errorf("mds mq: rebalance executor is nil")
	}
	if c.idempotency != nil {
		duplicate, err := c.idempotency.Execute(ctx, envelope, func(ctx context.Context) error {
			return c.executor.ExecuteReplicaPlanCopy(ctx, task.PlanID)
		})
		if err != nil {
			return err
		}
		if duplicate {
			return delivery.Ack(false)
		}
		return delivery.Ack(false)
	}
	if err := c.executor.ExecuteReplicaPlanCopy(ctx, task.PlanID); err != nil {
		return err
	}
	return delivery.Ack(false)
}
