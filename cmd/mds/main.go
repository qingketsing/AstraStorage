package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := newApplication(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap mds application: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	fmt.Fprintf(os.Stdout, "mds bootstrap complete: repo=%T service=%T handler=%T router=%T http_addr=%s grpc_addr=%s\n", app.repo, app.service, app.handler, app.router, app.httpAddr, app.grpcAddr)
	if err := app.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "run mds application: %v\n", err)
		os.Exit(1)
	}
}
