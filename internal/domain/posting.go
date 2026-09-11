package domain

import (
	"fmt"
	"math/big"
)

// ComputeWeight is the journal-balance amount:
//  1. Transaction price, if present → weight in price.commodity = units × price (exact)
//  2. Else units in the posting commodity
func ComputeWeight(posting PostingInput) (Weight, error) {
	if posting.Price != nil {
		minor, err := MultiplyUnitsByRational(posting.Units.Minor, posting.Price.PerUnit)
		if err != nil {
			return Weight{}, err
		}
		return Weight{Minor: minor, CommodityID: posting.Price.CommodityID}, nil
	}
	return Weight{
		Minor:       cloneInt(posting.Units.Minor),
		CommodityID: posting.Units.CommodityID,
	}, nil
}

// IdentityPrice keeps journal weight in the posting's native commodity.
func IdentityPrice(commodityID string) *TransactionPrice {
	r, _ := RationalFromPair(big.NewInt(1), big.NewInt(1))
	return &TransactionPrice{
		PerUnit:     r,
		CommodityID: commodityID,
		Source:      PriceSourceIdentity,
	}
}

func ResolvePostings(postings []PostingInput) ([]ResolvedPosting, error) {
	if len(postings) < 2 {
		return nil, InvalidJournalEntry("Journal entry must contain at least two postings")
	}
	out := make([]ResolvedPosting, len(postings))
	for i, posting := range postings {
		weight, err := ComputeWeight(posting)
		if err != nil {
			return nil, err
		}
		out[i] = ResolvedPosting{
			PostingInput: posting,
			LineOrder:    i,
			Weight:       weight,
		}
	}
	return out, nil
}

func ValidatePostingUnits(units Units) error {
	if units.Minor == nil || units.Minor.Sign() == 0 {
		return InvalidAmount("Posting units cannot be zero")
	}
	return nil
}

func ValidateCostAndPrice(cost *CostBasis, price *TransactionPrice) error {
	if cost != nil && cost.PerUnit.Denominator.Sign() == 0 {
		return InvalidAmount("Cost denominator cannot be zero")
	}
	if price != nil && price.PerUnit.Denominator.Sign() == 0 {
		return InvalidAmount("Price denominator cannot be zero")
	}
	return nil
}

// ValidateBalance checks that the sum of posting weights is zero independently
// for each weight currency.
func ValidateBalance(resolved []ResolvedPosting) error {
	totals := map[string]*big.Int{}
	for _, posting := range resolved {
		cur := totals[posting.Weight.CommodityID]
		if cur == nil {
			cur = big.NewInt(0)
			totals[posting.Weight.CommodityID] = cur
		}
		cur.Add(cur, posting.Weight.Minor)
	}

	var imbalances []string
	for commodityID, total := range totals {
		if total.Sign() != 0 {
			imbalances = append(imbalances, fmt.Sprintf("%s=%s", commodityID, total.String()))
		}
	}
	if len(imbalances) > 0 {
		msg := imbalances[0]
		for i := 1; i < len(imbalances); i++ {
			msg += ", " + imbalances[i]
		}
		return UnbalancedJournalEntry(msg)
	}
	return nil
}
