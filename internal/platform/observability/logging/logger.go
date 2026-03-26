package logging

import (
	"io"
	"log/slog"
)

func NewLogger(w io.Writer, service, component string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{})
	return slog.New(handler).With(
		"service", service,
		"component", component,
	)
}
