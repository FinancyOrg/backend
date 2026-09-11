package ledger

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func mockRates(rateMap map[string]domain.Rational) RateFetcher {
	return func(ctx context.Context, currency, date string) (domain.Rational, error) {
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if r, ok := rateMap[currency+":"+date]; ok {
			return r, nil
		}
		if r, ok := rateMap[currency]; ok {
			return r, nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	}
}

// mockINRTxnAndClosing uses txn INR/EUR on 2026-01-16 (and 01-17) and closing on any other date including today.
func mockINRTxnAndClosing(txnPerEur, closingPerEur int64) RateFetcher {
	return mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(txnPerEur, 1),
		"INR:2026-01-17": domain.MustRational(txnPerEur, 1),
		"INR":            domain.MustRational(closingPerEur, 1),
	})
}

func TestExchangeCurrency_TransactionCost(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR": domain.MustRational(105, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	inrBank, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.ExchangeCurrency(ctx, ExchangeCurrencyInput{
		EffectiveDate: "2026-01-16", FromAccountID: eurBank.ID, ToAccountID: inrBank.ID,
		FromAmountMinor: domain.Int(200_000), ToAmountMinor: domain.Int(19_000_000),
		Description: ptr("EUR to INR"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entry.Postings) != 3 {
		t.Fatal(len(result.Entry.Postings))
	}
	if result.ExpectedToMinor != "21000000" || result.ResidualToMinor != "2000000" {
		t.Fatalf("%+v", result)
	}
	if domain.MustInt(result.TransactionCostMinor).Sign() <= 0 {
		t.Fatal(result.TransactionCostMinor)
	}
	if result.EcbRateToPerFrom.Numerator.Cmp(domain.Int(105)) != 0 || result.EcbRateToPerFrom.Denominator.Cmp(domain.Int(1)) != 0 {
		t.Fatal(result.EcbRateToPerFrom)
	}
	eurBal, _ := svc.GetAccountBalance(ctx, eurBank.ID)
	inrBal, _ := svc.GetAccountBalance(ctx, inrBank.ID)
	if eurBal.Minor.Cmp(domain.Int(-200_000)) != 0 || inrBal.Minor.Cmp(domain.Int(19_000_000)) != 0 {
		t.Fatal(eurBal.Minor, inrBal.Minor)
	}
	cost, err := svc.GetAccountByCode(ctx, TransactionCostAccountCodeFor("EUR"))
	if err != nil {
		t.Fatal(err)
	}
	costBal, _ := svc.GetAccountBalance(ctx, cost.ID)
	if costBal.Minor.Cmp(domain.MustInt(result.TransactionCostMinor)) != 0 {
		t.Fatal(costBal.Minor, result.TransactionCostMinor)
	}
	nw, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nw.NetWorthMinor.Sign() == 0 {
		t.Fatal("net worth should not be zero")
	}
	prices, _ := svc.ListMarketPrices(ctx)
	found := false
	for _, p := range prices {
		if p.BaseCommodityID == "INR" && p.QuoteCommodityID == "EUR" && p.Source == "ecb" {
			found = true
		}
	}
	if !found {
		t.Fatal(prices)
	}
}

func TestExchangeCurrency_WeekendLookback(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(105, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	result, err := svc.ExchangeCurrency(ctx, ExchangeCurrencyInput{
		EffectiveDate: "2026-01-17", FromAccountID: eurBank.ID, ToAccountID: inrBank.ID,
		FromAmountMinor: domain.Int(200_000), ToAmountMinor: domain.Int(21_000_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if domain.UTCDate(result.Entry.EffectiveDate) != "2026-01-17" {
		t.Fatal(result.Entry.EffectiveDate)
	}
	if result.EcbRateToPerFrom.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatal(result.EcbRateToPerFrom)
	}
	if len(result.Entry.Postings) != 2 {
		t.Fatal(len(result.Entry.Postings))
	}
	var fromPrice *domain.TransactionPrice
	for _, p := range result.Entry.Postings {
		if p.AccountID == eurBank.ID {
			fromPrice = p.Price
		}
	}
	if fromPrice == nil || fromPrice.CommodityID != "INR" {
		t.Fatalf("from leg price: %+v", fromPrice)
	}
	if fromPrice.PerUnit.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatalf("ECB cross price: %+v", fromPrice.PerUnit)
	}
}

func TestExchangeCurrency_MissingRate(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	_, err := svc.ExchangeCurrency(ctx, ExchangeCurrencyInput{
		EffectiveDate: "2026-01-17", FromAccountID: eurBank.ID, ToAccountID: inrBank.ID,
		FromAmountMinor: domain.Int(200_000), ToAmountMinor: domain.Int(19_000_000),
	})
	requireCode(t, err, "MissingEcbRate")
}

func TestValuation_FillsECBOnMiss(t *testing.T) {
	svc, _ := testService(t, func(ctx context.Context, currency, _ string) (domain.Rational, error) {
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if currency == "INR" {
			return domain.MustRational(100, 1), nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, "")
	})
	ctx := context.Background()
	boot(t, svc, "EUR")
	inr := "INR"
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	equity, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Equity:Opening", Name: "Opening", AccountType: "equity", NativeCommodityID: &inr})
	_, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Description:   ptr("Open INR"),
		Postings: []domain.PostingInput{
			{AccountID: equity.ID, Units: domain.Units{Minor: domain.Int(-10_000_000), CommodityID: "INR"}},
			{AccountID: inrBank.ID, Units: domain.Units{Minor: domain.Int(10_000_000), CommodityID: "INR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.NetWorthMinor.Cmp(domain.Int(100_000)) != 0 || report.TotalAssetsMinor.Cmp(domain.Int(100_000)) != 0 {
		t.Fatalf("%s %s", report.NetWorthMinor, report.TotalAssetsMinor)
	}
	prices, _ := svc.ListMarketPrices(ctx)
	if len(prices) != 1 || prices[0].BaseCommodityID != "INR" || prices[0].Source != "ecb" {
		t.Fatal(prices)
	}
	// ListMarketPrices stamps the ECB print date, not the request date. Before
	// 16:00 Europe/Berlin today is gated, so lookback lands on a prior day.
	if !domain.IsOnUTCDate(prices[0].ObservedAt, latestPublishedEcbDate(t)) {
		t.Fatal(prices[0].ObservedAt)
	}
}

func latestPublishedEcbDate(t *testing.T) string {
	t.Helper()
	date := todayISO()
	now := nowUTC()
	for i := 0; i <= 10; i++ {
		if ecbSpotAvailable(date, now) {
			return date
		}
		date = PreviousUTCDate(date)
	}
	t.Fatal("no ECB date in the 10-day lookback window")
	return ""
}

func TestEnsureMarketPrice_UpdatesOncePerDay(t *testing.T) {
	var fetches int
	svc, _ := testService(t, func(ctx context.Context, currency, date string) (domain.Rational, error) {
		fetches++
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if currency == "USD" && date == "2026-01-16" {
			return domain.MustRational(108, 100), nil
		}
		if currency == "USD" && date == "2026-01-17" {
			return domain.MustRational(110, 100), nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	})
	boot(t, svc, "EUR")
	ctx := context.Background()

	first, err := svc.EnsureMarketPrice(ctx, "USD", "EUR", "2026-01-16")
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "ecb" || !domain.IsOnUTCDate(first.ObservedAt, "2026-01-16") {
		t.Fatalf("%+v", first)
	}
	if domain.IsUTCDateOnly(first.ObservedAt) {
		t.Fatal(first.ObservedAt)
	}
	afterFirst := fetches

	second, err := svc.EnsureMarketPrice(ctx, "USD", "EUR", "2026-01-16")
	if err != nil {
		t.Fatal(err)
	}
	if fetches != afterFirst {
		t.Fatalf("same-day refetch: %d -> %d", afterFirst, fetches)
	}
	if domain.CompareRational(second.Price, first.Price) != 0 {
		t.Fatal(second.Price)
	}
	if !domain.IsOnUTCDate(second.ObservedAt, "2026-01-16") {
		t.Fatal(second.ObservedAt)
	}

	third, err := svc.EnsureMarketPrice(ctx, "USD", "EUR", "2026-01-17")
	if err != nil {
		t.Fatal(err)
	}
	if fetches == afterFirst {
		t.Fatal("next day should fetch")
	}
	if !domain.IsOnUTCDate(third.ObservedAt, "2026-01-17") {
		t.Fatal(third.ObservedAt)
	}
	if domain.CompareRational(first.Price, third.Price) == 0 {
		t.Fatal("expected new daily rate")
	}
}

func TestEnsureMarketPrice_WeekendLookbackStoresAsOfTimestamp(t *testing.T) {
	var fetches int
	svc, _ := testService(t, func(ctx context.Context, currency, date string) (domain.Rational, error) {
		fetches++
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if currency == "INR" && date == "2026-01-16" {
			return domain.MustRational(105, 1), nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	})
	boot(t, svc, "EUR")
	ctx := context.Background()

	first, err := svc.EnsureMarketPrice(ctx, "INR", "EUR", "2026-01-17")
	if err != nil {
		t.Fatal(err)
	}
	if !domain.IsOnUTCDate(first.ObservedAt, "2026-01-17") {
		t.Fatal(first.ObservedAt)
	}
	afterFirst := fetches
	second, err := svc.EnsureMarketPrice(ctx, "INR", "EUR", "2026-01-17")
	if err != nil {
		t.Fatal(err)
	}
	if fetches != afterFirst {
		t.Fatalf("same as-of day refetch: %d -> %d", afterFirst, fetches)
	}
	if domain.CompareRational(second.Price, first.Price) != 0 {
		t.Fatal(second.Price)
	}
	if !domain.IsOnUTCDate(second.ObservedAt, "2026-01-17") {
		t.Fatal(second.ObservedAt)
	}
}

func TestEnsureMarketPricesForBalances_TwoFXUsesOnePriceList(t *testing.T) {
	svc, _ := testService(t, func(ctx context.Context, currency, _ string) (domain.Rational, error) {
		switch currency {
		case "EUR":
			return domain.MustRational(1, 1), nil
		case "INR":
			return domain.MustRational(100, 1), nil
		case "USD":
			return domain.MustRational(108, 100), nil
		default:
			return domain.Rational{}, domain.MissingEcbRate(currency, "")
		}
	})
	ctx := context.Background()
	boot(t, svc, "EUR")
	inr, usd := "INR", "USD"
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	usdBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:USD", Name: "USD Checking", AccountType: "asset", NativeCommodityID: &usd})
	equityINR, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Equity:OpenINR", Name: "Open INR", AccountType: "equity", NativeCommodityID: &inr})
	equityUSD, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Equity:OpenUSD", Name: "Open USD", AccountType: "equity", NativeCommodityID: &usd})
	_, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Description:   ptr("Open INR"),
		Postings: []domain.PostingInput{
			{AccountID: equityINR.ID, Units: domain.Units{Minor: domain.Int(-10_000_000), CommodityID: "INR"}},
			{AccountID: inrBank.ID, Units: domain.Units{Minor: domain.Int(10_000_000), CommodityID: "INR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Description:   ptr("Open USD"),
		Postings: []domain.PostingInput{
			{AccountID: equityUSD.ID, Units: domain.Units{Minor: domain.Int(-100_00), CommodityID: "USD"}},
			{AccountID: usdBank.ID, Units: domain.Units{Minor: domain.Int(100_00), CommodityID: "USD"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}
	prices, err := svc.ListMarketPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawINR, sawUSD bool
	for _, p := range prices {
		if p.BaseCommodityID == "INR" && p.QuoteCommodityID == "EUR" {
			sawINR = true
		}
		if p.BaseCommodityID == "USD" && p.QuoteCommodityID == "EUR" {
			sawUSD = true
		}
	}
	if !sawINR || !sawUSD {
		t.Fatalf("prices %+v", prices)
	}
}

func TestComputeNetWorth_MemoizesEcbFetches(t *testing.T) {
	var fetches int
	svc, _ := testService(t, func(ctx context.Context, currency, _ string) (domain.Rational, error) {
		fetches++
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if currency == "INR" {
			return domain.MustRational(100, 1), nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, "")
	})
	ctx := context.Background()
	boot(t, svc, "EUR")
	inr := "INR"
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	equity, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Equity:Opening", Name: "Opening", AccountType: "equity", NativeCommodityID: &inr})
	_, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Description:   ptr("Open INR"),
		Postings: []domain.PostingInput{
			{AccountID: equity.ID, Units: domain.Units{Minor: domain.Int(-10_000_000), CommodityID: "INR"}},
			{AccountID: inrBank.ID, Units: domain.Units{Minor: domain.Int(10_000_000), CommodityID: "INR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}
	afterFirst := fetches
	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}
	if fetches != afterFirst {
		t.Fatalf("second net worth same day fetched %d extra rates", fetches-afterFirst)
	}
}

func TestValuation_DoesNotInventSecurityPrices(t *testing.T) {
	svc, _ := testService(t, func(ctx context.Context, currency, date string) (domain.Rational, error) {
		return domain.MustRational(1, 1), nil
	})
	ctx := context.Background()
	boot(t, svc, "EUR")
	if _, err := svc.CreateCommodity(ctx, CreateCommodityInput{Code: "AAPL", Name: "Apple", MinorUnits: 0, Kind: domain.CommoditySecurity}); err != nil {
		t.Fatal(err)
	}
	aapl := "AAPL"
	broker, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Broker", Name: "Broker", AccountType: "asset", NativeCommodityID: &aapl})
	equity, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Equity:Opening", Name: "Opening", AccountType: "equity", NativeCommodityID: &aapl})
	_, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-16",
		Description:   ptr("Open shares"),
		Postings: []domain.PostingInput{
			{AccountID: equity.ID, Units: domain.Units{Minor: domain.Int(-10), CommodityID: "AAPL"}},
			{AccountID: broker.ID, Units: domain.Units{Minor: domain.Int(10), CommodityID: "AAPL"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ComputeNetWorth(ctx)
	requireCode(t, err, "MissingValuationPrice")
}
