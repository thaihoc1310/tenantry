// Command tenantry-api serves the Platform API and the portal BFF.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/auth"
	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tenantry-api stopped", "err", err)
		os.Exit(1)
	}
}

// run returns only on shutdown; a non-nil error means the API could not serve
// (bad config, port taken, database unreachable) and must end with a failing exit code.
func run() error {
	cfg, err := config.Load()
	authCfg, authErr := config.LoadAuth()
	if err := errors.Join(err, authErr); err != nil {
		return err
	}
	log := cfg.Logger()
	slog.SetDefault(log)

	// pgxpool connects lazily; Ping makes a wrong DATABASE_URL fail at start
	// instead of on the first request.
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	defer pool.Close()

	authn, err := auth.New(pool, authCfg)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           routes(pool, authn, cfg.TrustedProxies, log),
		ReadHeaderTimeout: 10 * time.Second,
		// Bodies are at most maxBodyBytes, so 30s only cuts off a client that
		// trickles one in to hold a connection open.
		ReadTimeout: 30 * time.Second,
		// Handlers are short database calls. A streaming endpoint (SSE) must
		// lift this for its own response with http.ResponseController.
		WriteTimeout: time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	// SIGTERM is how Kubernetes asks for a shutdown; without this the pod drops
	// in-flight requests on every rollout.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr: // never ErrServerClosed: only Shutdown below causes that
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
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

func routes(db *pgxpool.Pool, a *auth.Service, trustedProxies []netip.Prefix, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(httpapi.WithRequestID, httpapi.LogRequests(log), httpapi.Recover)
	// Liveness: the process answers. It never touches the database, so an
	// outage of PostgreSQL does not make Kubernetes restart every API pod.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// Readiness: take the pod out of the Service while it cannot reach the database.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			log.WarnContext(ctx, "not ready", "err", err)
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	httpapi.Register(r, db, a, trustedProxies)
	return http.MaxBytesHandler(r, maxBodyBytes)
}
