package client

type Publisher struct {
	confirmEnabled bool
}

func NewPublisher(confirmEnabled bool) *Publisher {
	return &Publisher{confirmEnabled: confirmEnabled}
}

func (p *Publisher) ConfirmEnabled() bool {
	if p == nil {
		return false
	}
	return p.confirmEnabled
}
