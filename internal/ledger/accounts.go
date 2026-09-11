package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	accountCodeSlugRe = regexp.MustCompile(`[^A-Za-z0-9 :._-]+`)
	spaceRunRe        = regexp.MustCompile(`\s+`)
)

func (s *Service) CreateAccount(ctx context.Context, input CreateAccountInput) (domain.Account, error) {
	defaultID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	native := input.NativeCommodityID
	if native == nil {
		native = &defaultID
	}
	input.NativeCommodityID = native
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domain.Account{}, domain.InvalidAccount("name is required")
	}
	if strings.TrimSpace(input.Code) == "" {
		code, err := s.uniqueAccountCode(ctx, accountCodeFor(input.AccountType, input.Name))
		if err != nil {
			return domain.Account{}, err
		}
		input.Code = code
	}
	account, err := s.createAccountUnchecked(ctx, input)
	if err != nil {
		return domain.Account{}, err
	}
	if err := s.postOpeningBalance(ctx, account, input.OpeningBalanceMinor); err != nil {
		return domain.Account{}, err
	}
	if err := s.putAccountFlags(ctx, account.ID, AccountFlags{Hidden: input.Hidden, Liquid: input.Liquid}); err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

func (s *Service) UpdateAccount(ctx context.Context, id string, input UpdateAccountInput) (domain.Account, error) {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return domain.Account{}, err
	}
	existing, err := s.GetAccount(ctx, id)
	if err != nil {
		return domain.Account{}, err
	}
	if existing == nil {
		return domain.Account{}, domain.AccountNotFound(id)
	}

	name := existing.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	if name == "" {
		return domain.Account{}, domain.InvalidAccount("name is required")
	}

	accountType := string(existing.AccountType)
	if input.AccountType != nil {
		accountType = strings.TrimSpace(*input.AccountType)
	}
	if !domain.IsAccountType(accountType) {
		return domain.Account{}, domain.InvalidAccountType(accountType)
	}

	native := existing.NativeCommodityID
	if input.NativeCommodityID != nil {
		commodityID := strings.TrimSpace(*input.NativeCommodityID)
		if commodityID == "" {
			return domain.Account{}, domain.InvalidCurrency("Unknown commodity: ")
		}
		commodity, err := s.GetCommodity(ctx, commodityID)
		if err != nil {
			return domain.Account{}, err
		}
		if commodity == nil {
			return domain.Account{}, domain.InvalidCurrency("Unknown commodity: " + commodityID)
		}
		if commodity.ID != existing.NativeCommodityID {
			hasPostings, err := s.accountHasPostings(ctx, id)
			if err != nil {
				return domain.Account{}, err
			}
			if hasPostings {
				return domain.Account{}, domain.AccountNotEmpty(id)
			}
			native = commodity.ID
		}
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE accounts
		SET name = $1, account_type = $2, native_commodity_id = $3
		WHERE id = $4
	`, name, accountType, native, id); err != nil {
		return domain.Account{}, err
	}
	if input.Hidden != nil || input.Liquid != nil {
		current := AccountFlags{}
		stored, err := s.listAccountFlags(ctx)
		if err != nil {
			return domain.Account{}, err
		}
		if flags, ok := stored[id]; ok {
			current = flags
		}
		if input.Hidden != nil {
			current.Hidden = *input.Hidden
		}
		if input.Liquid != nil {
			current.Liquid = *input.Liquid
		}
		if err := s.putAccountFlags(ctx, id, current); err != nil {
			return domain.Account{}, err
		}
	}
	s.invalidate(ctx, CacheKindAccounts, CacheKindNetWorth, CacheKindPeriods, CacheKindRetranslate)
	if err := s.RecordAudit(ctx, "account.updated", map[string]any{
		"accountId": id, "name": name, "accountType": accountType,
	}); err != nil {
		return domain.Account{}, err
	}
	account, err := s.GetAccount(ctx, id)
	if err != nil {
		return domain.Account{}, err
	}
	if account == nil {
		return domain.Account{}, domain.AccountNotFound(id)
	}
	return *account, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id string) error {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return err
	}
	existing, err := s.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return domain.AccountNotFound(id)
	}
	hasPostings, err := s.accountHasPostings(ctx, id)
	if err != nil {
		return err
	}
	if hasPostings {
		return domain.AccountNotEmpty(id)
	}
	var children int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE parent_id = $1`, id).Scan(&children); err != nil {
		return err
	}
	if children > 0 {
		return domain.AccountNotEmpty(id)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id); err != nil {
		return err
	}
	s.deleteAccountFlags(ctx, id)
	s.invalidate(ctx, CacheKindAccounts, CacheKindNetWorth, CacheKindPeriods, CacheKindRetranslate)
	return s.RecordAudit(ctx, "account.deleted", map[string]any{"accountId": id, "code": existing.Code})
}

func (s *Service) postOpeningBalance(ctx context.Context, account domain.Account, amount *big.Int) error {
	if amount == nil || amount.Sign() == 0 {
		return nil
	}
	opening, err := s.ensureAccount(
		ctx,
		OpeningEquityCodePrefix+":"+account.NativeCommodityID,
		"Opening",
		string(domain.AccountEquity),
		account.NativeCommodityID,
	)
	if err != nil {
		return err
	}
	desc := "Carry In By App"
	input := PostTransactionInput{
		CreditAmountMinor: new(big.Int).Abs(amount),
		Description:       &desc,
	}
	if amount.Sign() > 0 {
		input.CreditAccountID = opening.ID
		input.DebitAccountID = account.ID
	} else {
		input.CreditAccountID = account.ID
		input.DebitAccountID = opening.ID
	}
	_, err = s.PostTransaction(ctx, input)
	return err
}

func (s *Service) uniqueAccountCode(ctx context.Context, base string) (string, error) {
	code := base
	for i := 2; i < 1000; i++ {
		existing, err := s.GetAccountByCode(ctx, code)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return code, nil
		}
		code = fmt.Sprintf("%s-%d", base, i)
	}
	return "", domain.AccountAlreadyExists(base)
}

func accountCodeFor(accountType, name string) string {
	slug := spaceRunRe.ReplaceAllString(strings.TrimSpace(name), " ")
	slug = accountCodeSlugRe.ReplaceAllString(slug, "")
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "Account"
	}
	return accountTypePrefix(accountType) + ":" + slug
}

func accountTypePrefix(accountType string) string {
	switch domain.AccountType(accountType) {
	case domain.AccountAsset:
		return "Assets"
	case domain.AccountLiability:
		return "Liabilities"
	case domain.AccountEquity:
		return "Equity"
	case domain.AccountIncome:
		return "Income"
	case domain.AccountExpense:
		return "Expenses"
	default:
		return "Accounts"
	}
}

func (s *Service) accountHasPostings(ctx context.Context, id string) (bool, error) {
	var count int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM postings WHERE account_id = $1`, id).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) accountsWithPostings(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT account_id FROM postings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapAccountWriteError(err error, code string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if isUniqueViolation(err) {
		return domain.AccountAlreadyExists(code)
	}
	return err
}
