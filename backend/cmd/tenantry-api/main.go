// Command tenantry-api serves the Platform API and the portal BFF.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tenantry-api stopped", "err", err)
		os.Exit(1)
	}
}

// run returns only on shutdown; a non-nil error means the API could not serve
// (port taken, database unreachable) and must end with a failing exit code.
func run() error {
	addr := os.Getenv("TENANTRY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// pgxpool connects lazily; Ping makes a wrong DATABASE_URL fail at start
	// instead of on the first request.
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	defer pool.Close()

	opts := httpapi.Options{DevAuth: os.Getenv("TENANTRY_AUTH_DEV_HEADER") == "true"}
	if opts.DevAuth {
		slog.Warn("dev auth enabled: any client can act as any user, Platform Admin included, via " + httpapi.DevUserHeader + " and " + httpapi.DevRolesHeader)
	}

	srv := &http.Server{Addr: addr, Handler: routes(pool, opts), ReadHeaderTimeout: 10 * time.Second}

	// SIGTERM is how Kubernetes asks for a shutdown; without this the pod drops
	// in-flight requests on every rollout.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr: // never ErrServerClosed: only Shutdown below causes that
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// maxBodyBytes caps every request body. The API takes small JSON documents;
// without a cap one authenticated client could send gigabytes.
const maxBodyBytes = 1 << 20

func routes(db *pgxpool.Pool, opts httpapi.Options) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	httpapi.Register(r, db, opts)
	return http.MaxBytesHandler(httpapi.WithRequestID(r), maxBodyBytes)
}
