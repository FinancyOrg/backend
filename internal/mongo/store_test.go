package mongo

import (
	"context"
	"testing"
)

func TestStoreCollectionIsNotCache(t *testing.T) {
	if storeCollection != "store" {
		t.Fatalf("non-cache collection must be store, got %q", storeCollection)
	}
	if storeCollection == cacheCollection {
		t.Fatal("store must not share the cache collection")
	}
}

func TestStoreNilClient(t *testing.T) {
	var c *Client
	got, err := c.GetSetting(context.Background(), "timezone")
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	if err := c.PutSetting(context.Background(), "timezone", "Europe/Berlin"); err != nil {
		t.Fatal(err)
	}
	if err := c.PutSetting(context.Background(), "functionalChangeJob", `{"status":"running"}`); err != nil {
		t.Fatal(err)
	}
}
