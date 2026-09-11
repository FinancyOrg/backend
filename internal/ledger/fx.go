package ledger

import (
	"context"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
)

// EcbCrossRate is the ECB-derived cross rate for to-commodity units per 1 from-commodity unit.
type EcbCrossRate struct {
	FromCommodityID string
	ToCommodityID   string
	Rate            domain.Rational
	ObservedDate    string
	AsOfDate        string
}

// QuoteEcbCrossRate returns the ECB cross rate (to per from) for asOfDate, with the same
// lookback rules used when posting cross-currency transactions.
func (s *Service) QuoteEcbCrossRate(ctx context.Context, fromCommodityID, toCommodityID, asOfDate string) (EcbCrossRate, error) {
	from, err := s.requireCommodity(ctx, fromCommodityID)
	if err != nil {
		return EcbCrossRate{}, err
	}
	to, err := s.requireCommodity(ctx, toCommodityID)
	if err != nil {
		return EcbCrossRate{}, err
	}
	if from.ID == to.ID {
		return EcbCrossRate{}, domain.InvalidTransaction("from and to commodities must differ")
	}
	asOf := strings.TrimSpace(asOfDate)
	if asOf == "" {
		asOf = todayISO()
	}
	if !isoDate.MatchString(asOf) {
		return EcbCrossRate{}, domain.InvalidTransaction("date must be YYYY-MM-DD")
	}
	if err := rejectFutureEcbDate(asOf); err != nil {
		return EcbCrossRate{}, err
	}

	fromObs, err := FetchEcbRatePerEurWithLookback(ctx, from.ID, asOf, 10, s.fetchEcbRate)
	if err != nil {
		return EcbCrossRate{}, err
	}
	toObs, err := FetchEcbRatePerEurWithLookback(ctx, to.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		return EcbCrossRate{}, err
	}
	rate, err := domain.CrossRateBPerA(fromObs.Rate, toObs.Rate)
	if err != nil {
		return EcbCrossRate{}, err
	}
	return EcbCrossRate{
		FromCommodityID: from.ID,
		ToCommodityID:   to.ID,
		Rate:            rate,
		ObservedDate:    fromObs.ObservedDate,
		AsOfDate:        asOf,
	}, nil
}
