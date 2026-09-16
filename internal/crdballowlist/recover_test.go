package crdballowlist

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func testRecoverer(t *testing.T, api *fakeAPI, extra Options) *Recoverer {
	t.Helper()
	return &Recoverer{
		api:     api,
		cfg:     Config{ClusterID: "c1", Name: "cloudrun"},
		opts:    ensureOpts(t, extra),
		timeout: 5 * time.Second,
	}
}

func TestNewRecovererDisabled(t *testing.T) {
	r, err := NewRecoverer(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if r.Enabled() {
		t.Fatal("expected disabled recoverer")
	}
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewRecovererRequiresDatabaseURL(t *testing.T) {
	_, err := NewRecoverer(func(k string) string {
		switch k {
		case "CRDB_API_KEY":
			return "k"
		case "CRDB_CLUSTER_ID":
			return "id"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("expected DATABASE_URL error")
	}
}

func TestRecoverPutsAllowlistAfterProxyRefuse(t *testing.T) {
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	pings := 0
	r := testRecoverer(t, api, Options{
		PingSQL: func(context.Context) error {
			pings++
			if pings == 1 {
				return &pgconn.PgError{Code: "08C00", Message: "codeProxyRefusedConnection: connection refused"}
			}
			return nil
		},
	})
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if api.putCount() != 1 {
		t.Fatalf("puts=%d", api.putCount())
	}
	if pings < 2 {
		t.Fatalf("pings=%d want at least 2 (08C00 then success)", pings)
	}
}

func TestRecoverConcurrentSingleflight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	r := testRecoverer(t, api, Options{
		DiscoverIP: func(context.Context) (string, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return "203.0.113.10", nil
		},
	})

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			errCh <- r.Recover(context.Background())
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if api.putCount() != 1 {
		t.Fatalf("Ensure must run once, puts=%d", api.putCount())
	}
}

func TestRecoverDisabledNoAPICalls(t *testing.T) {
	api := &fakeAPI{}
	r := &Recoverer{}
	if r.Enabled() {
		t.Fatal("expected disabled")
	}
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if api.putCount() != 0 {
		t.Fatalf("puts=%d", api.putCount())
	}
}

func TestRecoverDoesNotUseRequestContext(t *testing.T) {
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	r := testRecoverer(t, api, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Recover(ctx); err != nil {
		t.Fatalf("cancelled request ctx must not abort Ensure: %v", err)
	}
	if api.putCount() != 1 {
		t.Fatalf("puts=%d", api.putCount())
	}
}

func TestRecoverConflictIsOK(t *testing.T) {
	api := &fakeAPI{
		putErr:  &statusErr{code: http.StatusConflict, msg: "conflict"},
		putResp: &http.Response{StatusCode: http.StatusConflict},
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	r := testRecoverer(t, api, Options{})
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverKeepsLaptop(t *testing.T) {
	current := entry("203.0.113.10", 32, "cloudrun", true, false)
	stale := entry("198.51.100.7", 32, "cloudrun", true, false)
	laptop := entry("192.0.2.50", 32, "laptop", true, false)
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, current, stale, laptop)},
		},
	}
	r := testRecoverer(t, api, Options{})
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.deletes) != 1 || api.deletes[0].ip != "198.51.100.7" {
		t.Fatalf("deletes: %+v", api.deletes)
	}
}

func TestRecoverLogsProxyString(t *testing.T) {
	// Ensure still retries waitSQL on the log-shaped 08C00 string.
	n := 0
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	r := testRecoverer(t, api, Options{
		PingSQL: func(context.Context) error {
			n++
			if n < 2 {
				return fmt.Errorf("FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)")
			}
			return nil
		},
	})
	if err := r.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if api.putCount() != 1 {
		t.Fatalf("puts=%d", api.putCount())
	}
}
