package ledger

import (
	"context"
	"math/big"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestRetranslation_SameCurrencyIsZero(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	at := "2026-01-26T09:00:00+01:00"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(700_000),
		Datetime:          &at,
		Description:       ptr("Salary"),
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("%+v", preview)
	}
	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry != nil {
		t.Fatal("expected no journal when nothing is pending")
	}
}

func TestRetranslation_BooksVisibleExpenseToMatchNetWorth(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})

	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(100_000),
		Datetime:          &at,
		Description:       ptr("Salary"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    inrBank.ID,
		CreditAmountMinor: domain.Int(20_000),
		Datetime:          &at,
		Description:       ptr("To INR checking"),
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() <= 0 {
		t.Fatalf("expected unrealised loss: %+v", preview)
	}

	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry == nil || result.ExpenseAccount == nil {
		t.Fatal("expected booked entry")
	}
	if result.ExpenseAccount.Code != ForexRetranslationExpenseCode || result.ExpenseAccount.AccountType != domain.AccountExpense {
		t.Fatalf("%+v", result.ExpenseAccount)
	}
	if result.OffsetAccount == nil || result.OffsetAccount.Code != RetranslationEquityCode || result.OffsetAccount.AccountType != domain.AccountEquity {
		t.Fatalf("%+v", result.OffsetAccount)
	}
	if result.Preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("pending after settle: %+v", result.Preview)
	}
	if result.Preview.IncomeExpenseMinor.Cmp(result.Preview.NetWorthMinor) != 0 {
		t.Fatalf("income-expense %s != net worth %s", result.Preview.IncomeExpenseMinor, result.Preview.NetWorthMinor)
	}

	expBal, _ := svc.GetAccountBalance(ctx, result.ExpenseAccount.ID)
	if expBal.CommodityID != "EUR" || expBal.Minor.Sign() <= 0 {
		t.Fatal(expBal)
	}
	offsetBal, _ := svc.GetAccountBalance(ctx, result.OffsetAccount.ID)
	if offsetBal.Minor.Cmp(new(big.Int).Neg(expBal.Minor)) != 0 {
		t.Fatal(offsetBal, expBal)
	}

	again, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Entry != nil {
		t.Fatal("second settle should be a no-op")
	}
}

func TestRetranslation_NegativeExpenseOnUnrealisedGain(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 90))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(100_000),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    inrBank.ID,
		CreditAmountMinor: domain.Int(20_000),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() >= 0 {
		t.Fatalf("expected unrealised gain: %+v", preview)
	}
	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	expBal, _ := svc.GetAccountBalance(ctx, result.ExpenseAccount.ID)
	if expBal.Minor.Sign() >= 0 {
		t.Fatal(expBal)
	}
	if result.Preview.IncomeExpenseMinor.Cmp(result.Preview.NetWorthMinor) != 0 {
		t.Fatalf("income-expense %s != net worth %s", result.Preview.IncomeExpenseMinor, result.Preview.NetWorthMinor)
	}
}

func TestRetranslation_IncludesCapitalGainsInIncomeExpense(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	gains, err := svc.CreateAccount(ctx, CreateAccountInput{
		Code: CapitalGainsAccountCode, Name: "Capital Gains", AccountType: string(domain.AccountIncome),
		NativeCommodityID: &eur,
	})
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(100_000),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    inrBank.ID,
		CreditAmountMinor: domain.Int(20_000),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}

	fxOnly, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fxOnly.RetranslationMinor.Sign() <= 0 {
		t.Fatalf("expected unrealised loss: %+v", fxOnly)
	}

	interest := domain.Int(50_000)
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   gains.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: interest,
		Datetime:          &at,
		Description:       ptr("Interest"),
	}); err != nil {
		t.Fatal(err)
	}

	withGains, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if withGains.RetranslationMinor.Cmp(fxOnly.RetranslationMinor) != 0 {
		t.Fatalf("capital gains must not change FX residual: fx-only %s with-gains %s", fxOnly.RetranslationMinor, withGains.RetranslationMinor)
	}

	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.BookedMinor.Cmp(fxOnly.RetranslationMinor) != 0 {
		t.Fatalf("booked %s want %s", result.BookedMinor, fxOnly.RetranslationMinor)
	}
	gainsBal, _ := svc.GetAccountBalance(ctx, gains.ID)
	if gainsBal.Minor.Cmp(new(big.Int).Neg(interest)) != 0 {
		t.Fatalf("retranslation must not post through capital gains: %v", gainsBal)
	}
}

func TestRetranslation_PartialBackdatedLeavesRemainder(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
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

	pending, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending.RetranslationMinor.Sign() <= 0 {
		t.Fatalf("expected loss: %+v", pending)
	}
	part := domain.Int(300)
	if part.CmpAbs(pending.RetranslationMinor) >= 0 {
		t.Fatalf("fixture residual too small: %s", pending.RetranslationMinor)
	}
	when := "2021-06-15T12:00:00Z"
	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{Datetime: &when, AmountMinor: part})
	if err != nil {
		t.Fatal(err)
	}
	if result.BookedMinor.Cmp(part) != 0 {
		t.Fatalf("booked %s want %s", result.BookedMinor, part)
	}
	if result.Entry == nil || result.Entry.EffectiveDate != "2021-06-15T12:00:00.000000000Z" {
		t.Fatalf("effective date: %+v", result.Entry)
	}
	if result.Entry.PostedAt == nil || *result.Entry.PostedAt != "2021-06-15T12:00:00.000000000Z" {
		t.Fatalf("posted at: %+v", result.Entry.PostedAt)
	}
	remain := new(big.Int).Sub(pending.RetranslationMinor, part)
	if result.Preview.RetranslationMinor.Cmp(remain) != 0 {
		t.Fatalf("remaining %s want %s", result.Preview.RetranslationMinor, remain)
	}

	over := new(big.Int).Add(remain, big.NewInt(1))
	_, err = svc.PostRetranslation(ctx, PostRetranslationInput{AmountMinor: over})
	requireCode(t, err, "InvalidAmount")

	gain := new(big.Int).Neg(part)
	afterGain, err := svc.PostRetranslation(ctx, PostRetranslationInput{AmountMinor: gain})
	if err != nil {
		t.Fatal(err)
	}
	if afterGain.BookedMinor.Cmp(gain) != 0 {
		t.Fatalf("booked gain %s want %s", afterGain.BookedMinor, gain)
	}
	wantRemain := new(big.Int).Sub(remain, gain)
	if afterGain.Preview.RetranslationMinor.Cmp(wantRemain) != 0 {
		t.Fatalf("remaining after opposite-sign slice %s want %s", afterGain.Preview.RetranslationMinor, wantRemain)
	}
}

func TestRetranslation_HistoricalExpenseNotRevaluedAtSpot(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
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
	spend := "2026-01-17T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: family.ID, CreditAmountMinor: domain.Int(1_050_000), Datetime: &spend,
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.IncomeExpenseMinor.Cmp(domain.Int(90_000)) != 0 {
		t.Fatalf("IE should keep historical Family cost €100: %+v", preview)
	}
	if preview.RetranslationMinor.Cmp(domain.Int(1_250)) != 0 {
		t.Fatalf("unrealised FX on remaining INR lots: %+v", preview)
	}

	result, err := svc.PostRetranslation(ctx, PostRetranslationInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview.IncomeExpenseMinor.Cmp(result.Preview.NetWorthMinor) != 0 {
		t.Fatalf("income-expense %s != net worth %s", result.Preview.IncomeExpenseMinor, result.Preview.NetWorthMinor)
	}
}

func TestRetranslation_RequiresDefaultCurrency(t *testing.T) {
	svc, _ := testService(t, nil)
	_, err := svc.PreviewRetranslation(context.Background())
	requireCode(t, err, "FunctionalCurrencyNotSet")
}

func TestRetranslation_SpendAllForeignIsZero(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
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
	spend := "2026-01-17T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: family.ID, CreditAmountMinor: domain.Int(2_100_000), Datetime: &spend,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("empty INR checking must leave preview 0: %+v", preview)
	}
	if preview.IncomeExpenseMinor.Cmp(preview.NetWorthMinor) != 0 {
		t.Fatalf("IE %s != NW %s", preview.IncomeExpenseMinor, preview.NetWorthMinor)
	}
}

func TestRetranslation_TransferThenDrainIsZero(t *testing.T) {
	svc, _ := testService(t, mockINRTxnAndClosing(105, 120))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	inrWallet, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR:Wallet", Name: "INR Wallet", AccountType: "asset", NativeCommodityID: &inr})
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
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: inrWallet.ID, CreditAmountMinor: domain.Int(2_100_000), Datetime: &at,
	}); err != nil {
		t.Fatal(err)
	}
	spend := "2026-01-17T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrWallet.ID, DebitAccountID: family.ID, CreditAmountMinor: domain.Int(2_100_000), Datetime: &spend,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("drained INR wallet must leave preview 0: %+v", preview)
	}
}

func TestRetranslation_OverdraftThenCoverIsZero(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"USD:2026-01-16": domain.MustRational(11, 10),
		"USD:2026-01-17": domain.MustRational(11, 10),
		"USD":            domain.MustRational(12, 10),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, usd := "EUR", "USD"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	usdBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:USD", Name: "USD Checking", AccountType: "asset", NativeCommodityID: &usd})
	server, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Server", Name: "Server", AccountType: "expense", NativeCommodityID: &eur})
	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID, CreditAmountMinor: domain.Int(100_000), Datetime: &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   usdBank.ID,
		DebitAccountID:    server.ID,
		CreditAmountMinor: domain.Int(5_500),
		DebitAmountMinor:  domain.Int(4_000),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    usdBank.ID,
		CreditAmountMinor: domain.Int(5_000),
		DebitAmountMinor:  domain.Int(5_500),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	usdBal, _ := svc.GetAccountBalance(ctx, usdBank.ID)
	if usdBal.Minor.Sign() != 0 {
		t.Fatalf("USD checking native %s", usdBal.Minor)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("covered USD overdraft must leave preview 0: %+v", preview)
	}
}

func TestRetranslation_ForeignCashToFunctionalExpenseUsesLotCost(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"USD:2026-01-16": domain.MustRational(11, 10),
		"USD:2026-01-17": domain.MustRational(11, 10),
		"USD":            domain.MustRational(12, 10),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, usd := "EUR", "USD"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	usdBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:USD", Name: "USD Checking", AccountType: "asset", NativeCommodityID: &usd})
	server, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Server", Name: "Server", AccountType: "expense", NativeCommodityID: &eur})
	at := "2026-01-16T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID, CreditAmountMinor: domain.Int(100_000), Datetime: &at,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    usdBank.ID,
		CreditAmountMinor: domain.Int(5_000),
		DebitAmountMinor:  domain.Int(5_500),
		Datetime:          &at,
	}); err != nil {
		t.Fatal(err)
	}
	spend := "2026-01-17T00:00:00Z"
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   usdBank.ID,
		DebitAccountID:    server.ID,
		CreditAmountMinor: domain.Int(5_500),
		DebitAmountMinor:  domain.Int(4_000),
		Datetime:          &spend,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("spent USD billed in EUR must use lot cost, preview 0: %+v", preview)
	}
	if preview.IncomeExpenseMinor.Cmp(domain.Int(95_000)) != 0 {
		t.Fatalf("IE should be salary minus USD lot €50: %+v", preview)
	}
}

func TestRetranslation_ForeignCashToFunctionalAssetRealizesLotDifference(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(100, 1),
		"INR:2026-01-17": domain.MustRational(120, 1),
		"INR":            domain.MustRational(120, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	house, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:House", Name: "House", AccountType: "asset", NativeCommodityID: &eur})

	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID,
		CreditAmountMinor: domain.Int(10_000), Datetime: ptr("2026-01-15T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: eurBank.ID, DebitAccountID: inrBank.ID,
		CreditAmountMinor: domain.Int(10_000), DebitAmountMinor: domain.Int(1_000_000),
		Datetime: ptr("2026-01-16T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	exit, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: house.ID,
		CreditAmountMinor: domain.Int(1_000_000), DebitAmountMinor: domain.Int(8_333),
		Datetime: ptr("2026-01-17T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("realized foreign exit must not remain as retranslation: %+v", preview)
	}
	if len(exit.Entry.Postings) != 3 {
		t.Fatalf("expected realized FX posting, got %d", len(exit.Entry.Postings))
	}
	var source, adjustment *domain.ResolvedPosting
	for i := range exit.Entry.Postings {
		p := &exit.Entry.Postings[i]
		switch {
		case p.AccountID == inrBank.ID:
			source = p
		case p.AccountID != house.ID && p.Units.CommodityID == "EUR":
			adjustment = p
		}
	}
	if source == nil || source.Price == nil || source.Price.CommodityID != "EUR" {
		t.Fatalf("source price: %+v", source)
	}
	if domain.CompareRational(source.Price.PerUnit, domain.MustRational(1, 100)) != 0 {
		t.Fatalf("source should leave INR checking at its lot cost: %+v", source.Price.PerUnit)
	}
	if adjustment == nil || adjustment.Units.Minor.Cmp(domain.Int(1_667)) != 0 || adjustment.Cost == nil {
		t.Fatalf("realized FX adjustment: %+v", adjustment)
	}
}

func TestRetranslation_ForeignIncomeUsesNetFunctionalGiven(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(100, 1),
		"INR":            domain.MustRational(100, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	income, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Interest", Name: "Interest", AccountType: "income", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})

	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: income.ID, DebitAccountID: inrBank.ID,
		CreditAmountMinor: domain.Int(10_000), DebitAmountMinor: domain.Int(990_000),
		Datetime: ptr("2026-01-16T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("foreign income fee must reduce the lot cost: %+v", preview)
	}
	if len(result.Entry.Postings) != 3 {
		t.Fatalf("expected transaction-cost posting, got %d", len(result.Entry.Postings))
	}
	inrBalance, err := svc.GetAccountBalance(ctx, inrBank.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inrBalance.Minor.Cmp(domain.Int(990_000)) != 0 {
		t.Fatalf("INR checking balance: %s", inrBalance.Minor)
	}
}

func TestRetranslation_ForeignCurrencyTransferRealizesLotDifference(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(100, 1),
		"INR:2026-01-17": domain.MustRational(120, 1),
		"INR":            domain.MustRational(120, 1),
		"USD:2026-01-17": domain.MustRational(1, 1),
		"USD":            domain.MustRational(1, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr, usd := "EUR", "INR", "USD"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	usdBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:USD", Name: "USD Checking", AccountType: "asset", NativeCommodityID: &usd})

	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID,
		CreditAmountMinor: domain.Int(10_000), Datetime: ptr("2026-01-15T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: eurBank.ID, DebitAccountID: inrBank.ID,
		CreditAmountMinor: domain.Int(10_000), DebitAmountMinor: domain.Int(1_000_000),
		Datetime: ptr("2026-01-16T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	transfer, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: usdBank.ID,
		CreditAmountMinor: domain.Int(1_000_000), DebitAmountMinor: domain.Int(8_333),
		Datetime: ptr("2026-01-17T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("foreign-to-foreign lot difference must be realized: %+v", preview)
	}
	if len(transfer.Entry.Postings) != 3 {
		t.Fatalf("expected realized FX posting, got %d", len(transfer.Entry.Postings))
	}
}

func TestRetranslation_OverdraftCoverRealizesCoverDifference(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{
		"INR:2026-01-16": domain.MustRational(100, 1),
		"INR:2026-01-17": domain.MustRational(200, 1),
		"INR":            domain.MustRational(200, 1),
	}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	server, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Server", Name: "Server", AccountType: "expense", NativeCommodityID: &eur})

	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: job.ID, DebitAccountID: eurBank.ID,
		CreditAmountMinor: domain.Int(10_000), Datetime: ptr("2026-01-15T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: inrBank.ID, DebitAccountID: server.ID,
		CreditAmountMinor: domain.Int(1_000_000), DebitAmountMinor: domain.Int(10_000),
		Datetime: ptr("2026-01-16T00:00:00Z"),
	}); err != nil {
		t.Fatal(err)
	}
	cover, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID: eurBank.ID, DebitAccountID: inrBank.ID,
		CreditAmountMinor: domain.Int(10_000), DebitAmountMinor: domain.Int(2_000_000),
		Datetime: ptr("2026-01-17T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewRetranslation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preview.RetranslationMinor.Sign() != 0 {
		t.Fatalf("covered overdraft must not remain as retranslation: %+v", preview)
	}
	if len(cover.Entry.Postings) != 3 {
		t.Fatalf("expected realized cover posting, got %d", len(cover.Entry.Postings))
	}
	var source, adjustment *domain.ResolvedPosting
	for i := range cover.Entry.Postings {
		p := &cover.Entry.Postings[i]
		switch {
		case p.AccountID == eurBank.ID:
			source = p
		case p.AccountID != inrBank.ID && p.Units.CommodityID == "EUR":
			adjustment = p
		}
	}
	if source == nil || source.Price == nil || source.Price.CommodityID != "INR" {
		t.Fatalf("cover source price: %+v", source)
	}
	if domain.CompareRational(source.Price.PerUnit, domain.MustRational(100, 1)) != 0 {
		t.Fatalf("cover source should balance at realized cost: %+v", source.Price.PerUnit)
	}
	if adjustment == nil || adjustment.Units.Minor.Cmp(domain.Int(-5_000)) != 0 || adjustment.Cost == nil {
		t.Fatalf("realized cover adjustment: %+v", adjustment)
	}
}
