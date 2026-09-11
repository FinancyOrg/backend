package mongo

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConnectEmptyURI(t *testing.T) {
	c, err := Connect(context.Background(), "  ")
	if err != nil {
		t.Fatalf("empty uri: %v", err)
	}
	if c != nil {
		t.Fatal("empty uri should skip connecting")
	}
}

func TestConnectInvalidURI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Connect(ctx, "://bad")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "mongo") {
		t.Fatalf("error %q", err)
	}
}

func TestPingNilClient(t *testing.T) {
	var c *Client
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestDatabaseFromURI(t *testing.T) {
	got := databaseFromURI("mongodb://host:443/financy?loadBalanced=true&tls=true")
	if got != "financy" {
		t.Fatal(got)
	}
	got = databaseFromURI("mongodb://user:pass@host:443/financy?tls=true")
	if got != "financy" {
		t.Fatal(got)
	}
	if databaseFromURI("mongodb://host:443") != "" {
		t.Fatal("expected empty")
	}
}
