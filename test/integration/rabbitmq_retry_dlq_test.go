package integration_test

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	retrypkg "AstraStorage/internal/platform/mq/rabbitmq/retry"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitMQIntegration_RetryAndDLQRouting(t *testing.T) {
	fixture := newRabbitMQFixture(t)
	defer fixture.Close(t)

	channel := fixture.OpenChannel(t)
	defer channel.Close()
	if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
		t.Fatalf("declare topology: %v", err)
	}

	envelope := contracts.Envelope{
		MessageID:  "msg-retry",
		EventID:    "evt-retry",
		TaskType:   contracts.TaskReplicaRepair,
		TraceID:    "trace-retry",
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.ReplicaRepairTask{
			PlanID:       "plan-retry",
			FileID:       "file-1",
			ChunkID:      "chunk-1",
			SourceNodeID: "node-1",
			TargetNodeID: "node-2",
		}),
	}
	outcome, err := retrypkg.RouteFailure(context.Background(), channel, retrypkg.Policy{MaxAttempts: 3}, envelope)
	if err != nil {
		t.Fatalf("route retry failure: %v", err)
	}
	if outcome != retrypkg.OutcomeRetry {
		t.Fatalf("expected retry outcome, got %q", outcome)
	}
	retryDelivery := fixture.MustConsumeOne(t, channel, "replica.repair.retry.q")
	if err := retryDelivery.Ack(false); err != nil {
		t.Fatalf("ack retry delivery: %v", err)
	}

	envelope.Attempt = 3
	outcome, err = retrypkg.RouteFailure(context.Background(), channel, retrypkg.Policy{MaxAttempts: 3}, envelope)
	if err != nil {
		t.Fatalf("route dlq failure: %v", err)
	}
	if outcome != retrypkg.OutcomeDLQ {
		t.Fatalf("expected dlq outcome, got %q", outcome)
	}
	dlqDelivery := fixture.MustConsumeOne(t, channel, "replica.repair.dlq")
	if err := dlqDelivery.Ack(false); err != nil {
		t.Fatalf("ack dlq delivery: %v", err)
	}
}

var _ retrypkg.Publisher = (*amqp.Channel)(nil)
