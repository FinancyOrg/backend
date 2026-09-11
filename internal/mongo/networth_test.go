package mongo

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestNetWorthRoundTrip(t *testing.T) {
	in := domain.NetWorthReport{
		ReportingCommodityID: "EUR",
		AsOf:                 "2026-09-05T12:00:00.000000000Z",
		Assets: []domain.ValuedBalance{{
			AccountID:            "acc-1",
			AccountType:          domain.AccountAsset,
			ReportingMinor:       big.NewInt(12345),
			ReportingCommodityID: "EUR",
			Native: domain.AccountBalance{
				AccountID: "acc-1", CommodityID: "INR", Minor: big.NewInt(100000),
			},
		}},
		Liabilities:           []domain.ValuedBalance{},
		TotalAssetsMinor:      big.NewInt(12345),
		TotalLiabilitiesMinor: big.NewInt(0),
		NetWorthMinor:         big.NewInt(12345),
	}
	got, err := decodeNetWorth(encodeNetWorth(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportingCommodityID != "EUR" || got.AsOf != in.AsOf {
		t.Fatalf("%+v", got)
	}
	if got.NetWorthMinor.String() != "12345" || len(got.Assets) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Assets[0].Native.Minor.String() != "100000" {
		t.Fatalf("%+v", got.Assets[0])
	}
	if encodeNetWorth(in).AsOfDate != "2026-09-05" {
		t.Fatal(encodeNetWorth(in).AsOfDate)
	}
}

func TestGetNetWorthNilClient(t *testing.T) {
	var c *Client
	_, hit, err := c.GetNetWorth(context.Background(), "EUR", "2026-09-05")
	if err != nil || hit {
		t.Fatalf("hit=%v err=%v", hit, err)
	}
}

func TestNetWorthCacheIDIncludesPresentation(t *testing.T) {
	in := domain.NetWorthReport{
		ReportingCommodityID: "JPY",
		AsOf:                 "2026-09-05T12:00:00.000000000Z",
	}
	doc := encodeNetWorth(in)
	if doc.ID != "networth:JPY:2026-09-05" {
		t.Fatal(doc.ID)
	}
}

func TestPutNetWorthNilClient(t *testing.T) {
	var c *Client
	if err := c.PutNetWorth(context.Background(), domain.NetWorthReport{}); err != nil {
		t.Fatal(err)
	}
}
