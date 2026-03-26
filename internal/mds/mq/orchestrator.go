package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"AstraStorage/internal/platform/mq/contracts"
	rabbitmqclient "AstraStorage/internal/platform/mq/rabbitmq/client"
	retrypkg "AstraStorage/internal/platform/mq/rabbitmq/retry"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ConsumerOrchestrator struct {
	manager   *rabbitmqclient.Manager
	prefetch  int
	retry     retrypkg.Policy
	consumers []registeredConsumer
}

type registeredConsumer struct {
	queue   string
	tag     string
	handler func(context.Context, Delivery) error
}

func NewOrchestrator(manager *rabbitmqclient.Manager, prefetch int, repair *RepairConsumer, cleanup *CleanupConsumer, rebalance *RebalanceConsumer, failover *FailoverConsumer) *ConsumerOrchestrator {
	return &ConsumerOrchestrator{
		manager:  manager,
		prefetch: prefetch,
		retry:    retrypkg.Policy{}.WithDefaults(),
		consumers: []registeredConsumer{
			{queue: "replica.repair.q", tag: "mds-repair", handler: repair.Handle},
			{queue: "cleanup.q", tag: "mds-cleanup", handler: cleanup.Handle},
			{queue: "rebalance.q", tag: "mds-rebalance", handler: rebalance.Handle},
			{queue: "failover.q", tag: "mds-failover", handler: failover.Handle},
		},
	}
}

func (o *ConsumerOrchestrator) Run(ctx context.Context) error {
	if o == nil || o.manager == nil {
		return nil
	}
	if o.manager.Connection() == nil || o.manager.Connection().IsClosed() {
		if err := o.manager.Dial(ctx); err != nil {
			return err
		}
	}
	var (
		wg       sync.WaitGroup
		errCh    = make(chan error, len(o.consumers))
		channels []*amqp.Channel
	)
	for _, consumer := range o.consumers {
		channel, err := o.manager.Connection().OpenChannel()
		if err != nil {
			closeAMQPChannels(channels)
			return err
		}
		if o.prefetch > 0 {
			if err := channel.Qos(o.prefetch, 0, false); err != nil {
				_ = channel.Close()
				closeAMQPChannels(channels)
				return err
			}
		}
		if err := topology.Declare(channel, topology.TaskTopology()); err != nil {
			_ = channel.Close()
			closeAMQPChannels(channels)
			return err
		}
		deliveries, err := channel.Consume(consumer.queue, consumer.tag, false, false, false, false, nil)
		if err != nil {
			_ = channel.Close()
			closeAMQPChannels(channels)
			return err
		}
		channels = append(channels, channel)
		wg.Add(1)
		go func(handler func(context.Context, Delivery) error, stream <-chan amqp.Delivery) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case delivery, ok := <-stream:
					if !ok {
						return
					}
					if err := handler(ctx, amqpDelivery{delivery: delivery}); err != nil {
						if _, routeErr := processDeliveryFailure(ctx, channel, amqpDelivery{delivery: delivery}, o.retry, err); routeErr != nil {
							errCh <- routeErr
							_ = delivery.Nack(false, true)
						}
					}
				}
			}
		}(consumer.handler, deliveries)
	}

	select {
	case <-ctx.Done():
		closeAMQPChannels(channels)
		wg.Wait()
		return ctx.Err()
	case err := <-errCh:
		closeAMQPChannels(channels)
		wg.Wait()
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("mds mq: consumer failed: %w", err)
	}
}

type amqpDelivery struct {
	delivery amqp.Delivery
}

func (d amqpDelivery) Body() []byte {
	return d.delivery.Body
}

func (d amqpDelivery) Ack(multiple bool) error {
	return d.delivery.Ack(multiple)
}

func (d amqpDelivery) Nack(multiple, requeue bool) error {
	return d.delivery.Nack(multiple, requeue)
}

func closeAMQPChannels(channels []*amqp.Channel) {
	for _, channel := range channels {
		if channel != nil && !channel.IsClosed() {
			_ = channel.Close()
		}
	}
}

func processDeliveryFailure(ctx context.Context, publisher retrypkg.Publisher, delivery Delivery, policy retrypkg.Policy, handlerErr error) (retrypkg.Outcome, error) {
	if handlerErr == nil {
		return "", nil
	}
	var envelope contracts.Envelope
	if err := contracts.DecodeEnvelope(delivery.Body(), &envelope); err != nil {
		return "", err
	}
	outcome, err := retrypkg.RouteFailure(ctx, publisher, policy, envelope)
	if err != nil {
		return "", err
	}
	if err := delivery.Ack(false); err != nil {
		return "", err
	}
	return outcome, nil
}
