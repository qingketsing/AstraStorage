package client

type confirmChannel interface {
	Confirm(noWait bool) error
}

type qosChannel interface {
	Qos(prefetchCount, prefetchSize int, global bool) error
}

func preparePublisherChannel(ch confirmChannel, enabled bool) error {
	if ch == nil || !enabled {
		return nil
	}
	return ch.Confirm(true)
}

func prepareConsumerChannel(ch qosChannel, prefetch int) error {
	if ch == nil || prefetch <= 0 {
		return nil
	}
	return ch.Qos(prefetch, 0, false)
}
