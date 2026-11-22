package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// WithTx makes tx the transaction of the request behind ctx: InTx then runs
// inside it. The HTTP layer does this for a request with an Idempotency-Key,
// so the change and the stored response commit together.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// InTx runs fn in a transaction: a savepoint of the request's transaction
// when ctx carries one, a new transaction on db otherwise. Services write
// through InTx only, never straight to the pool, or a write would commit
// apart from the request's transaction.
func InTx(ctx context.Context, db *pgxpool.Pool, fn func(pgx.Tx) error) error {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return pgx.BeginFunc(ctx, tx, fn) // Begin on a Tx is a savepoint
	}
	return pgx.BeginFunc(ctx, db, fn)
}
