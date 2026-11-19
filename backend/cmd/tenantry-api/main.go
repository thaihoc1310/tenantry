// Command tenantry-api serves the Platform API and the portal BFF.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
)

func main() {
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
		slog.Error("database unreachable", "err", err)
		os.Exit(1)
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

	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown failed", "err", err)
		os.Exit(1)
	}
}

func routes(db *pgxpool.Pool, opts httpapi.Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	httpapi.Register(mux, db, opts)
	return httpapi.WithRequestID(mux)
}
