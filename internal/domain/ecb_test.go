package domain

import "testing"

func TestSupportedCurrencyCodes(t *testing.T) {
	if !IsSupportedCurrency("EUR") || !IsSupportedCurrency("JPY") || !IsSupportedCurrency("INR") {
		t.Fatal("expected ECB allowlist currencies")
	}
	if IsSupportedCurrency("XXX") {
		t.Fatal("XXX must not be supported")
	}
	if len(SupportedCurrencyCodes) != 30 {
		t.Fatalf("expected 30 allowlisted codes, got %d", len(SupportedCurrencyCodes))
	}
}

func TestIsEcbQuoteCurrency(t *testing.T) {
	if !IsEcbQuoteCurrency("EUR") || !IsEcbQuoteCurrency("eur") || !IsEcbQuoteCurrency(" EUR ") {
		t.Fatal("EUR is the ECB quote unit")
	}
	if IsEcbQuoteCurrency("USD") || IsEcbQuoteCurrency("JPY") || IsEcbQuoteCurrency("") {
		t.Fatal("only EUR is the ECB quote unit")
	}
}

func TestIdentityPrice(t *testing.T) {
	price := IdentityPrice("INR")
	if price == nil || price.CommodityID != "INR" || price.Source != PriceSourceIdentity {
		t.Fatalf("%+v", price)
	}
	weight, err := ComputeWeight(PostingInput{
		AccountID: "a",
		Units:     Units{Minor: Int(100), CommodityID: "INR"},
		Price:     price,
	})
	if err != nil {
		t.Fatal(err)
	}
	if weight.Minor.Cmp(Int(100)) != 0 || weight.CommodityID != "INR" {
		t.Fatalf("got %+v", weight)
	}
}

func TestReportingMinor_ForeignWithoutCostErrors(t *testing.T) {
	spot, err := RationalFromPair(Int(1), Int(10))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReportingMinor(
		Units{Minor: Int(100), CommodityID: "INR"},
		nil,
		"EUR",
		[]MarketPrice{{BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: spot, ObservedAt: "x", Source: "ecb"}},
		map[string]Commodity{"INR": {ID: "INR", MinorUnits: 2}, "EUR": {ID: "EUR", MinorUnits: 2}},
	)
	de, ok := IsDomainError(err)
	if !ok || de.Code != "MissingFunctionalCost" {
		t.Fatalf("got %v", err)
	}
}

func TestIncomeExpenseFromPostings_ForeignWithoutCostErrors(t *testing.T) {
	spot, err := RationalFromPair(Int(1), Int(120))
	if err != nil {
		t.Fatal(err)
	}
	lines := []PnLPosting{
		{
			AccountCode: "Income:Job",
			AccountType: AccountIncome,
			Units:       Units{Minor: Int(-100_000), CommodityID: "EUR"},
		},
		{
			AccountCode: "Expenses:Family",
			AccountType: AccountExpense,
			Units:       Units{Minor: Int(1_050_000), CommodityID: "INR"},
		},
	}
	_, err = IncomeExpenseFromPostings(lines, "EUR", []MarketPrice{{
		BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: spot, ObservedAt: "2026-01-01", Source: "ecb",
	}}, testCommodities(), nil)
	de, ok := IsDomainError(err)
	if !ok || de.Code != "MissingFunctionalCost" {
		t.Fatalf("got %v", err)
	}
}
