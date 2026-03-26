package retry

import "time"

const (
	defaultMaxAttempts = 3
	defaultBaseDelay   = 30 * time.Second
)

type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

func (p Policy) WithDefaults() Policy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaultMaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = defaultBaseDelay
	}
	return p
}

type Outcome string

const (
	OutcomeRetry Outcome = "retry"
	OutcomeDLQ   Outcome = "dlq"
)
