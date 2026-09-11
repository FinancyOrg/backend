package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestLedgerTimezonePersistsInSettingsNotCRDB(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	ctx := context.Background()

	loc := svc.LedgerLocation(ctx)
	if loc.String() != domain.DefaultLedgerTimezone {
		t.Fatal(loc)
	}
	if store.items[timezoneSettingKey] != domain.DefaultLedgerTimezone {
		t.Fatalf("expected Mongo seed, got %#v", store.items)
	}
	assertNoFunctionalCurrency(t, pool)
}

func TestThemeDoesNotWriteCRDB(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	ctx := context.Background()
	if _, err := svc.SetUiTheme(ctx, ThemeDark); err != nil {
		t.Fatal(err)
	}
	if store.items[themeSettingKey] != string(ThemeDark) {
		t.Fatalf("store %#v", store.items)
	}
	assertNoFunctionalCurrency(t, pool)
}

func TestViewPeriodsHonorsStoredTimezone(t *testing.T) {
	svc, _ := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	boot(t, svc, "EUR")
	ctx := context.Background()
	if _, err := svc.SetLedgerTimezone(ctx, "Asia/Kolkata"); err != nil {
		t.Fatal(err)
	}

	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Salary", Name: "Salary", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Cash", Name: "Cash", AccountType: string(domain.AccountAsset),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 20:00 UTC 30 June is 22:00 CEST (June) and 01:30 IST (July).
	if _, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-06-30T20:00:00.000000000Z",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-100), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(100), CommodityID: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Timezone != "Asia/Kolkata" {
		t.Fatalf("timezone %s", result.Timezone)
	}
	july := findViewPeriod(result.Months, "2026-07")
	if july == nil || july.IncomeMinor.Cmp(domain.Int(100)) != 0 {
		t.Fatalf("expected July in Kolkata, got %+v", result.Months)
	}
}

func TestChangingTimezoneRebucketsExistingTransactions(t *testing.T) {
	svc, _ := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	boot(t, svc, "EUR")
	ctx := context.Background()
	_ = svc.LedgerLocation(ctx)

	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Salary", Name: "Salary", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Cash", Name: "Cash", AccountType: string(domain.AccountAsset),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-06-30T20:00:00.000000000Z",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-100), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(100), CommodityID: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	berlin, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if findViewPeriod(berlin.Months, "2026-06") == nil {
		t.Fatalf("Berlin should bucket into June, got %+v", berlin.Months)
	}

	if _, err := svc.SetLedgerTimezone(ctx, "Asia/Kolkata"); err != nil {
		t.Fatal(err)
	}
	kolkata, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kolkata.Timezone != "Asia/Kolkata" {
		t.Fatalf("timezone %s", kolkata.Timezone)
	}
	if findViewPeriod(kolkata.Months, "2026-07") == nil || findViewPeriod(kolkata.Months, "2026-06") != nil {
		t.Fatalf("Kolkata should rebucket into July, got %+v", kolkata.Months)
	}
}

func TestSetLedgerTimezoneRejectsUnknownZone(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithSettings(&memSettings{})
	if _, err := svc.SetLedgerTimezone(context.Background(), "Not/AZone"); err == nil {
		t.Fatal("expected invalid timezone")
	}
}

func TestFunctionalChangeEstimatedMs(t *testing.T) {
	if got := FunctionalChangeEstimatedMs(-3); got != functionalChangeProgressFloorMs {
		t.Fatalf("negative count: %d", got)
	}
	if got := FunctionalChangeEstimatedMs(0); got != functionalChangeProgressFloorMs {
		t.Fatalf("empty ledger: %d", got)
	}
	if got := FunctionalChangeEstimatedMs(10); got != functionalChangeProgressFloorMs+10*functionalChangeProgressPerJournalMs {
		t.Fatalf("10 journals: %d", got)
	}
}

func TestAppConfigUnsetDoesNotSeedTimezone(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	cfg, err := svc.AppConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PresentationCommodityID != nil {
		t.Fatalf("commodity %v", cfg.PresentationCommodityID)
	}
	if cfg.Theme != ThemeLight {
		t.Fatalf("theme %q", cfg.Theme)
	}
	if cfg.Timezone != domain.DefaultLedgerTimezone {
		t.Fatalf("timezone %s", cfg.Timezone)
	}
	if cfg.PostedJournalCount != 0 {
		t.Fatalf("journal count %d", cfg.PostedJournalCount)
	}
	if cfg.FunctionalChangeEstimatedMs != FunctionalChangeEstimatedMs(0) {
		t.Fatalf("estimated ms %d", cfg.FunctionalChangeEstimatedMs)
	}
	if cfg.FunctionalChangePollMs != 10000 {
		t.Fatalf("poll ms %d", cfg.FunctionalChangePollMs)
	}
	if cfg.FunctionalChange != nil {
		t.Fatalf("job %#v", cfg.FunctionalChange)
	}
	if _, ok := store.items[timezoneSettingKey]; ok {
		t.Fatal("AppConfig must not write timezone until the user sets it")
	}
	assertNoFunctionalCurrency(t, pool)
}

func TestAppConfigPostedJournalCountTimesRestampProgress(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithSettings(&memSettings{})
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Job", Name: "Job", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Cash", Name: "Cash", AccountType: string(domain.AccountAsset),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
			EffectiveDate: "2026-01-16T00:00:00Z",
			Postings: []domain.PostingInput{
				{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-100), CommodityID: "EUR"}},
				{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(100), CommodityID: "EUR"}},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := svc.AppConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostedJournalCount != 3 {
		t.Fatalf("journal count %d", cfg.PostedJournalCount)
	}
	if cfg.FunctionalChangeEstimatedMs != FunctionalChangeEstimatedMs(3) {
		t.Fatalf("estimated ms %d", cfg.FunctionalChangeEstimatedMs)
	}
}

func TestSetAppConfigWritesMongoNotCRDB(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR")

	cfg, err := svc.SetAppConfig(ctx, "EUR", ThemeDark, "Asia/Kolkata", false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PresentationCommodityID == nil || *cfg.PresentationCommodityID != "EUR" {
		t.Fatalf("commodity %v", cfg.PresentationCommodityID)
	}
	if cfg.Theme != ThemeDark {
		t.Fatalf("theme %q", cfg.Theme)
	}
	if cfg.Timezone != "Asia/Kolkata" {
		t.Fatalf("timezone %s", cfg.Timezone)
	}
	if store.items[presentationCommoditySettingKey] != "EUR" {
		t.Fatalf("store %#v", store.items)
	}
	if store.items[themeSettingKey] != string(ThemeDark) {
		t.Fatalf("theme store %q", store.items[themeSettingKey])
	}
	if store.items[timezoneSettingKey] != "Asia/Kolkata" {
		t.Fatalf("tz store %q", store.items[timezoneSettingKey])
	}
	assertFunctionalCurrency(t, pool, "EUR")
	var extra int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM app_config WHERE key <> $1
	`, functionalCommodityConfigKey).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatal("theme and timezone must not live in CRDB app_config")
	}
}

func TestSetAppConfigChangeRequiresConfirm(t *testing.T) {
	svc, pool := testService(t, mockRates(map[string]domain.Rational{
		"USD": domain.MustRational(11, 10),
	}))
	svc.WithSettings(&memSettings{})
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR", "USD")
	if _, err := svc.SetAppConfig(ctx, "EUR", ThemeDark, "Europe/Berlin", false); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SetAppConfig(ctx, "USD", ThemeDark, "Europe/Berlin", false)
	requireCode(t, err, "FunctionalCurrencyChangeNotConfirmed")
	assertFunctionalCurrency(t, pool, "EUR")
	if _, err := svc.SetAppConfig(ctx, "USD", ThemeDark, "Europe/Berlin", true); err != nil {
		t.Fatal(err)
	}
	waitFunctionalChange(t, svc)
	assertFunctionalCurrency(t, pool, "USD")
}

func TestSetAppConfigRejectsUnknownCurrency(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithSettings(&memSettings{})
	_, err := svc.SetAppConfig(context.Background(), "XXX", ThemeDark, "Europe/Berlin", false)
	if err == nil {
		t.Fatal("expected unknown currency")
	}
}

func TestStartFunctionalCurrencyChangeReturnsBeforeRestamp(t *testing.T) {
	block := make(chan struct{})
	fetch := func(ctx context.Context, currency, date string) (domain.Rational, error) {
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		<-block
		return domain.MustRational(11, 10), nil
	}
	svc, pool := testService(t, fetch)
	svc.WithSettings(&memSettings{})
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR", "USD")
	if _, err := svc.SetAppConfig(ctx, "EUR", ThemeDark, "Europe/Berlin", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.SetAppConfig(ctx, "USD", ThemeDark, "Europe/Berlin", true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FunctionalChange == nil || cfg.FunctionalChange.Status != FunctionalChangeRunning {
		t.Fatalf("job %#v", cfg.FunctionalChange)
	}
	if cfg.PresentationCommodityID == nil || *cfg.PresentationCommodityID != "EUR" {
		t.Fatalf("commodity %v", cfg.PresentationCommodityID)
	}
	assertFunctionalCurrency(t, pool, "EUR")
	close(block)
	waitFunctionalChange(t, svc)
	assertFunctionalCurrency(t, pool, "USD")
	cfg, err = svc.AppConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FunctionalChange == nil || cfg.FunctionalChange.Status != FunctionalChangeDone {
		t.Fatalf("done %#v", cfg.FunctionalChange)
	}
}

func TestStartFunctionalCurrencyChangeRejectsSecondJob(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithSettings(&memSettings{})
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR", "USD")
	if _, err := svc.SetAppConfig(ctx, "EUR", ThemeDark, "Europe/Berlin", false); err != nil {
		t.Fatal(err)
	}
	svc.changeMu.Lock()
	svc.changeRunning = true
	svc.changeJob = FunctionalChangeJob{Status: FunctionalChangeRunning, From: "EUR", To: "USD"}
	svc.changeMu.Unlock()
	_, err := svc.StartFunctionalCurrencyChange(ctx, "USD")
	requireCode(t, err, "FunctionalCurrencyChangeInProgress")
}

func TestAppConfigMarksInterruptedRestamp(t *testing.T) {
	svc, _ := testService(t, nil)
	store := &memSettings{items: map[string]string{
		functionalChangeJobKey: `{"status":"running","from":"EUR","to":"USD"}`,
	}}
	svc.WithSettings(store)
	cfg, err := svc.AppConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FunctionalChange == nil || cfg.FunctionalChange.Status != FunctionalChangeError {
		t.Fatalf("job %#v", cfg.FunctionalChange)
	}
	if cfg.FunctionalChange.Error != functionalChangeInterrupted {
		t.Fatalf("error %q", cfg.FunctionalChange.Error)
	}
}

func waitFunctionalChange(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s.changeMu.Lock()
		running := s.changeRunning
		status := s.changeJob.Status
		errMsg := s.changeJob.Error
		s.changeMu.Unlock()
		if !running && status == FunctionalChangeDone {
			return
		}
		if !running && status == FunctionalChangeError {
			t.Fatalf("functional change error: %s", errMsg)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for functional change")
}
