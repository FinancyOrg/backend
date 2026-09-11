package domain

import "testing"

func TestConsumeLots_FIFO(t *testing.T) {
	lots := []Lot{
		{UnitsMinor: Int(100), CostMinor: Int(10)},
		{UnitsMinor: Int(100), CostMinor: Int(20)},
	}
	cost, remaining, short := ConsumeLots(lots, Int(150))
	mustEq(t, cost, Int(20))
	mustEq(t, short, Int(0))
	if len(remaining) != 1 || remaining[0].UnitsMinor.Cmp(Int(50)) != 0 || remaining[0].CostMinor.Cmp(Int(10)) != 0 {
		t.Fatalf("%+v", remaining)
	}
}

func TestConsumeLots_Shortfall(t *testing.T) {
	cost, remaining, short := ConsumeLots(nil, Int(50))
	mustEq(t, cost, Int(0))
	mustEq(t, short, Int(50))
	if len(remaining) != 0 {
		t.Fatalf("%+v", remaining)
	}
}

func TestConsumeLots_PreservesOverdraft(t *testing.T) {
	lots := AddOverdraft(nil, Int(40), Int(8))
	lots = AddLot(lots, Int(100), Int(20))
	cost, remaining, short := ConsumeLots(lots, Int(50))
	mustEq(t, cost, Int(10))
	mustEq(t, short, Int(0))
	if len(remaining) != 2 {
		t.Fatalf("%+v", remaining)
	}
	mustEq(t, remaining[0].UnitsMinor, Int(-40))
	mustEq(t, remaining[0].CostMinor, Int(-8))
	mustEq(t, remaining[1].UnitsMinor, Int(50))
	mustEq(t, remaining[1].CostMinor, Int(10))
}

func TestCoverInflow_ClearsMatchingOverdraft(t *testing.T) {
	lots := AddOverdraft(nil, Int(50), Int(9))
	remaining, realized := CoverInflow(lots, Int(50), Int(11))
	if len(remaining) != 0 {
		t.Fatalf("empty native must drop leftover carrying: %+v", remaining)
	}
	mustEq(t, realized, Int(2))
	mustEq(t, LotCarrying(remaining), Int(0))
}

func TestApplyLotDelta_OverdraftThenCoverIsEmpty(t *testing.T) {
	lots := ApplyLotDelta(nil, Int(-50), Int(-9))
	if len(lots) != 1 || lots[0].UnitsMinor.Sign() >= 0 {
		t.Fatalf("%+v", lots)
	}
	lots = ApplyLotDelta(lots, Int(50), Int(11))
	if len(lots) != 0 {
		t.Fatalf("%+v", lots)
	}
}

func TestCostFromReportingAmount_Zero(t *testing.T) {
	cost, err := CostFromReportingAmount(Int(71), Int(0), "EUR", nil)
	if err != nil || cost == nil {
		t.Fatal(err, cost)
	}
	got, err := MultiplyUnitsByRational(Int(71), cost.PerUnit)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, got, Int(0))
}

func TestCostFromReportingAmount_Exact(t *testing.T) {
	cost, err := CostFromReportingAmount(Int(2_100_000), Int(20_000), "EUR", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MultiplyUnitsByRational(Int(2_100_000), cost.PerUnit)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, got, Int(20_000))
	neg, err := MultiplyUnitsByRational(Int(-2_100_000), cost.PerUnit)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, neg, Int(-20_000))
}

func TestIncomeExpenseFromPostings_UsesLotCostNotSpot(t *testing.T) {
	cost, err := CostFromReportingAmount(Int(1_050_000), Int(10_000), "EUR", nil)
	if err != nil {
		t.Fatal(err)
	}
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
			Cost:        cost,
		},
	}
	ie, err := IncomeExpenseFromPostings(lines, "EUR", []MarketPrice{{
		BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: spot, ObservedAt: "2026-01-01", Source: "manual",
	}}, testCommodities(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, ie, Int(90_000))
}

func TestAttachReportingCost_SetsIdentityPrice(t *testing.T) {
	p := PostingInput{AccountID: "inrBank", Units: Units{Minor: Int(2_100_000), CommodityID: "INR"}}
	if err := AttachReportingCost(&p, Int(20_000), "EUR", nil); err != nil {
		t.Fatal(err)
	}
	if p.Cost == nil || p.Price == nil || p.Price.CommodityID != "INR" {
		t.Fatalf("%+v", p)
	}
	w, err := ComputeWeight(p)
	if err != nil {
		t.Fatal(err)
	}
	if w.CommodityID != "INR" || w.Minor.Cmp(Int(2_100_000)) != 0 {
		t.Fatalf("%+v", w)
	}
}

func TestLotCarrying(t *testing.T) {
	if LotCarrying(nil).Sign() != 0 {
		t.Fatal("empty")
	}
	got := LotCarrying(AddLot(nil, Int(-100), Int(-25)))
	mustEq(t, got, Int(25))
}

func TestReportingMinor_PrefersCost(t *testing.T) {
	cost, err := CostFromReportingAmount(Int(100), Int(3), "EUR", nil)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := RationalFromPair(Int(1), Int(10))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReportingMinor(
		Units{Minor: Int(100), CommodityID: "INR"},
		cost,
		"EUR",
		[]MarketPrice{{BaseCommodityID: "INR", QuoteCommodityID: "EUR", Price: spot, ObservedAt: "x", Source: "t"}},
		map[string]Commodity{"INR": {ID: "INR", MinorUnits: 2}, "EUR": {ID: "EUR", MinorUnits: 2}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, got, Int(3))
}

func TestReportingMinor_FunctionalNativeUsesCostWhenPresent(t *testing.T) {
	cost, err := CostFromReportingAmount(Int(2_946), Int(2_931), "EUR", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReportingMinor(
		Units{Minor: Int(2_946), CommodityID: "EUR"},
		cost,
		"EUR",
		nil,
		map[string]Commodity{"EUR": {ID: "EUR", MinorUnits: 2}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mustEq(t, got, Int(2_931))
}
