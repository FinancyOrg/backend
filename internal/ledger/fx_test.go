package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestQuoteEcbCrossRate(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"USD": domain.MustRational(11, 10),
		"INR": domain.MustRational(95, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")

	quote, err := svc.QuoteEcbCrossRate(ctx, "USD", "INR", "2026-01-16")
	if err != nil {
		t.Fatal(err)
	}
	wantNum := big.NewInt(950)
	wantDen := big.NewInt(11)
	if quote.Rate.Numerator.Cmp(wantNum) != 0 || quote.Rate.Denominator.Cmp(wantDen) != 0 {
		t.Fatalf("got %s/%s want %s/%s", quote.Rate.Numerator, quote.Rate.Denominator, wantNum, wantDen)
	}
	if quote.ObservedDate == "" || quote.AsOfDate != "2026-01-16" {
		t.Fatalf("%+v", quote)
	}

	same, err := svc.QuoteEcbCrossRate(ctx, "EUR", "EUR", "2026-01-16")
	if err == nil {
		t.Fatalf("expected error, got %+v", same)
	}
}

func TestQuoteEcbCrossRate_RejectsFutureDate(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"USD": domain.MustRational(11, 10),
		"INR": domain.MustRational(95, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")

	future := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	_, err := svc.QuoteEcbCrossRate(ctx, "USD", "INR", future)
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "InvalidEcbDate" {
		t.Fatalf("err %v", err)
	}
}
