package mq

import (
	"context"
	"fmt"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/idempotency"
)

type CleanupExecutor interface {
	ExecuteCleanup(ctx context.Context, task contracts.CleanupTask) error
}

type CleanupConsumer struct {
	executor    CleanupExecutor
	idempotency *idempotency.Handler
}

func NewCleanupConsumer(executor CleanupExecutor) *CleanupConsumer {
	return &CleanupConsumer{executor: executor}
}

func (c *CleanupConsumer) SetIdempotencyHandler(handler *idempotency.Handler) {
	if c == nil {
		return
	}
	c.idempotency = handler
}

func (c *CleanupConsumer) Handle(ctx context.Context, delivery Delivery) error {
	var task contracts.CleanupTask
	envelope, err := decodeTaskDelivery(delivery, contracts.TaskCleanup, &task)
	if err != nil {
		return err
	}
	if c == nil || c.executor == nil {
		return fmt.Errorf("mds mq: cleanup executor is nil")
	}
	if c.idempotency != nil {
		duplicate, err := c.idempotency.Execute(ctx, envelope, func(ctx context.Context) error {
			return c.executor.ExecuteCleanup(ctx, task)
		})
		if err != nil {
			return err
		}
		if duplicate {
			return delivery.Ack(false)
		}
		return delivery.Ack(false)
	}
	if err := c.executor.ExecuteCleanup(ctx, task); err != nil {
		return err
	}
	return delivery.Ack(false)
}
