package mongo

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestRetranslationRoundTrip(t *testing.T) {
	in := domain.RetranslationPreview{
		ReportingCommodityID: "EUR",
		RetranslationMinor:   big.NewInt(100),
		NetWorthMinor:        big.NewInt(1000),
		IncomeExpenseMinor:   big.NewInt(900),
		AsOf:                 "2026-09-05T12:00:00.000000000Z",
	}
	got, err := decodeRetranslation(encodeRetranslation(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.RetranslationMinor.String() != "100" || got.ReportingCommodityID != "EUR" {
		t.Fatalf("%+v", got)
	}
	if encodeRetranslation(in).AsOfDate != "2026-09-05" {
		t.Fatal(encodeRetranslation(in).AsOfDate)
	}
}

func TestRetranslationNilClient(t *testing.T) {
	var c *Client
	if _, hit, err := c.GetRetranslation(context.Background(), "EUR", "2026-09-05"); err != nil || hit {
		t.Fatal(hit, err)
	}
	if err := c.PutRetranslation(context.Background(), domain.RetranslationPreview{}); err != nil {
		t.Fatal(err)
	}
}
