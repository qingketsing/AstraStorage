package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"AstraStorage/internal/gateway"
	"AstraStorage/internal/platform/observability/metrics"
)

type application struct {
	client          *gateway.UpstreamClient
	httpServer      *http.Server
	httpAddr        string
	mdsBaseURL      string
	dataNodeBaseURL string
	shutdownTimeout time.Duration
}

func newApplication() (*application, error) {
	cfg, err := gateway.LoadFromEnv()
	if err != nil {
		return nil, err
	}
	return newApplicationWithConfig(cfg)
}

func newApplicationWithConfig(cfg gateway.Config) (*application, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := gateway.NewUpstreamClient(cfg)
	if err != nil {
		return nil, err
	}
	registry := metrics.NewRegistry("gateway")
	handler, err := gateway.NewHTTPHandler(client, registry)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return &application{
		client:          client,
		httpServer:      server,
		httpAddr:        cfg.HTTPAddr,
		mdsBaseURL:      cfg.MDSHTTPBaseURL,
		dataNodeBaseURL: cfg.DataNodeBaseURL,
		shutdownTimeout: cfg.ShutdownTimeout,
	}, nil
}

func (app *application) Run(ctx context.Context) error {
	if app == nil || app.httpServer == nil {
		return errors.New("gateway bootstrap: http server is nil")
	}

	errCh := make(chan error, 1)
	go func() {
		err := app.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownTimeout := app.shutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("gateway bootstrap: shutdown http server: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("gateway bootstrap: serve http server: %w", err)
		}
		return nil
	}
}
