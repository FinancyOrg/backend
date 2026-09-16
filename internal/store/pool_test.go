package store

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/crdballowlist"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func proxyErr() error {
	return &pgconn.PgError{Code: "08C00", Message: "codeProxyRefusedConnection: connection refused"}
}

type fakeRec struct {
	calls   atomic.Int32
	err     error
	enabled bool
	block   chan struct{}
	started chan struct{}
}

func (f *fakeRec) Recover(context.Context) error {
	f.calls.Add(1)
	if f.started != nil {
		select {
		case <-f.started:
		default:
			close(f.started)
		}
	}
	if f.block != nil {
		<-f.block
	}
	return f.err
}

func (f *fakeRec) Enabled() bool { return f.enabled }

type fakeQ struct {
	mu       sync.Mutex
	queryErr []error
	rowErr   []error
	execErr  []error
	beginErr []error
	resets   int
	queries  int
	scans    int
	execs    int
	begins   int
}

func (f *fakeQ) next(errs *[]error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
}

func (f *fakeQ) Query(context.Context, string, ...any) (pgx.Rows, error) {
	f.mu.Lock()
	f.queries++
	f.mu.Unlock()
	return nil, f.next(&f.queryErr)
}

type scanRow struct{ f *fakeQ }

func (r scanRow) Scan(...any) error {
	r.f.mu.Lock()
	r.f.scans++
	r.f.mu.Unlock()
	return r.f.next(&r.f.rowErr)
}

func (f *fakeQ) QueryRow(context.Context, string, ...any) pgx.Row {
	return scanRow{f: f}
}

func (f *fakeQ) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	f.execs++
	f.mu.Unlock()
	return pgconn.CommandTag{}, f.next(&f.execErr)
}

func (f *fakeQ) Begin(context.Context) (pgx.Tx, error) {
	f.mu.Lock()
	f.begins++
	f.mu.Unlock()
	return nil, f.next(&f.beginErr)
}

func (f *fakeQ) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
}

func (f *fakeQ) Close() {}

func TestQueryRowScanRecovers(t *testing.T) {
	q := &fakeQ{rowErr: []error{proxyErr(), nil}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	if err := p.QueryRow(context.Background(), "SELECT 1").Scan(); err != nil {
		t.Fatal(err)
	}
	if rec.calls.Load() != 1 {
		t.Fatalf("recover calls=%d", rec.calls.Load())
	}
	if q.resets != 1 {
		t.Fatalf("resets=%d", q.resets)
	}
	if q.scans != 2 {
		t.Fatalf("scans=%d", q.scans)
	}
}

func TestQueryRecovers(t *testing.T) {
	q := &fakeQ{queryErr: []error{proxyErr(), nil}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	if _, err := p.Query(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if rec.calls.Load() != 1 || q.queries != 2 || q.resets != 1 {
		t.Fatalf("calls=%d queries=%d resets=%d", rec.calls.Load(), q.queries, q.resets)
	}
}

func TestExecRecovers(t *testing.T) {
	q := &fakeQ{execErr: []error{proxyErr(), nil}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	if _, err := p.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if rec.calls.Load() != 1 || q.execs != 2 {
		t.Fatalf("calls=%d execs=%d", rec.calls.Load(), q.execs)
	}
}

func TestBeginRecovers(t *testing.T) {
	q := &fakeQ{beginErr: []error{proxyErr(), nil}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	if _, err := p.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec.calls.Load() != 1 || q.begins != 2 {
		t.Fatalf("calls=%d begins=%d", rec.calls.Load(), q.begins)
	}
}

func TestUniqueViolationDoesNotRecover(t *testing.T) {
	orig := &pgconn.PgError{Code: "23505", Message: "duplicate"}
	q := &fakeQ{rowErr: []error{orig}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	err := p.QueryRow(context.Background(), "SELECT 1").Scan()
	if !errors.As(err, new(*pgconn.PgError)) || rec.calls.Load() != 0 {
		t.Fatalf("err=%v calls=%d", err, rec.calls.Load())
	}
}

func TestNonProxyErrorDoesNotRecover(t *testing.T) {
	orig := errors.New("unique violation")
	q := &fakeQ{queryErr: []error{orig}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	_, err := p.Query(context.Background(), "SELECT 1")
	if !errors.Is(err, orig) {
		t.Fatalf("got %v", err)
	}
	if rec.calls.Load() != 0 {
		t.Fatalf("calls=%d", rec.calls.Load())
	}
}

func TestNilRecovererReturns08C00(t *testing.T) {
	q := &fakeQ{rowErr: []error{proxyErr()}}
	p := Wrap(q, nil)
	err := p.QueryRow(context.Background(), "SELECT 1").Scan()
	if !crdballowlist.IsProxyRefused(err) {
		t.Fatalf("got %v", err)
	}
}

func TestDisabledRecovererReturns08C00(t *testing.T) {
	q := &fakeQ{rowErr: []error{proxyErr()}}
	rec := &fakeRec{enabled: false}
	p := Wrap(q, rec)
	err := p.QueryRow(context.Background(), "SELECT 1").Scan()
	if !crdballowlist.IsProxyRefused(err) {
		t.Fatalf("got %v", err)
	}
	if rec.calls.Load() != 0 {
		t.Fatal("disabled recoverer must not be called")
	}
}

func TestRecoverFailureKeeps08C00(t *testing.T) {
	q := &fakeQ{rowErr: []error{proxyErr()}}
	rec := &fakeRec{enabled: true, err: errors.New("cloud api down")}
	p := Wrap(q, rec)
	err := p.QueryRow(context.Background(), "SELECT 1").Scan()
	if !crdballowlist.IsProxyRefused(err) {
		t.Fatalf("must not hide 08C00: %v", err)
	}
	if q.scans != 1 {
		t.Fatalf("must not retry query after recover fail, scans=%d", q.scans)
	}
}

func TestNoInfiniteRetry(t *testing.T) {
	q := &fakeQ{rowErr: []error{proxyErr(), proxyErr(), proxyErr()}}
	rec := &fakeRec{enabled: true}
	p := Wrap(q, rec)
	err := p.QueryRow(context.Background(), "SELECT 1").Scan()
	if !crdballowlist.IsProxyRefused(err) {
		t.Fatalf("got %v", err)
	}
	if rec.calls.Load() != 1 {
		t.Fatalf("recover once, calls=%d", rec.calls.Load())
	}
	if q.scans != 2 {
		t.Fatalf("one retry, scans=%d", q.scans)
	}
}

func TestConcurrentScanSharesRecover(t *testing.T) {
	block := make(chan struct{})
	inner := &fakeRec{enabled: true, block: block}
	rec := &sharedRec{inner: inner}
	q := &gateQ{}
	p := Wrap(q, rec)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			errCh <- p.QueryRow(context.Background(), "SELECT value FROM app_config").Scan()
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for rec.waiters.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("waiters=%d want %d", rec.waiters.Load(), n)
		}
		runtime.Gosched()
	}
	close(block)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("singleflight recover, calls=%d", inner.calls.Load())
	}
	q.mu.Lock()
	resets := q.resets
	q.mu.Unlock()
	if resets < 1 {
		t.Fatal("expected pool reset")
	}
}

// sharedRec is the mutex + in-flight flag Recoverer uses so concurrent
// wrapper calls share one Ensure.
type sharedRec struct {
	inner   *fakeRec
	waiters atomic.Int32
	mu      sync.Mutex
	in      *sharedWait
}

type sharedWait struct {
	done chan struct{}
	err  error
}

func (s *sharedRec) Enabled() bool { return s.inner.Enabled() }

func (s *sharedRec) Recover(ctx context.Context) error {
	s.waiters.Add(1)
	s.mu.Lock()
	if s.in != nil {
		w := s.in
		s.mu.Unlock()
		<-w.done
		return w.err
	}
	w := &sharedWait{done: make(chan struct{})}
	s.in = w
	s.mu.Unlock()
	w.err = s.inner.Recover(ctx)
	s.mu.Lock()
	s.in = nil
	s.mu.Unlock()
	close(w.done)
	return w.err
}

// gateQ fails Scan with 08C00 until Reset, then succeeds. Models idle conns
// opened under the proxy deny.
type gateQ struct {
	mu     sync.Mutex
	resets int
	scans  int
}

func (g *gateQ) denied() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.resets == 0
}

func (g *gateQ) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if g.denied() {
		return nil, proxyErr()
	}
	return nil, nil
}

type gateRow struct{ g *gateQ }

func (r gateRow) Scan(...any) error {
	r.g.mu.Lock()
	r.g.scans++
	denied := r.g.resets == 0
	r.g.mu.Unlock()
	if denied {
		return proxyErr()
	}
	return nil
}

func (g *gateQ) QueryRow(context.Context, string, ...any) pgx.Row {
	return gateRow{g: g}
}

func (g *gateQ) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if g.denied() {
		return pgconn.CommandTag{}, proxyErr()
	}
	return pgconn.CommandTag{}, nil
}

func (g *gateQ) Begin(context.Context) (pgx.Tx, error) {
	if g.denied() {
		return nil, proxyErr()
	}
	return nil, nil
}

func (g *gateQ) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resets++
}

func (g *gateQ) Close() {}
