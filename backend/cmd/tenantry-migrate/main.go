// Command tenantry-migrate applies the embedded SQL migrations.
//
//	tenantry-migrate [up|down|status|...]   (default: up)
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/thaihoc1310/tenantry/backend/migrations"
)

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	err = migrations.Run(context.Background(), db, command, os.Args[min(2, len(os.Args)):]...)
	_ = db.Close()
	if err != nil {
		slog.Error("migrate", "command", command, "err", err)
		os.Exit(1)
	}
}
