package crdballowlist

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsProxyRefused(t *testing.T) {
	pg08C00 := &pgconn.PgError{Code: "08C00", Message: "connection refused"}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pg 08C00", pg08C00, true},
		{"wrapped pg 08C00", fmt.Errorf("query: %w", pg08C00), true},
		{"log-shaped string", fmt.Errorf("FATAL: codeProxyRefusedConnection: connection refused (SQLSTATE 08C00)"), true},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"canceled", context.Canceled, false},
		{"no rows", pgx.ErrNoRows, false},
		{"generic refused", errors.New("connection refused"), false},
		{"mongo-shaped", errors.New("server selection timeout"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsProxyRefused(c.err); got != c.want {
				t.Fatalf("IsProxyRefused(%v)=%v want %v", c.err, got, c.want)
			}
		})
	}
}
