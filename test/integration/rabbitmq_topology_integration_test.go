package integration_test

import (
	"testing"

	"AstraStorage/internal/platform/mq/rabbitmq/topology"
)

func TestRabbitMQIntegration_DeclaresTaskTopology(t *testing.T) {
	fixture := newRabbitMQFixture(t)
	defer fixture.Close(t)

	channel := fixture.OpenChannel(t)
	defer channel.Close()

	if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
		t.Fatalf("declare topology: %v", err)
	}
	if _, err := channel.QueueInspect("replica.repair.q"); err != nil {
		t.Fatalf("inspect repair queue: %v", err)
	}
	if _, err := channel.QueueInspect("replica.repair.retry.q"); err != nil {
		t.Fatalf("inspect retry queue: %v", err)
	}
	if _, err := channel.QueueInspect("replica.repair.dlq"); err != nil {
		t.Fatalf("inspect dlq queue: %v", err)
	}
}
