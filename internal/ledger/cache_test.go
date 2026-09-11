package ledger

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

type memCache struct {
	mu          sync.Mutex
	accounts    []domain.Account
	accountsSet bool
	commodities []domain.Commodity
	commsSet    bool
	balances    []domain.AccountBalance
	balancesSet bool
	networth    *domain.NetWorthReport
	periods     *ViewPeriodsResult
	retranslate *domain.RetranslationPreview
	postings    map[string][]domain.ResolvedPosting
	gets        int
	puts        int
	clears      int
	invalidates int
	getErr      error
	putErr      error
}

func (m *memCache) GetAccounts(context.Context) ([]domain.Account, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return nil, false, m.getErr
	}
	if !m.accountsSet {
		return nil, false, nil
	}
	return append([]domain.Account(nil), m.accounts...), true, nil
}

func (m *memCache) PutAccounts(_ context.Context, items []domain.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	m.accounts = append([]domain.Account(nil), items...)
	m.accountsSet = true
	return nil
}

func (m *memCache) GetCommodities(context.Context) ([]domain.Commodity, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return nil, false, m.getErr
	}
	if !m.commsSet {
		return nil, false, nil
	}
	return append([]domain.Commodity(nil), m.commodities...), true, nil
}

func (m *memCache) PutCommodities(_ context.Context, items []domain.Commodity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	m.commodities = append([]domain.Commodity(nil), items...)
	m.commsSet = true
	return nil
}

func (m *memCache) GetBalances(context.Context) ([]domain.AccountBalance, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return nil, false, m.getErr
	}
	if !m.balancesSet {
		return nil, false, nil
	}
	return append([]domain.AccountBalance(nil), m.balances...), true, nil
}

func (m *memCache) PutBalances(_ context.Context, items []domain.AccountBalance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	m.balances = append([]domain.AccountBalance(nil), items...)
	m.balancesSet = true
	return nil
}

func (m *memCache) GetNetWorth(_ context.Context, presentationCommodityID, asOfDate string) (domain.NetWorthReport, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return domain.NetWorthReport{}, false, m.getErr
	}
	if m.networth == nil {
		return domain.NetWorthReport{}, false, nil
	}
	if m.networth.ReportingCommodityID != presentationCommodityID || domain.UTCDate(m.networth.AsOf) != asOfDate {
		return domain.NetWorthReport{}, false, nil
	}
	return *m.networth, true, nil
}

func (m *memCache) PutNetWorth(_ context.Context, report domain.NetWorthReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	copied := report
	m.networth = &copied
	return nil
}

func (m *memCache) GetPeriods(_ context.Context, presentationCommodityID, timezone string, ledgerRevision int64) (ViewPeriodsResult, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return ViewPeriodsResult{}, false, m.getErr
	}
	if m.periods == nil {
		return ViewPeriodsResult{}, false, nil
	}
	if m.periods.Currency.ID != presentationCommodityID || m.periods.Timezone != timezone {
		return ViewPeriodsResult{}, false, nil
	}
	return *m.periods, true, nil
}

func (m *memCache) PutPeriods(_ context.Context, result ViewPeriodsResult, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	copied := result
	m.periods = &copied
	return nil
}

func (m *memCache) GetRetranslation(_ context.Context, presentationCommodityID, asOfDate string) (domain.RetranslationPreview, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return domain.RetranslationPreview{}, false, m.getErr
	}
	if m.retranslate == nil {
		return domain.RetranslationPreview{}, false, nil
	}
	if m.retranslate.ReportingCommodityID != presentationCommodityID || domain.UTCDate(m.retranslate.AsOf) != asOfDate {
		return domain.RetranslationPreview{}, false, nil
	}
	return *m.retranslate, true, nil
}

func (m *memCache) PutRetranslation(_ context.Context, preview domain.RetranslationPreview) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	copied := preview
	m.retranslate = &copied
	return nil
}

func (m *memCache) GetPostings(_ context.Context, journalIDs []string) (map[string][]domain.ResolvedPosting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return nil, m.getErr
	}
	out := map[string][]domain.ResolvedPosting{}
	for _, id := range journalIDs {
		if p, ok := m.postings[id]; ok {
			out[id] = append([]domain.ResolvedPosting(nil), p...)
		}
	}
	return out, nil
}

func (m *memCache) PutPostings(_ context.Context, journalID string, postings []domain.ResolvedPosting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.putErr != nil {
		return m.putErr
	}
	if m.postings == nil {
		m.postings = map[string][]domain.ResolvedPosting{}
	}
	m.postings[journalID] = append([]domain.ResolvedPosting(nil), postings...)
	return nil
}

func (m *memCache) DeletePostings(_ context.Context, journalIDs ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range journalIDs {
		delete(m.postings, id)
	}
	return nil
}

func (m *memCache) Invalidate(_ context.Context, kinds ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(kinds) == 0 {
		return nil
	}
	m.invalidates++
	for _, kind := range kinds {
		switch kind {
		case CacheKindAccounts:
			m.accounts = nil
			m.accountsSet = false
		case CacheKindCommodities:
			m.commodities = nil
			m.commsSet = false
		case CacheKindBalances:
			m.balances = nil
			m.balancesSet = false
		case CacheKindNetWorth:
			m.networth = nil
		case CacheKindPeriods:
			m.periods = nil
		case CacheKindRetranslate:
			m.retranslate = nil
		case CacheKindView:
			// no-op until view bundle cache is implemented
		case CacheKindPostings, CacheKindJournal:
			m.postings = nil
		}
	}
	return nil
}

func (m *memCache) ClearLedger(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clears++
	m.accounts = nil
	m.accountsSet = false
	m.commodities = nil
	m.commsSet = false
	m.balances = nil
	m.balancesSet = false
	m.networth = nil
	m.periods = nil
	m.retranslate = nil
	m.postings = nil
	return nil
}

func TestThemePersistsInStoreNotCRDBOrCache(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	cache := &memCache{}
	svc.WithSettings(store)
	svc.WithCache(cache)
	ctx := context.Background()
	if _, err := svc.SetUiTheme(ctx, ThemeDark); err != nil {
		t.Fatal(err)
	}
	if cache.invalidates != 0 {
		t.Fatalf("theme must not invalidate CRDB config cache, got %d", cache.invalidates)
	}
	theme, err := svc.GetUiTheme(ctx)
	if err != nil || theme != ThemeDark {
		t.Fatalf("theme=%q err=%v", theme, err)
	}
	if store.items[themeSettingKey] != string(ThemeDark) {
		t.Fatalf("store %#v", store.items)
	}
	assertNoFunctionalCurrency(t, pool)
}

func TestPresentationSurvivesCacheAfterSetDefaultCurrency(t *testing.T) {
	svc, _ := testService(t, nil)
	cache := &memCache{}
	svc.WithCache(cache)
	boot(t, svc, "EUR")
	if cache.invalidates < 1 {
		t.Fatalf("invalidates %d", cache.invalidates)
	}
	id, err := svc.GetDefaultCommodityID(context.Background())
	if err != nil || id == nil || *id != "EUR" {
		t.Fatalf("id=%v err=%v", id, err)
	}
}

func TestWithCacheNilIsNoop(t *testing.T) {
	svc, _ := testService(t, nil)
	if svc.WithCache(nil).cache != nil {
		t.Fatal("nil cache should not attach")
	}
	theme, err := svc.GetUiTheme(context.Background())
	if err != nil || theme != ThemeLight {
		t.Fatalf("theme=%q err=%v", theme, err)
	}
}

func cachedNetWorth(asOf string, netWorth int64) domain.NetWorthReport {
	return domain.NetWorthReport{
		ReportingCommodityID:  "EUR",
		AsOf:                  asOf,
		Assets:                []domain.ValuedBalance{},
		Liabilities:           []domain.ValuedBalance{},
		TotalAssetsMinor:      big.NewInt(0),
		TotalLiabilitiesMinor: big.NewInt(0),
		NetWorthMinor:         big.NewInt(netWorth),
	}
}

func TestNetWorthCacheHitSkipsSQL(t *testing.T) {
	want := cachedNetWorth(nowISO(), 42)
	eur := "EUR"
	svc := &Service{cache: &memCache{networth: &want}, presentationOverride: &eur}
	got, err := svc.ComputeNetWorth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.NetWorthMinor.String() != "42" {
		t.Fatalf("%+v", got)
	}
}

func TestNetWorthCacheStoresThenHits(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	first, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	putsAfterMiss := cache.puts
	if cache.networth == nil {
		t.Fatal("expected put")
	}
	second, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cache.puts != putsAfterMiss {
		t.Fatalf("puts %d -> %d", putsAfterMiss, cache.puts)
	}
	if first.NetWorthMinor.Cmp(second.NetWorthMinor) != 0 {
		t.Fatal(first.NetWorthMinor, second.NetWorthMinor)
	}
}

func TestNetWorthCacheServesAnyAsOf(t *testing.T) {
	stale := cachedNetWorth(nowISO(), 99)
	eur := "EUR"
	svc := &Service{cache: &memCache{networth: &stale}, presentationOverride: &eur}
	got, err := svc.ComputeNetWorth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.NetWorthMinor.String() != "99" {
		t.Fatalf("%+v", got)
	}
}

func TestClearCache(t *testing.T) {
	cache := &memCache{networth: &domain.NetWorthReport{NetWorthMinor: big.NewInt(1)}}
	svc := (&Service{}).WithCache(cache)
	if err := svc.ClearCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cache.clears != 1 || cache.networth != nil {
		t.Fatalf("clears=%d networth=%v", cache.clears, cache.networth)
	}
}

func TestNetWorthCacheClearedOnJournalPost(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.networth == nil {
		t.Fatal("expected cached report")
	}
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	if cache.networth != nil {
		t.Fatal("create account should clear networth")
	}
	if _, err := svc.ComputeNetWorth(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.networth == nil {
		t.Fatal("expected recache")
	}
	if _, err := svc.ListAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	if !cache.accountsSet {
		t.Fatal("expected accounts cached before journal post")
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Description:   ptr("Salary"),
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-300000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(300000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cache.networth != nil {
		t.Fatal("journal post should clear networth")
	}
	if !cache.accountsSet {
		t.Fatal("journal post must not clear accounts")
	}
}

func TestNetWorthCacheGetErrorFallsThrough(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	svc.WithCache(&memCache{getErr: fmt.Errorf("mongo down")})
	if _, err := svc.ComputeNetWorth(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAccountsCacheHitAndGetByCode(t *testing.T) {
	svc := &Service{cache: &memCache{
		accounts: []domain.Account{{
			ID: "a1", Code: "Assets:Cash", Name: "Cash", AccountType: domain.AccountAsset,
			NativeCommodityID: "EUR",
		}},
		accountsSet: true,
	}}
	list, err := svc.ListAccounts(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	got, err := svc.GetAccountByCode(context.Background(), "Assets:Cash")
	if err != nil || got == nil || got.ID != "a1" {
		t.Fatal(got, err)
	}
}

func TestCommoditiesCacheStoresThenHits(t *testing.T) {
	svc, _ := testService(t, nil)
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR")
	first, err := svc.ListCommodities(ctx)
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	puts := cache.puts
	second, err := svc.ListCommodities(ctx)
	if err != nil || len(second) != len(first) {
		t.Fatal(second, err)
	}
	if cache.puts != puts {
		t.Fatalf("extra puts %d -> %d", puts, cache.puts)
	}
	eur, err := svc.GetCommodity(ctx, "EUR")
	if err != nil || eur == nil || eur.Code != "EUR" {
		t.Fatal(eur, err)
	}
}

func TestAccountsCacheClearedOnCreate(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	if _, err := svc.ListAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	if !cache.accountsSet {
		t.Fatal("expected accounts cached")
	}
	if _, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Cash", Name: "Cash", AccountType: "asset"}); err != nil {
		t.Fatal(err)
	}
	if cache.accountsSet {
		t.Fatal("create should clear accounts")
	}
}

func TestBalancesCache(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Description:   ptr("Salary"),
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-300000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(300000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.GetAccountBalances(ctx)
	if err != nil || len(first) == 0 {
		t.Fatal(first, err)
	}
	puts := cache.puts
	second, err := svc.GetAccountBalances(ctx)
	if err != nil || len(second) != len(first) {
		t.Fatal(second, err)
	}
	if cache.puts != puts {
		t.Fatalf("balances re-put %d -> %d", puts, cache.puts)
	}
	bal, err := svc.GetAccountBalance(ctx, checking.ID)
	if err != nil || bal.Minor.Cmp(domain.Int(300000)) != 0 {
		t.Fatal(bal, err)
	}
}

func TestRetranslationCacheHit(t *testing.T) {
	want := domain.RetranslationPreview{
		ReportingCommodityID: "EUR",
		RetranslationMinor:   big.NewInt(42),
		NetWorthMinor:        big.NewInt(100),
		IncomeExpenseMinor:   big.NewInt(58),
		AsOf:                 nowISO(),
	}
	eur := "EUR"
	svc := &Service{cache: &memCache{retranslate: &want}, presentationOverride: &eur}
	got, err := svc.PreviewRetranslation(context.Background())
	if err != nil || got.RetranslationMinor.String() != "42" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRetranslationCacheStoresThenHits(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	first, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	puts := cache.puts
	if cache.retranslate == nil {
		t.Fatal("expected put")
	}
	second, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cache.puts != puts {
		t.Fatalf("extra puts %d -> %d", puts, cache.puts)
	}
	if first.RetranslationMinor.Cmp(second.RetranslationMinor) != 0 {
		t.Fatal(first, second)
	}
}

func cachedPeriods() ViewPeriodsResult {
	return ViewPeriodsResult{
		Currency: domain.Commodity{ID: "EUR", Code: "EUR", Name: "Euro", MinorUnits: 2, Kind: domain.CommodityCurrency},
		Timezone: domain.DefaultLedgerTimezone,
		Months: []ViewPeriod{{
			Key: "2026-01", Label: "Jan 2026", CurrencyID: "EUR",
			IncomeMinor: big.NewInt(100), ExpenseMinor: big.NewInt(40),
			CapitalGainsMinor: big.NewInt(0), EffectMinor: big.NewInt(60),
			NetSavingsMinor: big.NewInt(60), NetWorthMinor: big.NewInt(60),
			Factor: 1, Good: true,
		}},
		Years: []ViewPeriod{},
	}
}

func TestPeriodsCacheHitSkipsSQL(t *testing.T) {
	want := cachedPeriods()
	eur := "EUR"
	svc := &Service{cache: &memCache{periods: &want}, presentationOverride: &eur}
	got, err := svc.ViewPeriods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 1 || got.Months[0].IncomeMinor.String() != "100" {
		t.Fatalf("%+v", got)
	}
}

func TestPeriodsCacheMissesStaleTimezone(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	stale := cachedPeriods()
	stale.Timezone = ""
	cache := &memCache{periods: &stale}
	svc.WithCache(cache)
	got, err := svc.ViewPeriods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Timezone != domain.DefaultLedgerTimezone {
		t.Fatalf("timezone %s", got.Timezone)
	}
	if cache.periods == nil || cache.periods.Timezone != domain.DefaultLedgerTimezone {
		t.Fatal("expected periods rewrite with ledger timezone")
	}
	if len(got.Months) == 1 && got.Months[0].IncomeMinor.String() == "100" {
		t.Fatal("stale UTC buckets must not be reused")
	}
}

func TestPeriodsCacheStoresThenHits(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	first, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	putsAfterMiss := cache.puts
	if cache.periods == nil {
		t.Fatal("expected put")
	}
	second, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cache.puts != putsAfterMiss {
		t.Fatalf("puts %d -> %d", putsAfterMiss, cache.puts)
	}
	if first.Currency.ID != second.Currency.ID || len(first.Months) != len(second.Months) {
		t.Fatal(first, second)
	}
}

func TestPeriodsCacheClearedOnJournalPost(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	if _, err := svc.ViewPeriods(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.periods == nil {
		t.Fatal("expected cached periods")
	}
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	if cache.periods != nil {
		t.Fatal("create account should clear periods")
	}
	if _, err := svc.ViewPeriods(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.periods == nil {
		t.Fatal("expected recache")
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Description:   ptr("Salary"),
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-300000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(300000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cache.periods != nil {
		t.Fatal("journal post should clear periods")
	}
}

func TestPeriodsCacheClearedOnJournalUpdateAndDelete(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Description:   ptr("Salary"),
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-300000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(300000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ViewPeriods(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.periods == nil {
		t.Fatal("expected cached periods")
	}
	desc := "Updated"
	if _, err := svc.UpdateJournalEntry(ctx, entry.ID, UpdateJournalEntryInput{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if cache.periods == nil {
		t.Fatal("description-only update must leave periods cached")
	}
	newDate := "2026-02-15"
	if _, err := svc.UpdateJournalEntry(ctx, entry.ID, UpdateJournalEntryInput{Datetime: &newDate}); err != nil {
		t.Fatal(err)
	}
	if cache.periods != nil {
		t.Fatal("date change should clear periods")
	}
	if _, err := svc.ViewPeriods(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteJournalEntry(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if cache.periods != nil {
		t.Fatal("journal delete should clear periods")
	}
	if _, ok := cache.postings[entry.ID]; ok {
		t.Fatal("journal delete should remove postings cache entry")
	}
}

func TestPeriodsCacheGetErrorFallsThrough(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	svc.WithCache(&memCache{getErr: fmt.Errorf("mongo down")})
	if _, err := svc.ViewPeriods(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestJournalPostPreservesAccountsAndCachesPostings(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	cache := &memCache{}
	svc.WithCache(cache)
	ctx := context.Background()
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListCommodities(ctx); err != nil {
		t.Fatal(err)
	}
	if !cache.accountsSet || !cache.commsSet {
		t.Fatal("expected catalog warm")
	}
	entry, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Description:   ptr("Salary"),
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-300000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(300000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cache.accountsSet || !cache.commsSet {
		t.Fatal("journal post must preserve catalog caches")
	}
	if !cache.balancesSet {
		t.Fatal("balances should be write-through refreshed after journal post")
	}
	if len(cache.postings[entry.ID]) != 2 {
		t.Fatalf("expected postings cached, got %d", len(cache.postings[entry.ID]))
	}
}

func TestPostingsCacheHitSkipsSQL(t *testing.T) {
	want := []domain.ResolvedPosting{{
		PostingInput: domain.PostingInput{
			AccountID: "a1",
			Units:     domain.Units{Minor: big.NewInt(100), CommodityID: "EUR"},
		},
		LineOrder: 0,
		Weight:    domain.Weight{Minor: big.NewInt(100), CommodityID: "EUR"},
	}}
	cache := &memCache{postings: map[string][]domain.ResolvedPosting{"j1": want}}
	svc := &Service{cache: cache}
	got, err := svc.loadPostings(context.Background(), []string{"j1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["j1"]) != 1 || got["j1"][0].AccountID != "a1" {
		t.Fatalf("%+v", got)
	}
}

func TestThemeChangeDoesNotClearNetWorth(t *testing.T) {
	cache := &memCache{
		networth: &domain.NetWorthReport{NetWorthMinor: big.NewInt(42)},
	}
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	svc.WithCache(cache)
	svc.WithSettings(&memSettings{})
	if _, err := svc.SetUiTheme(context.Background(), ThemeDark); err != nil {
		t.Fatal(err)
	}
	if cache.networth == nil || cache.networth.NetWorthMinor.String() != "42" {
		t.Fatal("theme change must not clear networth")
	}
}

func TestClearCacheWipesPostings(t *testing.T) {
	cache := &memCache{
		networth: &domain.NetWorthReport{NetWorthMinor: big.NewInt(1)},
		postings: map[string][]domain.ResolvedPosting{"j1": {}},
	}
	svc := (&Service{}).WithCache(cache)
	if err := svc.ClearCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cache.clears != 1 || cache.networth != nil || cache.postings != nil {
		t.Fatalf("clears=%d networth=%v postings=%v", cache.clears, cache.networth, cache.postings)
	}
}
