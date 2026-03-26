package integration_test

import (
	"testing"
	"time"
)

func TestRabbitMQIntegration_ClusterRemainsAvailableAfterSingleNodeStop(t *testing.T) {
	fixture := newRabbitMQFixture(t)
	defer fixture.Close(t)

	fixture.RequireDockerClusterControl(t)
	fixture.StopNode(t, "rabbitmq-cluster-rabbitmq-3-1")

	channel := fixture.WaitForQueueAvailable(t, "replica.repair.q", 20*time.Second)
	defer channel.Close()
	fixture.PublishRepairTask(t, channel, "msg-failover", "evt-failover")
	delivery := fixture.MustConsumeOneWithin(t, channel, "replica.repair.q", 10*time.Second)
	if err := delivery.Ack(false); err != nil {
		t.Fatalf("ack failover delivery: %v", err)
	}
}
