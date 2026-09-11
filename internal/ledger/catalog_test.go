package ledger

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestLedger_JPYWorksWithoutEURCommodity(t *testing.T) {
	svc, pool := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "JPY")

	jpy := "JPY"
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Cash:JPY", Name: "Yen cash", AccountType: "asset", NativeCommodityID: &jpy,
	})
	if err != nil {
		t.Fatal(err)
	}
	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Job:JPY", Name: "Job", AccountType: "income", NativeCommodityID: &jpy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-1000), CommodityID: "JPY"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(1000), CommodityID: "JPY"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM commodities WHERE id = $1`, domain.EcbQuoteCurrency).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("EUR commodity must not be required for a JPY ledger")
	}
}

func TestLedger_EcbFetchDoesNotNeedEURCommodity(t *testing.T) {
	svc, pool := testService(t, mockRates(map[string]domain.Rational{
		"INR": domain.MustRational(105, 1),
		"JPY": domain.MustRational(160, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "JPY")

	_, err := FetchEcbRatePerEurWithLookback(ctx, "INR", "2026-01-16", 10, svc.fetchEcbRate)
	if err != nil {
		t.Fatal(err)
	}

	var eurCommodities int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM commodities WHERE id = $1`, domain.EcbQuoteCurrency).Scan(&eurCommodities); err != nil {
		t.Fatal(err)
	}
	if eurCommodities != 0 {
		t.Fatal("EUR commodity must not be created to fetch an INR per EUR spot")
	}
}

func TestLedger_EcbQuoteIdentityNotPersisted(t *testing.T) {
	cache := &memEcbCache{rates: map[string]domain.Rational{}}
	svc, _ := testService(t, mockRates(nil))
	svc.WithEcbCache(cache)
	ctx := context.Background()
	boot(t, svc, domain.EcbQuoteCurrency)

	rate, err := FetchEcbRatePerEurWithLookback(ctx, domain.EcbQuoteCurrency, "2026-01-16", 10, svc.fetchEcbRate)
	if err != nil {
		t.Fatal(err)
	}
	if rate.Rate.Numerator.Cmp(domain.Int(1)) != 0 || rate.Rate.Denominator.Cmp(domain.Int(1)) != 0 {
		t.Fatalf("%+v", rate)
	}
	if _, ok := cache.rates["EUR|2026-01-16"]; ok {
		t.Fatal("ECB quote identity must not persist a cache row")
	}
}

func TestLedger_NoFxTablesInCRDB(t *testing.T) {
	_, pool := testService(t, nil)
	ctx := context.Background()
	for _, name := range []string{"market_prices", "ecb_observations"} {
		var n int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("expected no %s table", name)
		}
	}
}

func TestLedger_UpsertMarketPriceRejected(t *testing.T) {
	svc, _ := testService(t, mockRates(nil))
	boot(t, svc, "EUR")
	err := svc.UpsertMarketPrice(context.Background())
	requireCode(t, err, "InvalidPrice")
}

func TestLedger_EcbCachePersistsSuccessfulFetch(t *testing.T) {
	cache := &memEcbCache{}
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR": domain.MustRational(105, 1),
	}))
	svc.WithEcbCache(cache)
	ctx := context.Background()
	boot(t, svc, "JPY")
	got, err := FetchEcbRatePerEurWithLookback(ctx, "INR", "2026-01-16", 10, svc.fetchEcbRate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rate.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatalf("%+v", got)
	}
	cached, ok := cache.rates["INR|2026-01-16"]
	if !ok || cached.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatalf("%+v", cache.rates)
	}
}
