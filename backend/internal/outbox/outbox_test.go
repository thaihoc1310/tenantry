package outbox_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestBackoff(t *testing.T) {
	cases := map[int32]time.Duration{
		0: 0, 1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 9: 256 * time.Second,
		10: 5 * time.Minute, 40: 5 * time.Minute, 1000: 5 * time.Minute,
	}
	for failures, want := range cases {
		if got := outbox.Backoff(failures); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", failures, got, want)
		}
	}
}

func newDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	if _, err := sqlDB.Exec(`
		INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'iss', 'a');
		INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('org-a', 'acme', 'ACME', 'usr-a', 'Active');
		INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain)
		VALUES ('cluster-a', 'Cluster A', 'Ready', 'https://kube-a.test', 'https://console-a.test', 'apps.test');`); err != nil {
		t.Fatal(err)
	}
	pool := testdb.Pool(t, url)
	return pool
}

// enqueue starts an operation on resource and queues one event per type.
func enqueue(t *testing.T, db *pgxpool.Pool, resource string, types ...string) store.Operation {
	t.Helper()
	ctx := context.Background()
	q := store.New(db)
	op, err := operation.Create(ctx, q, operation.New{
		OrgID: "org-a", ResourceType: "Workspace", ResourceID: resource, Type: "workspace.create", CreatedBy: "usr-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, typ := range types {
		if _, err := outbox.Enqueue(ctx, q, op, outbox.Event{
			TargetClusterID: "cluster-a", Type: typ, Payload: map[string]any{"step": i}, Version: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return op
}

// recorder is a handler that remembers what it saw and fails event types listed in fail.
type recorder struct {
	mu   sync.Mutex
	seen []string
	fail map[string]bool
}

func (r *recorder) Handle(_ context.Context, e store.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, e.Type)
	if r.fail[e.Type] {
		return errors.New("cluster unreachable")
	}
	return nil
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.seen
	r.seen = nil
	return s
}

func runOnce(t *testing.T, w *outbox.Worker) int {
	t.Helper()
	n, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func getOp(t *testing.T, db *pgxpool.Pool, id string) store.Operation {
	t.Helper()
	var op store.Operation
	err := db.QueryRow(context.Background(), `SELECT state, error_code, error_message, finished_at FROM operations WHERE id = $1`, id).
		Scan(&op.State, &op.ErrorCode, &op.ErrorMessage, &op.FinishedAt)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

// makeDue skips the backoff of every waiting event.
func makeDue(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE outbox_events SET next_attempt_at = now() WHERE processed_at IS NULL`); err != nil {
		t.Fatal(err)
	}
}

func TestDeliversInOrderAndMarksOperationRunning(t *testing.T) {
	db := newDB(t)
	op := enqueue(t, db, "ws-1", "first", "second", "third")
	h := &recorder{}
	w := outbox.NewWorker(db, "cluster-a", h, quiet)

	if n := runOnce(t, w); n != 1 {
		t.Fatalf("handled %d, want 1: events of one resource go one at a time", n)
	}
	runOnce(t, w)
	runOnce(t, w)
	if got := h.take(); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Fatalf("delivered %v", got)
	}
	if n := runOnce(t, w); n != 0 {
		t.Fatalf("delivered %d events twice", n)
	}
	if got := getOp(t, db, op.ID); got.State != "Running" {
		t.Fatalf("operation %s, want Running", got.State)
	}
}

func TestFailuresBackOffThenFailTheOperation(t *testing.T) {
	db := newDB(t)
	op := enqueue(t, db, "ws-1", "apply")
	h := &recorder{fail: map[string]bool{"apply": true}}
	w := outbox.NewWorker(db, "cluster-a", h, quiet)

	runOnce(t, w)
	var attempts int32
	var waitMS int64
	var lastErr string
	if err := db.QueryRow(context.Background(),
		`SELECT attempts, (EXTRACT(EPOCH FROM next_attempt_at - now()) * 1000)::bigint, last_error FROM outbox_events`).
		Scan(&attempts, &waitMS, &lastErr); err != nil {
		t.Fatal(err)
	}
	wait := time.Duration(waitMS) * time.Millisecond
	if attempts != 1 || lastErr != "cluster unreachable" || wait < 500*time.Millisecond || wait > 1500*time.Millisecond {
		t.Fatalf("after one failure: attempts=%d wait=%v last_error=%q", attempts, wait, lastErr)
	}
	if n := runOnce(t, w); n != 0 {
		t.Fatal("event retried before its backoff elapsed")
	}

	for i := 2; i < outbox.FailAfter; i++ {
		makeDue(t, db)
		runOnce(t, w)
	}
	if got := getOp(t, db, op.ID); got.State != "Pending" {
		t.Fatalf("after %d failures operation is %s, want still Pending", outbox.FailAfter-1, got.State)
	}
	makeDue(t, db)
	runOnce(t, w)
	got := getOp(t, db, op.ID)
	if got.State != "Failed" || got.ErrorCode != operation.ApplyFailed || got.ErrorMessage != "cluster unreachable" || got.FinishedAt == nil {
		t.Fatalf("after %d failures: %+v", outbox.FailAfter, got)
	}

	// Delivery keeps trying; a late success revives the operation.
	h.fail = nil
	makeDue(t, db)
	runOnce(t, w)
	got = getOp(t, db, op.ID)
	if got.State != "Running" || got.ErrorCode != "" || got.FinishedAt != nil {
		t.Fatalf("after a late success: %+v", got)
	}
}

func TestFailingEventHoldsBackOnlyItsOwnResource(t *testing.T) {
	db := newDB(t)
	enqueue(t, db, "ws-1", "ws1-create", "ws1-delete")
	enqueue(t, db, "ws-2", "ws2-create")
	h := &recorder{fail: map[string]bool{"ws1-create": true}}
	w := outbox.NewWorker(db, "cluster-a", h, quiet)

	runOnce(t, w)
	if got := h.take(); !slices.Equal(got, []string{"ws1-create", "ws2-create"}) {
		t.Fatalf("first batch %v", got)
	}
	makeDue(t, db)
	runOnce(t, w)
	if got := h.take(); !slices.Equal(got, []string{"ws1-create"}) {
		t.Fatalf("ws1-delete overtook the failing ws1-create: %v", got)
	}

	h.fail = nil
	makeDue(t, db)
	runOnce(t, w)
	runOnce(t, w)
	if got := h.take(); !slices.Equal(got, []string{"ws1-create", "ws1-delete"}) {
		t.Fatalf("after recovery %v", got)
	}
}

func TestHandlerPanicCountsAsFailure(t *testing.T) {
	db := newDB(t)
	enqueue(t, db, "ws-1", "apply")
	w := outbox.NewWorker(db, "cluster-a", outbox.HandlerFunc(func(context.Context, store.OutboxEvent) error {
		panic("nil map")
	}), quiet)

	runOnce(t, w)
	var attempts int32
	var lastErr string
	if err := db.QueryRow(context.Background(), `SELECT attempts, last_error FROM outbox_events`).Scan(&attempts, &lastErr); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || !strings.Contains(lastErr, "panicked: nil map") {
		t.Fatalf("attempts=%d last_error=%q", attempts, lastErr)
	}
}

// Several workers may run at once (SKIP LOCKED); each event is still handled once.
func TestConcurrentWorkersDeliverEachEventOnce(t *testing.T) {
	db := newDB(t)
	const events = 60
	for i := range events {
		enqueue(t, db, fmt.Sprint("ws-", i), fmt.Sprint("ev-", i))
	}

	var mu sync.Mutex
	count := map[string]int{}
	h := outbox.HandlerFunc(func(_ context.Context, e store.OutboxEvent) error {
		time.Sleep(2 * time.Millisecond) // keep batches overlapping
		mu.Lock()
		count[e.Type]++
		mu.Unlock()
		return nil
	})

	var wg sync.WaitGroup
	for range 4 {
		w := outbox.NewWorker(db, "cluster-a", h, quiet)
		wg.Go(func() {
			for {
				n, err := w.RunOnce(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				if n == 0 {
					return
				}
			}
		})
	}
	wg.Wait()

	if len(count) != events {
		t.Fatalf("delivered %d distinct events, want %d", len(count), events)
	}
	for typ, n := range count {
		if n != 1 {
			t.Fatalf("%s delivered %d times", typ, n)
		}
	}
}

// Run stops when its context is cancelled and leaves no event half-done.
func TestRunStopsOnCancel(t *testing.T) {
	db := newDB(t)
	op := enqueue(t, db, "ws-1", "apply")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		outbox.NewWorker(db, "cluster-a", &recorder{}, quiet).Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for getOp(t, db, op.ID).State != "Running" {
		if time.Now().After(deadline) {
			t.Fatal("event not delivered by Run")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// A worker delivers its own cluster's events only, and none while that
// cluster is Unavailable: they wait without spending attempts.
func TestClusterScopeAndUnavailable(t *testing.T) {
	db := newDB(t)
	op := enqueue(t, db, "ws-1", "apply")
	h := &recorder{}
	if n := runOnce(t, outbox.NewWorker(db, "cluster-b", h, quiet)); n != 0 {
		t.Fatalf("another cluster's worker took %d events", n)
	}
	if _, err := db.Exec(context.Background(), `UPDATE clusters SET status = 'Unavailable' WHERE id = 'cluster-a'`); err != nil {
		t.Fatal(err)
	}
	w := outbox.NewWorker(db, "cluster-a", h, quiet)
	if n := runOnce(t, w); n != 0 {
		t.Fatalf("delivered %d events to an Unavailable cluster", n)
	}
	var attempts int
	if err := db.QueryRow(context.Background(), `SELECT attempts FROM outbox_events`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("attempts %d (%v), want 0", attempts, err)
	}
	if got := getOp(t, db, op.ID); got.State != "Pending" {
		t.Fatalf("operation %s, want Pending", got.State)
	}
	if _, err := db.Exec(context.Background(), `UPDATE clusters SET status = 'Ready' WHERE id = 'cluster-a'`); err != nil {
		t.Fatal(err)
	}
	if n := runOnce(t, w); n != 1 {
		t.Fatal("not delivered once the cluster is Ready again")
	}
}

// A failure retrying cannot fix fails the operation at once.
func TestPermanentFailure(t *testing.T) {
	db := newDB(t)
	op := enqueue(t, db, "ws-1", "apply")
	w := outbox.NewWorker(db, "cluster-a", outbox.HandlerFunc(func(context.Context, store.OutboxEvent) error {
		return outbox.Permanent(errors.New("forbidden: the credential may not create platformworkspaces"))
	}), quiet)
	runOnce(t, w)
	if got := getOp(t, db, op.ID); got.State != "Failed" || got.ErrorCode != operation.ApplyFailed ||
		!strings.Contains(got.ErrorMessage, "forbidden") {
		t.Fatalf("after a permanent failure: %+v", got)
	}
}
