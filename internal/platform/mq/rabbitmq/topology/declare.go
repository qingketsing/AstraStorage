package topology

import amqp "github.com/rabbitmq/amqp091-go"

type Definitions struct {
	Exchanges []ExchangeDefinition
	Queues    []QueueDefinition
	Bindings  []BindingDefinition
}

type Declarer interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
}

func TaskTopology() Definitions {
	return Definitions{
		Exchanges: Exchanges(),
		Queues:    Queues(),
		Bindings:  Bindings(),
	}
}

func Declare(declarer Declarer, defs Definitions) error {
	for _, exchange := range defs.Exchanges {
		if err := declarer.ExchangeDeclare(exchange.Name, exchange.Kind, exchange.Durable, exchange.AutoDelete, exchange.Internal, exchange.NoWait, nil); err != nil {
			return err
		}
	}
	for _, queue := range defs.Queues {
		if _, err := declarer.QueueDeclare(queue.Name, queue.Durable, queue.AutoDelete, queue.Exclusive, queue.NoWait, queue.Arguments); err != nil {
			return err
		}
	}
	for _, binding := range defs.Bindings {
		if err := declarer.QueueBind(binding.Queue, binding.RoutingKey, binding.Exchange, binding.NoWait, nil); err != nil {
			return err
		}
	}
	return nil
}
