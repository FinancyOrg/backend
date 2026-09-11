package mongo

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/ledger"
)

func TestFlagsNilClient(t *testing.T) {
	var c *Client
	got, err := c.ListAccountFlags(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if err := c.PutAccountFlags(context.Background(), "a1", ledger.AccountFlags{Hidden: true, Liquid: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteAccountFlags(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
}
