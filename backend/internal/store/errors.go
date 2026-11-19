package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// UniqueViolation reports which unique constraint or index err violated, so a
// service can turn a lost race into the right domain error. ok is false for
// any other error, including nil.
func UniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return pgErr.ConstraintName, true
	}
	return "", false
}
