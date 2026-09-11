package crdballowlist

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const defaultSQLPort = 26257

func sqlAddr(databaseURL string) (string, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return "", fmt.Errorf("DATABASE_URL is required")
	}
	cfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return "", fmt.Errorf("DATABASE_URL: %w", err)
	}
	if cfg.Host == "" {
		return "", fmt.Errorf("DATABASE_URL: missing host")
	}
	port := int(cfg.Port)
	if port == 0 {
		port = defaultSQLPort
	}
	return net.JoinHostPort(cfg.Host, strconv.Itoa(port)), nil
}

func waitTCP(ctx context.Context, addr string, sleep func(context.Context, time.Duration) error, pollEvery time.Duration, dial func(context.Context, string, string) (net.Conn, error)) error {
	if addr == "" {
		return fmt.Errorf("sql address is required")
	}
	if dial == nil {
		d := net.Dialer{Timeout: 2 * time.Second}
		dial = d.DialContext
	}
	var last error
	for {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("wait for sql tcp %s: %w (last: %v)", addr, err, last)
			}
			return fmt.Errorf("wait for sql tcp %s: %w", addr, err)
		}
		conn, err := dial(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		if err := sleep(ctx, pollEvery); err != nil {
			return fmt.Errorf("wait for sql tcp %s: %w (last: %v)", addr, err, last)
		}
	}
}

// waitSQL retries ping until SELECT now() (or the injected ping) succeeds.
// TCP accepting is not enough: CRDB's SQL proxy can handshake and still
// return codeProxyRefusedConnection until the allowlist is live.
func waitSQL(ctx context.Context, ping func(context.Context) error, sleep func(context.Context, time.Duration) error, pollEvery time.Duration) error {
	if ping == nil {
		return fmt.Errorf("sql ping is required")
	}
	var last error
	for {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("wait for sql SELECT now(): %w (last: %v)", err, last)
			}
			return fmt.Errorf("wait for sql SELECT now(): %w", err)
		}
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := ping(attempt)
		cancel()
		if err == nil {
			return nil
		}
		last = err
		if err := sleep(ctx, pollEvery); err != nil {
			return fmt.Errorf("wait for sql SELECT now(): %w (last: %v)", err, last)
		}
	}
}

func pingSelectNow(databaseURL string) func(context.Context) error {
	return func(ctx context.Context) error {
		databaseURL = strings.TrimSpace(databaseURL)
		if databaseURL == "" {
			return fmt.Errorf("DATABASE_URL is required")
		}
		conn, err := pgx.Connect(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		var now time.Time
		return conn.QueryRow(ctx, "SELECT now()").Scan(&now)
	}
}
