package ledger

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestViewPeriodsAndYears(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"

	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Salary", Name: "Salary", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	expense, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Expenses:Groceries", Name: "Groceries", AccountType: string(domain.AccountExpense),
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

	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-01-15",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-10000), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(10000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-02-15",
		Postings: []domain.PostingInput{
			{AccountID: expense.ID, Units: domain.Units{Minor: domain.Int(2500), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(-2500), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-03-15",
		Postings: []domain.PostingInput{
			{AccountID: expense.ID, Units: domain.Units{Minor: domain.Int(-1000), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(1000), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-04-15",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(1200), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(-1200), CommodityID: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	january := findViewPeriod(result.Months, "2026-01")
	if january == nil {
		t.Fatalf("January period missing from %+v", result.Months)
	}
	if january.IncomeMinor.Cmp(domain.Int(10000)) != 0 ||
		january.ExpenseMinor.Sign() != 0 ||
		january.EffectMinor.Cmp(domain.Int(10000)) != 0 ||
		january.NetWorthMinor.Cmp(domain.Int(10000)) != 0 {
		t.Fatalf("unexpected January metrics: %+v", january)
	}
	february := findViewPeriod(result.Months, "2026-02")
	if february == nil {
		t.Fatalf("February period missing from %+v", result.Months)
	}
	if february.ExpenseMinor.Cmp(domain.Int(2500)) != 0 ||
		february.EffectMinor.Cmp(domain.Int(-2500)) != 0 ||
		february.NetWorthMinor.Cmp(domain.Int(7500)) != 0 {
		t.Fatalf("unexpected February metrics: %+v", february)
	}
	march := findViewPeriod(result.Months, "2026-03")
	if march == nil {
		t.Fatalf("March period missing from %+v", result.Months)
	}
	if march.IncomeMinor.Cmp(domain.Int(1000)) != 0 ||
		march.ExpenseMinor.Sign() != 0 ||
		march.EffectMinor.Cmp(domain.Int(1000)) != 0 ||
		march.NetWorthMinor.Cmp(domain.Int(8500)) != 0 {
		t.Fatalf("unexpected March reimbursement metrics: %+v", march)
	}
	april := findViewPeriod(result.Months, "2026-04")
	if april == nil {
		t.Fatalf("April period missing from %+v", result.Months)
	}
	if april.IncomeMinor.Sign() != 0 ||
		april.ExpenseMinor.Cmp(domain.Int(1200)) != 0 ||
		april.EffectMinor.Cmp(domain.Int(-1200)) != 0 ||
		april.NetWorthMinor.Cmp(domain.Int(7300)) != 0 {
		t.Fatalf("unexpected April income reversal metrics: %+v", april)
	}
	year := findViewPeriod(result.Years, "2026")
	if year == nil {
		t.Fatalf("2026 period missing from %+v", result.Years)
	}
	if year.IncomeMinor.Cmp(domain.Int(11000)) != 0 ||
		year.ExpenseMinor.Cmp(domain.Int(3700)) != 0 ||
		year.NetSavingsMinor.Cmp(domain.Int(7300)) != 0 ||
		year.NetWorthMinor.Cmp(domain.Int(7300)) != 0 {
		t.Fatalf("unexpected year metrics: %+v", year)
	}
}

func TestViewAccountsAndTransactions(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
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
	description := "Monthly salary"
	_, err = svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   income.ID,
		DebitAccountID:    cash.ID,
		CreditAmountMinor: domain.Int(12345),
		Description:       &description,
		Datetime:          stringPtr("2026-03-10T12:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}

	accounts, err := svc.ViewAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cashView *ViewAccount
	for i := range accounts {
		if accounts[i].Account.ID == cash.ID {
			cashView = &accounts[i]
			break
		}
	}
	if cashView == nil || cashView.Balance.Cmp(domain.Int(12345)) != 0 {
		t.Fatalf("cash view missing or wrong: %+v", cashView)
	}
	if cashView.IsLiquid || cashView.IsHidden {
		t.Fatalf("missing flags should default false: %+v", cashView)
	}

	limit := 10
	transactions, err := svc.ViewTransactions(ctx, ListJournalEntriesInput{
		Limit:  &limit,
		Status: string(domain.JournalPosted),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions.Entries) != 1 {
		t.Fatalf("got %d transactions: %+v", len(transactions.Entries), transactions)
	}
	transaction := transactions.Entries[0]
	if transaction.Category == nil || *transaction.Category != ViewTransactionIncome ||
		transaction.From == nil || transaction.From.AccountID != income.ID ||
		transaction.To == nil || transaction.To.AccountID != cash.ID ||
		transaction.AmountMinor.Cmp(domain.Int(12345)) != 0 {
		t.Fatalf("unexpected transaction view: %+v", transaction)
	}
}

func TestViewDashboardLiquidUsesFlags(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithAccountFlags(&memFlags{})
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	if _, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name: "Cash", AccountType: "asset", NativeCommodityID: &eur,
		OpeningBalanceMinor: domain.Int(10000), Liquid: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name: "House", AccountType: "asset", NativeCommodityID: &eur,
		OpeningBalanceMinor: domain.Int(50000),
	}); err != nil {
		t.Fatal(err)
	}

	dash, err := svc.ViewDashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dash.LiquidMinor.Cmp(domain.Int(10000)) != 0 {
		t.Fatalf("liquid %s", dash.LiquidMinor)
	}
	if dash.Report.TotalAssetsMinor.Cmp(domain.Int(60000)) != 0 {
		t.Fatalf("assets %s", dash.Report.TotalAssetsMinor)
	}
}

func TestViewPeriodsTranslatesNativeExpenseAtECBRate(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{"INR": domain.MustRational(50, 1)}))
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur, inr := "EUR", "INR"

	expense, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Expenses:Travel", Name: "Travel", AccountType: string(domain.AccountExpense),
		NativeCommodityID: &inr,
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
	price := domain.MustRational(100, 1) // 100 INR minor per 1 EUR minor
	_, err = svc.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: "2026-05-15",
		Postings: []domain.PostingInput{
			{
				AccountID: expense.ID,
				Units:     domain.Units{Minor: domain.Int(1_000_000), CommodityID: inr},
			},
			{
				AccountID: cash.ID,
				Units:     domain.Units{Minor: domain.Int(-10_000), CommodityID: eur},
				Price:     &domain.TransactionPrice{PerUnit: price, CommodityID: inr, Source: domain.PriceSourceActual},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	may := findViewPeriod(result.Months, "2026-05")
	if may == nil {
		t.Fatalf("May period missing from %+v", result.Months)
	}
	// 1_000_000 INR minors at 50 INR/EUR → 20_000 EUR minors
	if may.ExpenseMinor.Cmp(domain.Int(20_000)) != 0 {
		t.Fatalf("month view should translate at ECB rate, got %+v", may)
	}
}

func TestViewTransactionsClassifiesCategories(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"

	expense, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Expenses:Health", Name: "Health", AccountType: string(domain.AccountExpense),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: string(domain.AccountAsset),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}

	income, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: "Income:Salary", Name: "Salary", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}

	expenseDescription := "Doctor"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   cash.ID,
		DebitAccountID:    expense.ID,
		CreditAmountMinor: domain.Int(100000),
		Description:       &expenseDescription,
		Datetime:          stringPtr("2026-07-01T12:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	reimbursementDescription := "Insurance reimbursement"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   expense.ID,
		DebitAccountID:    cash.ID,
		CreditAmountMinor: domain.Int(87818),
		Description:       &reimbursementDescription,
		Datetime:          stringPtr("2026-07-02T12:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	reversalDescription := "Salary correction"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   cash.ID,
		DebitAccountID:    income.ID,
		CreditAmountMinor: domain.Int(4000),
		Description:       &reversalDescription,
		Datetime:          stringPtr("2026-07-03T12:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}

	from, to, limit := "2026-07-01", "2026-07-31", 10
	result, err := svc.ViewTransactions(ctx, ListJournalEntriesInput{
		From:   &from,
		To:     &to,
		Limit:  &limit,
		Status: string(domain.JournalPosted),
	})
	if err != nil {
		t.Fatal(err)
	}

	var reimbursement *ViewTransaction
	for i := range result.Entries {
		if result.Entries[i].Description != nil && *result.Entries[i].Description == reimbursementDescription {
			reimbursement = &result.Entries[i]
			break
		}
	}
	if reimbursement == nil {
		t.Fatalf("reimbursement missing from %+v", result.Entries)
	}
	if reimbursement.Category == nil || *reimbursement.Category != ViewTransactionIncome ||
		reimbursement.From == nil || reimbursement.From.AccountID != expense.ID ||
		reimbursement.To == nil || reimbursement.To.AccountID != cash.ID ||
		reimbursement.AmountMinor.Cmp(domain.Int(87818)) != 0 {
		t.Fatalf("unexpected reimbursement view: %+v", reimbursement)
	}
	var reversal *ViewTransaction
	for i := range result.Entries {
		if result.Entries[i].Description != nil && *result.Entries[i].Description == reversalDescription {
			reversal = &result.Entries[i]
			break
		}
	}
	if reversal == nil || reversal.Category == nil || *reversal.Category != ViewTransactionExpense {
		t.Fatalf("unexpected income reversal view: %+v", reversal)
	}
	if len(result.TopExpenses) != 1 ||
		result.TopExpenses[0].AccountID != expense.ID ||
		result.TopExpenses[0].Amount.Cmp(domain.Int(100000)) != 0 {
		t.Fatalf("unexpected top expenses: %+v", result.TopExpenses)
	}
	if len(result.TopIncomes) != 0 {
		t.Fatalf("unexpected top incomes: %+v", result.TopIncomes)
	}
}

func TestViewPeriodsBucketsCivilDateInLedgerTimezone(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
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
		EffectiveDate: "2026-06-30T23:05:00.000000000Z",
		Postings: []domain.PostingInput{
			{AccountID: income.ID, Units: domain.Units{Minor: domain.Int(-661), CommodityID: "EUR"}},
			{AccountID: cash.ID, Units: domain.Units{Minor: domain.Int(661), CommodityID: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ViewPeriods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Timezone != domain.DefaultLedgerTimezone {
		t.Fatalf("timezone %s", result.Timezone)
	}
	july := findViewPeriod(result.Months, "2026-07")
	if july == nil {
		t.Fatalf("July missing from %+v", result.Months)
	}
	if july.StartDate != "2026-07-01" || july.EndDate != "2026-07-31" {
		t.Fatalf("july bounds %+v", july)
	}
	if july.IncomeMinor.Cmp(domain.Int(661)) != 0 {
		t.Fatalf("expected July income, got %+v", july)
	}
	june := findViewPeriod(result.Months, "2026-06")
	if june != nil && june.IncomeMinor.Sign() != 0 {
		t.Fatalf("June should not hold the CEST July posting: %+v", june)
	}

	from, to, limit := "2026-07-01", "2026-07-31", 10
	julyTx, err := svc.ViewTransactions(ctx, ListJournalEntriesInput{
		From: &from, To: &to, Limit: &limit, Status: string(domain.JournalPosted),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(julyTx.Entries) != 1 {
		t.Fatalf("july list %d: %+v", len(julyTx.Entries), julyTx.Entries)
	}
	juneFrom, juneTo := "2026-06-01", "2026-06-30"
	juneTx, err := svc.ViewTransactions(ctx, ListJournalEntriesInput{
		From: &juneFrom, To: &juneTo, Limit: &limit, Status: string(domain.JournalPosted),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(juneTx.Entries) != 0 {
		t.Fatalf("june list should be empty, got %+v", juneTx.Entries)
	}
}

func findViewPeriod(periods []ViewPeriod, key string) *ViewPeriod {
	for i := range periods {
		if periods[i].Key == key {
			return &periods[i]
		}
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}
