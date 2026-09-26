// Package migrations holds the SQL schema, embedded so every binary built
// from backend/ carries it (the Helm migrate job runs the same image).
package migrations

import (
	"context"
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var files embed.FS

// Run executes a goose command ("up", "down", "status", ...) against db.
func Run(ctx context.Context, db *sql.DB, command string, args ...string) error {
	goose.SetBaseFS(files)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(ctx, command, db, ".", args...)
}
