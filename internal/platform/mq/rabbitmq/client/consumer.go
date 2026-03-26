package client

type Consumer struct {
	prefetch int
}

func NewConsumer(prefetch int) *Consumer {
	return &Consumer{prefetch: prefetch}
}

func (c *Consumer) Prefetch() int {
	if c == nil {
		return 0
	}
	return c.prefetch
}
