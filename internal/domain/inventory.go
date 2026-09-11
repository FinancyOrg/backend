package domain

import "math/big"

// Lot is remaining native units of a monetary foreign-currency balance and
// the functional-currency carrying amount of those units (IAS 21 historical cost).
type Lot struct {
	UnitsMinor *big.Int
	CostMinor  *big.Int
}

// CostFromReportingAmount builds a per-unit cost so units × cost = reportingAmount.
// A zero reporting amount stores 0/1 so IE can ignore a native F residual.
func CostFromReportingAmount(unitsMinor, reportingMinor *big.Int, reportingCommodityID string, date *string) (*CostBasis, error) {
	if unitsMinor == nil || unitsMinor.Sign() == 0 || reportingMinor == nil {
		return nil, nil
	}
	if reportingMinor.Sign() == 0 {
		per, err := NormalizeRational(Rational{Numerator: big.NewInt(0), Denominator: big.NewInt(1)})
		if err != nil {
			return nil, err
		}
		return &CostBasis{PerUnit: per, CommodityID: reportingCommodityID, Date: date}, nil
	}
	per, err := NormalizeRational(Rational{
		Numerator:   new(big.Int).Abs(reportingMinor),
		Denominator: new(big.Int).Abs(unitsMinor),
	})
	if err != nil {
		return nil, err
	}
	return &CostBasis{PerUnit: per, CommodityID: reportingCommodityID, Date: date}, nil
}

func CloneCost(c *CostBasis) *CostBasis {
	if c == nil {
		return nil
	}
	out := CostBasis{PerUnit: c.PerUnit.Clone(), CommodityID: c.CommodityID}
	if c.Date != nil {
		d := *c.Date
		out.Date = &d
	}
	if c.Label != nil {
		l := *c.Label
		out.Label = &l
	}
	return &out
}

// AttachReportingCost stores lot basis in F and, when the posting is in another
// currency, an identity price so journal weight stays native.
func AttachReportingCost(posting *PostingInput, reportingMinor *big.Int, reportingCommodityID string, date *string) error {
	if posting == nil {
		return nil
	}
	cost, err := CostFromReportingAmount(posting.Units.Minor, reportingMinor, reportingCommodityID, date)
	if err != nil {
		return err
	}
	if cost == nil {
		return nil
	}
	posting.Cost = cost
	EnsureNativeWeight(posting, reportingCommodityID)
	return nil
}

func EnsureNativeWeight(posting *PostingInput, reportingCommodityID string) {
	if posting == nil || posting.Price != nil || posting.Cost == nil {
		return
	}
	if posting.Units.CommodityID == posting.Cost.CommodityID {
		return
	}
	if posting.Units.CommodityID == reportingCommodityID {
		return
	}
	posting.Price = IdentityPrice(posting.Units.CommodityID)
}

func CostReportingAmount(units Units, cost *CostBasis, reportingCommodityID string) *big.Int {
	if cost == nil || cost.CommodityID != reportingCommodityID || units.Minor == nil {
		return nil
	}
	got, err := MultiplyUnitsByRational(units.Minor, cost.PerUnit)
	if err != nil {
		return nil
	}
	return got
}

func AddLot(lots []Lot, unitsMinor, costMinor *big.Int) []Lot {
	u := new(big.Int).Abs(unitsMinor)
	c := new(big.Int).Abs(costMinor)
	if u.Sign() == 0 {
		return lots
	}
	return append(lots, Lot{UnitsMinor: u, CostMinor: c})
}

// AddOverdraft records a short (spend with no inventory) at the given F cost.
// Native 0 after a later cover requires this short so leftover carrying can clear.
func AddOverdraft(lots []Lot, unitsMinor, costMinor *big.Int) []Lot {
	u := new(big.Int).Abs(unitsMinor)
	c := new(big.Int).Abs(costMinor)
	if u.Sign() == 0 {
		return lots
	}
	short := Lot{UnitsMinor: new(big.Int).Neg(u), CostMinor: new(big.Int).Neg(c)}
	out := make([]Lot, 0, len(lots)+1)
	i := 0
	for i < len(lots) && lots[i].UnitsMinor.Sign() < 0 {
		out = append(out, lots[i])
		i++
	}
	out = append(out, short)
	return append(out, lots[i:]...)
}

// CoverInflow applies a long inflow against shorts FIFO, then adds leftover units.
// Covered units drop both the short carrying and the inflow cost allocated to them
// so empty native has 0 remaining carrying. realizedMinor is inflow alloc minus
// retired short carrying (goes to P&L on the covering journal when present).
func CoverInflow(lots []Lot, unitsMinor, costMinor *big.Int) (remaining []Lot, realizedMinor *big.Int) {
	u := new(big.Int).Abs(unitsMinor)
	c := new(big.Int).Abs(nilToZero(costMinor))
	realizedMinor = big.NewInt(0)
	if u.Sign() == 0 {
		return lots, realizedMinor
	}
	out := make([]Lot, 0, len(lots)+1)
	i := 0
	for i < len(lots) && lots[i].UnitsMinor.Sign() < 0 && u.Sign() > 0 {
		lot := lots[i]
		shortU := new(big.Int).Abs(lot.UnitsMinor)
		shortC := new(big.Int).Abs(lot.CostMinor)
		if u.Cmp(shortU) < 0 {
			partShortC := new(big.Int).Quo(new(big.Int).Mul(shortC, u), shortU)
			realizedMinor.Add(realizedMinor, new(big.Int).Sub(c, partShortC))
			leftU := new(big.Int).Sub(shortU, u)
			leftC := new(big.Int).Sub(shortC, partShortC)
			if leftU.Sign() > 0 {
				out = append(out, Lot{UnitsMinor: new(big.Int).Neg(leftU), CostMinor: new(big.Int).Neg(leftC)})
			}
			return append(out, lots[i+1:]...), realizedMinor
		}
		inflowAlloc := new(big.Int).Quo(new(big.Int).Mul(c, shortU), u)
		realizedMinor.Add(realizedMinor, new(big.Int).Sub(inflowAlloc, shortC))
		c = new(big.Int).Sub(c, inflowAlloc)
		u = new(big.Int).Sub(u, shortU)
		i++
	}
	if u.Sign() == 0 {
		realizedMinor.Add(realizedMinor, c)
		return append(out, lots[i:]...), realizedMinor
	}
	out = append(out, lots[i:]...)
	return AddLot(out, u, c), realizedMinor
}

// ConsumeLots removes qty native units FIFO and returns the functional cost of
// those units. Shortfall is returned when long inventory is insufficient.
// Negative lots (overdrafts) are left in place at the front.
func ConsumeLots(lots []Lot, qty *big.Int) (costMinor *big.Int, remaining []Lot, shortfall *big.Int) {
	need := new(big.Int).Abs(qty)
	costMinor = big.NewInt(0)
	if need.Sign() == 0 {
		return costMinor, lots, big.NewInt(0)
	}
	out := make([]Lot, 0, len(lots))
	i := 0
	for i < len(lots) && lots[i].UnitsMinor.Sign() < 0 {
		out = append(out, lots[i])
		i++
	}
	for i < len(lots) {
		if need.Sign() == 0 {
			out = append(out, lots[i:]...)
			break
		}
		lot := lots[i]
		if lot.UnitsMinor.Sign() <= 0 {
			out = append(out, lot)
			i++
			continue
		}
		if lot.UnitsMinor.Cmp(need) <= 0 {
			costMinor.Add(costMinor, lot.CostMinor)
			need.Sub(need, lot.UnitsMinor)
			i++
			continue
		}
		partCost := new(big.Int).Quo(new(big.Int).Mul(lot.CostMinor, need), lot.UnitsMinor)
		costMinor.Add(costMinor, partCost)
		leftUnits := new(big.Int).Sub(lot.UnitsMinor, need)
		leftCost := new(big.Int).Sub(lot.CostMinor, partCost)
		if leftUnits.Sign() > 0 {
			out = append(out, Lot{UnitsMinor: leftUnits, CostMinor: leftCost})
		} else if leftCost.Sign() != 0 {
			costMinor.Add(costMinor, leftCost)
		}
		out = append(out, lots[i+1:]...)
		need = big.NewInt(0)
		break
	}
	return costMinor, out, need
}

// ApplyLotDelta reconstructs remaining lots from a stored posting: longs via
// FIFO consume / cover, shorts from leftover units using leftover stored cost.
func ApplyLotDelta(lots []Lot, units, costMinor *big.Int) []Lot {
	if units == nil || units.Sign() == 0 {
		return lots
	}
	costMinor = nilToZero(costMinor)
	if units.Sign() > 0 {
		remaining, _ := CoverInflow(lots, units, costMinor)
		return remaining
	}
	consumed, remaining, short := ConsumeLots(lots, units)
	if short.Sign() == 0 {
		return remaining
	}
	stored := new(big.Int).Abs(costMinor)
	shortCost := new(big.Int).Sub(stored, consumed)
	if shortCost.Sign() < 0 {
		shortCost = big.NewInt(0)
	}
	if consumed.Sign() == 0 {
		shortCost = stored
	}
	return AddOverdraft(remaining, short, shortCost)
}

func LotCarrying(lots []Lot) *big.Int {
	total := big.NewInt(0)
	for _, lot := range lots {
		total.Add(total, lot.CostMinor)
	}
	return total
}

func ScaleReportingCost(srcUnits, srcReporting, destUnits *big.Int) *big.Int {
	if srcUnits == nil || destUnits == nil || srcReporting == nil {
		return nil
	}
	srcQty := new(big.Int).Abs(srcUnits)
	dstQty := new(big.Int).Abs(destUnits)
	srcCost := new(big.Int).Abs(srcReporting)
	if srcQty.Sign() == 0 {
		return nil
	}
	if srcQty.Cmp(dstQty) == 0 {
		return srcCost
	}
	return new(big.Int).Quo(new(big.Int).Mul(srcCost, dstQty), srcQty)
}

func nilToZero(n *big.Int) *big.Int {
	if n == nil {
		return big.NewInt(0)
	}
	return n
}
