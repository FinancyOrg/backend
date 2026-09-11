package crdballowlist

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSQLAddr(t *testing.T) {
	addr, err := sqlAddr("postgresql://root@financy-20000.jxf.gcp-europe-west3.cockroachlabs.cloud:26257/defaultdb?sslmode=verify-full")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "financy-20000.jxf.gcp-europe-west3.cockroachlabs.cloud:26257" {
		t.Fatal(addr)
	}
}

func TestSQLAddrRejectsEmpty(t *testing.T) {
	if _, err := sqlAddr("  "); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitTCPPasses(t *testing.T) {
	addr := openSQLPort(t)
	if err := waitTCP(context.Background(), addr, func(context.Context, time.Duration) error { return nil }, time.Nanosecond, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWaitTCPRetriesThenPasses(t *testing.T) {
	n := 0
	err := waitTCP(context.Background(), "db.example:26257", func(context.Context, time.Duration) error { return nil }, time.Nanosecond, func(context.Context, string, string) (net.Conn, error) {
		n++
		if n < 3 {
			return nil, fmt.Errorf("connection refused")
		}
		c1, c2 := net.Pipe()
		_ = c2.Close()
		return c1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("dials=%d", n)
	}
}

func TestWaitTCPTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := waitTCP(ctx, "127.0.0.1:1", func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}, time.Nanosecond, func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestWaitSQLRetriesThenPasses(t *testing.T) {
	n := 0
	err := waitSQL(context.Background(), func(context.Context) error {
		n++
		if n < 3 {
			return fmt.Errorf("FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)")
		}
		return nil
	}, func(context.Context, time.Duration) error { return nil }, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("pings=%d", n)
	}
}

func TestWaitSQLTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := waitSQL(ctx, func(context.Context) error {
		return errors.New("connection refused")
	}, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}, time.Nanosecond)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "SELECT now()") {
		t.Fatalf("got %v", err)
	}
}

func TestPingSelectNowRequiresURL(t *testing.T) {
	err := pingSelectNow("  ")(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("got %v", err)
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	err := Run(context.Background(), func(k string) string {
		switch k {
		case "CRDB_API_KEY":
			return "k"
		case "CRDB_CLUSTER_ID":
			return "id"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("got %v", err)
	}
}
