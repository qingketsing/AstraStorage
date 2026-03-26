package integration_test

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitMQIntegration_PublishConsumeRoundTrip(t *testing.T) {
	fixture := newRabbitMQFixture(t)
	defer fixture.Close(t)

	channel := fixture.OpenChannel(t)
	defer channel.Close()
	if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
		t.Fatalf("declare topology: %v", err)
	}

	body := fixture.MustEnvelope(t, contracts.Envelope{
		MessageID:  "msg-roundtrip",
		EventID:    "evt-roundtrip",
		TaskType:   contracts.TaskReplicaRepair,
		TraceID:    "trace-roundtrip",
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.ReplicaRepairTask{
			PlanID:       "plan-roundtrip",
			FileID:       "file-1",
			ChunkID:      "chunk-1",
			SourceNodeID: "node-1",
			TargetNodeID: "node-2",
		}),
	})
	if err := channel.PublishWithContext(context.Background(), topology.TasksExchange, "task.replica.repair", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		t.Fatalf("publish roundtrip message: %v", err)
	}

	delivery := fixture.MustConsumeOne(t, channel, "replica.repair.q")
	if err := delivery.Ack(false); err != nil {
		t.Fatalf("ack roundtrip message: %v", err)
	}

	var envelope contracts.Envelope
	if err := contracts.DecodeEnvelope(delivery.Body, &envelope); err != nil {
		t.Fatalf("decode consumed message: %v", err)
	}
	if envelope.TaskType != contracts.TaskReplicaRepair {
		t.Fatalf("expected replica repair task, got %q", envelope.TaskType)
	}
}
