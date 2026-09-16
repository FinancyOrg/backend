package crdballowlist

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	proxyRefusedSQLState = "08C00"
	proxyRefusedCode     = "codeProxyRefusedConnection"
)

// IsProxyRefused reports whether err is Cockroach Cloud's SQL proxy denying
// this client's IP (SQLSTATE 08C00 / codeProxyRefusedConnection).
func IsProxyRefused(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == proxyRefusedSQLState {
		return true
	}
	return strings.Contains(err.Error(), proxyRefusedCode)
}
