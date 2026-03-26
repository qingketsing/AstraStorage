package client

type HealthSummary struct {
	Endpoint         string
	EndpointCount    int
	Connected        bool
	PublisherConfirm bool
	ConsumerPrefetch int
}

func (m *Manager) HealthSummary() HealthSummary {
	if m == nil {
		return HealthSummary{}
	}
	connected := m.conn != nil && !m.conn.IsClosed()
	return HealthSummary{
		Endpoint:         m.activeEndpoint,
		EndpointCount:    len(m.cfg.Endpoints),
		Connected:        connected,
		PublisherConfirm: m.cfg.PublisherConfirm,
		ConsumerPrefetch: m.cfg.ConsumerPrefetch,
	}
}
