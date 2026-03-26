package retry

import (
	"context"
	"fmt"
	"time"

	"AstraStorage/internal/platform/mq/contracts"
	"AstraStorage/internal/platform/mq/rabbitmq/topology"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
}

func RetryRoutingKey(taskType contracts.TaskType) (string, error) {
	switch taskType {
	case contracts.TaskReplicaRepair:
		return "retry.replica.repair", nil
	case contracts.TaskCleanup:
		return "retry.cleanup", nil
	case contracts.TaskRebalance:
		return "retry.rebalance", nil
	case contracts.TaskFailover:
		return "retry.failover", nil
	default:
		return "", fmt.Errorf("rabbitmq retry: unsupported task type %q", taskType)
	}
}

func DLQRoutingKey(taskType contracts.TaskType) (string, error) {
	switch taskType {
	case contracts.TaskReplicaRepair:
		return "dlq.replica.repair", nil
	case contracts.TaskCleanup:
		return "dlq.cleanup", nil
	case contracts.TaskRebalance:
		return "dlq.rebalance", nil
	case contracts.TaskFailover:
		return "dlq.failover", nil
	default:
		return "", fmt.Errorf("rabbitmq retry: unsupported task type %q", taskType)
	}
}

func RouteFailure(ctx context.Context, publisher Publisher, policy Policy, envelope contracts.Envelope) (Outcome, error) {
	if publisher == nil {
		return "", fmt.Errorf("rabbitmq retry: publisher is nil")
	}
	policy = policy.WithDefaults()
	if Attempt(envelope) >= policy.MaxAttempts {
		routingKey, err := DLQRoutingKey(envelope.TaskType)
		if err != nil {
			return "", err
		}
		if err := publishEnvelope(ctx, publisher, topology.DLXExchange, routingKey, envelope, DelayForAttempt(policy, Attempt(envelope))); err != nil {
			return "", err
		}
		return OutcomeDLQ, nil
	}

	envelope = NextAttempt(envelope)
	routingKey, err := RetryRoutingKey(envelope.TaskType)
	if err != nil {
		return "", err
	}
	if err := publishEnvelope(ctx, publisher, topology.RetryExchange, routingKey, envelope, DelayForAttempt(policy, envelope.Attempt)); err != nil {
		return "", err
	}
	return OutcomeRetry, nil
}

func publishEnvelope(ctx context.Context, publisher Publisher, exchange, routingKey string, envelope contracts.Envelope, delay time.Duration) error {
	body, err := contracts.EncodeEnvelope(envelope)
	if err != nil {
		return err
	}
	return publisher.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    envelope.MessageID,
		Type:         string(envelope.TaskType),
		Timestamp:    time.Now().UTC(),
		Headers: amqp.Table{
			"x-attempt":  envelope.Attempt,
			"x-delay-ms": int64(delay / time.Millisecond),
		},
		Body: body,
	})
}
