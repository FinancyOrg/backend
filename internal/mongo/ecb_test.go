package mongo

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

type memEcb struct {
	mu      sync.Mutex
	entries map[string]ecbCacheDoc
	getErr  error
	putErr  error
	gets    int
	puts    int
}

func (m *memEcb) GetEcbRate(_ context.Context, currency, date string) (domain.Rational, bool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return domain.Rational{}, false, false, m.getErr
	}
	doc, ok := m.entries[ecbCacheID(currency, date)]
	if !ok {
		return domain.Rational{}, false, false, nil
	}
	if doc.Missing {
		return domain.Rational{}, true, true, nil
	}
	rate, err := parseCachedRational(doc.Numerator, doc.Denominator)
	if err != nil {
		return domain.Rational{}, false, false, err
	}
	return rate, false, true, nil
}

func (m *memEcb) PutEcbRate(_ context.Context, currency, date string, rate domain.Rational, missing bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	if m.entries == nil {
		m.entries = map[string]ecbCacheDoc{}
	}
	doc := ecbCacheDoc{ID: ecbCacheID(currency, date), Currency: currency, Date: date, Missing: missing}
	if !missing {
		doc.Numerator = rate.Numerator.String()
		doc.Denominator = rate.Denominator.String()
	}
	m.entries[doc.ID] = doc
	return nil
}

func TestWrapEcbFetch_HitSkipsInner(t *testing.T) {
	cache := &memEcb{entries: map[string]ecbCacheDoc{
		"ecb:USD:2026-09-04": {Numerator: "108", Denominator: "100"},
	}}
	calls := 0
	fetch := wrapEcbFetch(cache, func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.Rational{}, fmt.Errorf("should not fetch")
	}, func() string { return "2026-09-05" })
	rate, err := fetch(context.Background(), "usd", "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("inner calls %d", calls)
	}
	if rate.Numerator.String() != "27" || rate.Denominator.String() != "25" {
		t.Fatalf("rate %s/%s", rate.Numerator, rate.Denominator)
	}
}

func TestWrapEcbFetch_StoresSuccess(t *testing.T) {
	cache := &memEcb{}
	calls := 0
	want := domain.MustRational(105397, 1000)
	fetch := wrapEcbFetch(cache, func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return want, nil
	}, func() string { return "2026-09-05" })
	ctx := context.Background()
	first, err := fetch(ctx, "INR", "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetch(ctx, "INR", "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("inner calls %d", calls)
	}
	if cache.puts != 1 {
		t.Fatalf("puts %d", cache.puts)
	}
	if first.Numerator.Cmp(second.Numerator) != 0 || first.Denominator.Cmp(second.Denominator) != 0 {
		t.Fatal(first, second)
	}
}

func TestWrapEcbFetch_CachesPastMiss(t *testing.T) {
	cache := &memEcb{}
	calls := 0
	fetch := wrapEcbFetch(cache, func(_ context.Context, currency, date string) (domain.Rational, error) {
		calls++
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	}, func() string { return "2026-09-05" })
	ctx := context.Background()
	_, err := fetch(ctx, "USD", "2026-09-04")
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "MissingEcbRate" {
		t.Fatalf("err %v", err)
	}
	_, err = fetch(ctx, "USD", "2026-09-04")
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "MissingEcbRate" {
		t.Fatalf("err %v", err)
	}
	if calls != 1 {
		t.Fatalf("inner calls %d", calls)
	}
	if cache.puts != 1 {
		t.Fatalf("puts %d", cache.puts)
	}
}

func TestWrapEcbFetch_DoesNotCacheTodayMiss(t *testing.T) {
	cache := &memEcb{}
	fetch := wrapEcbFetch(cache, func(_ context.Context, currency, date string) (domain.Rational, error) {
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	}, func() string { return "2026-09-05" })
	_, _ = fetch(context.Background(), "USD", "2026-09-05")
	if cache.puts != 0 {
		t.Fatalf("puts %d", cache.puts)
	}
}

func TestWrapEcbFetch_GetErrorFallsThrough(t *testing.T) {
	cache := &memEcb{getErr: fmt.Errorf("mongo down")}
	calls := 0
	want := domain.MustRational(2, 1)
	fetch := wrapEcbFetch(cache, func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return want, nil
	}, func() string { return "2026-09-05" })
	rate, err := fetch(context.Background(), "USD", "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("inner calls %d", calls)
	}
	if rate.Numerator.Cmp(want.Numerator) != 0 {
		t.Fatal(rate)
	}
}

func TestWrapEcbFetch_EURSkipsCache(t *testing.T) {
	cache := &memEcb{}
	calls := 0
	fetch := wrapEcbFetch(cache, func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(1, 1), nil
	}, func() string { return "2026-09-05" })
	_, err := fetch(context.Background(), "EUR", "2026-09-05")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || cache.gets != 0 || cache.puts != 0 {
		t.Fatalf("calls %d gets %d puts %d", calls, cache.gets, cache.puts)
	}
}

func TestWrapEcbFetch_FutureDateSkipsCacheAndInner(t *testing.T) {
	cache := &memEcb{}
	calls := 0
	fetch := wrapEcbFetch(cache, func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(105, 1), nil
	}, func() string { return "2026-09-07" })
	_, err := fetch(context.Background(), "USD", "2026-09-25")
	if de, ok := domain.IsDomainError(err); !ok || de.Code != "InvalidEcbDate" {
		t.Fatalf("err %v", err)
	}
	if calls != 0 || cache.gets != 0 || cache.puts != 0 {
		t.Fatalf("calls %d gets %d puts %d", calls, cache.gets, cache.puts)
	}
}

func TestWrapEcbRateFetcherNilClient(t *testing.T) {
	var c *Client
	calls := 0
	inner := func(context.Context, string, string) (domain.Rational, error) {
		calls++
		return domain.MustRational(1, 1), nil
	}
	fetch := c.WrapEcbRateFetcher(inner)
	_, err := fetch(context.Background(), "USD", "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}

func TestEcbCacheID(t *testing.T) {
	if got := ecbCacheID(" usd ", "2026-09-04"); got != "ecb:USD:2026-09-04" {
		t.Fatal(got)
	}
}
