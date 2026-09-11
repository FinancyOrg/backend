package ledger

import (
	"context"
	"testing"

	"github.com/FinancyOrg/backend/internal/domain"
)

func TestCreateAccountGeneratesCodeAndOpeningBalance(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"

	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name:                "Checking",
		AccountType:         "asset",
		NativeCommodityID:   &eur,
		OpeningBalanceMinor: domain.Int(12_500),
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Code != "Assets:Checking" || account.Name != "Checking" {
		t.Fatalf("%+v", account)
	}
	balance, err := svc.GetAccountBalance(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Minor.Cmp(domain.Int(12_500)) != 0 {
		t.Fatalf("opening balance %s", balance.Minor)
	}

	dup, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name:        "Checking",
		AccountType: "asset",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dup.Code != "Assets:Checking-2" {
		t.Fatalf("expected unique code, got %s", dup.Code)
	}
}

func TestUpdateAndDeleteAccount(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()

	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name:        "Cash",
		AccountType: "asset",
	})
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := svc.UpdateAccount(ctx, account.ID, UpdateAccountInput{
		Name: ptr("Wallet"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Wallet" {
		t.Fatalf("%+v", renamed)
	}

	if err := svc.DeleteAccount(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	gone, err := svc.GetAccount(ctx, account.ID)
	if err != nil || gone != nil {
		t.Fatalf("expected deleted account, got %+v %v", gone, err)
	}
}

func TestDeleteAccountRejectsPostings(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	job, err := svc.CreateAccount(ctx, CreateAccountInput{Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{Name: "Cash", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    cash.ID,
		CreditAmountMinor: domain.Int(1000),
	}); err != nil {
		t.Fatal(err)
	}
	err = svc.DeleteAccount(ctx, cash.ID)
	requireCode(t, err, "AccountNotEmpty")

	usd := "USD"
	_, err = svc.UpdateAccount(ctx, cash.ID, UpdateAccountInput{NativeCommodityID: &usd})
	requireCode(t, err, "AccountNotEmpty")
}

func TestUpdateJournalEntryNotesAndDate(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"
	job, err := svc.CreateAccount(ctx, CreateAccountInput{Name: "Job", AccountType: "income", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.CreateAccount(ctx, CreateAccountInput{Name: "Cash", AccountType: "asset", NativeCommodityID: &eur})
	if err != nil {
		t.Fatal(err)
	}
	at := "2026-01-26T09:00:00+01:00"
	posted, err := svc.PostTransaction(ctx, PostTransactionInput{
		CreditAccountID:   job.ID,
		DebitAccountID:    cash.ID,
		CreditAmountMinor: domain.Int(700_000),
		Datetime:          &at,
		Description:       ptr("Salary"),
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := svc.UpdateJournalEntry(ctx, posted.Entry.ID, UpdateJournalEntryInput{
		Description: ptr(" January salary "),
		Datetime:    ptr("2026-02-01T12:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description == nil || *updated.Description != "January salary" {
		t.Fatalf("description %+v", updated.Description)
	}
	if updated.EffectiveDate != "2026-02-01T12:00:00.000000000Z" {
		t.Fatalf("date %s", updated.EffectiveDate)
	}

	view, err := svc.ViewTransaction(ctx, posted.Entry.ID)
	if err != nil || view == nil {
		t.Fatal(view, err)
	}
	if view.From == nil || view.From.AccountID != job.ID || view.To == nil || view.To.AccountID != cash.ID {
		t.Fatalf("%+v", view)
	}
}

func TestViewAccountDeletable(t *testing.T) {
	svc, _ := testService(t, nil)
	boot(t, svc, "EUR")
	ctx := context.Background()
	empty, err := svc.CreateAccount(ctx, CreateAccountInput{Name: "Empty", AccountType: "asset"})
	if err != nil {
		t.Fatal(err)
	}
	views, err := svc.ViewAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range views {
		if view.Account.ID == empty.ID {
			found = true
			if !view.Deletable {
				t.Fatal("empty account should be deletable")
			}
		}
	}
	if !found {
		t.Fatal("missing account view")
	}
}

func TestAccountFlagsPersistOutsideLedger(t *testing.T) {
	svc, _ := testService(t, nil)
	svc.WithAccountFlags(&memFlags{})
	boot(t, svc, "EUR")
	ctx := context.Background()
	eur := "EUR"

	cash, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name: "Cash", AccountType: "asset", NativeCommodityID: &eur, Liquid: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.CreateAccount(ctx, CreateAccountInput{
		Name: "Vault", AccountType: "asset", NativeCommodityID: &eur, Hidden: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	views, err := svc.ViewAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ViewAccount{}
	for _, view := range views {
		byID[view.Account.ID] = view
	}
	if !byID[cash.ID].IsLiquid || byID[cash.ID].IsHidden {
		t.Fatalf("cash flags %+v", byID[cash.ID])
	}
	if !byID[hidden.ID].IsHidden || byID[hidden.ID].IsLiquid {
		t.Fatalf("hidden flags %+v", byID[hidden.ID])
	}

	off := false
	on := true
	if _, err := svc.UpdateAccount(ctx, cash.ID, UpdateAccountInput{Hidden: &on, Liquid: &off}); err != nil {
		t.Fatal(err)
	}
	views, err = svc.ViewAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.Account.ID == cash.ID && (!view.IsHidden || view.IsLiquid) {
			t.Fatalf("updated flags %+v", view)
		}
	}

	if err := svc.DeleteAccount(ctx, hidden.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.flags.ListAccountFlags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := listed[hidden.ID]; ok {
		t.Fatalf("deleted flags still present: %+v", listed)
	}
}
