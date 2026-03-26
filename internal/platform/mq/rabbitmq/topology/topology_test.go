package topology

import (
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestExchangeDefinitions_ExposeStableNames(t *testing.T) {
	exchanges := Exchanges()
	if len(exchanges) != 4 {
		t.Fatalf("expected 4 exchanges, got %d", len(exchanges))
	}

	got := []string{
		exchanges[0].Name,
		exchanges[1].Name,
		exchanges[2].Name,
		exchanges[3].Name,
	}
	want := []string{
		"astra.tasks",
		"astra.events",
		"astra.retry",
		"astra.dlx",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected exchange order: got %#v want %#v", got, want)
		}
	}
}

func TestRepairQueue_UsesQuorumQueueAndDLX(t *testing.T) {
	queue := RepairQueue()
	assertQueueArg(t, queue.Arguments, "x-queue-type", "quorum")
	assertQueueArg(t, queue.Arguments, "x-dead-letter-exchange", "astra.dlx")
	assertQueueArg(t, queue.Arguments, "x-dead-letter-routing-key", "dlq.replica.repair")
}

func TestRetryQueue_UsesTTLAndRoutesBackToTaskExchange(t *testing.T) {
	queue := RetryQueue("replica.repair.retry.q", "task.replica.repair", 30*time.Second)
	assertQueueArg(t, queue.Arguments, "x-message-ttl", int32(30000))
	assertQueueArg(t, queue.Arguments, "x-dead-letter-exchange", "astra.tasks")
	assertQueueArg(t, queue.Arguments, "x-dead-letter-routing-key", "task.replica.repair")
}

func TestBindings_IncludeTaskRoutingKeys(t *testing.T) {
	bindings := Bindings()
	found := false
	for _, binding := range bindings {
		if binding.Exchange == "astra.tasks" && binding.Queue == "replica.repair.q" && binding.RoutingKey == "task.replica.repair" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected replica repair task binding to exist")
	}
}

func TestTaskTopology_AggregatesExchangesQueuesAndBindings(t *testing.T) {
	topology := TaskTopology()
	if len(topology.Exchanges) != 4 {
		t.Fatalf("expected 4 exchanges, got %d", len(topology.Exchanges))
	}
	if len(topology.Queues) != 12 {
		t.Fatalf("expected 12 queues, got %d", len(topology.Queues))
	}
	if len(topology.Bindings) == 0 {
		t.Fatal("expected task topology to include bindings")
	}
}

func assertQueueArg(t *testing.T, args amqp.Table, key string, want any) {
	t.Helper()
	got, ok := args[key]
	if !ok {
		t.Fatalf("expected queue arg %q to exist", key)
	}
	if got != want {
		t.Fatalf("unexpected queue arg %q: got %#v want %#v", key, got, want)
	}
}
