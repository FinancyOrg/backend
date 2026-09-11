package mongo

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestBalancesRoundTrip(t *testing.T) {
	in := []domain.AccountBalance{{
		AccountID: "a1", CommodityID: "EUR", Minor: big.NewInt(12345),
	}}
	got, err := decodeBalances(encodeBalances(in))
	if err != nil || len(got) != 1 || got[0].Minor.String() != "12345" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestBalancesNilClient(t *testing.T) {
	var c *Client
	if _, hit, err := c.GetBalances(context.Background()); err != nil || hit {
		t.Fatal(hit, err)
	}
	if err := c.PutBalances(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
