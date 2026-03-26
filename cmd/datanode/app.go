package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"AstraStorage/internal/datanode"
	"AstraStorage/internal/platform/observability/metrics"
)

type application struct {
	store           *datanode.Store
	mdsClient       *datanode.MDSClient
	httpServer      *http.Server
	httpAddr        string
	dataDir         string
	nodeID          string
	advertiseURL    string
	capacityBytes   int64
	heartbeatTicker time.Duration
	shutdownTimeout time.Duration
}

func newApplication() (*application, error) {
	cfg, err := datanode.LoadFromEnv()
	if err != nil {
		return nil, err
	}
	return newApplicationWithConfig(cfg)
}

func newApplicationWithConfig(cfg datanode.Config) (*application, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	store, err := datanode.NewStore(cfg)
	if err != nil {
		return nil, err
	}
	var mdsClient *datanode.MDSClient
	registry := metrics.NewRegistry("datanode")
	if cfg.MDSHTTPBaseURL != "" {
		mdsClient, err = datanode.NewMDSClient(cfg.MDSHTTPBaseURL)
		if err != nil {
			return nil, err
		}
		if err := mdsClient.AttachObservability(registry); err != nil {
			return nil, err
		}
	}
	handler, err := datanode.NewHTTPHandler(store, registry)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return &application{
		store:           store,
		mdsClient:       mdsClient,
		httpServer:      server,
		httpAddr:        cfg.HTTPAddr,
		dataDir:         cfg.DataDir,
		nodeID:          cfg.NodeID,
		advertiseURL:    cfg.AdvertiseURL,
		capacityBytes:   cfg.CapacityBytes,
		heartbeatTicker: cfg.HeartbeatInterval,
		shutdownTimeout: cfg.ShutdownTimeout,
	}, nil
}

func (app *application) Run(ctx context.Context) error {
	if app == nil || app.httpServer == nil {
		return errors.New("datanode bootstrap: http server is nil")
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
	if app.mdsClient != nil {
		now := time.Now().UTC()
		if err := app.registerNode(ctx, now); err != nil {
			_ = app.httpServer.Close()
			return fmt.Errorf("datanode bootstrap: register node: %w", err)
		}
		if app.heartbeatTicker > 0 {
			go app.runHeartbeatLoop(ctx)
		}
	}

	select {
	case <-ctx.Done():
		shutdownTimeout := app.shutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("datanode bootstrap: shutdown http server: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("datanode bootstrap: serve http server: %w", err)
		}
		return nil
	}
}

func (app *application) runHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(app.heartbeatTicker)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = app.sendHeartbeat(ctx, time.Now().UTC())
		}
	}
}

func (app *application) registerNode(ctx context.Context, now time.Time) error {
	if app == nil || app.mdsClient == nil {
		return nil
	}
	used, err := app.currentUsedBytes()
	if err != nil {
		return err
	}
	return app.mdsClient.RegisterNode(ctx, datanode.NodeRegistration{
		NodeID:     app.nodeID,
		Address:    app.advertiseURL,
		Capacity:   app.capacityBytes,
		Used:       used,
		Healthy:    true,
		LastSeenAt: &now,
		UpdatedAt:  now,
	})
}

func (app *application) sendHeartbeat(ctx context.Context, now time.Time) error {
	if app == nil || app.mdsClient == nil {
		return nil
	}
	used, err := app.currentUsedBytes()
	if err != nil {
		return err
	}
	return app.mdsClient.HeartbeatNode(ctx, datanode.NodeHeartbeat{
		NodeID:     app.nodeID,
		Healthy:    true,
		Capacity:   app.capacityBytes,
		Used:       used,
		LastSeenAt: now,
	})
}

func (app *application) currentUsedBytes() (int64, error) {
	if app == nil || app.store == nil {
		return 0, errors.New("datanode bootstrap: store is nil")
	}
	used, err := app.store.UsageBytes()
	if err != nil {
		return 0, fmt.Errorf("datanode bootstrap: read usage bytes: %w", err)
	}
	return used, nil
}
