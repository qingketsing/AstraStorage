package topology

const (
	TasksExchange  = "astra.tasks"
	EventsExchange = "astra.events"
	RetryExchange  = "astra.retry"
	DLXExchange    = "astra.dlx"
)

type ExchangeDefinition struct {
	Name       string
	Kind       string
	Durable    bool
	AutoDelete bool
	Internal   bool
	NoWait     bool
}

func Exchanges() []ExchangeDefinition {
	return []ExchangeDefinition{
		{Name: TasksExchange, Kind: "direct", Durable: true},
		{Name: EventsExchange, Kind: "topic", Durable: true},
		{Name: RetryExchange, Kind: "direct", Durable: true},
		{Name: DLXExchange, Kind: "direct", Durable: true},
	}
}
