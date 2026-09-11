package domain

import (
	"math/big"
	"strings"
	"testing"
)

func TestComputeWeight_UnitsOnly(t *testing.T) {
	weight, err := ComputeWeight(PostingInput{
		AccountID: "a",
		Units:     Units{Minor: Int(5000), CommodityID: "USD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weight.Minor.Cmp(Int(5000)) != 0 || weight.CommodityID != "USD" {
		t.Fatalf("got %+v", weight)
	}
}

func TestComputeWeight_UsesPrice(t *testing.T) {
	price, err := RationalFromDecimal("1.08", 2)
	if err != nil {
		t.Fatal(err)
	}
	weight, err := ComputeWeight(PostingInput{
		AccountID: "a",
		Units:     Units{Minor: Int(-100000), CommodityID: "EUR"},
		Price:     &TransactionPrice{PerUnit: price, CommodityID: "USD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weight.Minor.Cmp(Int(-108000)) != 0 || weight.CommodityID != "USD" {
		t.Fatalf("got %+v", weight)
	}
}

func TestComputeWeight_UsesPriceOverUnits(t *testing.T) {
	cost, err := RationalFromPair(Int(52345), Int(100))
	if err != nil {
		t.Fatal(err)
	}
	price, err := RationalFromPair(Int(1), Int(1))
	if err != nil {
		t.Fatal(err)
	}
	weight, err := ComputeWeight(PostingInput{
		AccountID: "a",
		Units:     Units{Minor: Int(11_200_00), CommodityID: "INR"},
		Cost:      &CostBasis{PerUnit: cost, CommodityID: "EUR"},
		Price:     &TransactionPrice{PerUnit: price, CommodityID: "INR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weight.Minor.Cmp(Int(11_200_00)) != 0 || weight.CommodityID != "INR" {
		t.Fatalf("got %+v", weight)
	}
}

func TestComputeWeight_IgnoresCost(t *testing.T) {
	cost, err := RationalFromPair(Int(52345), Int(100))
	if err != nil {
		t.Fatal(err)
	}
	weight, err := ComputeWeight(PostingInput{
		AccountID: "a",
		Units:     Units{Minor: Int(1000), CommodityID: "INR"},
		Cost:      &CostBasis{PerUnit: cost, CommodityID: "EUR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weight.Minor.Cmp(Int(1000)) != 0 || weight.CommodityID != "INR" {
		t.Fatalf("cost must not affect weight, got %+v", weight)
	}
}

func TestValidateBalance_AcceptsBalanced(t *testing.T) {
	resolved, err := ResolvePostings([]PostingInput{
		{AccountID: "a", Units: Units{Minor: Int(1000), CommodityID: "EUR"}},
		{AccountID: "b", Units: Units{Minor: Int(-1000), CommodityID: "EUR"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBalance(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBalance_RejectsUnbalanced(t *testing.T) {
	resolved, err := ResolvePostings([]PostingInput{
		{AccountID: "a", Units: Units{Minor: Int(1000), CommodityID: "EUR"}},
		{AccountID: "b", Units: Units{Minor: Int(-900), CommodityID: "EUR"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateBalance(resolved)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unbalanced") {
		t.Fatalf("expected unbalanced, got %v", err)
	}
}

func TestValidateBalance_CrossCurrencyViaPrice(t *testing.T) {
	price, err := RationalFromDecimal("1.08", 2)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolvePostings([]PostingInput{
		{
			AccountID: "eur",
			Units:     Units{Minor: Int(-100000), CommodityID: "EUR"},
			Price:     &TransactionPrice{PerUnit: price, CommodityID: "USD"},
		},
		{AccountID: "usd", Units: Units{Minor: Int(108000), CommodityID: "USD"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBalance(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestParseFormatJPY(t *testing.T) {
	if ParseMinor("1500", 0).Cmp(Int(1500)) != 0 {
		t.Fatal("parse")
	}
	if got := FormatMinor(Int(1500), 0); got != "1500.0" {
		t.Fatalf("format got %q", got)
	}
}

func TestRepeatingExchangeRateExact(t *testing.T) {
	rate, err := RationalFromPair(Int(1), Int(3))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MultiplyUnitsByRational(Int(300), rate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(Int(100)) != 0 {
		t.Fatalf("got %s", got)
	}
}

func TestRoundHalfAwayFromZero(t *testing.T) {
	got, err := RoundHalfAwayFromZero(Int(15), Int(10))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(Int(2)) != 0 {
		t.Fatalf("1.5 → got %s", got)
	}
	got, err = RoundHalfAwayFromZero(Int(-15), Int(10))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(Int(-2)) != 0 {
		t.Fatalf("-1.5 → got %s", got)
	}
	got, err = RoundHalfAwayFromZero(Int(16_900_000), Int(95))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(Int(177_895)) != 0 {
		t.Fatalf("16900000/95 → got %s", got)
	}
}

func TestConvertMinorAtRateINR(t *testing.T) {
	rate, err := RationalFromPair(Int(1), Int(95))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ConvertMinorAtRate(Int(16_900_000), rate, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(Int(177_895)) != 0 {
		t.Fatalf("got %s", got)
	}
}

func mustEq(t *testing.T, a, b *big.Int) {
	t.Helper()
	if a.Cmp(b) != 0 {
		t.Fatalf("%s != %s", a, b)
	}
}
