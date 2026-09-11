package mongo

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestPostingsRoundTrip(t *testing.T) {
	date := "2026-01-15"
	label := "lot"
	memo := "note"
	in := []domain.ResolvedPosting{
		{
			PostingInput: domain.PostingInput{
				AccountID: "acc-1",
				Units:     domain.Units{Minor: big.NewInt(-100), CommodityID: "EUR"},
				Cost: &domain.CostBasis{
					PerUnit:     domain.Rational{Numerator: big.NewInt(1), Denominator: big.NewInt(1)},
					CommodityID: "EUR",
					Date:        &date,
					Label:       &label,
				},
				Memo: &memo,
			},
			LineOrder: 0,
			Weight:    domain.Weight{Minor: big.NewInt(-100), CommodityID: "EUR"},
		},
		{
			PostingInput: domain.PostingInput{
				AccountID: "acc-2",
				Units:     domain.Units{Minor: big.NewInt(100), CommodityID: "EUR"},
				Price: &domain.TransactionPrice{
					PerUnit:     domain.Rational{Numerator: big.NewInt(2), Denominator: big.NewInt(1)},
					CommodityID: "USD",
				},
			},
			LineOrder: 1,
			Weight:    domain.Weight{Minor: big.NewInt(200), CommodityID: "USD"},
		},
	}
	got, err := decodePostings(encodePostings("j1", in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].AccountID != "acc-1" || got[0].Units.Minor.String() != "-100" {
		t.Fatalf("%+v", got[0])
	}
	if got[0].Cost == nil || got[0].Cost.CommodityID != "EUR" || *got[0].Cost.Label != "lot" {
		t.Fatalf("%+v", got[0].Cost)
	}
	if got[1].Price == nil || got[1].Price.PerUnit.Numerator.String() != "2" {
		t.Fatalf("%+v", got[1].Price)
	}
	doc := encodePostings("j1", in)
	if doc.ID != "journal:j1" || doc.Kind != journalCacheKind {
		t.Fatal(doc)
	}
}

func TestGetPutDeletePostingsNilClient(t *testing.T) {
	var c *Client
	found, err := c.GetPostings(context.Background(), []string{"j1"})
	if err != nil || len(found) != 0 {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if err := c.PutPostings(context.Background(), "j1", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.DeletePostings(context.Background(), "j1"); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidateNilClient(t *testing.T) {
	var c *Client
	if err := c.Invalidate(context.Background(), "balances", "periods"); err != nil {
		t.Fatal(err)
	}
	if err := c.Invalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
}
