package topology

type BindingDefinition struct {
	Queue      string
	Exchange   string
	RoutingKey string
	NoWait     bool
}

func Bindings() []BindingDefinition {
	return []BindingDefinition{
		{Exchange: TasksExchange, Queue: "replica.repair.q", RoutingKey: "task.replica.repair"},
		{Exchange: TasksExchange, Queue: "cleanup.q", RoutingKey: "task.cleanup"},
		{Exchange: TasksExchange, Queue: "rebalance.q", RoutingKey: "task.rebalance"},
		{Exchange: TasksExchange, Queue: "failover.q", RoutingKey: "task.failover"},
		{Exchange: RetryExchange, Queue: "replica.repair.retry.q", RoutingKey: "retry.replica.repair"},
		{Exchange: RetryExchange, Queue: "cleanup.retry.q", RoutingKey: "retry.cleanup"},
		{Exchange: RetryExchange, Queue: "rebalance.retry.q", RoutingKey: "retry.rebalance"},
		{Exchange: RetryExchange, Queue: "failover.retry.q", RoutingKey: "retry.failover"},
		{Exchange: DLXExchange, Queue: "replica.repair.dlq", RoutingKey: "dlq.replica.repair"},
		{Exchange: DLXExchange, Queue: "cleanup.dlq", RoutingKey: "dlq.cleanup"},
		{Exchange: DLXExchange, Queue: "rebalance.dlq", RoutingKey: "dlq.rebalance"},
		{Exchange: DLXExchange, Queue: "failover.dlq", RoutingKey: "dlq.failover"},
	}
}
