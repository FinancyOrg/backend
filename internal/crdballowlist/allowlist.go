// Package crdballowlist updates the CockroachDB Cloud SQL IP allowlist with
// this process's current public egress address before DATABASE_URL is opened.
//
// Cloud Run SNAT addresses are shared and rotate per instance, so they must
// not be pinned in Terraform. Local development leaves CRDB_API_KEY and
// CRDB_CLUSTER_ID unset and this package is a no-op.
package crdballowlist

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/cockroach-cloud-sdk-go/v6/pkg/client"
)

const (
	// DefaultName is the allowlist entry name for Cloud Run (sql only, not DB Console).
	DefaultName = "cloudrun"
	cidrMask    = 32
)

// API is the CockroachDB Cloud IP-allowlist surface used at startup.
// The official SDK's client.Service satisfies this interface.
type API interface {
	AddAllowlistEntry2(ctx context.Context, clusterId string, cidrIp string, cidrMask int32, entry *client.AllowlistEntry1) (*client.AllowlistEntry, *http.Response, error)
	ListAllowlistEntries(ctx context.Context, clusterId string, options *client.ListAllowlistEntriesOptions) (*client.ListAllowlistEntriesResponse, *http.Response, error)
	DeleteAllowlistEntry(ctx context.Context, clusterId string, cidrIp string, cidrMask int32) (*client.AllowlistEntry, *http.Response, error)
}

// Config is the Cloud API identity used to mutate the cluster allowlist.
type Config struct {
	APIKey    string
	ClusterID string
	Name      string
}

// Options customizes Ensure. Zero values use production defaults.
type Options struct {
	DiscoverIP  func(ctx context.Context) (string, error)
	Sleep       func(ctx context.Context, d time.Duration) error
	PollEvery   time.Duration
	SQLAddr     string
	DatabaseURL string
	PingSQL     func(ctx context.Context) error
	Dial        func(ctx context.Context, network, address string) (net.Conn, error)
}

// LoadConfig reads CRDB_API_KEY and CRDB_CLUSTER_ID.
// Both unset means local/dev: (*Config, nil) with a nil config.
// Only one set is a configuration error.
func LoadConfig(getenv func(string) string) (*Config, error) {
	key := strings.TrimSpace(getenv("CRDB_API_KEY"))
	id := strings.TrimSpace(getenv("CRDB_CLUSTER_ID"))
	switch {
	case key == "" && id == "":
		return nil, nil
	case key == "" || id == "":
		return nil, fmt.Errorf("both CRDB_API_KEY and CRDB_CLUSTER_ID are required")
	}
	name := strings.TrimSpace(getenv("CRDB_ALLOWLIST_NAME"))
	if name == "" {
		name = DefaultName
	}
	return &Config{APIKey: key, ClusterID: id, Name: name}, nil
}

// NewAPI builds the official CockroachDB Cloud SDK client, pinned to Cc-Version 2024-09-16.
func NewAPI(apiKey string) API {
	cfg := client.NewConfiguration(apiKey)
	cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	return client.NewService(client.NewClient(cfg))
}

// Run loads config from the environment and, when enabled, allowlists this
// instance's egress /32, waits until the Cloud API reports the change has
// propagated, then waits until TCP and SELECT now() against Cockroach succeed.
func Run(ctx context.Context, getenv func(string) string) error {
	cfg, err := LoadConfig(getenv)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	databaseURL := getenv("DATABASE_URL")
	addr, err := sqlAddr(databaseURL)
	if err != nil {
		return err
	}
	ip, err := Ensure(ctx, NewAPI(cfg.APIKey), *cfg, Options{SQLAddr: addr, DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	log.Printf("crdb allowlist: %s/32 propagated; sql %s ready", ip, addr)
	return nil
}

// Ensure discovers the current public IPv4, PUTs it as a /32 SQL allowlist
// entry, waits until the Cloud API reports propagation finished, waits until
// the SQL port accepts TCP and SELECT now() succeeds, then deletes stale
// Cloud Run entries.
func Ensure(ctx context.Context, api API, cfg Config, opts Options) (string, error) {
	if cfg.Name == "" {
		cfg.Name = DefaultName
	}
	discover := opts.DiscoverIP
	if discover == nil {
		discover = DiscoverEgressIPv4
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	pollEvery := opts.PollEvery
	if pollEvery <= 0 {
		pollEvery = 2 * time.Second
	}

	ip, err := discover(ctx)
	if err != nil {
		return "", fmt.Errorf("discover egress ip: %w", err)
	}
	if err := validateIPv4(ip); err != nil {
		return "", err
	}

	entry := client.NewAllowlistEntry1(true, false)
	entry.SetName(cfg.Name)
	if err := withRetry(ctx, sleep, func() (*http.Response, error) {
		_, resp, err := api.AddAllowlistEntry2(ctx, cfg.ClusterID, ip, cidrMask, entry)
		if resp != nil && resp.StatusCode == http.StatusConflict {
			return resp, nil
		}
		return resp, err
	}); err != nil {
		return "", fmt.Errorf("put allowlist %s/32: %w", ip, err)
	}

	if err := waitPropagated(ctx, api, cfg, ip, sleep, pollEvery); err != nil {
		return "", err
	}

	if err := waitTCP(ctx, opts.SQLAddr, sleep, pollEvery, opts.Dial); err != nil {
		return "", err
	}

	ping := opts.PingSQL
	if ping == nil {
		ping = pingSelectNow(opts.DatabaseURL)
	}
	if err := waitSQL(ctx, ping, sleep, pollEvery); err != nil {
		return "", err
	}

	pruneStale(ctx, api, cfg, ip, sleep)
	return ip, nil
}

func waitPropagated(ctx context.Context, api API, cfg Config, ip string, sleep func(context.Context, time.Duration) error, pollEvery time.Duration) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for allowlist %s/32: %w", ip, err)
		}
		var listed *client.ListAllowlistEntriesResponse
		if err := withRetry(ctx, sleep, func() (*http.Response, error) {
			var resp *http.Response
			var err error
			listed, resp, err = api.ListAllowlistEntries(ctx, cfg.ClusterID, emptyListOpts())
			return resp, err
		}); err != nil {
			return fmt.Errorf("list allowlist: %w", err)
		}
		if listed != nil && !listed.GetPropagating() && hasSQL32(listed.GetAllowlist(), ip) {
			return nil
		}
		if err := sleep(ctx, pollEvery); err != nil {
			return fmt.Errorf("wait for allowlist %s/32: %w", ip, err)
		}
	}
}

func pruneStale(ctx context.Context, api API, cfg Config, currentIP string, sleep func(context.Context, time.Duration) error) {
	var listed *client.ListAllowlistEntriesResponse
	if err := withRetry(ctx, sleep, func() (*http.Response, error) {
		var resp *http.Response
		var err error
		listed, resp, err = api.ListAllowlistEntries(ctx, cfg.ClusterID, emptyListOpts())
		return resp, err
	}); err != nil {
		log.Printf("crdb allowlist: list for prune: %v", err)
		return
	}
	if listed == nil {
		return
	}
	for _, e := range listed.GetAllowlist() {
		if !staleCloudRun(e, currentIP, cfg.Name) {
			continue
		}
		err := withRetry(ctx, sleep, func() (*http.Response, error) {
			_, resp, err := api.DeleteAllowlistEntry(ctx, cfg.ClusterID, e.GetCidrIp(), e.GetCidrMask())
			return resp, err
		})
		if err != nil {
			log.Printf("crdb allowlist: delete stale %s/%d: %v", e.GetCidrIp(), e.GetCidrMask(), err)
		}
	}
}

func staleCloudRun(e client.AllowlistEntry, currentIP, namePrefix string) bool {
	if e.GetCidrIp() == "0.0.0.0" {
		return false
	}
	if e.GetCidrIp() == currentIP && e.GetCidrMask() == cidrMask {
		return false
	}
	return strings.HasPrefix(e.GetName(), namePrefix)
}

func hasSQL32(entries []client.AllowlistEntry, ip string) bool {
	for _, e := range entries {
		if e.GetCidrIp() == ip && e.GetCidrMask() == cidrMask && e.GetSql() {
			return true
		}
	}
	return false
}

func emptyListOpts() *client.ListAllowlistEntriesOptions {
	limit := int32(50)
	return &client.ListAllowlistEntriesOptions{PaginationLimit: &limit}
}

func validateIPv4(ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("egress address %q is not an IPv4 address", ip)
	}
	return nil
}

func withRetry(ctx context.Context, sleep func(context.Context, time.Duration) error, fn func() (*http.Response, error)) error {
	backoff := time.Second
	for {
		resp, err := fn()
		if err == nil {
			return nil
		}
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			wait := backoff
			if ra := retryAfter(resp); ra > 0 {
				wait = ra
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			if backoff < 8*time.Second {
				backoff *= 2
			}
			continue
		}
		return err
	}
}

func retryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	d, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s")
	if err != nil {
		return 0
	}
	return d
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
