package ledger

import (
	"context"
	"log"
	"math/big"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
)

type PostRetranslationInput struct {
	Datetime    *string
	AmountMinor *big.Int
}

type RetranslationResult struct {
	Preview        domain.RetranslationPreview
	BookedMinor    *big.Int
	Entry          *domain.JournalEntry
	ExpenseAccount *domain.Account
	OffsetAccount  *domain.Account
}

func (s *Service) PreviewRetranslation(ctx context.Context) (domain.RetranslationPreview, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	asOf := nowISO()
	asOfDate := domain.UTCDate(asOf)
	if s.cache != nil {
		preview, hit, err := s.cache.GetRetranslation(ctx, reportingID, asOfDate)
		if err != nil {
			log.Printf("retranslate cache get: %v", err)
		} else if hit {
			return preview, nil
		}
	}
	preview, err := s.previewRetranslationUncached(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	if s.cache != nil {
		if putErr := s.cache.PutRetranslation(ctx, preview); putErr != nil {
			log.Printf("retranslate cache put: %v", putErr)
		}
	}
	return preview, nil
}

func (s *Service) previewRetranslationUncached(ctx context.Context) (domain.RetranslationPreview, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	balances, err := s.GetAccountBalances(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	prices, err := s.marketPricesForBalances(ctx, balances, reportingID, todayISO())
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	asOf := nowISO()
	nw, err := domain.ComputeNetWorth(accounts, balances, reportingID, prices, commodities, asOf)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	pnl, err := s.ListPnLPostings(ctx)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	ie, err := domain.IncomeExpenseFromPostings(pnl, reportingID, prices, commodities, nil)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	return domain.RetranslationPreview{
		ReportingCommodityID: reportingID,
		RetranslationMinor:   domain.RetranslationExpenseMinor(ie, nw.NetWorthMinor),
		NetWorthMinor:        nw.NetWorthMinor,
		IncomeExpenseMinor:   ie,
		AsOf:                 asOf,
	}, nil
}

func (s *Service) PostRetranslation(ctx context.Context, input PostRetranslationInput) (RetranslationResult, error) {
	preview, err := s.PreviewRetranslation(ctx)
	if err != nil {
		return RetranslationResult{}, err
	}
	if preview.RetranslationMinor.Sign() == 0 {
		return RetranslationResult{Preview: preview, BookedMinor: big.NewInt(0)}, nil
	}

	booked, err := resolveRetranslationAmount(preview.RetranslationMinor, input.AmountMinor)
	if err != nil {
		return RetranslationResult{}, err
	}

	datetime := nowISO()
	if input.Datetime != nil && strings.TrimSpace(*input.Datetime) != "" {
		datetime, err = domain.CanonicalUTC(*input.Datetime)
		if err != nil {
			return RetranslationResult{}, err
		}
	}

	expense, err := s.ensureExpenseAccount(ctx, ForexRetranslationExpenseCode, "Forex Retranslation", preview.ReportingCommodityID)
	if err != nil {
		return RetranslationResult{}, err
	}
	// Capital Gains is real earnings. Offsetting there would wash the expense
	// back out of income − expense. Equity is outside both IE and net worth.
	offset, err := s.ensureAccount(ctx, RetranslationEquityCode, "Retranslation", string(domain.AccountEquity), preview.ReportingCommodityID)
	if err != nil {
		return RetranslationResult{}, err
	}

	desc := "Foreign Currency Retranslation"
	expenseMemo := "Forex retranslation"
	offsetMemo := "Forex retranslation"
	entry, err := s.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: datetime,
		PostedAt:      &datetime,
		Description:   &desc,
		Postings: []domain.PostingInput{
			{
				AccountID: expense.ID,
				Units:     domain.Units{Minor: cloneBig(booked), CommodityID: preview.ReportingCommodityID},
				Memo:      &expenseMemo,
			},
			{
				AccountID: offset.ID,
				Units:     domain.Units{Minor: new(big.Int).Neg(booked), CommodityID: preview.ReportingCommodityID},
				Memo:      &offsetMemo,
			},
		},
	})
	if err != nil {
		return RetranslationResult{}, err
	}
	if err := s.RecordAudit(ctx, "retranslation.posted", map[string]any{
		"journalEntryId":       entry.ID,
		"retranslationMinor":   booked.String(),
		"pendingMinor":         preview.RetranslationMinor.String(),
		"datetime":             datetime,
		"reportingCommodityId": preview.ReportingCommodityID,
	}); err != nil {
		return RetranslationResult{}, err
	}
	after, err := s.PreviewRetranslation(ctx)
	if err != nil {
		return RetranslationResult{}, err
	}
	return RetranslationResult{
		Preview:        after,
		BookedMinor:    booked,
		Entry:          &entry,
		ExpenseAccount: &expense,
		OffsetAccount:  &offset,
	}, nil
}

func resolveRetranslationAmount(pending, requested *big.Int) (*big.Int, error) {
	if requested == nil {
		return cloneBig(pending), nil
	}
	if requested.Sign() == 0 {
		return nil, domain.InvalidAmount("amountMinor must be non-zero")
	}
	if requested.Sign() == pending.Sign() && requested.CmpAbs(pending) > 0 {
		return nil, domain.InvalidAmount("amountMinor cannot exceed pending retranslation")
	}
	return cloneBig(requested), nil
}
