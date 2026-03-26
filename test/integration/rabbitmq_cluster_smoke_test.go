package integration_test

import "testing"

func TestRabbitMQClusterSmoke_DialsCluster(t *testing.T) {
	fixture := newRabbitMQFixture(t)
	defer fixture.Close(t)

	if fixture.manager.ActiveEndpoint() == "" {
		t.Fatal("expected active RabbitMQ endpoint after dialing cluster")
	}
}
