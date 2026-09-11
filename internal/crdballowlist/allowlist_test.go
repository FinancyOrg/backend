package crdballowlist

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach-cloud-sdk-go/v6/pkg/client"
)

func openSQLPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func ensureOpts(t *testing.T, extra Options) Options {
	t.Helper()
	if extra.DiscoverIP == nil {
		extra.DiscoverIP = func(context.Context) (string, error) { return "203.0.113.10", nil }
	}
	if extra.Sleep == nil {
		extra.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	if extra.SQLAddr == "" {
		extra.SQLAddr = openSQLPort(t)
	}
	if extra.PingSQL == nil {
		extra.PingSQL = func(context.Context) error { return nil }
	}
	return extra
}

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadConfig(func(string) string { return "" })
	if err != nil || cfg != nil {
		t.Fatalf("both unset: cfg=%v err=%v", cfg, err)
	}

	_, err = LoadConfig(func(k string) string {
		if k == "CRDB_API_KEY" {
			return "secret"
		}
		return ""
	})
	if err == nil {
		t.Fatal("only API key must be fatal")
	}

	cfg, err = LoadConfig(func(k string) string {
		switch k {
		case "CRDB_API_KEY":
			return "secret"
		case "CRDB_CLUSTER_ID":
			return "cluster-uuid"
		case "CRDB_ALLOWLIST_NAME":
			return "cloudrun-prod"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "secret" || cfg.ClusterID != "cluster-uuid" || cfg.Name != "cloudrun-prod" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadConfigDefaultName(t *testing.T) {
	cfg, err := LoadConfig(func(k string) string {
		if k == "CRDB_API_KEY" {
			return "k"
		}
		if k == "CRDB_CLUSTER_ID" {
			return "id"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != DefaultName {
		t.Fatal(cfg.Name)
	}
}

func TestEnsurePutsAndWaits(t *testing.T) {
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(true, entry("203.0.113.10", 32, "cloudrun", true, false))},
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	ip, err := Ensure(context.Background(), api, Config{ClusterID: "c1", Name: "cloudrun"}, ensureOpts(t, Options{
		PollEvery: time.Nanosecond,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.10" {
		t.Fatal(ip)
	}
	if len(api.puts) != 1 || api.puts[0].ip != "203.0.113.10" || api.puts[0].mask != 32 {
		t.Fatalf("puts: %+v", api.puts)
	}
	if api.puts[0].sql != true || api.puts[0].ui != false || api.puts[0].name != "cloudrun" {
		t.Fatalf("put entry: %+v", api.puts[0])
	}
}

func TestEnsureConflictIsOK(t *testing.T) {
	api := &fakeAPI{
		putErr:  &statusErr{code: http.StatusConflict, msg: "conflict"},
		putResp: &http.Response{StatusCode: http.StatusConflict},
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	if _, err := Ensure(context.Background(), api, Config{ClusterID: "c1"}, ensureOpts(t, Options{})); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRetriesRateLimit(t *testing.T) {
	api := &fakeAPI{
		putErrs: []error{&statusErr{code: http.StatusTooManyRequests, msg: "rate"}},
		putResps: []*http.Response{{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"1"}},
		}},
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	if _, err := Ensure(context.Background(), api, Config{ClusterID: "c1"}, ensureOpts(t, Options{})); err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 2 {
		t.Fatalf("expected retry, puts=%d", len(api.puts))
	}
}

func TestEnsureRejectsNonIPv4(t *testing.T) {
	_, err := Ensure(context.Background(), &fakeAPI{}, Config{ClusterID: "c1"}, Options{
		DiscoverIP: func(context.Context) (string, error) { return "2001:db8::1", nil },
	})
	if err == nil || !strings.Contains(err.Error(), "IPv4") {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureTimesOutWhilePropagating(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(true, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
		onList: func(int) {
			cancel()
		},
	}
	_, err := Ensure(ctx, api, Config{ClusterID: "c1"}, Options{
		DiscoverIP: func(context.Context) (string, error) { return "203.0.113.10", nil },
		SQLAddr:    "127.0.0.1:1",
		Sleep: func(ctx context.Context, _ time.Duration) error {
			return ctx.Err()
		},
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "wait for allowlist") {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureTimesOutWaitingForSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dialN := 0
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	_, err := Ensure(ctx, api, Config{ClusterID: "c1"}, Options{
		DiscoverIP: func(context.Context) (string, error) { return "203.0.113.10", nil },
		SQLAddr:    "127.0.0.1:1",
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialN++
			return nil, fmt.Errorf("connection refused")
		},
		Sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "sql tcp") {
		t.Fatalf("got %v", err)
	}
	if dialN < 1 {
		t.Fatal("expected a tcp dial")
	}
}

func TestEnsureRetriesSQLPingAfterTCP(t *testing.T) {
	n := 0
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, entry("203.0.113.10", 32, "cloudrun", true, false))},
		},
	}
	ip, err := Ensure(context.Background(), api, Config{ClusterID: "c1"}, ensureOpts(t, Options{
		PingSQL: func(context.Context) error {
			n++
			if n < 3 {
				return fmt.Errorf("FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)")
			}
			return nil
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.10" {
		t.Fatal(ip)
	}
	if n != 3 {
		t.Fatalf("pings=%d", n)
	}
}

func TestEnsurePrunesStaleCloudRunKeepsLaptop(t *testing.T) {
	current := entry("203.0.113.10", 32, "cloudrun", true, false)
	stale := entry("198.51.100.7", 32, "cloudrun", true, false)
	laptop := entry("192.0.2.50", 32, "laptop", true, false)
	open := entry("0.0.0.0", 0, "open", true, true)
	api := &fakeAPI{
		list: []listResult{
			{resp: listResp(false, current, stale, laptop, open)},
		},
	}
	if _, err := Ensure(context.Background(), api, Config{ClusterID: "c1", Name: "cloudrun"}, ensureOpts(t, Options{})); err != nil {
		t.Fatal(err)
	}
	if len(api.deletes) != 1 || api.deletes[0].ip != "198.51.100.7" || api.deletes[0].mask != 32 {
		t.Fatalf("deletes: %+v", api.deletes)
	}
}

func TestRunDisabled(t *testing.T) {
	if err := Run(context.Background(), func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
}

func TestStaleCloudRun(t *testing.T) {
	cases := []struct {
		e    client.AllowlistEntry
		want bool
	}{
		{entry("198.51.100.7", 32, "cloudrun", true, false), true},
		{entry("198.51.100.7", 32, "cloudrun-old", true, false), true},
		{entry("203.0.113.10", 32, "cloudrun", true, false), false},
		{entry("192.0.2.50", 32, "laptop", true, false), false},
		{entry("0.0.0.0", 0, "anywhere", true, true), false},
	}
	for _, c := range cases {
		if got := staleCloudRun(c.e, "203.0.113.10", "cloudrun"); got != c.want {
			t.Errorf("%s/%d name=%s: got %v want %v", c.e.GetCidrIp(), c.e.GetCidrMask(), c.e.GetName(), got, c.want)
		}
	}
}

func TestDiscoverEgressIPv4(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer srv.Close()
	ip, err := discoverEgressIPv4(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.9" {
		t.Fatal(ip)
	}
}

func TestDiscoverEgressIPv4RejectsGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	defer srv.Close()
	if _, err := discoverEgressIPv4(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected error")
	}
}

type putCall struct {
	cluster, ip, name string
	mask              int32
	sql, ui           bool
}

type cidrCall struct {
	ip   string
	mask int32
}

type listResult struct {
	resp *client.ListAllowlistEntriesResponse
	err  error
	http *http.Response
}

type fakeAPI struct {
	puts     []putCall
	putErr   error
	putResp  *http.Response
	putErrs  []error
	putResps []*http.Response

	list   []listResult
	listN  int
	onList func(int)

	deletes []cidrCall
}

func (f *fakeAPI) AddAllowlistEntry2(_ context.Context, clusterId string, cidrIp string, cidrMask int32, entry *client.AllowlistEntry1) (*client.AllowlistEntry, *http.Response, error) {
	name := ""
	sql, ui := false, false
	if entry != nil {
		name = entry.GetName()
		sql = entry.GetSql()
		ui = entry.GetUi()
	}
	f.puts = append(f.puts, putCall{cluster: clusterId, ip: cidrIp, mask: cidrMask, name: name, sql: sql, ui: ui})
	if len(f.putErrs) > 0 {
		err := f.putErrs[0]
		f.putErrs = f.putErrs[1:]
		var resp *http.Response
		if len(f.putResps) > 0 {
			resp = f.putResps[0]
			f.putResps = f.putResps[1:]
		}
		return nil, resp, err
	}
	return nil, f.putResp, f.putErr
}

func (f *fakeAPI) ListAllowlistEntries(_ context.Context, _ string, options *client.ListAllowlistEntriesOptions) (*client.ListAllowlistEntriesResponse, *http.Response, error) {
	if options == nil {
		return nil, nil, fmt.Errorf("ListAllowlistEntries options must not be nil")
	}
	i := f.listN
	if i >= len(f.list) {
		i = len(f.list) - 1
	}
	f.listN++
	if f.onList != nil {
		f.onList(f.listN)
	}
	if i < 0 {
		return listResp(false), nil, nil
	}
	r := f.list[i]
	return r.resp, r.http, r.err
}

func (f *fakeAPI) DeleteAllowlistEntry(_ context.Context, _ string, cidrIp string, cidrMask int32) (*client.AllowlistEntry, *http.Response, error) {
	f.deletes = append(f.deletes, cidrCall{ip: cidrIp, mask: cidrMask})
	return nil, nil, nil
}

func listResp(propagating bool, entries ...client.AllowlistEntry) *client.ListAllowlistEntriesResponse {
	return client.NewListAllowlistEntriesResponse(entries, propagating)
}

func entry(ip string, mask int32, name string, sql, ui bool) client.AllowlistEntry {
	e := client.NewAllowlistEntry(ip, mask, sql, ui)
	e.SetName(name)
	return *e
}

type statusErr struct {
	code int
	msg  string
}

func (e *statusErr) Error() string { return e.msg }
