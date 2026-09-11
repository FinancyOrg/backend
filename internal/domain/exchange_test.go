package domain

import "testing"

func TestBuildExchangePostings_BooksFXCostWhenBankWorse(t *testing.T) {
	rate, err := RationalFromDecimal("105", 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := RationalFromPair(Int(1), Int(105))
	if err != nil {
		t.Fatal(err)
	}
	built, err := BuildExchangePostings(BuildExchangePostingsInput{
		FromAccountID:            "eurBank",
		ToAccountID:              "inrBank",
		TransactionCostAccountID: "txn-cost",
		FromCommodityID:          "EUR",
		ToCommodityID:            "INR",
		DefaultCommodityID:       "EUR",
		FromAmountMinor:          Int(200_000),
		ToAmountMinor:            Int(19_000_000),
		FromMinorUnits:           2,
		ToMinorUnits:             2,
		DefaultMinorUnits:        2,
		RateToPerFrom:            rate,
		RateDefaultPerTo:         inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, built.ExpectedToMinor, Int(21_000_000))
	mustEq(t, built.ResidualToMinor, Int(2_000_000))
	if built.TransactionCostMinor.Sign() <= 0 {
		t.Fatalf("expected positive transaction cost, got %s", built.TransactionCostMinor)
	}
	if len(built.Postings) != 3 {
		t.Fatalf("postings %d", len(built.Postings))
	}
	resolved, err := ResolvePostings(built.Postings)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBalance(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestBuildExchangePostings_BooksFXGainWhenBankBeatsECB(t *testing.T) {
	rate, err := RationalFromDecimal("100", 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := RationalFromPair(Int(1), Int(100))
	if err != nil {
		t.Fatal(err)
	}
	built, err := BuildExchangePostings(BuildExchangePostingsInput{
		FromAccountID:            "eurBank",
		ToAccountID:              "inrBank",
		TransactionCostAccountID: "txn-cost",
		FromCommodityID:          "EUR",
		ToCommodityID:            "INR",
		DefaultCommodityID:       "EUR",
		FromAmountMinor:          Int(200_000),
		ToAmountMinor:            Int(22_000_000),
		FromMinorUnits:           2,
		ToMinorUnits:             2,
		DefaultMinorUnits:        2,
		RateToPerFrom:            rate,
		RateDefaultPerTo:         inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, built.ResidualToMinor, Int(-2_000_000))
	if built.TransactionCostMinor.Sign() >= 0 {
		t.Fatalf("expected negative transaction cost, got %s", built.TransactionCostMinor)
	}
	resolved, err := ResolvePostings(built.Postings)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBalance(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestBuildExchangePostings_OmitsTransactionCostWhenExact(t *testing.T) {
	rate, err := RationalFromDecimal("90", 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := RationalFromPair(Int(1), Int(90))
	if err != nil {
		t.Fatal(err)
	}
	built, err := BuildExchangePostings(BuildExchangePostingsInput{
		FromAccountID:            "eurBank",
		ToAccountID:              "inrBank",
		TransactionCostAccountID: "txn-cost",
		FromCommodityID:          "EUR",
		ToCommodityID:            "INR",
		DefaultCommodityID:       "EUR",
		FromAmountMinor:          Int(200_000),
		ToAmountMinor:            Int(18_000_000),
		FromMinorUnits:           2,
		ToMinorUnits:             2,
		DefaultMinorUnits:        2,
		RateToPerFrom:            rate,
		RateDefaultPerTo:         inv,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, built.ResidualToMinor, Int(0))
	if len(built.Postings) != 2 {
		t.Fatalf("postings %d", len(built.Postings))
	}
	resolved, err := ResolvePostings(built.Postings)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBalance(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestCrossRateBPerA(t *testing.T) {
	usdPerEur, err := RationalFromDecimal("1.10", 2)
	if err != nil {
		t.Fatal(err)
	}
	inrPerEur, err := RationalFromDecimal("100", 0)
	if err != nil {
		t.Fatal(err)
	}
	inrPerUsd, err := CrossRateBPerA(usdPerEur, inrPerEur)
	if err != nil {
		t.Fatal(err)
	}
	got := float64(inrPerUsd.Numerator.Int64()) / float64(inrPerUsd.Denominator.Int64())
	want := 100.0 / 1.1
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-5 {
		t.Fatalf("got %f want %f", got, want)
	}
}
