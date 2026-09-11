package mongo

import (
	"context"
	"testing"
)

func TestClearLedgerNilClient(t *testing.T) {
	var c *Client
	if err := c.ClearLedger(context.Background()); err != nil {
		t.Fatal(err)
	}
}
