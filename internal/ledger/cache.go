package ledger

import (
	"context"
	"log"

	"github.com/FinancyOrg/backend/internal/domain"
)

// Cache document kinds. Invalidate(kinds...) deletes by these values.
// There is no TTL — docs live until a mutation invalidates them or ClearLedger runs.
const (
	CacheKindAccounts    = "accounts"
	CacheKindCommodities = "commodities"
	CacheKindBalances    = "balances"
	CacheKindNetWorth    = "networth"
	CacheKindPeriods     = "periods"
	CacheKindRetranslate = "retranslate"
	CacheKindPostings    = "postings" // legacy alias; journal docs use CacheKindJournal
	CacheKindView        = "view"
	CacheKindJournal     = "journal"
)

// Cache is the Mongo snapshot store for CRDB reads. A nil Cache on
// Service (local `make backend`, tests) is a no-op: every call hits SQL.
type Cache interface {
	GetAccounts(ctx context.Context) (items []domain.Account, hit bool, err error)
	PutAccounts(ctx context.Context, items []domain.Account) error
	GetCommodities(ctx context.Context) (items []domain.Commodity, hit bool, err error)
	PutCommodities(ctx context.Context, items []domain.Commodity) error
	GetBalances(ctx context.Context) (items []domain.AccountBalance, hit bool, err error)
	PutBalances(ctx context.Context, items []domain.AccountBalance) error
	GetNetWorth(ctx context.Context, presentationCommodityID, asOfDate string) (report domain.NetWorthReport, hit bool, err error)
	PutNetWorth(ctx context.Context, report domain.NetWorthReport) error
	GetPeriods(ctx context.Context, presentationCommodityID, timezone string, ledgerRevision int64) (result ViewPeriodsResult, hit bool, err error)
	PutPeriods(ctx context.Context, result ViewPeriodsResult, ledgerRevision int64) error
	GetRetranslation(ctx context.Context, presentationCommodityID, asOfDate string) (preview domain.RetranslationPreview, hit bool, err error)
	PutRetranslation(ctx context.Context, preview domain.RetranslationPreview) error
	GetPostings(ctx context.Context, journalIDs []string) (found map[string][]domain.ResolvedPosting, err error)
	PutPostings(ctx context.Context, journalID string, postings []domain.ResolvedPosting) error
	DeletePostings(ctx context.Context, journalIDs ...string) error
	// Invalidate deletes docs whose kind is in kinds. Empty kinds is a no-op.
	Invalidate(ctx context.Context, kinds ...string) error
	// ClearLedger deletes all non-ECB docs (manual / emergency full wipe).
	ClearLedger(ctx context.Context) error
}

// WithCache attaches a snapshot store. Ignored when c is nil.
func (s *Service) WithCache(c Cache) *Service {
	if c != nil {
		s.cache = c
	}
	return s
}

func (s *Service) invalidate(ctx context.Context, kinds ...string) {
	if s.cache == nil || len(kinds) == 0 {
		return
	}
	if err := s.cache.Invalidate(ctx, kinds...); err != nil {
		log.Printf("cache invalidate: %v", err)
	}
}

func (s *Service) invalidateDerived(ctx context.Context) {
	s.invalidate(ctx, CacheKindNetWorth, CacheKindPeriods, CacheKindRetranslate, CacheKindView)
}

// ClearCache deletes CRDB-backed Mongo cache docs (ECB rates kept).
// Used by DELETE /api/v1/cache.
func (s *Service) ClearCache(ctx context.Context) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.ClearLedger(ctx)
}
