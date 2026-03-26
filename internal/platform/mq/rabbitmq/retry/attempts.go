package retry

import "AstraStorage/internal/platform/mq/contracts"

func Attempt(envelope contracts.Envelope) int {
	if envelope.Attempt <= 0 {
		return 1
	}
	return envelope.Attempt
}

func NextAttempt(envelope contracts.Envelope) contracts.Envelope {
	next := envelope
	next.Attempt = Attempt(envelope) + 1
	return next
}
