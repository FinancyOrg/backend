package ledger

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLedger_FunctionalCurrencyCanChange(t *testing.T) {
	svc, pool := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(105, 1),
		"INR:2026-01-17": domain.MustRational(105, 1),
		"INR":            domain.MustRational(120, 1),
		"JPY":            domain.MustRational(160, 1),
		"USD":            domain.MustRational(11, 10),
	}))
	svc.WithCache(&memCache{})
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	card, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Liabilities:Card:EUR", Name: "EUR Card", AccountType: "liability", NativeCommodityID: &eur})
	family, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Family", Name: "Family", AccountType: "expense", NativeCommodityID: &inr})
	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID, CreditAmountMinor: domain.Int(100_000), Datetime: &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: eurBank.ID, DebitAccountID: inrBank.ID, CreditAmountMinor: domain.Int(20_000), Datetime: &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: at,
		Postings: []domain.PostingInput{
			{AccountID: card.ID, Units: domain.Units{Minor: domain.Int(5_000), CommodityID: "EUR"}},
			{AccountID: eurBank.ID, Units: domain.Units{Minor: domain.Int(-5_000), CommodityID: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	spend := "2026-01-17T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: family.ID, CreditAmountMinor: domain.Int(1_050_000), Datetime: &spend,
	}); err != nil {
		t.Fatal(err)
	}

	if got := pnlCost(t, svc, "Expenses:Family", "EUR"); got.Cmp(domain.Int(10_000)) != 0 {
		t.Fatalf("Family lot cost before change %s", got)
	}
	nativeBefore := snapshotNatives(t, pool)
	journalsBefore := countJournals(t, pool)
	inrBefore, err := svc.GetAccountBalance(ctx, inrBank.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.SetDefaultCurrency(ctx, "JPY")
	requireCode(t, err, "FunctionalCurrencyChangeNotConfirmed")
	assertFunctionalCurrency(t, pool, "EUR")
	if got := pnlCost(t, svc, "Expenses:Family", "EUR"); got.Cmp(domain.Int(10_000)) != 0 {
		t.Fatal("unconfirmed change must not restamp costs")
	}

	id, txnCost, err := svc.ChangeFunctionalCurrency(ctx, "JPY")
	if err != nil || id != "JPY" {
		t.Fatalf("id=%s err=%v", id, err)
	}
	if txnCost.Code != TransactionCostAccountCodeFor("JPY") || txnCost.NativeCommodityID != "JPY" {
		t.Fatalf("jpy fee %+v", txnCost)
	}
	assertFunctionalCurrency(t, pool, "JPY")

	if inrAfter, err := svc.GetAccountBalance(ctx, inrBank.ID); err != nil {
		t.Fatal(err)
	} else if inrAfter.Minor.Cmp(inrBefore.Minor) != 0 || inrAfter.CommodityID != "INR" {
		t.Fatalf("native INR checking changed: %+v → %+v", inrBefore, inrAfter)
	}
	if got := pnlCost(t, svc, "Expenses:Family", "JPY"); got.Cmp(domain.Int(16_000)) != 0 {
		t.Fatalf("Family cost after EUR→JPY restamp %s want 16000 (not 2026-01-17 INRJPY)", got)
	}
	if got := pnlCost(t, svc, "Income:Job", "JPY"); got.Cmp(domain.Int(160_000)) != 0 {
		t.Fatalf("EUR-native income cost after EUR→JPY restamp %s want 160000", got)
	}
	assertRestampedPostingCost(t, pool, eurBank.ID, -20_000, "JPY", -32_000)
	assertRestampedPostingCost(t, pool, card.ID, 5_000, "JPY", 8_000)
	var missingCosts int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		WHERE p.commodity_id = 'EUR'
		  AND a.account_type IN ('asset', 'liability', 'income', 'expense')
		  AND (
			p.cost_per_unit_numerator IS NULL
			OR p.cost_per_unit_denominator IS NULL
			OR p.cost_commodity_id IS NULL
		  )
	`).Scan(&missingCosts); err != nil {
		t.Fatal(err)
	}
	if missingCosts != 0 {
		t.Fatalf("EUR-native monetary/P&L postings without INR cost: %d", missingCosts)
	}
	if _, err := svc.PreviewRetranslation(ctx); err != nil {
		t.Fatalf("preview after functional-currency change: %v", err)
	}
	if _, err := svc.ViewDashboard(ctx); err != nil {
		t.Fatalf("dashboard after functional-currency change: %v", err)
	}
	if journalsAfter := countJournals(t, pool); journalsAfter != journalsBefore {
		t.Fatalf("F change must not post a retranslation journal: %d → %d", journalsBefore, journalsAfter)
	}
	assertNativesUnchanged(t, pool, nativeBefore)

	events, err := svc.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawChange bool
	for _, e := range events {
		if e.EventType == "functional.set" {
			if from, _ := e.Payload["from"].(string); from == "EUR" {
				if to, _ := e.Payload["to"].(string); to == "JPY" {
					sawChange = true
				}
			}
		}
	}
	if !sawChange {
		t.Fatalf("missing functional.set EUR→JPY audit: %+v", events)
	}

	if _, _, err := svc.ChangeFunctionalCurrency(ctx, "USD"); err != nil {
		t.Fatal(err)
	}
	assertFunctionalCurrency(t, pool, "USD")
	if got := pnlCost(t, svc, "Expenses:Family", "USD"); got.Cmp(domain.Int(11_000)) != 0 {
		t.Fatalf("Family cost after JPY→USD restamp %s want 11000", got)
	}
	if inrAfter, err := svc.GetAccountBalance(ctx, inrBank.ID); err != nil {
		t.Fatal(err)
	} else if inrAfter.Minor.Cmp(inrBefore.Minor) != 0 {
		t.Fatalf("native INR checking after second change %s", inrAfter.Minor)
	}
}

type nativeRow struct {
	id, commodityID, journalID string
	units                      int64
	line                       int
}

func snapshotNatives(t *testing.T, pool *pgxpool.Pool) []nativeRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, journal_entry_id, line_order, units_minor, commodity_id
		FROM postings
		ORDER BY journal_entry_id, line_order
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []nativeRow
	for rows.Next() {
		var row nativeRow
		if err := rows.Scan(&row.id, &row.journalID, &row.line, &row.units, &row.commodityID); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertNativesUnchanged(t *testing.T, pool *pgxpool.Pool, want []nativeRow) {
	t.Helper()
	got := snapshotNatives(t, pool)
	if len(got) != len(want) {
		t.Fatalf("posting count %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("native posting %d changed: %+v → %+v", i, want[i], got[i])
		}
	}
}

func countJournals(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM journal_entries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func pnlCost(t *testing.T, svc *Service, code, reportingID string) *big.Int {
	t.Helper()
	lines, err := svc.ListPnLPostings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if line.AccountCode != code {
			continue
		}
		got := domain.CostReportingAmount(line.Units, line.Cost, reportingID)
		if got == nil {
			t.Fatalf("%s missing cost in %s: %+v", code, reportingID, line.Cost)
		}
		return new(big.Int).Abs(got)
	}
	t.Fatalf("no P&L posting %s", code)
	return nil
}

func assertRestampedPostingCost(t *testing.T, pool *pgxpool.Pool, accountID string, units int64, commodityID string, wantMinor int64) {
	t.Helper()
	var gotUnits int64
	var numerator, denominator, gotCommodity string
	err := pool.QueryRow(context.Background(), `
		SELECT units_minor, cost_per_unit_numerator, cost_per_unit_denominator, cost_commodity_id
		FROM postings
		WHERE account_id = $1 AND units_minor = $2
		ORDER BY id
		LIMIT 1
	`, accountID, units).Scan(&gotUnits, &numerator, &denominator, &gotCommodity)
	if err != nil {
		t.Fatal(err)
	}
	if gotCommodity != commodityID {
		t.Fatalf("posting cost commodity %s want %s", gotCommodity, commodityID)
	}
	num, ok := new(big.Int).SetString(numerator, 10)
	if !ok {
		t.Fatalf("invalid cost numerator %q", numerator)
	}
	den, ok := new(big.Int).SetString(denominator, 10)
	if !ok {
		t.Fatalf("invalid cost denominator %q", denominator)
	}
	got, err := domain.MultiplyUnitsByRational(domain.Int(gotUnits), domain.Rational{
		Numerator: num, Denominator: den,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(domain.Int(wantMinor)) != 0 {
		t.Fatalf("posting %d units cost %s want %d", gotUnits, got, wantMinor)
	}
}
