package mq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQTaskProducer struct {
	manager        *rabbitmqclient.Manager
	channel        *amqp.Channel
	confirmEnabled bool
	mu             sync.Mutex
}

func NewRabbitMQTaskProducer(manager *rabbitmqclient.Manager) (*RabbitMQTaskProducer, error) {
	if manager == nil {
		return nil, fmt.Errorf("mds mq: rabbitmq manager is nil")
	}
	producer := &RabbitMQTaskProducer{
		manager:        manager,
		confirmEnabled: manager.Config().PublisherConfirm,
	}
	if err := producer.reopenChannel(context.Background()); err != nil {
		return nil, err
	}
	return producer, nil
}

func (p *RabbitMQTaskProducer) PublishReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error {
	return p.publishTask(ctx, "task.replica.repair", task.Kind(), task)
}

func (p *RabbitMQTaskProducer) PublishCleanup(ctx context.Context, task contracts.CleanupTask) error {
	return p.publishTask(ctx, "task.cleanup", task.Kind(), task)
}

func (p *RabbitMQTaskProducer) PublishRebalance(ctx context.Context, task contracts.RebalanceTask) error {
	return p.publishTask(ctx, "task.rebalance", task.Kind(), task)
}

func (p *RabbitMQTaskProducer) PublishFailover(ctx context.Context, task contracts.FailoverTask) error {
	return p.publishTask(ctx, "task.failover", task.Kind(), task)
}

func (p *RabbitMQTaskProducer) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel == nil {
		return nil
	}
	err := p.channel.Close()
	p.channel = nil
	return err
}

func (p *RabbitMQTaskProducer) publishTask(ctx context.Context, routingKey string, taskType contracts.TaskType, payload any) error {
	if p == nil {
		return fmt.Errorf("mds mq: task producer is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureChannelLocked(ctx); err != nil {
		return err
	}
	envelope := contracts.Envelope{
		MessageID:  newEnvelopeID(),
		EventID:    newEnvelopeID(),
		TaskType:   taskType,
		TraceID:    newEnvelopeID(),
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload:    contracts.MustPayload(payload),
	}
	body, err := contracts.EncodeEnvelope(envelope)
	if err != nil {
		return err
	}
	return p.channel.PublishWithContext(ctx, topology.TasksExchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    envelope.MessageID,
		Type:         string(taskType),
		Timestamp:    envelope.OccurredAt,
		Body:         body,
	})
}

func (p *RabbitMQTaskProducer) ensureChannelLocked(ctx context.Context) error {
	if p.channel != nil && !p.channel.IsClosed() {
		return nil
	}
	return p.reopenChannel(ctx)
}

func (p *RabbitMQTaskProducer) reopenChannel(ctx context.Context) error {
	if p.manager.Connection() == nil || p.manager.Connection().IsClosed() {
		if err := p.manager.Dial(ctx); err != nil {
			return err
		}
	}
	channel, err := p.manager.Connection().OpenChannel()
	if err != nil {
		return err
	}
	if p.confirmEnabled {
		if err := channel.Confirm(false); err != nil {
			_ = channel.Close()
			return err
		}
	}
	if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
		_ = channel.Close()
		return err
	}
	if p.channel != nil && !p.channel.IsClosed() {
		_ = p.channel.Close()
	}
	p.channel = channel
	return nil
}

func newEnvelopeID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("mq-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buf)
}
