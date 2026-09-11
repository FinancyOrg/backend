package ledger

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
)

type PostTransactionInput struct {
	CreditAccountID   string
	DebitAccountID    string
	CreditAmountMinor *big.Int
	DebitAmountMinor  *big.Int
	Datetime          *string
	Description       *string
}

type PostTransactionResult struct {
	Entry                domain.JournalEntry
	TransactionCostMinor string
}

func (s *Service) PostTransaction(ctx context.Context, input PostTransactionInput) (PostTransactionResult, error) {
	defaultID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return PostTransactionResult{}, err
	}
	if strings.TrimSpace(input.CreditAccountID) == "" || strings.TrimSpace(input.DebitAccountID) == "" {
		return PostTransactionResult{}, domain.InvalidTransaction("creditAccountId and debitAccountId are required")
	}
	if input.CreditAccountID == input.DebitAccountID {
		return PostTransactionResult{}, domain.InvalidTransaction("credit and debit accounts must differ")
	}
	if input.CreditAmountMinor == nil || input.CreditAmountMinor.Sign() <= 0 {
		return PostTransactionResult{}, domain.InvalidTransaction("creditAmountMinor must be positive")
	}
	if input.DebitAmountMinor != nil && input.DebitAmountMinor.Sign() <= 0 {
		return PostTransactionResult{}, domain.InvalidTransaction("debitAmountMinor must be positive")
	}

	datetime := domain.NowUTC()
	if input.Datetime != nil && strings.TrimSpace(*input.Datetime) != "" {
		datetime, err = domain.CanonicalUTC(*input.Datetime)
		if err != nil {
			return PostTransactionResult{}, err
		}
		if err := s.rejectFutureDatetime(ctx, datetime); err != nil {
			return PostTransactionResult{}, err
		}
	}
	ecbDate, err := s.civilDateOf(ctx, datetime)
	if err != nil {
		return PostTransactionResult{}, err
	}

	credit, err := s.requireAccount(ctx, input.CreditAccountID)
	if err != nil {
		return PostTransactionResult{}, err
	}
	debit, err := s.requireAccount(ctx, input.DebitAccountID)
	if err != nil {
		return PostTransactionResult{}, err
	}
	creditCcy, err := s.requireCommodity(ctx, credit.NativeCommodityID)
	if err != nil {
		return PostTransactionResult{}, err
	}
	debitCcy, err := s.requireCommodity(ctx, debit.NativeCommodityID)
	if err != nil {
		return PostTransactionResult{}, err
	}
	defaultCcy, err := s.requireCommodity(ctx, defaultID)
	if err != nil {
		return PostTransactionResult{}, err
	}

	txnCost, err := s.ensureTransactionCostAccount(ctx, defaultCcy.ID)
	if err != nil {
		return PostTransactionResult{}, err
	}

	desc := fmt.Sprintf("Transaction %s → %s", credit.Code, debit.Code)
	if input.Description != nil && strings.TrimSpace(*input.Description) != "" {
		desc = strings.TrimSpace(*input.Description)
	}

	var postings []domain.PostingInput
	txnCostMinor := big.NewInt(0)

	if creditCcy.ID == debitCcy.ID {
		debitAmt := cloneBig(input.CreditAmountMinor)
		if input.DebitAmountMinor != nil {
			debitAmt = cloneBig(input.DebitAmountMinor)
		}
		creditMemo := "Credit"
		debitMemo := "Debit"
		postings = []domain.PostingInput{
			{
				AccountID: credit.ID,
				Units:     domain.Units{Minor: new(big.Int).Neg(input.CreditAmountMinor), CommodityID: creditCcy.ID},
				Memo:      &creditMemo,
			},
			{
				AccountID: debit.ID,
				Units:     domain.Units{Minor: debitAmt, CommodityID: debitCcy.ID},
				Memo:      &debitMemo,
			},
		}
		residual := new(big.Int).Sub(input.CreditAmountMinor, debitAmt)
		if residual.Sign() != 0 {
			rateDefaultPerResidual := domain.MustRational(1, 1)
			if creditCcy.ID != defaultCcy.ID {
				txnObs, err := FetchEcbRatePerEurWithLookback(ctx, creditCcy.ID, ecbDate, 10, s.fetchEcbRate)
				if err != nil {
					return PostTransactionResult{}, err
				}
				defaultObs, err := FetchEcbRatePerEurWithLookback(ctx, defaultCcy.ID, txnObs.ObservedDate, 10, s.fetchEcbRate)
				if err != nil {
					return PostTransactionResult{}, err
				}
				rateDefaultPerResidual, err = domain.CrossRateBPerA(txnObs.Rate, defaultObs.Rate)
				if err != nil {
					return PostTransactionResult{}, err
				}
			}
			costMemo := "Transaction cost"
			costPosting, cost, err := domain.ResidualCostPosting(domain.ResidualCostInput{
				AccountID:              txnCost.ID,
				ResidualMinor:          residual,
				ResidualCommodityID:    creditCcy.ID,
				DefaultCommodityID:     defaultCcy.ID,
				ResidualMinorUnits:     creditCcy.MinorUnits,
				DefaultMinorUnits:      defaultCcy.MinorUnits,
				RateDefaultPerResidual: rateDefaultPerResidual,
				Memo:                   costMemo,
			})
			if err != nil {
				return PostTransactionResult{}, err
			}
			txnCostMinor = cost
			if costPosting != nil {
				postings = append(postings, *costPosting)
			}
		}
	} else {
		debitAmt := input.DebitAmountMinor
		fromObs, err := FetchEcbRatePerEurWithLookback(ctx, creditCcy.ID, ecbDate, 10, s.fetchEcbRate)
		if err != nil {
			return PostTransactionResult{}, err
		}
		toObs, err := FetchEcbRatePerEurWithLookback(ctx, debitCcy.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
		if err != nil {
			return PostTransactionResult{}, err
		}
		defaultObs, err := FetchEcbRatePerEurWithLookback(ctx, defaultCcy.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
		if err != nil {
			return PostTransactionResult{}, err
		}
		rateToPerFrom, err := domain.CrossRateBPerA(fromObs.Rate, toObs.Rate)
		if err != nil {
			return PostTransactionResult{}, err
		}
		rateDefaultPerTo, err := domain.CrossRateBPerA(toObs.Rate, defaultObs.Rate)
		if err != nil {
			return PostTransactionResult{}, err
		}
		if debitAmt == nil {
			debitAmt, err = domain.ConvertMinorAtRate(
				input.CreditAmountMinor, rateToPerFrom, creditCcy.MinorUnits, debitCcy.MinorUnits,
			)
			if err != nil {
				return PostTransactionResult{}, err
			}
			if debitAmt.Sign() <= 0 {
				return PostTransactionResult{}, domain.InvalidTransaction("converted debit amount must be positive")
			}
		}
		built, err := domain.BuildExchangePostings(domain.BuildExchangePostingsInput{
			FromAccountID:            credit.ID,
			ToAccountID:              debit.ID,
			TransactionCostAccountID: txnCost.ID,
			FromCommodityID:          creditCcy.ID,
			ToCommodityID:            debitCcy.ID,
			DefaultCommodityID:       defaultCcy.ID,
			FromAmountMinor:          input.CreditAmountMinor,
			ToAmountMinor:            debitAmt,
			FromMinorUnits:           creditCcy.MinorUnits,
			ToMinorUnits:             debitCcy.MinorUnits,
			DefaultMinorUnits:        defaultCcy.MinorUnits,
			RateToPerFrom:            rateToPerFrom,
			RateDefaultPerTo:         rateDefaultPerTo,
		})
		if err != nil {
			return PostTransactionResult{}, err
		}
		postings = built.Postings
		txnCostMinor = built.TransactionCostMinor
	}

	entry, err := s.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: datetime,
		PostedAt:      &datetime,
		Description:   &desc,
		Postings:      postings,
	})
	if err != nil {
		return PostTransactionResult{}, err
	}
	if err := s.RecordAudit(ctx, "transaction.posted", map[string]any{
		"journalEntryId":       entry.ID,
		"creditAccountId":      credit.ID,
		"debitAccountId":       debit.ID,
		"datetime":             datetime,
		"transactionCostMinor": txnCostMinor.String(),
	}); err != nil {
		return PostTransactionResult{}, err
	}
	return PostTransactionResult{
		Entry:                entry,
		TransactionCostMinor: txnCostMinor.String(),
	}, nil
}

func (s *Service) requireAccount(ctx context.Context, id string) (domain.Account, error) {
	a, err := s.GetAccount(ctx, id)
	if err != nil {
		return domain.Account{}, err
	}
	if a == nil {
		return domain.Account{}, domain.AccountNotFound(id)
	}
	return *a, nil
}

func (s *Service) requireCommodity(ctx context.Context, id string) (domain.Commodity, error) {
	c, err := s.GetCommodity(ctx, id)
	if err != nil {
		return domain.Commodity{}, err
	}
	if c == nil {
		return domain.Commodity{}, domain.InvalidCurrency("Unknown commodity: " + id)
	}
	return *c, nil
}

func cloneBig(n *big.Int) *big.Int {
	if n == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(n)
}
