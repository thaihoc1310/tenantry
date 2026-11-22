package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

const (
	IdempotencyHeader = "Idempotency-Key"
	// ReplayedHeader marks a stored response sent again.
	ReplayedHeader = "Idempotent-Replayed"
)

var errKeyReused = apperr.New(apperr.IdempotencyKeyReused,
	"this Idempotency-Key came before with another request; use a new key for a new change")

// idempotent makes a POST carrying an Idempotency-Key happen at most once
// per user and key (IMPLEMENTATION-PLAN §3.4). The OpenAPI validator has
// checked the key's format by the time this runs.
//
// The key is claimed before the handler runs, in a transaction the handler's
// writes join (store.InTx), and the response is stored in it before it
// commits: the change and its stored response commit together or not at all.
// Claiming first matters for concurrent repeats: the second waits on the key
// and then replays the first's response. Claiming last, it would run into
// the first's change instead (the slug it just took) and answer 409.
func idempotent(db *pgxpool.Pool) oapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(IdempotencyHeader)
			if r.Method != http.MethodPost || key == "" {
				next.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, r, err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			// The raw path goes into the hash, never into a column: it may
			// carry a secret (POST /invitations/{token}:accept). The column
			// keeps the route pattern, for people reading the table.
			h := sha256.New()
			_, _ = io.WriteString(h, r.Method+" "+r.URL.Path+"\n")
			_, _ = h.Write(body)
			claim := store.ClaimIdempotencyKeyParams{
				UserID: userID(r.Context()), Key: key, Method: r.Method, Path: routePattern(r), RequestHash: h.Sum(nil),
			}
			if err := runOnce(w, r, db, claim, next); err != nil {
				writeError(w, r, err)
			}
		})
	}
}

func runOnce(w http.ResponseWriter, r *http.Request, db *pgxpool.Pool, claim store.ClaimIdempotencyKeyParams, next http.Handler) error {
	ctx := r.Context()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // no-op once committed
	q := store.New(tx)

	claimed, err := q.ClaimIdempotencyKey(ctx, claim)
	if err != nil {
		return err
	}
	if claimed == 0 {
		prev, err := q.GetIdempotencyKey(ctx, store.GetIdempotencyKeyParams{UserID: claim.UserID, Key: claim.Key})
		if err != nil {
			return err
		}
		if prev.Method != claim.Method || prev.Path != claim.Path || !bytes.Equal(prev.RequestHash, claim.RequestHash) {
			return errKeyReused
		}
		return replay(w, prev)
	}

	resp := &bufferedResponse{header: http.Header{}}
	stored := &storedBody{}
	next.ServeHTTP(resp, r.WithContext(context.WithValue(store.WithTx(ctx, tx), storedBodyKey{}, stored)))
	if resp.status == 0 {
		resp.status = http.StatusOK // the handler wrote nothing
	}
	if resp.status < 200 || resp.status > 299 {
		// Only successes are kept: a failed request can be corrected and sent
		// again under the same key.
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		resp.flush(w)
		return nil
	}
	done := store.CompleteIdempotencyKeyParams{UserID: claim.UserID, Key: claim.Key, ResponseStatus: int32(resp.status)}
	if loc := resp.header.Get("Location"); loc != "" {
		done.ResponseLocation = &loc
	}
	switch {
	case stored.body != nil:
		done.ResponseBody = stored.body // the handler left a secret out
	case resp.body.Len() > 0:
		done.ResponseBody = resp.body.Bytes()
	}
	if err := q.CompleteIdempotencyKey(ctx, done); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	resp.flush(w)
	return nil
}

type storedBodyKey struct{}

// storedBody is what a handler asks to store instead of the response it sends.
type storedBody struct{ body []byte }

// storeForReplay makes a repeat of this request get v instead of the response
// sent now. For a response carrying a one-time secret (an invitation link):
// the secret is sent once and never stored (02-data-model §2). A no-op
// without an Idempotency-Key.
func storeForReplay(ctx context.Context, v any) error {
	s, ok := ctx.Value(storedBodyKey{}).(*storedBody)
	if !ok {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.body = b
	return nil
}

// replay sends a stored response again: status, Location and body.
func replay(w http.ResponseWriter, k store.IdempotencyKey) error {
	if k.ResponseStatus == 0 {
		return errors.New("idempotency key committed without a response")
	}
	if k.ResponseLocation != nil {
		w.Header().Set("Location", *k.ResponseLocation)
	}
	if k.ResponseBody != nil {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set(ReplayedHeader, "true")
	w.WriteHeader(int(k.ResponseStatus))
	_, _ = w.Write(k.ResponseBody)
	return nil
}

// bufferedResponse holds a response until the transaction behind it has
// committed: a client must not see a success that is then rolled back.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	return b.body.Write(p)
}

func (b *bufferedResponse) flush(w http.ResponseWriter) {
	maps.Copy(w.Header(), b.header)
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}
