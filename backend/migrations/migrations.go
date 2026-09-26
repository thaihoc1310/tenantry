// Package migrations holds the SQL schema, embedded so every binary built
// from backend/ carries it (the Helm migrate job runs the same image).
package migrations

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var files embed.FS

// NewProvider returns a goose provider over the embedded migrations.
//
// A Postgres session-level advisory lock serialises concurrent runs, e.g. two
// migrate jobs racing during overlapping helm upgrades. Postgres releases the
// lock itself if the connection drops, so a killed pod cannot leave it held.
func NewProvider(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
}
