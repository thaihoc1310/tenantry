package housekeeping_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/housekeeping"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

func TestExpiredSessionsGo(t *testing.T) {
	url := testdb.New(t)
	ctx := context.Background()
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	p, err := migrations.NewProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	// Sessions named by the first byte of their hash: 1 expired, 2 alive.
	if _, err := sqlDB.Exec(`
		INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'iss', 'a');
		INSERT INTO sessions (id_hash, user_id, refresh_token_enc, access_expires_at, expires_at) VALUES
		  (decode(repeat('01', 32), 'hex'), 'usr-a', '\x00', now() - interval '9 hours', now() - interval '1 second'),
		  (decode(repeat('02', 32), 'hex'), 'usr-a', '\x00', now() - interval '1 hour', now() + interval '1 hour');`); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := housekeeping.RunOnce(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	var left string
	if err := pool.QueryRow(ctx, `SELECT string_agg(get_byte(id_hash, 0)::text, ',') FROM sessions`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != "2" {
		t.Fatalf("sessions left: %s, want only the live one (2)", left)
	}
}
