package mq

import (
	"context"
	"encoding/json"
	"fmt"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/idempotency"
)

type Delivery interface {
	Body() []byte
	Ack(multiple bool) error
	Nack(multiple, requeue bool) error
}

type ReplicaRepairExecutor interface {
	ExecuteReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error
}

type RepairConsumer struct {
	executor    ReplicaRepairExecutor
	idempotency *idempotency.Handler
}

func NewRepairConsumer(executor ReplicaRepairExecutor) *RepairConsumer {
	return &RepairConsumer{executor: executor}
}

func (c *RepairConsumer) SetIdempotencyHandler(handler *idempotency.Handler) {
	if c == nil {
		return
	}
	c.idempotency = handler
}

func (c *RepairConsumer) Handle(ctx context.Context, delivery Delivery) error {
	var task contracts.ReplicaRepairTask
	envelope, err := decodeTaskDelivery(delivery, contracts.TaskReplicaRepair, &task)
	if err != nil {
		return err
	}
	if c == nil || c.executor == nil {
		return fmt.Errorf("mds mq: repair executor is nil")
	}
	if c.idempotency != nil {
		duplicate, err := c.idempotency.Execute(ctx, envelope, func(ctx context.Context) error {
			return c.executor.ExecuteReplicaRepair(ctx, task)
		})
		if err != nil {
			return err
		}
		if duplicate {
			return delivery.Ack(false)
		}
		return delivery.Ack(false)
	}
	if err := c.executor.ExecuteReplicaRepair(ctx, task); err != nil {
		return err
	}
	return delivery.Ack(false)
}

func decodeTaskDelivery(delivery Delivery, kind contracts.TaskType, target any) (contracts.Envelope, error) {
	if delivery == nil {
		return contracts.Envelope{}, fmt.Errorf("mds mq: delivery is nil")
	}
	var envelope contracts.Envelope
	if err := contracts.DecodeEnvelope(delivery.Body(), &envelope); err != nil {
		return contracts.Envelope{}, err
	}
	if envelope.TaskType != kind {
		return contracts.Envelope{}, fmt.Errorf("mds mq: unexpected task type %q", envelope.TaskType)
	}
	if err := json.Unmarshal(envelope.Payload, target); err != nil {
		return contracts.Envelope{}, err
	}
	return envelope, nil
}
