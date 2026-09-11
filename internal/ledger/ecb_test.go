package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestParseEcbCsvObsValue(t *testing.T) {
	csv := `KEY,FREQ,CURRENCY,CURRENCY_DENOM,EXR_TYPE,EXR_SUFFIX,TIME_PERIOD,OBS_VALUE
EXR.D.INR.EUR.SP00.A,D,INR,EUR,SP00,A,2026-01-16,105.397
`
	if got := ParseEcbCsvObsValue(csv); got != "105.397" {
		t.Fatalf("got %q", got)
	}
	if got := ParseEcbCsvObsValue("KEY,FREQ,CURRENCY,TIME_PERIOD,OBS_VALUE\n"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchEcbRatePerEurWithLookback(t *testing.T) {
	var calls []string
	fetchRate := func(_ context.Context, currency, date string) (domain.Rational, error) {
		calls = append(calls, date)
		if date == "2026-01-16" {
			return domain.MustRational(105, 1), nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	}
	result, err := FetchEcbRatePerEurWithLookback(context.Background(), "INR", "2026-01-18", 5, fetchRate)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedDate != "2026-01-16" {
		t.Fatal(result.ObservedDate)
	}
	if result.Rate.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatal(result.Rate)
	}
	want := []string{"2026-01-18", "2026-01-17", "2026-01-16"}
	if len(calls) != 3 || calls[0] != want[0] || calls[1] != want[1] || calls[2] != want[2] {
		t.Fatal(calls)
	}
}

func TestPreviousUTCDate(t *testing.T) {
	if got := PreviousUTCDate("2026-01-18"); got != "2026-01-17" {
		t.Fatal(got)
	}
}

func TestEcbSpotAvailable(t *testing.T) {
	// Summer (CEST): 16:00 Berlin = 14:00 UTC.
	summerBefore := time.Date(2026, 9, 6, 13, 59, 59, 0, time.UTC)
	summerAt := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	if ecbSpotAvailable("2026-09-06", summerBefore) {
		t.Fatal("summer before 16:00 Berlin should wait")
	}
	if !ecbSpotAvailable("2026-09-06", summerAt) {
		t.Fatal("summer at 16:00 Berlin should fetch")
	}
	if !ecbSpotAvailable("2026-09-05", summerBefore) {
		t.Fatal("past days should fetch")
	}
	if ecbSpotAvailable("2026-09-07", summerBefore) {
		t.Fatal("future days should not fetch")
	}

	// Winter (CET): 16:00 Berlin = 15:00 UTC.
	winterBefore := time.Date(2026, 1, 15, 14, 59, 59, 0, time.UTC)
	winterAt := time.Date(2026, 1, 15, 15, 0, 0, 0, time.UTC)
	if ecbSpotAvailable("2026-01-15", winterBefore) {
		t.Fatal("winter before 16:00 Berlin should wait")
	}
	if !ecbSpotAvailable("2026-01-15", winterAt) {
		t.Fatal("winter at 16:00 Berlin should fetch")
	}
}

func TestFetchEcbRatePerEurWithLookback_RejectsFutureDate(t *testing.T) {
	defer func(prev func() time.Time) { nowUTC = prev }(nowUTC)
	nowUTC = func() time.Time { return time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC) }

	calls := 0
	fetchRate := func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(105, 1), nil
	}
	_, err := FetchEcbRatePerEurWithLookback(context.Background(), "INR", "2026-09-25", 10, fetchRate)
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "InvalidEcbDate" {
		t.Fatalf("err %v", err)
	}
	if calls != 0 {
		t.Fatalf("lookback hit fetcher %d times", calls)
	}
}

func TestCachedEcbRate_RejectsFutureDate(t *testing.T) {
	defer func(prev func() time.Time) { nowUTC = prev }(nowUTC)
	nowUTC = func() time.Time { return time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC) }

	calls := 0
	s := &Service{rateMemo: map[string]rateMemo{}}
	inner := func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(105, 1), nil
	}
	_, err := s.cachedEcbRate(context.Background(), inner, "USD", "2026-09-25")
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "InvalidEcbDate" {
		t.Fatalf("err %v", err)
	}
	if calls != 0 {
		t.Fatalf("future date hit ECB %d times", calls)
	}
}

func TestCachedEcbRate_SkipsTodayBeforePublish(t *testing.T) {
	defer func(prev func() time.Time) { nowUTC = prev }(nowUTC)
	nowUTC = func() time.Time { return time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC) }

	calls := 0
	s := &Service{rateMemo: map[string]rateMemo{}}
	inner := func(_ context.Context, currency, date string) (domain.Rational, error) {
		calls++
		return domain.MustRational(105, 1), nil
	}

	_, err := s.cachedEcbRate(context.Background(), inner, "USD", "2026-09-06")
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "MissingEcbRate" {
		t.Fatalf("err %v", err)
	}
	if calls != 0 {
		t.Fatalf("today before 16:00 Berlin hit ECB %d times", calls)
	}

	rate, err := s.cachedEcbRate(context.Background(), inner, "USD", "2026-09-05")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || rate.Numerator.Cmp(domain.Int(105)) != 0 {
		t.Fatalf("calls %d rate %v", calls, rate)
	}

	_, err = s.cachedEcbRate(context.Background(), inner, "EUR", "2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("EUR should still fetch, calls %d", calls)
	}
}

func TestCachedEcbRate_FetchesTodayAfterPublish(t *testing.T) {
	defer func(prev func() time.Time) { nowUTC = prev }(nowUTC)
	nowUTC = func() time.Time { return time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC) }

	calls := 0
	s := &Service{rateMemo: map[string]rateMemo{}}
	inner := func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(108, 100), nil
	}

	first, err := s.cachedEcbRate(context.Background(), inner, "USD", "2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.cachedEcbRate(context.Background(), inner, "USD", "2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	if first.Numerator.Cmp(second.Numerator) != 0 {
		t.Fatal(first, second)
	}
}
