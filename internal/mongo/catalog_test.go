package mongo

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestAccountsRoundTrip(t *testing.T) {
	parent := "p1"
	in := []domain.Account{{
		ID: "a1", Code: "Assets:Cash", Name: "Cash", AccountType: domain.AccountAsset,
		ParentID: &parent, NativeCommodityID: "EUR", CreatedAt: "2026-01-01T00:00:00.000000000Z",
	}}
	got := decodeAccounts(encodeAccounts(in))
	if len(got) != 1 || got[0].Code != "Assets:Cash" || got[0].ParentID == nil || *got[0].ParentID != "p1" {
		t.Fatalf("%+v", got)
	}
}

func TestCommoditiesRoundTrip(t *testing.T) {
	in := []domain.Commodity{{
		ID: "EUR", Code: "EUR", Name: "Euro", MinorUnits: 2, Kind: domain.CommodityCurrency,
	}}
	got := decodeCommodities(encodeCommodities(in))
	if len(got) != 1 || got[0].ID != "EUR" || got[0].Kind != domain.CommodityCurrency {
		t.Fatalf("%+v", got)
	}
}

func TestCatalogNilClient(t *testing.T) {
	var c *Client
	if _, hit, err := c.GetAccounts(context.Background()); err != nil || hit {
		t.Fatal(hit, err)
	}
	if err := c.PutAccounts(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := c.GetCommodities(context.Background()); err != nil || hit {
		t.Fatal(hit, err)
	}
	if err := c.PutCommodities(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
