package mongo

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/ledger"
)

func TestPeriodsRoundTrip(t *testing.T) {
	in := ledger.ViewPeriodsResult{
		Currency: domain.Commodity{
			ID: "EUR", Code: "EUR", Name: "Euro", MinorUnits: 2, Kind: domain.CommodityCurrency,
		},
		Months: []ledger.ViewPeriod{{
			Key: "2026-01", Label: "Jan 2026", StartDate: "2026-01-01", EndDate: "2026-01-31",
			CurrencyID: "EUR", IncomeMinor: big.NewInt(1000), ExpenseMinor: big.NewInt(400),
			CapitalGainsMinor: big.NewInt(50), EffectMinor: big.NewInt(650),
			NetSavingsMinor: big.NewInt(600), NetWorthMinor: big.NewInt(650),
			Factor: 0.8, Good: true,
		}},
		Years: []ledger.ViewPeriod{{
			Key: "2026", Label: "2026", StartDate: "2026-01-01", EndDate: "2026-12-31",
			CurrencyID: "EUR", IncomeMinor: big.NewInt(1000), ExpenseMinor: big.NewInt(400),
			CapitalGainsMinor: big.NewInt(50), EffectMinor: big.NewInt(650),
			NetSavingsMinor: big.NewInt(600), NetWorthMinor: big.NewInt(650),
			Factor: 0.8, Good: true,
		}},
		Timezone: "Europe/Berlin",
	}
	got, err := decodePeriods(encodePeriods(in, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency.ID != "EUR" || len(got.Months) != 1 || len(got.Years) != 1 {
		t.Fatalf("%+v", got)
	}
	m := got.Months[0]
	if m.IncomeMinor.String() != "1000" || m.ExpenseMinor.String() != "400" || !m.Good {
		t.Fatalf("%+v", m)
	}
	doc := encodePeriods(in, 0)
	if doc.Kind != "periods" || doc.ID != "periods:EUR:Europe/Berlin:0" {
		t.Fatal(doc)
	}
}

func TestGetPeriodsNilClient(t *testing.T) {
	var c *Client
	_, hit, err := c.GetPeriods(context.Background(), "EUR", "Europe/Berlin", 0)
	if err != nil || hit {
		t.Fatalf("hit=%v err=%v", hit, err)
	}
}

func TestPutPeriodsNilClient(t *testing.T) {
	var c *Client
	if err := c.PutPeriods(context.Background(), ledger.ViewPeriodsResult{}, 0); err != nil {
		t.Fatal(err)
	}
}
