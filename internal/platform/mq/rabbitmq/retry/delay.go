package retry

import "time"

func DelayForAttempt(policy Policy, attempt int) time.Duration {
	policy = policy.WithDefaults()
	if attempt <= 0 {
		attempt = 1
	}
	return time.Duration(attempt) * policy.BaseDelay
}
