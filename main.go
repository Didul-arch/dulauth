// dul-auth is a personal OpenID Connect identity provider. See PRD.md.
//
// This file is milestone M1: an empty server that only answers /healthz.
// Everything is in package main on purpose; split into packages when a file
// gets hard to navigate, not before.
// look at: https://go.dev/doc/modules/layout
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// STEP 1: main does as little as possible.
//
// It wires real-world things (OS signals, env vars) into run() and turns an
// error into an exit code. All logic lives in run() so tests can call it
// without touching the real environment.
// look at: https://grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/
func main() {
	// ctx is cancelled on Ctrl+C or `docker stop` (SIGTERM). That
	// cancellation is what triggers the graceful shutdown in STEP 5.
	// look at: https://pkg.go.dev/os/signal#NotifyContext
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// run starts the server and blocks until ctx is cancelled or the server fails.
// getenv is passed in (instead of calling os.Getenv directly) so a test can
// supply its own config.
func run(ctx context.Context, getenv func(string) string) error {
	// STEP 2: structured JSON logs, as required by PRD §11.
	// Never log passwords or tokens through this logger.
	// look at: https://go.dev/blog/slog
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	// STEP 3: read config from the environment (PRD §10).
	// Only PORT exists so far. ISSUER, DATABASE_URL, SIGNING_KEY_PATH and
	// CLIENTS_FILE get added in the milestone that first needs them.
	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// STEP 4: build the server with explicit timeouts.
	// The zero value of http.Server has NO timeouts, so one slow or
	// malicious client can hold a connection open forever (Slowloris).
	// look at: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// STEP 5: serve in a goroutine, then wait for whichever comes first:
	// the server failing (e.g. port already in use) or ctx being cancelled.
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting new connections and give in-flight
	// requests up to 10s to finish. A fresh context is needed here because
	// ctx is already cancelled.
	// look at: https://pkg.go.dev/net/http#Server.Shutdown
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	// ListenAndServe always returns ErrServerClosed after Shutdown; that is
	// the normal exit, not a failure.
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// STEP 6: all routes are registered in one place, so the whole API surface
// of the service is readable in a single function (PRD §7.1 is the target).
//
// It returns http.Handler rather than starting a server, which lets tests
// drive it directly with httptest. See main_test.go.
func newHandler() http.Handler {
	mux := http.NewServeMux()

	// The "GET " prefix is Go 1.22+ method routing. Any other method on
	// this path gets 405 Method Not Allowed automatically, which is the M1
	// acceptance criterion. No router library needed.
	// look at: https://go.dev/blog/routing-enhancements
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// NEXT (M3): mount the zitadel/oidc provider here. It supplies
	// /.well-known/openid-configuration, /authorize, /oauth/token, /keys,
	// /userinfo and /end_session.
	// look at: https://github.com/zitadel/oidc/tree/main/example/server

	return mux
}
