package crdballowlist

import (
	"context"
	"fmt"
	"log"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultTimeout is the bound for startup Ensure and for runtime Recover.
const DefaultTimeout = 100 * time.Second

// Recoverer re-runs Ensure when a live process hits codeProxyRefusedConnection.
// Local/dev (LoadConfig nil) is disabled: Recover is a no-op success and
// Enabled is false so the DB wrapper returns 08C00 unchanged.
type Recoverer struct {
	api     API
	cfg     Config
	opts    Options
	timeout time.Duration
	group   singleflight.Group
}

// NewRecoverer loads Cloud API identity from the environment. Both
// CRDB_API_KEY and CRDB_CLUSTER_ID unset yields a disabled Recoverer.
func NewRecoverer(getenv func(string) string) (*Recoverer, error) {
	cfg, err := LoadConfig(getenv)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &Recoverer{}, nil
	}
	databaseURL := getenv("DATABASE_URL")
	addr, err := sqlAddr(databaseURL)
	if err != nil {
		return nil, err
	}
	return &Recoverer{
		api:     NewAPI(cfg.APIKey),
		cfg:     *cfg,
		opts:    Options{SQLAddr: addr, DatabaseURL: databaseURL},
		timeout: DefaultTimeout,
	}, nil
}

// Enabled is true when Cloud API credentials are configured.
func (r *Recoverer) Enabled() bool {
	return r != nil && r.api != nil
}

// Run allowlists this instance's egress /32 at process start. No-op when disabled.
func (r *Recoverer) Run(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}
	ip, err := Ensure(ctx, r.api, r.cfg, r.opts)
	if err != nil {
		return err
	}
	log.Printf("crdb allowlist: %s/32 propagated; sql %s ready", ip, r.opts.SQLAddr)
	return nil
}

// Recover re-discovers the current egress /32 and runs Ensure. Concurrent
// callers share one Cloud API update. The request context is not used to
// cancel Cloud API work; Ensure runs against a bounded background context.
func (r *Recoverer) Recover(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}
	_, err, _ := r.group.Do("ensure", func() (any, error) {
		return nil, r.recoverOnce()
	})
	return err
}

func (r *Recoverer) recoverOnce() error {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ensureCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	discover := r.opts.DiscoverIP
	if discover == nil {
		discover = DiscoverEgressIPv4
	}
	ip, err := discover(ensureCtx)
	if err != nil {
		log.Printf("crdb allowlist: recover failed: discover egress ip: %v", err)
		return fmt.Errorf("discover egress ip: %w", err)
	}
	log.Printf("crdb allowlist: recover start %s/32", ip)
	opts := r.opts
	opts.DiscoverIP = func(context.Context) (string, error) { return ip, nil }
	got, err := Ensure(ensureCtx, r.api, r.cfg, opts)
	if err != nil {
		log.Printf("crdb allowlist: recover failed %s/32: %v", ip, err)
		return err
	}
	log.Printf("crdb allowlist: recover ok %s/32", got)
	return nil
}

// Run loads config from the environment and, when enabled, allowlists this
// instance's egress /32, waits until the Cloud API reports the change has
// propagated, then waits until TCP and SELECT now() against Cockroach succeed.
func Run(ctx context.Context, getenv func(string) string) error {
	r, err := NewRecoverer(getenv)
	if err != nil {
		return err
	}
	return r.Run(ctx)
}
