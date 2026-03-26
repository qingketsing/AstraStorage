package contracts

type Header struct {
	MessageID string
	EventID   string
	TraceID   string
	Attempt   int
}
