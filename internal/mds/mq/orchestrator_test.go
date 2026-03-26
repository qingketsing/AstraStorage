package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/retry"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestProcessDeliveryFailure_RoutesToRetryAndAcksOriginal(t *testing.T) {
	publisher := &capturingAMQPPublisher{}
	delivery := &recordingDelivery{body: mustTestEnvelope(t, contracts.TaskReplicaRepair, 1)}

	outcome, err := processDeliveryFailure(context.Background(), publisher, delivery, retry.Policy{MaxAttempts: 3}, errors.New("boom"))
	if err != nil {
		t.Fatalf("process delivery failure: %v", err)
	}
	if outcome != retry.OutcomeRetry {
		t.Fatalf("expected retry outcome, got %q", outcome)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected original delivery to be acked, got %d", delivery.ackCount)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].exchange != topology.RetryExchange {
		t.Fatalf("expected message to be republished to retry exchange, got %#v", publisher.messages)
	}
}

func TestProcessDeliveryFailure_RoutesToDLQAfterMaxAttempts(t *testing.T) {
	publisher := &capturingAMQPPublisher{}
	delivery := &recordingDelivery{body: mustTestEnvelope(t, contracts.TaskCleanup, 3)}

	outcome, err := processDeliveryFailure(context.Background(), publisher, delivery, retry.Policy{MaxAttempts: 3}, errors.New("boom"))
	if err != nil {
		t.Fatalf("process delivery failure: %v", err)
	}
	if outcome != retry.OutcomeDLQ {
		t.Fatalf("expected dlq outcome, got %q", outcome)
	}
	if delivery.ackCount != 1 {
		t.Fatalf("expected original delivery to be acked, got %d", delivery.ackCount)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].exchange != topology.DLXExchange {
		t.Fatalf("expected message to be republished to dlx exchange, got %#v", publisher.messages)
	}
}

type recordingDelivery struct {
	body      []byte
	ackCount  int
	nackCount int
}

func (d *recordingDelivery) Body() []byte {
	return d.body
}

func (d *recordingDelivery) Ack(multiple bool) error {
	d.ackCount++
	return nil
}

func (d *recordingDelivery) Nack(multiple, requeue bool) error {
	d.nackCount++
	return nil
}

type capturingAMQPPublisher struct {
	messages []publishedAMQPMessage
}

type publishedAMQPMessage struct {
	exchange   string
	routingKey string
	body       []byte
}

func (c *capturingAMQPPublisher) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	c.messages = append(c.messages, publishedAMQPMessage{
		exchange:   exchange,
		routingKey: key,
		body:       append([]byte(nil), msg.Body...),
	})
	return nil
}

func mustTestEnvelope(t *testing.T, taskType contracts.TaskType, attempt int) []byte {
	t.Helper()
	body, err := contracts.EncodeEnvelope(contracts.Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   taskType,
		TraceID:    "trace-1",
		Attempt:    attempt,
		OccurredAt: time.Now().UTC(),
		Payload:    contracts.MustPayload(map[string]any{"plan_id": "plan-1"}),
	})
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return body
}
