package domain

import "math/big"

type BuildExchangePostingsInput struct {
	FromAccountID            string
	ToAccountID              string
	TransactionCostAccountID string
	FromCommodityID          string
	ToCommodityID            string
	DefaultCommodityID       string
	// FromAmountMinor is positive minor units leaving the from account.
	FromAmountMinor *big.Int
	// ToAmountMinor is positive minor units entering the to account.
	ToAmountMinor     *big.Int
	FromMinorUnits    int
	ToMinorUnits      int
	DefaultMinorUnits int
	// RateToPerFrom is units of to-commodity per 1 unit of from-commodity (ECB-derived).
	RateToPerFrom Rational
	// RateDefaultPerTo is units of default-commodity per 1 unit of to-commodity.
	RateDefaultPerTo Rational
}

type BuiltExchange struct {
	Postings             []PostingInput
	ExpectedToMinor      *big.Int
	ResidualToMinor      *big.Int
	TransactionCostMinor *big.Int
	FromLegPrice         Rational
}

type ResidualCostInput struct {
	AccountID              string
	ResidualMinor          *big.Int
	ResidualCommodityID    string
	DefaultCommodityID     string
	ResidualMinorUnits     int
	DefaultMinorUnits      int
	RateDefaultPerResidual Rational
	Memo                   string
}

// ResidualCostPosting books a residual into an expense account in the default
// currency. If the residual is already in the default currency, units equal the
// residual. Otherwise the residual is converted at RateDefaultPerResidual and a
// transaction price keeps journal weights balanced in the residual commodity.
func ResidualCostPosting(input ResidualCostInput) (*PostingInput, *big.Int, error) {
	if input.ResidualMinor == nil || input.ResidualMinor.Sign() == 0 {
		return nil, big.NewInt(0), nil
	}
	memo := input.Memo
	if input.ResidualCommodityID == input.DefaultCommodityID {
		cost := cloneInt(input.ResidualMinor)
		return &PostingInput{
			AccountID: input.AccountID,
			Units:     Units{Minor: cost, CommodityID: input.DefaultCommodityID},
			Memo:      &memo,
		}, cost, nil
	}
	cost, err := ConvertMinorAtRate(
		input.ResidualMinor,
		input.RateDefaultPerResidual,
		input.ResidualMinorUnits,
		input.DefaultMinorUnits,
	)
	if err != nil {
		return nil, nil, err
	}
	if cost.Sign() == 0 {
		if input.ResidualMinor.Sign() > 0 {
			cost = big.NewInt(1)
		} else {
			cost = big.NewInt(-1)
		}
	}
	price, err := NormalizeRational(Rational{
		Numerator:   cloneInt(input.ResidualMinor),
		Denominator: cloneInt(cost),
	})
	if err != nil {
		return nil, nil, err
	}
	return &PostingInput{
		AccountID: input.AccountID,
		Units:     Units{Minor: cost, CommodityID: input.DefaultCommodityID},
		Price:     &TransactionPrice{PerUnit: price, CommodityID: input.ResidualCommodityID},
		Memo:      &memo,
	}, cost, nil
}

// CrossRateBPerA returns ECB rate for B per 1 A given each currency's ECB quote vs EUR (1 if EUR).
func CrossRateBPerA(rateAPerEur, rateBPerEur Rational) (Rational, error) {
	return NormalizeRational(Rational{
		Numerator:   new(big.Int).Mul(rateBPerEur.Numerator, rateAPerEur.Denominator),
		Denominator: new(big.Int).Mul(rateBPerEur.Denominator, rateAPerEur.Numerator),
	})
}

// BuildExchangePostings builds balanced exchange postings using an ECB reference rate.
// Bank vs ECB residual → Expenses:Transaction Cost in the default currency.
//
// Weights balance in the to-commodity:
//
//	−expectedTo + actualTo + residualTo = 0
func BuildExchangePostings(input BuildExchangePostingsInput) (BuiltExchange, error) {
	if input.FromAmountMinor.Sign() <= 0 || input.ToAmountMinor.Sign() <= 0 {
		return BuiltExchange{}, InvalidExchange("fromAmountMinor and toAmountMinor must be positive")
	}
	if input.FromAccountID == input.ToAccountID {
		return BuiltExchange{}, InvalidExchange("from and to accounts must differ")
	}
	if input.FromCommodityID == input.ToCommodityID {
		return BuiltExchange{}, InvalidExchange("from and to commodities must differ")
	}

	expectedToMinor, err := ConvertMinorAtRate(
		input.FromAmountMinor,
		input.RateToPerFrom,
		input.FromMinorUnits,
		input.ToMinorUnits,
	)
	if err != nil {
		return BuiltExchange{}, err
	}
	residualToMinor := new(big.Int).Sub(expectedToMinor, input.ToAmountMinor)
	fromLegPrice, err := NormalizeRational(Rational{
		Numerator:   cloneInt(expectedToMinor),
		Denominator: cloneInt(input.FromAmountMinor),
	})
	if err != nil {
		return BuiltExchange{}, err
	}

	outMemo := "Exchange out"
	inMemo := "Exchange in"
	postings := []PostingInput{
		{
			AccountID: input.FromAccountID,
			Units:     Units{Minor: new(big.Int).Neg(input.FromAmountMinor), CommodityID: input.FromCommodityID},
			Price:     &TransactionPrice{PerUnit: fromLegPrice, CommodityID: input.ToCommodityID},
			Memo:      &outMemo,
		},
		{
			AccountID: input.ToAccountID,
			Units:     Units{Minor: cloneInt(input.ToAmountMinor), CommodityID: input.ToCommodityID},
			Memo:      &inMemo,
		},
	}

	costMemo := "Transaction cost vs ECB"
	costPosting, txnCostMinor, err := ResidualCostPosting(ResidualCostInput{
		AccountID:              input.TransactionCostAccountID,
		ResidualMinor:          residualToMinor,
		ResidualCommodityID:    input.ToCommodityID,
		DefaultCommodityID:     input.DefaultCommodityID,
		ResidualMinorUnits:     input.ToMinorUnits,
		DefaultMinorUnits:      input.DefaultMinorUnits,
		RateDefaultPerResidual: input.RateDefaultPerTo,
		Memo:                   costMemo,
	})
	if err != nil {
		return BuiltExchange{}, err
	}
	if costPosting != nil {
		postings = append(postings, *costPosting)
	}

	return BuiltExchange{
		Postings:             postings,
		ExpectedToMinor:      expectedToMinor,
		ResidualToMinor:      residualToMinor,
		TransactionCostMinor: txnCostMinor,
		FromLegPrice:         fromLegPrice,
	}, nil
}
