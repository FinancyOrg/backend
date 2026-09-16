package store

import (
	"context"
	"fmt"

	"github.com/FinancyOrg/backend/internal/crdballowlist"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// recoverer is the runtime allowlist injector. *crdballowlist.Recoverer
// satisfies it; tests use fakes.
type recoverer interface {
	Recover(ctx context.Context) error
	Enabled() bool
}

// querier is the pgx pool surface retried after Recover.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Reset()
	Close()
}

// Pool wraps a pgx pool so Query, QueryRow.Scan, Exec, and Begin retry once
// after a Cockroach proxy deny (08C00) if a recoverer is enabled.
type Pool struct {
	inner querier
	rec   recoverer
}

// Connect opens a pgx pool. rec may be nil or disabled (local/dev): 08C00 is
// then returned unchanged.
func Connect(ctx context.Context, databaseURL string, rec *crdballowlist.Recoverer) (*Pool, error) {
	inner, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return Wrap(inner, rec), nil
}

// Wrap is the production recovering pool. inner is typically *pgxpool.Pool.
func Wrap(inner querier, rec recoverer) *Pool {
	return &Pool{inner: inner, rec: rec}
}

var (
	_ querier   = (*pgxpool.Pool)(nil)
	_ recoverer = (*crdballowlist.Recoverer)(nil)
)

// Inner returns the underlying pgx pool when Connect opened one.
func (p *Pool) Inner() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	pool, _ := p.inner.(*pgxpool.Pool)
	return pool
}

func (p *Pool) Close() {
	if p != nil && p.inner != nil {
		p.inner.Close()
	}
}

func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := p.inner.Query(ctx, sql, args...)
	retry, err := p.recoverIfProxy(ctx, err)
	if !retry {
		return rows, err
	}
	if rows != nil {
		rows.Close()
	}
	return p.inner.Query(ctx, sql, args...)
}

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	copied := append([]any(nil), args...)
	return &recoveringRow{
		ctx:  ctx,
		pool: p,
		sql:  sql,
		args: copied,
		row:  p.inner.QueryRow(ctx, sql, copied...),
	}
}

func (p *Pool) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tag, err := p.inner.Exec(ctx, sql, arguments...)
	retry, err := p.recoverIfProxy(ctx, err)
	if !retry {
		return tag, err
	}
	return p.inner.Exec(ctx, sql, arguments...)
}

func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.inner.Begin(ctx)
	retry, err := p.recoverIfProxy(ctx, err)
	if !retry {
		return tx, err
	}
	return p.inner.Begin(ctx)
}

// recoverIfProxy runs Recover once on 08C00. retry means the caller should
// run the operation again. When Recover fails, out is the original SQL error
// (still identifiable as 08C00).
func (p *Pool) recoverIfProxy(ctx context.Context, err error) (retry bool, out error) {
	if p == nil || !crdballowlist.IsProxyRefused(err) {
		return false, err
	}
	if p.rec == nil || !p.rec.Enabled() {
		return false, err
	}
	if recErr := p.rec.Recover(ctx); recErr != nil {
		return false, fmt.Errorf("%w", err)
	}
	p.inner.Reset()
	return true, nil
}

type recoveringRow struct {
	ctx  context.Context
	pool *Pool
	sql  string
	args []any
	row  pgx.Row
}

func (r *recoveringRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if r.pool == nil {
		return err
	}
	retry, err := r.pool.recoverIfProxy(r.ctx, err)
	if !retry {
		return err
	}
	return r.pool.inner.QueryRow(r.ctx, r.sql, r.args...).Scan(dest...)
}
