package retry

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRouteFailure_PublishesRetryMessageWhileAttemptsRemain(t *testing.T) {
	publisher := &capturingPublisher{}
	envelope := contracts.Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   contracts.TaskReplicaRepair,
		TraceID:    "trace-1",
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.ReplicaRepairTask{
			PlanID:       "plan-1",
			FileID:       "file-1",
			ChunkID:      "chunk-1",
			SourceNodeID: "node-1",
			TargetNodeID: "node-2",
		}),
	}

	outcome, err := RouteFailure(context.Background(), publisher, Policy{MaxAttempts: 3}, envelope)
	if err != nil {
		t.Fatalf("route failure: %v", err)
	}
	if outcome != OutcomeRetry {
		t.Fatalf("expected retry outcome, got %q", outcome)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("expected one published message, got %d", len(publisher.messages))
	}
	got := publisher.messages[0]
	if got.exchange != topology.RetryExchange {
		t.Fatalf("expected retry exchange, got %q", got.exchange)
	}
	if got.routingKey != "retry.replica.repair" {
		t.Fatalf("expected retry routing key, got %q", got.routingKey)
	}
	var published contracts.Envelope
	if err := contracts.DecodeEnvelope(got.body, &published); err != nil {
		t.Fatalf("decode published envelope: %v", err)
	}
	if published.Attempt != 2 {
		t.Fatalf("expected attempt to increment to 2, got %d", published.Attempt)
	}
}

func TestRouteFailure_PublishesDLQAfterMaximumAttempts(t *testing.T) {
	publisher := &capturingPublisher{}
	envelope := contracts.Envelope{
		MessageID:  "msg-1",
		EventID:    "evt-1",
		TaskType:   contracts.TaskCleanup,
		TraceID:    "trace-1",
		Attempt:    3,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.CleanupTask{
			PlanID: "plan-1",
			FileID: "file-1",
			NodeID: "node-1",
			Reason: "cleanup",
		}),
	}

	outcome, err := RouteFailure(context.Background(), publisher, Policy{MaxAttempts: 3}, envelope)
	if err != nil {
		t.Fatalf("route failure: %v", err)
	}
	if outcome != OutcomeDLQ {
		t.Fatalf("expected dlq outcome, got %q", outcome)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("expected one published message, got %d", len(publisher.messages))
	}
	got := publisher.messages[0]
	if got.exchange != topology.DLXExchange {
		t.Fatalf("expected dlx exchange, got %q", got.exchange)
	}
	if got.routingKey != "dlq.cleanup" {
		t.Fatalf("expected dlq routing key, got %q", got.routingKey)
	}
}

type capturingPublisher struct {
	messages []publishedMessage
}

type publishedMessage struct {
	exchange   string
	routingKey string
	body       []byte
}

func (c *capturingPublisher) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	c.messages = append(c.messages, publishedMessage{
		exchange:   exchange,
		routingKey: key,
		body:       append([]byte(nil), msg.Body...),
	})
	return nil
}
