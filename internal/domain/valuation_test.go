package domain

import (
	"math/big"
	"testing"
)

func testCommodities() []Commodity {
	return []Commodity{
		{ID: "EUR", Code: "EUR", Name: "Euro", MinorUnits: 2, Kind: CommodityCurrency},
		{ID: "USD", Code: "USD", Name: "US Dollar", MinorUnits: 2, Kind: CommodityCurrency},
		{ID: "GBP", Code: "GBP", Name: "British Pound", MinorUnits: 2, Kind: CommodityCurrency},
		{ID: "INR", Code: "INR", Name: "Indian Rupee", MinorUnits: 2, Kind: CommodityCurrency},
	}
}

func testAccounts() []Account {
	return []Account{
		{ID: "a-eur", Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: AccountAsset, NativeCommodityID: "EUR", CreatedAt: "2026-01-01T00:00:00.000Z"},
		{ID: "a-usd", Code: "Assets:Bank:USD", Name: "USD Checking", AccountType: AccountAsset, NativeCommodityID: "USD", CreatedAt: "2026-01-01T00:00:00.000Z"},
		{ID: "a-gbp", Code: "Assets:Bank:GBP", Name: "GBP Savings", AccountType: AccountAsset, NativeCommodityID: "GBP", CreatedAt: "2026-01-01T00:00:00.000Z"},
		{ID: "a-inr", Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: AccountAsset, NativeCommodityID: "INR", CreatedAt: "2026-01-01T00:00:00.000Z"},
	}
}

func TestComputeNetWorth_CurrentPrices(t *testing.T) {
	usd, err := RationalFromDecimal("0.92", 2)
	if err != nil {
		t.Fatal(err)
	}
	gbp, err := RationalFromDecimal("1.15", 2)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ComputeNetWorth(
		testAccounts(),
		[]AccountBalance{
			{AccountID: "a-eur", CommodityID: "EUR", Minor: Int(1_000_000)},
			{AccountID: "a-usd", CommodityID: "USD", Minor: Int(500_000)},
			{AccountID: "a-gbp", CommodityID: "GBP", Minor: Int(200_000)},
		},
		"EUR",
		[]MarketPrice{
			{BaseCommodityID: "USD", QuoteCommodityID: "EUR", Price: usd, ObservedAt: "2026-01-01", Source: "manual"},
			{BaseCommodityID: "GBP", QuoteCommodityID: "EUR", Price: gbp, ObservedAt: "2026-01-01", Source: "manual"},
		},
		testCommodities(),
		"2026-01-01",
	)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, report.NetWorthMinor, Int(1_690_000))
}

func TestComputeNetWorth_NonExactFX(t *testing.T) {
	rate, err := RationalFromPair(Int(1), Int(95))
	if err != nil {
		t.Fatal(err)
	}
	report, err := ComputeNetWorth(
		testAccounts(),
		[]AccountBalance{
			{AccountID: "a-eur", CommodityID: "EUR", Minor: Int(211_000)},
			{AccountID: "a-inr", CommodityID: "INR", Minor: Int(16_900_000)},
		},
		"EUR",
		[]MarketPrice{
			{BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: rate, ObservedAt: "2026-08-29", Source: "manual"},
		},
		testCommodities(),
		"2026-08-29",
	)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, report.NetWorthMinor, Int(388_895))
}

func TestComputeNetWorth_SpotChangeWithoutTouchingBalances(t *testing.T) {
	accounts := []Account{}
	for _, a := range testAccounts() {
		if a.ID != "a-gbp" {
			accounts = append(accounts, a)
		}
	}
	balances := []AccountBalance{
		{AccountID: "a-eur", CommodityID: "EUR", Minor: Int(0)},
		{AccountID: "a-usd", CommodityID: "USD", Minor: Int(500_000)},
	}
	lowPrice, _ := RationalFromDecimal("0.90", 2)
	highPrice, _ := RationalFromDecimal("1.00", 2)

	low, err := ComputeNetWorth(accounts, balances, "EUR", []MarketPrice{{
		BaseCommodityID: "USD", QuoteCommodityID: "EUR", Price: lowPrice, ObservedAt: "2026-01-01", Source: "manual",
	}}, testCommodities(), "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	high, err := ComputeNetWorth(accounts, balances, "EUR", []MarketPrice{{
		BaseCommodityID: "USD", QuoteCommodityID: "EUR", Price: highPrice, ObservedAt: "2026-02-01", Source: "manual",
	}}, testCommodities(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, low.NetWorthMinor, Int(450_000))
	mustEq(t, high.NetWorthMinor, Int(500_000))
}

func TestRetranslationExpenseMinor_MakesIncomeExpenseMatchNetWorth(t *testing.T) {
	accounts := []Account{
		{ID: "income", AccountType: AccountIncome, NativeCommodityID: "EUR"},
		{ID: "groc", AccountType: AccountExpense, NativeCommodityID: "EUR"},
		{ID: "eurBank", AccountType: AccountAsset, NativeCommodityID: "EUR"},
		{ID: "inrBank", AccountType: AccountAsset, NativeCommodityID: "INR"},
	}
	balances := []AccountBalance{
		{AccountID: "income", CommodityID: "EUR", Minor: Int(-700_000)},
		{AccountID: "groc", CommodityID: "EUR", Minor: Int(65_917)},
		{AccountID: "eurBank", CommodityID: "EUR", Minor: Int(595_100)},
		{AccountID: "inrBank", CommodityID: "INR", Minor: Int(3_200_000)},
	}
	rate, err := RationalFromPair(Int(400), Int(44_539))
	if err != nil {
		t.Fatal(err)
	}
	prices := []MarketPrice{{
		BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: rate, ObservedAt: "2026-08-26", Source: "ecb",
	}}
	ie, err := IncomeExpenseMinor(accounts, balances, "EUR", prices, testCommodities(), nil)
	if err != nil {
		t.Fatal(err)
	}
	nw, err := ComputeNetWorth(accounts, balances, "EUR", prices, testCommodities(), "2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	pending := RetranslationExpenseMinor(ie, nw.NetWorthMinor)
	if pending.Sign() <= 0 {
		t.Fatalf("expected unrealised loss, ie=%s nw=%s pending=%s", ie, nw.NetWorthMinor, pending)
	}
	mustEq(t, new(big.Int).Sub(ie, pending), nw.NetWorthMinor)
}

func TestLookupMarketPrice_InvertsMissingDirect(t *testing.T) {
	price, err := RationalFromDecimal("1.10", 2)
	if err != nil {
		t.Fatal(err)
	}
	found, err := LookupMarketPrice([]MarketPrice{{
		BaseCommodityID: "EUR", QuoteCommodityID: "USD", Price: price, ObservedAt: "2026-01-01", Source: "manual",
	}}, "USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	left := new(big.Int).Mul(found.Price.Numerator, Int(110))
	right := new(big.Int).Mul(found.Price.Denominator, Int(100))
	if left.Cmp(right) != 0 {
		t.Fatalf("numerator*110 != denominator*100")
	}
}
