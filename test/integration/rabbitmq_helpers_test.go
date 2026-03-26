package integration_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

type rabbitMQFixture struct {
	manager *rabbitmqclient.Manager
	cfg     rabbitmqclient.Config
}

func newRabbitMQFixture(t *testing.T) *rabbitMQFixture {
	t.Helper()

	cfg := rabbitMQTestConfig()
	if len(cfg.Endpoints) == 0 {
		t.Skip("set MDS_TEST_RABBITMQ_ENDPOINTS to run RabbitMQ integration tests")
	}
	manager, err := rabbitmqclient.NewManager(cfg)
	if err != nil {
		t.Fatalf("new rabbitmq manager: %v", err)
	}
	if err := manager.Dial(context.Background()); err != nil {
		t.Fatalf("dial rabbitmq cluster: %v", err)
	}
	fixture := &rabbitMQFixture{
		manager: manager,
		cfg:     cfg,
	}
	channel := fixture.OpenChannel(t)
	defer channel.Close()
	if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
		t.Fatalf("declare task topology: %v", err)
	}
	fixture.purgeQueues(t, channel)
	return fixture
}

func (f *rabbitMQFixture) Close(t *testing.T) {
	t.Helper()
	if f == nil || f.manager == nil {
		return
	}
	_ = f.manager.Close()
}

func (f *rabbitMQFixture) OpenChannel(t *testing.T) *amqp.Channel {
	t.Helper()
	channel, err := f.manager.Connection().OpenChannel()
	if err != nil {
		t.Fatalf("open rabbitmq channel: %v", err)
	}
	return channel
}

func (f *rabbitMQFixture) MustEnvelope(t *testing.T, envelope contracts.Envelope) []byte {
	t.Helper()
	body, err := contracts.EncodeEnvelope(envelope)
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return body
}

func (f *rabbitMQFixture) MustConsumeOne(t *testing.T, channel *amqp.Channel, queue string) amqp.Delivery {
	t.Helper()
	return f.MustConsumeOneWithin(t, channel, queue, 5*time.Second)
}

func (f *rabbitMQFixture) MustConsumeOneWithin(t *testing.T, channel *amqp.Channel, queue string, timeout time.Duration) amqp.Delivery {
	t.Helper()
	deliveries, err := channel.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume queue %s: %v", queue, err)
	}
	select {
	case delivery := <-deliveries:
		return delivery
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for delivery from %s", queue)
		return amqp.Delivery{}
	}
}

func (f *rabbitMQFixture) WaitForQueueAvailable(t *testing.T, queue string, timeout time.Duration) *amqp.Channel {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		channel := f.OpenChannel(t)
		_, err := channel.QueueInspect(queue)
		if err == nil {
			return channel
		}
		lastErr = err
		_ = channel.Close()
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("queue %s did not become available after node interruption: %v", queue, lastErr)
	return nil
}

func (f *rabbitMQFixture) PublishRepairTask(t *testing.T, channel *amqp.Channel, messageID, eventID string) {
	t.Helper()
	body := f.MustEnvelope(t, contracts.Envelope{
		MessageID:  messageID,
		EventID:    eventID,
		TaskType:   contracts.TaskReplicaRepair,
		TraceID:    "trace-" + eventID,
		Attempt:    1,
		OccurredAt: time.Now().UTC(),
		Payload: contracts.MustPayload(contracts.ReplicaRepairTask{
			PlanID:       "plan-" + eventID,
			FileID:       "file-1",
			ChunkID:      "chunk-1",
			SourceNodeID: "node-1",
			TargetNodeID: "node-2",
		}),
	})
	if err := channel.PublishWithContext(context.Background(), topology.TasksExchange, "task.replica.repair", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    messageID,
		Body:         body,
	}); err != nil {
		t.Fatalf("publish repair task: %v", err)
	}
}

func (f *rabbitMQFixture) RequireDockerClusterControl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required to run RabbitMQ failover integration test")
	}
}

func (f *rabbitMQFixture) StopNode(t *testing.T, container string) {
	t.Helper()
	f.runDockerCommand(t, "exec", container, "rabbitmqctl", "stop_app")
	t.Cleanup(func() {
		f.runDockerCommand(t, "exec", container, "rabbitmqctl", "start_app")
	})
}

func (f *rabbitMQFixture) runDockerCommand(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run docker %v: %v\n%s", args, err, strings.TrimSpace(string(output)))
	}
}

func (f *rabbitMQFixture) purgeQueues(t *testing.T, channel *amqp.Channel) {
	t.Helper()
	for _, queue := range []string{
		"replica.repair.q",
		"cleanup.q",
		"rebalance.q",
		"failover.q",
		"replica.repair.retry.q",
		"cleanup.retry.q",
		"rebalance.retry.q",
		"failover.retry.q",
		"replica.repair.dlq",
		"cleanup.dlq",
		"rebalance.dlq",
		"failover.dlq",
	} {
		if _, err := channel.QueuePurge(queue, false); err != nil {
			t.Fatalf("purge queue %s: %v", queue, err)
		}
	}
}

func rabbitMQTestConfig() rabbitmqclient.Config {
	endpoints := splitNonEmpty(os.Getenv("MDS_TEST_RABBITMQ_ENDPOINTS"))
	if len(endpoints) == 0 {
		return rabbitmqclient.Config{}
	}
	cfg := rabbitmqclient.Config{
		Endpoints: endpoints,
		Username:  strings.TrimSpace(os.Getenv("MDS_TEST_RABBITMQ_USERNAME")),
		Password:  strings.TrimSpace(os.Getenv("MDS_TEST_RABBITMQ_PASSWORD")),
		VHost:     strings.TrimSpace(os.Getenv("MDS_TEST_RABBITMQ_VHOST")),
	}
	if cfg.Username == "" {
		cfg.Username = "astra"
	}
	if cfg.Password == "" {
		cfg.Password = "astra-dev"
	}
	if cfg.VHost == "" {
		cfg.VHost = "/astra"
	}
	return cfg.WithDefaults()
}
