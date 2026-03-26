package topology

import (
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type QueueDefinition struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Arguments  amqp.Table
}

func RepairQueue() QueueDefinition {
	return quorumTaskQueue("replica.repair.q", "dlq.replica.repair")
}

func CleanupQueue() QueueDefinition {
	return quorumTaskQueue("cleanup.q", "dlq.cleanup")
}

func RebalanceQueue() QueueDefinition {
	return quorumTaskQueue("rebalance.q", "dlq.rebalance")
}

func FailoverQueue() QueueDefinition {
	return quorumTaskQueue("failover.q", "dlq.failover")
}

func RetryQueue(name, routingKey string, ttl time.Duration) QueueDefinition {
	return QueueDefinition{
		Name:    name,
		Durable: true,
		Arguments: amqp.Table{
			"x-message-ttl":             int32(ttl / time.Millisecond),
			"x-dead-letter-exchange":    TasksExchange,
			"x-dead-letter-routing-key": routingKey,
		},
	}
}

func DLQQueue(name string) QueueDefinition {
	return QueueDefinition{
		Name:    name,
		Durable: true,
		Arguments: amqp.Table{
			"x-queue-type": "quorum",
		},
	}
}

func Queues() []QueueDefinition {
	return []QueueDefinition{
		RepairQueue(),
		CleanupQueue(),
		RebalanceQueue(),
		FailoverQueue(),
		RetryQueue("replica.repair.retry.q", "task.replica.repair", 30*time.Second),
		RetryQueue("cleanup.retry.q", "task.cleanup", 30*time.Second),
		RetryQueue("rebalance.retry.q", "task.rebalance", 30*time.Second),
		RetryQueue("failover.retry.q", "task.failover", 30*time.Second),
		DLQQueue("replica.repair.dlq"),
		DLQQueue("cleanup.dlq"),
		DLQQueue("rebalance.dlq"),
		DLQQueue("failover.dlq"),
	}
}

func quorumTaskQueue(name, dlqRoutingKey string) QueueDefinition {
	return QueueDefinition{
		Name:    name,
		Durable: true,
		Arguments: amqp.Table{
			"x-queue-type":              "quorum",
			"x-dead-letter-exchange":    DLXExchange,
			"x-dead-letter-routing-key": dlqRoutingKey,
		},
	}
}
