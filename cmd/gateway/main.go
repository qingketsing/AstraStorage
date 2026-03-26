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
		fmt.Fprintf(os.Stderr, "bootstrap gateway application: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "gateway bootstrap complete: http_addr=%s mds=%s datanode=%s\n", app.httpAddr, app.mdsBaseURL, app.dataNodeBaseURL)
	if err := app.Run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "run gateway application: %v\n", err)
		os.Exit(1)
	}
}
