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

	app, err := newApplication()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap datanode application: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "datanode bootstrap complete: http_addr=%s data_dir=%s\n", app.httpAddr, app.dataDir)
	if err := app.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "run datanode application: %v\n", err)
		os.Exit(1)
	}
}
