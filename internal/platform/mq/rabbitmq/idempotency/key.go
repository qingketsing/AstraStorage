package idempotency

import (
	"encoding/json"
	"fmt"
	"strings"

	"AstraStorage/internal/platform/mq/contracts"
)

func KeyForEnvelope(envelope contracts.Envelope) (string, error) {
	if trimmed := strings.TrimSpace(envelope.EventID); trimmed != "" {
		return trimmed, nil
	}
	var payload struct {
		PlanID string `json:"plan_id"`
	}
	if len(envelope.Payload) > 0 && json.Unmarshal(envelope.Payload, &payload) == nil && strings.TrimSpace(payload.PlanID) != "" {
		return string(envelope.TaskType) + ":" + strings.TrimSpace(payload.PlanID), nil
	}
	if trimmed := strings.TrimSpace(envelope.MessageID); trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf("rabbitmq idempotency: envelope has no stable identifier")
}
