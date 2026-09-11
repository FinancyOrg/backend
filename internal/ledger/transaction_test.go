package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestPostTransaction_SalarySameCurrency(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	job, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	eurBank, err := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-01-26T09:00:00+01:00"
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(700_000),
		Datetime:          &at,
		Description:       ptr("Salary"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entry.EffectiveDate != "2026-01-26T08:00:00.000000000Z" {
		t.Fatal(result.Entry.EffectiveDate)
	}
	if result.TransactionCostMinor != "0" {
		t.Fatalf("%+v", result)
	}
	if len(result.Entry.Postings) != 2 {
		t.Fatal(len(result.Entry.Postings))
	}
	eurBal, _ := svc.GetAccountBalance(ctx, eurBank.ID)
	if eurBal.Minor.Cmp(domain.Int(700_000)) != 0 {
		t.Fatal(eurBal.Minor)
	}
}

func TestPostTransaction_SameCurrencyFeeInDefault(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	groc, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Groceries", Name: "Groceries", AccountType: "expense", NativeCommodityID: &eur})
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    groc.ID,
		CreditAmountMinor: domain.Int(10_000),
		DebitAmountMinor:  domain.Int(9_000),
		Datetime:          ptr("2026-05-04T12:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TransactionCostMinor != "1000" {
		t.Fatalf("%+v", result)
	}
	cost, err := svc.GetAccountByCode(ctx, TransactionCostAccountCodeFor("EUR"))
	if err != nil || cost == nil {
		t.Fatal(cost, err)
	}
	costBal, _ := svc.GetAccountBalance(ctx, cost.ID)
	if costBal.Minor.Cmp(domain.Int(1_000)) != 0 {
		t.Fatal(costBal.Minor)
	}
}

func TestPostTransaction_SameCurrencyFeeConvertedToDefault(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{"INR": domain.MustRational(105, 1)}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	inr := "INR"
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	groc, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Groceries:IN", Name: "Groceries IN", AccountType: "expense", NativeCommodityID: &inr})
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   inrBank.ID,
		DebitAccountID:    groc.ID,
		CreditAmountMinor: domain.Int(1_050_000),
		DebitAmountMinor:  domain.Int(945_000),
		Datetime:          ptr("2026-05-04"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// residual ₹1,050.00 at 105 INR/EUR → €10.00
	if result.TransactionCostMinor != "1000" {
		t.Fatal(result.TransactionCostMinor)
	}
	cost, _ := svc.GetAccountByCode(ctx, TransactionCostAccountCodeFor("EUR"))
	costBal, _ := svc.GetAccountBalance(ctx, cost.ID)
	if costBal.CommodityID != "EUR" || costBal.Minor.Cmp(domain.Int(1_000)) != 0 {
		t.Fatal(costBal)
	}
}

func TestPostTransaction_CrossCurrencyUsesTransactionCost(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{"INR": domain.MustRational(105, 1)}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    inrBank.ID,
		CreditAmountMinor: domain.Int(200_000),
		DebitAmountMinor:  domain.Int(19_000_000),
		Datetime:          ptr("2026-02-03T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if domain.MustInt(result.TransactionCostMinor).Sign() <= 0 {
		t.Fatal(result.TransactionCostMinor)
	}
	cost, _ := svc.GetAccountByCode(ctx, TransactionCostAccountCodeFor("EUR"))
	costBal, _ := svc.GetAccountBalance(ctx, cost.ID)
	if costBal.Minor.Cmp(domain.MustInt(result.TransactionCostMinor)) != 0 {
		t.Fatal(costBal.Minor, result.TransactionCostMinor)
	}
}

func TestPostTransaction_OmitsDebitAmountUsesECB(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{"INR": domain.MustRational(105, 1)}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	inrBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:INR", Name: "INR Checking", AccountType: "asset", NativeCommodityID: &inr})
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    inrBank.ID,
		CreditAmountMinor: domain.Int(200_000),
		Datetime:          ptr("2026-02-03"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TransactionCostMinor != "0" {
		t.Fatal(result.TransactionCostMinor)
	}
	inrBal, _ := svc.GetAccountBalance(ctx, inrBank.ID)
	if inrBal.Minor.Cmp(domain.Int(21_000_000)) != 0 {
		t.Fatal(inrBal.Minor)
	}
}

func TestPostTransaction_LooksUpAccountCurrency(t *testing.T) {
	svc, _ := testService(t, mockRates(map[string]domain.Rational{"INR": domain.MustRational(105, 1)}))
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur, inr := "EUR", "INR"
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	grocEUR, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Groceries", Name: "Groceries", AccountType: "expense", NativeCommodityID: &eur})
	grocINR, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Expenses:Groceries:IN", Name: "Groceries IN", AccountType: "expense", NativeCommodityID: &inr})

	same, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    grocEUR.ID,
		CreditAmountMinor: domain.Int(5_000),
		Datetime:          ptr("2026-05-04"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if same.TransactionCostMinor != "0" || len(same.Entry.Postings) != 2 {
		t.Fatalf("%+v", same)
	}

	fx, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   eurBank.ID,
		DebitAccountID:    grocINR.ID,
		CreditAmountMinor: domain.Int(5_000),
		DebitAmountMinor:  domain.Int(400_000),
		Datetime:          ptr("2026-05-04"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if domain.MustInt(fx.TransactionCostMinor).Sign() == 0 {
		t.Fatal("expected transaction cost residual vs ECB")
	}
}

func TestPostTransaction_Delete(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	result, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(100),
		Datetime:          ptr("2026-01-26T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteJournalEntry(ctx, result.Entry.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetJournalEntry(ctx, result.Entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected deleted")
	}
}

func TestPostTransaction_RejectsFutureDatetime(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	boot(t, svc, "EUR")
	eur := "EUR"
	job, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Income:Job", Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	eurBank, _ := svc.CreateAccount(ctx, CreateAccountInput{Code: "Assets:Bank:EUR", Name: "EUR Checking", AccountType: "asset", NativeCommodityID: &eur})
	future := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02T15:04:05Z")
	_, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    eurBank.ID,
		CreditAmountMinor: domain.Int(100),
		Datetime:          &future,
	})
	if err == nil {
		t.Fatal("expected future datetime to be rejected")
	}
	de, ok := domain.IsDomainError(err)
	if !ok || de.Code != "InvalidJournalEntry" {
		t.Fatalf("got %+v", err)
	}
}

func TestLedger_SetDefaultCurrencyProvisionsTransactionCost(t *testing.T) {
	svc, _ := testService(t, nil)
	ctx := context.Background()
	ensureCurrency(t, svc, "EUR")
	if _, _, err := svc.SetDefaultCurrency(ctx, "EUR"); err != nil {
		t.Fatal(err)
	}
	cost, err := svc.GetAccountByCode(ctx, TransactionCostAccountCodeFor("EUR"))
	if err != nil || cost == nil || cost.NativeCommodityID != "EUR" {
		t.Fatal(cost, err)
	}
	gains, err := svc.GetAccountByCode(ctx, CapitalGainsAccountCode)
	if err != nil {
		t.Fatal(err)
	}
	if gains != nil {
		t.Fatal("capital gains should not be auto-provisioned on presentation set")
	}
}
