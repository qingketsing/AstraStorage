package topology

import amqp "github.com/rabbitmq/amqp091-go"

func QuorumQueueArgs() amqp.Table {
	return amqp.Table{
		"x-queue-type": "quorum",
	}
}
