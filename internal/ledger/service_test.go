package ledger

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestLedger_RejectsUntilDefaultCurrency(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	_, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Cash", Name: "Cash", AccountType: "asset"})
	requireCode(t, err, "FunctionalCurrencyNotSet")
	_, err = svc.ComputeNetWorth(ctx)
	requireCode(t, err, "FunctionalCurrencyNotSet")
}

func TestLedger_SetDefaultCurrencyWritesSettings(t *testing.T) {
	svc, pool := testService(t, nil)
	store := &memSettings{}
	svc.WithSettings(store)
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR", "USD")
	if _, _, err := svc.SetDefaultCurrency(ctx, "EUR"); err != nil {
		t.Fatal(err)
	}
	if store.items[presentationCommoditySettingKey] != "EUR" {
		t.Fatalf("store %#v", store.items)
	}
	assertFunctionalCurrency(t, pool, "EUR")
	_, _, err := svc.SetDefaultCurrency(ctx, "USD")
	requireCode(t, err, "FunctionalCurrencyChangeNotConfirmed")
	got, err := svc.GetDefaultCommodityID(ctx)
	if err != nil || got == nil || *got != "EUR" {
		t.Fatal(got, err)
	}
	if store.items[presentationCommoditySettingKey] != "EUR" {
		t.Fatalf("store %#v", store.items)
	}
	assertFunctionalCurrency(t, pool, "EUR")
}

func TestLedger_SetDefaultCurrencySwitchable(t *testing.T) {
	svc, pool := testService(t, mockRates(map[string]domain.Rational{
		"USD": domain.MustRational(11, 10),
	}))
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR", "USD")
	id, txnCost, err := svc.SetDefaultCurrency(ctx, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	wantCode := TransactionCostAccountCodeFor("EUR")
	if id != "EUR" || txnCost.Code != wantCode || txnCost.NativeCommodityID != "EUR" {
		t.Fatalf("got %s %+v", id, txnCost)
	}
	got, err := svc.GetDefaultCommodityID(ctx)
	if err != nil || got == nil || *got != "EUR" {
		t.Fatal(got, err)
	}
	_, _, err = svc.SetDefaultCurrency(ctx, "USD")
	requireCode(t, err, "FunctionalCurrencyChangeNotConfirmed")
	assertFunctionalCurrency(t, pool, "EUR")

	id, txnCost, err = svc.ChangeFunctionalCurrency(ctx, "USD")
	if err != nil || id != "USD" {
		t.Fatalf("switch functional: id=%s err=%v", id, err)
	}
	wantUSD := TransactionCostAccountCodeFor("USD")
	if txnCost.Code != wantUSD || txnCost.NativeCommodityID != "USD" {
		t.Fatalf("usd fee account %+v", txnCost)
	}
	got, err = svc.GetDefaultCommodityID(ctx)
	if err != nil || got == nil || *got != "USD" {
		t.Fatal(got, err)
	}
	assertFunctionalCurrency(t, pool, "USD")
}

func TestLedger_DefaultAccountCommodity(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	account, err := svc.CreateAccount(context.Background(), CreateAccountInput{
		Code: "Assets:Cash", Name: "Cash", AccountType: "asset",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.NativeCommodityID != "EUR" {
		t.Fatal(account.NativeCommodityID)
	}
}

func TestLedger_CommodityCatalogStartsEmpty(t *testing.T) {
	svc, _ := testService(t, nil)
	list, err := svc.ListCommodities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty catalog after migrate, got %+v", list)
	}
}

func TestLedger_CreateCommodity(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	gbp, err := svc.CreateCommodity(ctx, CreateCommodityInput{Code: "GBP", Name: "British Pound", MinorUnits: 2, Kind: domain.CommodityCurrency})
	if err != nil {
		t.Fatal(err)
	}
	if gbp.ID != "GBP" || gbp.MinorUnits != 2 {
		t.Fatalf("%+v", gbp)
	}
	_, err = svc.CreateCommodity(ctx, CreateCommodityInput{Code: "GBP", Name: "Dup", MinorUnits: 2})
	requireCode(t, err, "CommodityAlreadyExists")
}

func TestLedger_CreateAccountRejectsInvalidType(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.NativeCommodityID != "EUR" {
		t.Fatal(account)
	}
	_, err = svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Bad", Name: "Bad", AccountType: "invalid", NativeCommodityID: &eur,
	})
	requireCode(t, err, "InvalidAccountType")
}

func TestLedger_PostSameCurrencySalary(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
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
	bal, err := svc.GetAccountBalance(ctx, checking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Minor.Cmp(domain.Int(300000)) != 0 {
		t.Fatal(bal.Minor)
	}
}

func TestLedger_CrossCurrencyPurchase(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	usd, eur := "USD", "EUR"
	expense, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Shopping", Name: "Shopping", AccountType: "expense", NativeCommodityID: &usd})
	if err != nil {
		t.Fatal(err)
	}
	checking, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.RationalFromDecimal("0.92", 2)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-02-01",
		Description:   ptr("USD purchase paid from EUR"),
		Postings: []domain.PostingInput{
			{AccountID: expense.ID, Units: domain.Units{Minor: domain.Int(9200), CommodityID: "USD"}},
			{
				AccountID: checking.ID,
				Units:     domain.Units{Minor: domain.Int(-10000), CommodityID: "EUR"},
				Price:     &domain.TransactionPrice{PerUnit: price, CommodityID: "USD"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Postings[1].Price == nil || domain.CompareRational(entry.Postings[1].Price.PerUnit, price) != 0 {
		t.Fatal(entry.Postings[1].Price)
	}
	bal, err := svc.GetAccountBalance(ctx, checking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Minor.Cmp(domain.Int(-10000)) != 0 {
		t.Fatal(bal.Minor)
	}
}

func TestLedger_SpotPriceChangeAffectsNetWorthNotLedger(t *testing.T) {
	usdPerEur := domain.MustRational(10, 9) // 0.90 EUR per USD
	svc, _ := testService(t, func(ctx context.Context, currency, _ string) (domain.Rational, error) {
		if domain.IsEcbQuoteCurrency(currency) {
			return domain.MustRational(1, 1), nil
		}
		if currency == "USD" {
			return usdPerEur, nil
		}
		return domain.Rational{}, domain.MissingEcbRate(currency, "")
	})
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur, usd := "EUR", "USD"
	eurAcct, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	usdAcct, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:USD", Name: "USD", AccountType: "asset", NativeCommodityID: &usd})
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.RationalFromDecimal("1.08", 2)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-10",
		Description:   ptr("FX"),
		Postings: []domain.PostingInput{
			{AccountID: eurAcct.ID, Units: domain.Units{Minor: domain.Int(-100000), CommodityID: "EUR"}, Price: &domain.TransactionPrice{PerUnit: price, CommodityID: "USD"}},
			{AccountID: usdAcct.ID, Units: domain.Units{Minor: domain.Int(108000), CommodityID: "USD"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	low, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	usdPerEur = domain.MustRational(100, 95) // 0.95 EUR per USD
	svc.rateMu.Lock()
	svc.rateMemo = map[string]rateMemo{}
	svc.rateMu.Unlock()
	high, err := svc.ComputeNetWorth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := svc.GetJournalEntry(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantPrice, _ := domain.RationalFromDecimal("1.08", 2)
	if domain.CompareRational(unchanged.Postings[0].Price.PerUnit, wantPrice) != 0 {
		t.Fatal(unchanged.Postings[0].Price)
	}
	if (func() *domain.AccountBalance {
		b, _ := svc.GetAccountBalance(ctx, eurAcct.ID)
		return &b
	}()).Minor.Cmp(domain.Int(-100000)) != 0 {
		t.Fatal("eur balance changed")
	}
	if high.NetWorthMinor.Cmp(low.NetWorthMinor) <= 0 {
		t.Fatalf("high %s low %s", high.NetWorthMinor, low.NetWorthMinor)
	}
}

func TestLedger_DeleteRestoresBalances(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	income, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	checking, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "Checking", AccountType: "asset", NativeCommodityID: &eur})
	original, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-100000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(100000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bal, _ := svc.GetAccountBalance(ctx, checking.ID)
	if bal.Minor.Cmp(domain.Int(100000)) != 0 {
		t.Fatal(bal.Minor)
	}
	if err := svc.DeleteJournalEntry(ctx, original.ID); err != nil {
		t.Fatal(err)
	}
	bal, _ = svc.GetAccountBalance(ctx, checking.ID)
	if bal.Minor.Sign() != 0 {
		t.Fatal(bal.Minor)
	}
	got, err := svc.GetJournalEntry(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected deleted entry to be gone")
	}
	err = svc.DeleteJournalEntry(ctx, original.ID)
	requireCode(t, err, "JournalEntryNotFound")
}

func TestLedger_AuditAndTheme(t *testing.T) {
	svc, pool := testService(t, nil)
	svc.WithSettings(&memSettings{})
	ctx := context.Background()
	theme, err := svc.GetUiTheme(ctx)
	if err != nil || theme != ThemeLight {
		t.Fatal(theme, err)
	}
	boot(t, svc, "EUR")
	eur := "EUR"
	if _, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Cash:EUR", Name: "Cash", AccountType: "asset", NativeCommodityID: &eur}); err != nil {
		t.Fatal(err)
	}
	events, err := svc.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawPresentation, sawAccount bool
	for _, e := range events {
		if e.EventType == "functional.set" {
			sawPresentation = true
		}
		if e.EventType == "account.created" {
			sawAccount = true
		}
	}
	if !sawPresentation || !sawAccount {
		t.Fatal(events)
	}
	if got, err := svc.SetUiTheme(ctx, ThemeDark); err != nil || got != ThemeDark {
		t.Fatal(got, err)
	}
	if got, _ := svc.GetUiTheme(ctx); got != ThemeDark {
		t.Fatal(got)
	}
	assertFunctionalCurrency(t, pool, "EUR")
}

func TestLedger_ListJournalEntriesPagination(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	income, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	checking, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	for i := 0; i < 5; i++ {
		desc := "Salary batch " + string(rune('0'+i))
		_, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
			EffectiveDate: "2026-01-" + pad2(10+i),
			Description:   &desc,
			Postings: []domain.PostingInput{
				{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-1000), CommodityID: "EUR"}},
				{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(1000), CommodityID: "EUR"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	limit := 2
	offset := 0
	page1, err := svc.ListJournalEntries(ctx, ListJournalEntriesInput{Limit: &limit, Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Entries) != 2 || !page1.HasMore {
		t.Fatalf("%+v", page1)
	}
	offset = 2
	page2, _ := svc.ListJournalEntries(ctx, ListJournalEntriesInput{Limit: &limit, Offset: &offset})
	if len(page2.Entries) != 2 || page2.Entries[0].ID == page1.Entries[0].ID {
		t.Fatal(page2)
	}
	offset = 4
	last, _ := svc.ListJournalEntries(ctx, ListJournalEntriesInput{Limit: &limit, Offset: &offset})
	if len(last.Entries) != 1 || last.HasMore {
		t.Fatal(last)
	}
	q := "batch 3"
	filtered, _ := svc.ListJournalEntries(ctx, ListJournalEntriesInput{Q: &q, Limit: ptr(10)})
	if len(filtered.Entries) != 1 || filtered.Entries[0].Description == nil || *filtered.Entries[0].Description != "Salary batch 3" {
		t.Fatal(filtered)
	}
	byAccount, _ := svc.ListJournalEntries(ctx, ListJournalEntriesInput{AccountID: &checking.ID, Limit: ptr(10)})
	if len(byAccount.Entries) != 5 {
		t.Fatal(len(byAccount.Entries))
	}
}

func TestLedger_AccountBalancesCache(t *testing.T) {
	svc, pool := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	income, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Salary", Name: "Salary", AccountType: "income", NativeCommodityID: &eur})
	checking, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "Checking", AccountType: "asset", NativeCommodityID: &eur})
	entry, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-40000), CommodityID: "EUR"}},
			{AccountID: checking.ID, Units: domain.Units{Minor: domain.Int(40000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var cached int64
	if err := pool.QueryRow(ctx, `SELECT units_minor FROM account_balances WHERE account_id = $1`, checking.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != 40000 {
		t.Fatal(cached)
	}
	if err := svc.DeleteJournalEntry(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `SELECT units_minor FROM account_balances WHERE account_id = $1`, checking.ID).Scan(&cached)
	if err == nil {
		t.Fatalf("expected no balance row, got %d", cached)
	}
	bal, _ := svc.GetAccountBalance(ctx, checking.ID)
	if bal.Minor.Sign() != 0 {
		t.Fatal(bal.Minor)
	}
}

func TestLedger_BalanceTriggersOnDirectSQL(t *testing.T) {
	svc, pool := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	income, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Other", Name: "Other", AccountType: "income", NativeCommodityID: &eur})
	checking, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Cash", Name: "Cash", AccountType: "asset", NativeCommodityID: &eur})

	entryID := newID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO journal_entries (id, effective_date, description, status, created_at, posted_at)
		VALUES ($1, '2026-01-01', 'direct', 'posted', $2, $2)
	`, entryID, nowISO()); err != nil {
		t.Fatal(err)
	}
	postingID := newID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO postings (
			id, journal_entry_id, line_order, account_id,
			units_minor, commodity_id, weight_minor, weight_commodity_id
		) VALUES ($1, $2, 0, $3, 500, 'EUR', 500, 'EUR')
	`, postingID, entryID, checking.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO postings (
			id, journal_entry_id, line_order, account_id,
			units_minor, commodity_id, weight_minor, weight_commodity_id
		) VALUES ($1, $2, 1, $3, -500, 'EUR', -500, 'EUR')
	`, newID(), entryID, income.ID); err != nil {
		t.Fatal(err)
	}

	var cached int64
	if err := pool.QueryRow(ctx, `SELECT units_minor FROM account_balances WHERE account_id = $1`, checking.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != 500 {
		t.Fatalf("insert trigger: got %d", cached)
	}

	if _, err := pool.Exec(ctx, `UPDATE postings SET units_minor = 200, weight_minor = 200 WHERE id = $1`, postingID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT units_minor FROM account_balances WHERE account_id = $1`, checking.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != 200 {
		t.Fatalf("update trigger: got %d", cached)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM postings WHERE journal_entry_id = $1`, entryID); err != nil {
		t.Fatal(err)
	}
	err := pool.QueryRow(ctx, `SELECT units_minor FROM account_balances WHERE account_id = $1`, checking.ID).Scan(&cached)
	if err == nil {
		t.Fatalf("delete trigger: expected no row, got %d", cached)
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
