// dul-auth is a personal OpenID Connect identity provider. See PRD.md.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 && os.Args[1] == "create-user" {
		if err := runCreateUser(ctx, os.Args[2:], os.Getenv); err != nil {
			slog.Error("create-user failed", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := run(ctx, os.Getenv); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// ctx is already cancelled here, so shutdown needs its own deadline.
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	// ErrServerClosed is the normal result after Shutdown.
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHandler registers every route. It returns a plain http.Handler so tests
// can call it with httptest.
func newHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return mux
}

func runCreateUser(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	email := fs.String("email", "", "user email")

	fs.Parse(args)
	if *email == "" {
		return errors.New("--email is required")
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	password := scanner.Text()

	// database activity
	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, databaseURL)

	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	err = createUser(ctx, pool, *email, hash)
	if err != nil {
		return err
	}

	return nil
}
