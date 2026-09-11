package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TransactionCostAccountPrefix  = "Expenses:Transaction Cost"
	ForexRetranslationExpenseCode = "Expenses:Forex Retranslation"
	RetranslationEquityCode       = "Equity:Retranslation"
	OpeningEquityCodePrefix       = "Equity:Opening"
	CapitalGainsAccountPrefix     = "Income:Capital Gains"
	// CapitalGainsAccountCode is the legacy unscoped code; prefer CapitalGainsAccountCodeFor.
	CapitalGainsAccountCode      = CapitalGainsAccountPrefix
	functionalCommodityConfigKey = "functional_commodity_id"
)

func TransactionCostAccountCodeFor(commodityID string) string {
	return TransactionCostAccountPrefix + ":" + commodityID
}

func CapitalGainsAccountCodeFor(commodityID string) string {
	return CapitalGainsAccountPrefix + ":" + commodityID
}

func IsCapitalGainsAccountCode(code string) bool {
	return code == CapitalGainsAccountCode || strings.HasPrefix(code, CapitalGainsAccountPrefix+":")
}

type UiTheme string

const (
	ThemeLight UiTheme = "light"
	ThemeDark  UiTheme = "dark"
)

type ListJournalEntriesInput struct {
	Limit     *int
	Offset    *int
	AccountID *string
	From      *string
	To        *string
	Q         *string
	Status    string // posted | draft | all (default all)
}

type ListJournalEntriesResult struct {
	Entries []domain.JournalEntry
	HasMore bool
	Limit   int
	Offset  int
}

type CreateCommodityInput struct {
	Code       string
	Name       string
	MinorUnits int
	Kind       domain.CommodityKind
}

type CreateAccountInput struct {
	Code                string
	Name                string
	AccountType         string
	ParentID            *string
	NativeCommodityID   *string
	OpeningBalanceMinor *big.Int
	Hidden              bool
	Liquid              bool
}

type UpdateAccountInput struct {
	Name              *string
	AccountType       *string
	NativeCommodityID *string
	Hidden            *bool
	Liquid            *bool
}

type UpdateJournalEntryInput struct {
	Description *string
	Datetime    *string
}

type ExchangeCurrencyInput struct {
	EffectiveDate   string
	FromAccountID   string
	ToAccountID     string
	FromAmountMinor *big.Int
	ToAmountMinor   *big.Int
	Description     *string
}

type ExchangeResult struct {
	Entry                domain.JournalEntry
	ExpectedToMinor      string
	ResidualToMinor      string
	TransactionCostMinor string
	EcbRateToPerFrom     domain.Rational
}

type PostJournalEntryInput struct {
	EffectiveDate string
	Description   *string
	Postings      []domain.PostingInput
	PostedAt      *string
}

type Service struct {
	db                   *pgxpool.Pool
	cache                Cache
	flags                AccountFlagsStore
	settings             SettingsStore
	ecbCache             EcbCache
	fetchEcbRate         RateFetcher
	presentationOverride *string // nil-Mongo / SQL-only tests when app_config has no F yet
	rateMu               sync.Mutex
	rateMemo             map[string]rateMemo
	changeMu             sync.Mutex
	changeRunning        bool
	changeJob            FunctionalChangeJob
}

type rateMemo struct {
	rate domain.Rational
	err  error
}

func New(db *pgxpool.Pool, fetchEcbRate RateFetcher) *Service {
	if fetchEcbRate == nil {
		fetchEcbRate = FetchEcbRatePerEur
	}
	s := &Service{db: db, rateMemo: map[string]rateMemo{}}
	inner := s.wrapEcbFetch(fetchEcbRate)
	s.fetchEcbRate = func(ctx context.Context, currency, date string) (domain.Rational, error) {
		return s.cachedEcbRate(ctx, inner, currency, date)
	}
	return s
}

// WithPresentationOverride sets F for SQL-only tests without writing app_config.
func (s *Service) WithPresentationOverride(commodityID string) *Service {
	s.presentationOverride = &commodityID
	return s
}

func (s *Service) cachedEcbRate(ctx context.Context, fetch RateFetcher, currency, date string) (domain.Rational, error) {
	if err := rejectFutureEcbDate(date); err != nil {
		return domain.Rational{}, err
	}
	if !domain.IsEcbQuoteCurrency(currency) && !ecbSpotAvailable(date, nowUTC()) {
		// Do not memoize: a process that stays up past 16:00 Europe/Berlin must still fetch today.
		return domain.Rational{}, domain.MissingEcbRate(currency, date)
	}
	key := currency + "|" + date
	s.rateMu.Lock()
	if hit, ok := s.rateMemo[key]; ok {
		s.rateMu.Unlock()
		return hit.rate, hit.err
	}
	s.rateMu.Unlock()
	rate, err := fetch(ctx, currency, date)
	s.rateMu.Lock()
	s.rateMemo[key] = rateMemo{rate: rate, err: err}
	s.rateMu.Unlock()
	return rate, err
}

func (s *Service) GetDefaultCommodityID(ctx context.Context) (*string, error) {
	if s.db != nil {
		var value string
		err := s.db.QueryRow(ctx, `SELECT value FROM app_config WHERE key = $1`, functionalCommodityConfigKey).Scan(&value)
		if err == nil && strings.TrimSpace(value) != "" {
			id := strings.TrimSpace(value)
			return &id, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if s.presentationOverride != nil {
		return s.presentationOverride, nil
	}
	return nil, nil
}

func (s *Service) civilDateOf(ctx context.Context, timestamp string) (string, error) {
	return domain.CivilDateIn(timestamp, s.LedgerLocation(ctx))
}

func (s *Service) rejectFutureDatetime(ctx context.Context, canonical string) error {
	return domain.RejectFutureIn(canonical, s.LedgerLocation(ctx), time.Now())
}

func (s *Service) RequireDefaultCommodityID(ctx context.Context) (string, error) {
	id, err := s.GetDefaultCommodityID(ctx)
	if err != nil {
		return "", err
	}
	if id == nil {
		return "", domain.FunctionalCurrencyNotSet()
	}
	return *id, nil
}

func (s *Service) SetDefaultCurrency(ctx context.Context, commodityID string) (string, domain.Account, error) {
	return s.setFunctionalCurrency(ctx, commodityID, false)
}

func (s *Service) ChangeFunctionalCurrency(ctx context.Context, commodityID string) (string, domain.Account, error) {
	return s.setFunctionalCurrency(ctx, commodityID, true)
}

func (s *Service) setFunctionalCurrency(ctx context.Context, commodityID string, confirm bool) (string, domain.Account, error) {
	commodity, err := s.GetCommodity(ctx, commodityID)
	if err != nil {
		return "", domain.Account{}, err
	}
	if commodity == nil || commodity.Kind != domain.CommodityCurrency {
		return "", domain.Account{}, domain.InvalidCurrency("Unknown or non-currency commodity: " + commodityID)
	}
	if !domain.IsSupportedCurrency(commodity.ID) {
		return "", domain.Account{}, domain.UnsupportedCurrency(commodity.ID)
	}

	existing, err := s.GetDefaultCommodityID(ctx)
	if err != nil {
		return "", domain.Account{}, err
	}

	changeDate, err := s.civilDateOf(ctx, nowISO())
	if err != nil {
		return "", domain.Account{}, err
	}

	if existing != nil && *existing != commodity.ID {
		if !confirm {
			return "", domain.Account{}, domain.FunctionalCurrencyChangeNotConfirmed(*existing, commodity.ID)
		}
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return "", domain.Account{}, err
		}
		defer tx.Rollback(ctx)
		rate, err := s.restampFunctionalCosts(ctx, tx, *existing, commodity.ID, changeDate)
		if err != nil {
			return "", domain.Account{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_config (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value
		`, functionalCommodityConfigKey, commodity.ID); err != nil {
			return "", domain.Account{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", domain.Account{}, err
		}
		s.presentationOverride = &commodity.ID
		s.invalidate(ctx, CacheKindJournal)
		s.invalidateDerived(ctx)
		if err := s.RecordAudit(ctx, "functional.set", map[string]any{
			"from":            *existing,
			"to":              commodity.ID,
			"effectiveDate":   changeDate,
			"rateNumerator":   rate.Numerator.String(),
			"rateDenominator": rate.Denominator.String(),
		}); err != nil {
			return "", domain.Account{}, err
		}
		_ = s.putSetting(ctx, presentationCommoditySettingKey, commodity.ID)
		txnCost, err := s.ensureTransactionCostAccount(ctx, commodity.ID)
		if err != nil {
			return "", domain.Account{}, err
		}
		return commodity.ID, txnCost, nil
	}

	if existing == nil {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO app_config (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value
		`, functionalCommodityConfigKey, commodity.ID); err != nil {
			return "", domain.Account{}, err
		}
		id := commodity.ID
		s.presentationOverride = &id
		s.invalidateDerived(ctx)
		if err := s.RecordAudit(ctx, "functional.set", map[string]any{
			"commodityId": commodity.ID,
		}); err != nil {
			return "", domain.Account{}, err
		}
		_ = s.putSetting(ctx, presentationCommoditySettingKey, commodity.ID)
	}

	txnCost, err := s.ensureTransactionCostAccount(ctx, commodity.ID)
	if err != nil {
		return "", domain.Account{}, err
	}
	return commodity.ID, txnCost, nil
}

func (s *Service) ensureTransactionCostAccount(ctx context.Context, commodityID string) (domain.Account, error) {
	code := TransactionCostAccountCodeFor(commodityID)
	return s.ensureAccount(ctx, code, "Transaction Cost", string(domain.AccountExpense), commodityID)
}

func (s *Service) ensureExpenseAccount(ctx context.Context, code, name, commodityID string) (domain.Account, error) {
	return s.ensureAccount(ctx, code, name, string(domain.AccountExpense), commodityID)
}

func (s *Service) ensureAccount(ctx context.Context, code, name, accountType, commodityID string) (domain.Account, error) {
	existing, err := s.GetAccountByCode(ctx, code)
	if err != nil {
		return domain.Account{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	return s.createAccountUnchecked(ctx, CreateAccountInput{
		Code:              code,
		Name:              name,
		AccountType:       accountType,
		NativeCommodityID: &commodityID,
	})
}

func (s *Service) ListCommodities(ctx context.Context) ([]domain.Commodity, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetCommodities(ctx)
		if err != nil {
			log.Printf("commodities cache get: %v", err)
		} else if hit {
			return items, nil
		}
	}
	rows, err := s.db.Query(ctx, `SELECT id, code, name, minor_units, kind FROM commodities ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Commodity
	for rows.Next() {
		c, err := scanCommodity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []domain.Commodity{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.cache != nil {
		if putErr := s.cache.PutCommodities(ctx, out); putErr != nil {
			log.Printf("commodities cache put: %v", putErr)
		}
	}
	return out, nil
}

func (s *Service) GetCommodity(ctx context.Context, id string) (*domain.Commodity, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetCommodities(ctx)
		if err != nil {
			log.Printf("commodities cache get: %v", err)
		} else if hit {
			for i := range items {
				if items[i].ID == id || items[i].Code == id {
					c := items[i]
					return &c, nil
				}
			}
			return nil, nil
		}
	}
	row := s.db.QueryRow(ctx, `SELECT id, code, name, minor_units, kind FROM commodities WHERE id = $1 OR code = $2`, id, id)
	c, err := scanCommodity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

var commodityCodeRe = regexp.MustCompile(`^[A-Z0-9]{2,16}$`)

func (s *Service) CreateCommodity(ctx context.Context, input CreateCommodityInput) (domain.Commodity, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if !commodityCodeRe.MatchString(code) {
		return domain.Commodity{}, domain.InvalidCurrency("Invalid commodity code: " + input.Code)
	}
	if input.MinorUnits < 0 || input.MinorUnits > 18 {
		return domain.Commodity{}, domain.InvalidCurrency(fmt.Sprintf("Invalid minorUnits: %d", input.MinorUnits))
	}
	kind := input.Kind
	if kind == "" {
		kind = domain.CommodityCurrency
	}
	if kind != domain.CommodityCurrency && kind != domain.CommoditySecurity && kind != domain.CommodityOther {
		return domain.Commodity{}, domain.InvalidCurrency("Invalid commodity kind: " + string(kind))
	}

	existing, err := s.GetCommodity(ctx, code)
	if err != nil {
		return domain.Commodity{}, err
	}
	if existing != nil {
		return domain.Commodity{}, domain.CommodityAlreadyExists(code)
	}
	if kind == domain.CommodityCurrency && !domain.IsSupportedCurrency(code) {
		return domain.Commodity{}, domain.UnsupportedCurrency(code)
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO commodities (id, code, name, minor_units, kind, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, code, code, strings.TrimSpace(input.Name), input.MinorUnits, string(kind), nowISO()); err != nil {
		return domain.Commodity{}, err
	}
	s.invalidate(ctx, CacheKindCommodities)
	if err := s.RecordAudit(ctx, "commodity.created", map[string]any{
		"code": code, "minorUnits": input.MinorUnits, "kind": kind,
	}); err != nil {
		return domain.Commodity{}, err
	}
	c, err := s.GetCommodity(ctx, code)
	if err != nil {
		return domain.Commodity{}, err
	}
	if c == nil {
		return domain.Commodity{}, domain.InvalidCurrency("Failed to create commodity: " + code)
	}
	return *c, nil
}

func (s *Service) createAccountUnchecked(ctx context.Context, input CreateAccountInput) (domain.Account, error) {
	if !domain.IsAccountType(input.AccountType) {
		return domain.Account{}, domain.InvalidAccountType(input.AccountType)
	}
	if input.NativeCommodityID == nil {
		return domain.Account{}, domain.InvalidCurrency("Unknown commodity: ")
	}
	commodity, err := s.GetCommodity(ctx, *input.NativeCommodityID)
	if err != nil {
		return domain.Account{}, err
	}
	if commodity == nil {
		return domain.Account{}, domain.InvalidCurrency("Unknown commodity: " + *input.NativeCommodityID)
	}
	if commodity.Kind == domain.CommodityCurrency && !domain.IsSupportedCurrency(commodity.ID) {
		return domain.Account{}, domain.UnsupportedCurrency(commodity.ID)
	}

	id := newID()
	createdAt := nowISO()
	if _, err := s.db.Exec(ctx, `
		INSERT INTO accounts (id, code, name, account_type, parent_id, native_commodity_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, input.Code, input.Name, input.AccountType, input.ParentID, *input.NativeCommodityID, createdAt); err != nil {
		return domain.Account{}, mapAccountWriteError(err, input.Code)
	}
	s.invalidate(ctx, CacheKindAccounts, CacheKindNetWorth, CacheKindPeriods, CacheKindRetranslate)
	if err := s.RecordAudit(ctx, "account.created", map[string]any{"accountId": id, "code": input.Code}); err != nil {
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

func (s *Service) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetAccounts(ctx)
		if err != nil {
			log.Printf("accounts cache get: %v", err)
		} else if hit {
			return items, nil
		}
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, code, name, account_type, parent_id, native_commodity_id, created_at
		FROM accounts ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []domain.Account{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.cache != nil {
		if putErr := s.cache.PutAccounts(ctx, out); putErr != nil {
			log.Printf("accounts cache put: %v", putErr)
		}
	}
	return out, nil
}

func (s *Service) GetAccount(ctx context.Context, id string) (*domain.Account, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetAccounts(ctx)
		if err != nil {
			log.Printf("accounts cache get: %v", err)
		} else if hit {
			for i := range items {
				if items[i].ID == id {
					a := items[i]
					return &a, nil
				}
			}
			return nil, nil
		}
	}
	row := s.db.QueryRow(ctx, `
		SELECT id, code, name, account_type, parent_id, native_commodity_id, created_at
		FROM accounts WHERE id = $1
	`, id)
	a, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) GetAccountByCode(ctx context.Context, code string) (*domain.Account, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetAccounts(ctx)
		if err != nil {
			log.Printf("accounts cache get: %v", err)
		} else if hit {
			for i := range items {
				if items[i].Code == code {
					a := items[i]
					return &a, nil
				}
			}
			return nil, nil
		}
	}
	row := s.db.QueryRow(ctx, `
		SELECT id, code, name, account_type, parent_id, native_commodity_id, created_at
		FROM accounts WHERE code = $1
	`, code)
	a, err := scanAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) resolvePostings(ctx context.Context, postings []domain.PostingInput) ([]domain.ResolvedPosting, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accountMap := map[string]domain.Account{}
	for _, a := range accounts {
		accountMap[a.ID] = a
	}
	for _, posting := range postings {
		if err := domain.ValidatePostingUnits(posting.Units); err != nil {
			return nil, err
		}
		if err := domain.ValidateCostAndPrice(posting.Cost, posting.Price); err != nil {
			return nil, err
		}
		account, ok := accountMap[posting.AccountID]
		if !ok {
			return nil, domain.AccountNotFound(posting.AccountID)
		}
		if err := domain.ValidateAccountCurrency(account, posting.Units.CommodityID); err != nil {
			return nil, err
		}
	}
	resolved, err := domain.ResolvePostings(postings)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateBalance(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (s *Service) PostJournalEntry(ctx context.Context, input PostJournalEntryInput) (domain.JournalEntry, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	entryID := newID()
	createdAt := nowISO()
	effectiveAt, err := domain.CanonicalUTC(input.EffectiveDate)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	if err := s.rejectFutureDatetime(ctx, effectiveAt); err != nil {
		return domain.JournalEntry{}, err
	}
	postedAt := createdAt
	if input.PostedAt != nil && *input.PostedAt != "" {
		postedAt, err = domain.CanonicalUTC(*input.PostedAt)
		if err != nil {
			return domain.JournalEntry{}, err
		}
	}
	stamped, err := s.stampReportingLots(ctx, input.Postings, reportingID, effectiveAt, postedAt)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	input.Postings = stamped
	resolved, err := s.resolvePostings(ctx, input.Postings)
	if err != nil {
		return domain.JournalEntry{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO journal_entries (id, effective_date, description, status, created_at, posted_at)
		VALUES ($1, $2, $3, 'posted', $4, $5)
	`, entryID, effectiveAt, input.Description, createdAt, postedAt); err != nil {
		return domain.JournalEntry{}, err
	}
	if err := insertPostings(ctx, tx, entryID, resolved); err != nil {
		return domain.JournalEntry{}, err
	}

	payload, _ := json.Marshal(map[string]any{"journalEntryId": entryID})
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (id, event_type, payload_json, created_at) VALUES ($1,$2,$3,$4)`,
		newID(), "journal_entry.posted", string(payload), createdAt); err != nil {
		return domain.JournalEntry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.JournalEntry{}, err
	}
	s.refreshBalancesCache(ctx)
	s.invalidateDerived(ctx)
	if s.cache != nil {
		if putErr := s.cache.PutPostings(ctx, entryID, resolved); putErr != nil {
			log.Printf("postings cache put: %v", putErr)
		}
	}

	entry, err := s.GetJournalEntry(ctx, entryID)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	if entry == nil {
		return domain.JournalEntry{}, domain.JournalEntryNotFound(entryID)
	}
	return *entry, nil
}

func (s *Service) DeleteJournalEntry(ctx context.Context, id string) error {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return err
	}
	existing, err := s.GetJournalEntry(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return domain.JournalEntryNotFound(id)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM postings WHERE journal_entry_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM journal_entries WHERE id = $1`, id); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"journalEntryId": id})
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (id, event_type, payload_json, created_at) VALUES ($1,$2,$3,$4)`,
		newID(), "journal_entry.deleted", string(payload), nowISO()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if s.cache != nil {
		if delErr := s.cache.DeletePostings(ctx, id); delErr != nil {
			log.Printf("postings cache delete: %v", delErr)
		}
	}
	s.refreshBalancesCache(ctx)
	s.invalidateDerived(ctx)
	return nil
}

func (s *Service) UpdateJournalEntry(ctx context.Context, id string, input UpdateJournalEntryInput) (domain.JournalEntry, error) {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return domain.JournalEntry{}, err
	}
	existing, err := s.GetJournalEntry(ctx, id)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	if existing == nil {
		return domain.JournalEntry{}, domain.JournalEntryNotFound(id)
	}

	datetime := existing.EffectiveDate
	if input.Datetime != nil && strings.TrimSpace(*input.Datetime) != "" {
		datetime, err = domain.CanonicalUTC(*input.Datetime)
		if err != nil {
			return domain.JournalEntry{}, err
		}
		if err := s.rejectFutureDatetime(ctx, datetime); err != nil {
			return domain.JournalEntry{}, err
		}
	}
	description := existing.Description
	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		if trimmed == "" {
			description = nil
		} else {
			description = &trimmed
		}
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE journal_entries
		SET effective_date = $1, description = $2, posted_at = $3
		WHERE id = $4
	`, datetime, description, datetime, id); err != nil {
		return domain.JournalEntry{}, err
	}
	if datetime != existing.EffectiveDate {
		s.invalidateDerived(ctx)
	}
	if err := s.RecordAudit(ctx, "journal_entry.updated", map[string]any{
		"journalEntryId": id, "datetime": datetime,
	}); err != nil {
		return domain.JournalEntry{}, err
	}
	entry, err := s.GetJournalEntry(ctx, id)
	if err != nil {
		return domain.JournalEntry{}, err
	}
	if entry == nil {
		return domain.JournalEntry{}, domain.JournalEntryNotFound(id)
	}
	return *entry, nil
}

func (s *Service) ExchangeCurrency(ctx context.Context, input ExchangeCurrencyInput) (ExchangeResult, error) {
	defaultCommodityID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return ExchangeResult{}, err
	}
	txnCost, err := s.ensureTransactionCostAccount(ctx, defaultCommodityID)
	if err != nil {
		return ExchangeResult{}, err
	}
	fromAccount, err := s.GetAccount(ctx, input.FromAccountID)
	if err != nil {
		return ExchangeResult{}, err
	}
	if fromAccount == nil {
		return ExchangeResult{}, domain.AccountNotFound(input.FromAccountID)
	}
	toAccount, err := s.GetAccount(ctx, input.ToAccountID)
	if err != nil {
		return ExchangeResult{}, err
	}
	if toAccount == nil {
		return ExchangeResult{}, domain.AccountNotFound(input.ToAccountID)
	}
	if fromAccount.AccountType != domain.AccountAsset || toAccount.AccountType != domain.AccountAsset {
		return ExchangeResult{}, domain.InvalidExchange("exchangeCurrency requires two asset accounts")
	}

	fromCommodity, err := s.GetCommodity(ctx, fromAccount.NativeCommodityID)
	if err != nil {
		return ExchangeResult{}, err
	}
	toCommodity, err := s.GetCommodity(ctx, toAccount.NativeCommodityID)
	if err != nil {
		return ExchangeResult{}, err
	}
	defaultCommodity, err := s.GetCommodity(ctx, defaultCommodityID)
	if err != nil {
		return ExchangeResult{}, err
	}
	if fromCommodity == nil || toCommodity == nil || defaultCommodity == nil {
		return ExchangeResult{}, domain.InvalidCurrency("Missing commodity definition for exchange")
	}

	ecbDate, err := s.civilDateOf(ctx, input.EffectiveDate)
	if err != nil {
		return ExchangeResult{}, err
	}
	fromObs, err := FetchEcbRatePerEurWithLookback(ctx, fromCommodity.ID, ecbDate, 10, s.fetchEcbRate)
	if err != nil {
		return ExchangeResult{}, err
	}
	toObs, err := FetchEcbRatePerEurWithLookback(ctx, toCommodity.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		return ExchangeResult{}, err
	}
	defaultObs, err := FetchEcbRatePerEurWithLookback(ctx, defaultCommodity.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		return ExchangeResult{}, err
	}

	rateToPerFrom, err := domain.CrossRateBPerA(fromObs.Rate, toObs.Rate)
	if err != nil {
		return ExchangeResult{}, err
	}
	rateDefaultPerTo, err := domain.CrossRateBPerA(toObs.Rate, defaultObs.Rate)
	if err != nil {
		return ExchangeResult{}, err
	}

	built, err := domain.BuildExchangePostings(domain.BuildExchangePostingsInput{
		FromAccountID:            fromAccount.ID,
		ToAccountID:              toAccount.ID,
		TransactionCostAccountID: txnCost.ID,
		FromCommodityID:          fromCommodity.ID,
		ToCommodityID:            toCommodity.ID,
		DefaultCommodityID:       defaultCommodity.ID,
		FromAmountMinor:          input.FromAmountMinor,
		ToAmountMinor:            input.ToAmountMinor,
		FromMinorUnits:           fromCommodity.MinorUnits,
		ToMinorUnits:             toCommodity.MinorUnits,
		DefaultMinorUnits:        defaultCommodity.MinorUnits,
		RateToPerFrom:            rateToPerFrom,
		RateDefaultPerTo:         rateDefaultPerTo,
	})
	if err != nil {
		return ExchangeResult{}, err
	}

	desc := fmt.Sprintf("Exchange %s → %s", fromCommodity.Code, toCommodity.Code)
	if input.Description != nil && *input.Description != "" {
		desc = *input.Description
	}
	entry, err := s.PostJournalEntry(ctx, PostJournalEntryInput{
		EffectiveDate: input.EffectiveDate,
		Description:   &desc,
		Postings:      built.Postings,
	})
	if err != nil {
		return ExchangeResult{}, err
	}

	if err := s.RecordAudit(ctx, "exchange.posted", map[string]any{
		"journalEntryId":       entry.ID,
		"fromAccountId":        fromAccount.ID,
		"toAccountId":          toAccount.ID,
		"effectiveDate":        input.EffectiveDate,
		"ecbObservedDate":      fromObs.ObservedDate,
		"expectedToMinor":      built.ExpectedToMinor.String(),
		"residualToMinor":      built.ResidualToMinor.String(),
		"transactionCostMinor": built.TransactionCostMinor.String(),
	}); err != nil {
		return ExchangeResult{}, err
	}

	return ExchangeResult{
		Entry:                entry,
		ExpectedToMinor:      built.ExpectedToMinor.String(),
		ResidualToMinor:      built.ResidualToMinor.String(),
		TransactionCostMinor: built.TransactionCostMinor.String(),
		EcbRateToPerFrom:     rateToPerFrom,
	}, nil
}

func (s *Service) GetJournalEntry(ctx context.Context, id string) (*domain.JournalEntry, error) {
	var row journalRow
	err := s.db.QueryRow(ctx, `
		SELECT id, effective_date, description, status, created_at, posted_at
		FROM journal_entries WHERE id = $1
	`, id).Scan(&row.ID, &row.EffectiveDate, &row.Description, &row.Status, &row.CreatedAt, &row.PostedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	postings, err := s.loadPostings(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	entry := mapJournal(row, postings[id])
	return &entry, nil
}

func (s *Service) ListJournalEntries(ctx context.Context, input ListJournalEntriesInput) (ListJournalEntriesResult, error) {
	limit := 50
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	offset := 0
	if input.Offset != nil {
		offset = *input.Offset
	}
	if offset < 0 {
		offset = 0
	}
	status := input.Status
	if status == "" {
		status = "all"
	}

	var where []string
	var binds []any
	n := 1
	if status == "all" {
		where = append(where, `j.status IN ('posted', 'draft')`)
	} else {
		where = append(where, fmt.Sprintf(`j.status = $%d`, n))
		binds = append(binds, status)
		n++
	}
	loc := s.LedgerLocation(ctx)
	if input.From != nil && *input.From != "" {
		from := *input.From
		if start, err := domain.RangeStartIn(from, loc); err == nil {
			from = start
		}
		where = append(where, fmt.Sprintf(`j.effective_date >= $%d`, n))
		binds = append(binds, from)
		n++
	}
	if input.To != nil && *input.To != "" {
		to := *input.To
		if end, err := domain.RangeEndIn(to, loc); err == nil {
			to = end
		}
		where = append(where, fmt.Sprintf(`j.effective_date <= $%d`, n))
		binds = append(binds, to)
		n++
	}
	if input.Q != nil && strings.TrimSpace(*input.Q) != "" {
		needle := "%" + strings.ToLower(strings.TrimSpace(*input.Q)) + "%"
		where = append(where, fmt.Sprintf(`(lower(coalesce(j.description, '')) LIKE $%d OR lower(j.id) LIKE $%d OR EXISTS (
			SELECT 1 FROM postings p
			JOIN accounts a ON a.id = p.account_id
			WHERE p.journal_entry_id = j.id
			  AND (lower(a.name) LIKE $%d OR lower(a.code) LIKE $%d)
		))`, n, n+1, n+2, n+3))
		binds = append(binds, needle, needle, needle, needle)
		n += 4
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	orderSQL := `ORDER BY j.effective_date DESC, j.posted_at DESC, j.id DESC`
	fetchLimit := limit + 1

	var query string
	args := append([]any{}, binds...)
	if input.AccountID != nil && *input.AccountID != "" {
		query = fmt.Sprintf(`SELECT j.id
			FROM postings p
			JOIN journal_entries j ON j.id = p.journal_entry_id
			%s AND p.account_id = $%d
			GROUP BY j.id, j.effective_date, j.posted_at
			%s
			LIMIT $%d OFFSET $%d`, whereSQL, n, orderSQL, n+1, n+2)
		args = append(args, *input.AccountID, fetchLimit, offset)
	} else {
		query = fmt.Sprintf(`SELECT j.id FROM journal_entries j
			%s
			%s
			LIMIT $%d OFFSET $%d`, whereSQL, orderSQL, n, n+1)
		args = append(args, fetchLimit, offset)
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return ListJournalEntriesResult{}, err
	}
	defer rows.Close()
	var fetchedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ListJournalEntriesResult{}, err
		}
		fetchedIDs = append(fetchedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return ListJournalEntriesResult{}, err
	}

	hasMore := len(fetchedIDs) > limit
	ids := fetchedIDs
	if hasMore {
		ids = fetchedIDs[:limit]
	}
	if len(ids) == 0 {
		return ListJournalEntriesResult{Entries: []domain.JournalEntry{}, HasMore: false, Limit: limit, Offset: offset}, nil
	}

	entryRows, err := s.db.Query(ctx, `
		SELECT id, effective_date, description, status, created_at, posted_at
		FROM journal_entries WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return ListJournalEntriesResult{}, err
	}
	defer entryRows.Close()
	byID := map[string]journalRow{}
	for entryRows.Next() {
		var row journalRow
		if err := entryRows.Scan(&row.ID, &row.EffectiveDate, &row.Description, &row.Status, &row.CreatedAt, &row.PostedAt); err != nil {
			return ListJournalEntriesResult{}, err
		}
		byID[row.ID] = row
	}
	if err := entryRows.Err(); err != nil {
		return ListJournalEntriesResult{}, err
	}

	postings, err := s.loadPostings(ctx, ids)
	if err != nil {
		return ListJournalEntriesResult{}, err
	}

	entries := make([]domain.JournalEntry, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			continue
		}
		entries = append(entries, mapJournal(row, postings[id]))
	}
	return ListJournalEntriesResult{Entries: entries, HasMore: hasMore, Limit: limit, Offset: offset}, nil
}

func (s *Service) loadAccountBalancesFromSQL(ctx context.Context) ([]domain.AccountBalance, error) {
	rows, err := s.db.Query(ctx, `
		SELECT account_id, commodity_id, units_minor
		FROM account_balances WHERE units_minor != 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AccountBalance
	for rows.Next() {
		var accountID, commodityID string
		var minor int64
		if err := rows.Scan(&accountID, &commodityID, &minor); err != nil {
			return nil, err
		}
		out = append(out, domain.AccountBalance{
			AccountID: accountID, CommodityID: commodityID, Minor: domain.Int(minor),
		})
	}
	if out == nil {
		out = []domain.AccountBalance{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) refreshBalancesCache(ctx context.Context) {
	if s.cache == nil || s.db == nil {
		return
	}
	out, err := s.loadAccountBalancesFromSQL(ctx)
	if err != nil {
		log.Printf("balances cache refresh: %v", err)
		return
	}
	if putErr := s.cache.PutBalances(ctx, out); putErr != nil {
		log.Printf("balances cache put: %v", putErr)
	}
}

func (s *Service) GetAccountBalances(ctx context.Context) ([]domain.AccountBalance, error) {
	if s.cache != nil {
		items, hit, err := s.cache.GetBalances(ctx)
		if err != nil {
			log.Printf("balances cache get: %v", err)
		} else if hit {
			return items, nil
		}
	}
	out, err := s.loadAccountBalancesFromSQL(ctx)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if putErr := s.cache.PutBalances(ctx, out); putErr != nil {
			log.Printf("balances cache put: %v", putErr)
		}
	}
	return out, nil
}

func (s *Service) GetAccountBalance(ctx context.Context, accountID string) (domain.AccountBalance, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return domain.AccountBalance{}, err
	}
	if account == nil {
		return domain.AccountBalance{}, domain.AccountNotFound(accountID)
	}
	if s.cache != nil {
		items, hit, err := s.cache.GetBalances(ctx)
		if err != nil {
			log.Printf("balances cache get: %v", err)
		} else if hit {
			for _, b := range items {
				if b.AccountID == accountID && b.CommodityID == account.NativeCommodityID {
					return b, nil
				}
			}
			return domain.AccountBalance{
				AccountID: accountID, CommodityID: account.NativeCommodityID, Minor: domain.Int(0),
			}, nil
		}
	}
	var minor int64
	err = s.db.QueryRow(ctx, `
		SELECT units_minor FROM account_balances WHERE account_id = $1 AND commodity_id = $2
	`, accountID, account.NativeCommodityID).Scan(&minor)
	if errors.Is(err, pgx.ErrNoRows) {
		minor = 0
	} else if err != nil {
		return domain.AccountBalance{}, err
	}
	return domain.AccountBalance{
		AccountID: accountID, CommodityID: account.NativeCommodityID, Minor: domain.Int(minor),
	}, nil
}

func (s *Service) ListMarketPrices(ctx context.Context) ([]domain.MarketPrice, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return nil, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return nil, err
	}
	needed := map[string]struct{}{}
	commodityByID := map[string]domain.Commodity{}
	for _, c := range commodities {
		commodityByID[c.ID] = c
		if c.Kind == domain.CommodityCurrency {
			needed[c.ID] = struct{}{}
		}
	}
	reporting, ok := commodityByID[reportingID]
	if !ok {
		return nil, domain.InvalidCurrency(reportingID)
	}
	return s.spotMarketPrices(ctx, needed, reporting, commodityByID)
}

func (s *Service) UpsertMarketPrice(ctx context.Context) error {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return err
	}
	return domain.InvalidPrice("currency market prices are computed from ECB and are not stored")
}

func (s *Service) EnsureMarketPrice(ctx context.Context, baseCommodityID, quoteCommodityID, asOfDate string) (domain.MarketPrice, error) {
	if _, err := s.RequireDefaultCommodityID(ctx); err != nil {
		return domain.MarketPrice{}, err
	}
	return s.quoteMarketPrice(ctx, baseCommodityID, quoteCommodityID, asOfDate)
}

func (s *Service) quoteMarketPrice(ctx context.Context, baseCommodityID, quoteCommodityID, asOfDate string) (domain.MarketPrice, error) {
	if asOfDate == "" {
		asOfDate = todayISO()
	} else {
		asOfDate = domain.UTCDate(asOfDate)
	}
	if baseCommodityID == quoteCommodityID {
		r := domain.MustRational(1, 1)
		return domain.MarketPrice{
			BaseCommodityID: baseCommodityID, QuoteCommodityID: quoteCommodityID,
			Price: r, ObservedAt: domain.ClockOnUTCDate(asOfDate, time.Now()), Source: "identity",
		}, nil
	}
	base, err := s.GetCommodity(ctx, baseCommodityID)
	if err != nil {
		return domain.MarketPrice{}, err
	}
	quote, err := s.GetCommodity(ctx, quoteCommodityID)
	if err != nil {
		return domain.MarketPrice{}, err
	}
	if base == nil || quote == nil || base.Kind != domain.CommodityCurrency || quote.Kind != domain.CommodityCurrency {
		return domain.MarketPrice{}, domain.MissingValuationPrice(baseCommodityID, quoteCommodityID)
	}

	baseObs, err := FetchEcbRatePerEurWithLookback(ctx, base.ID, asOfDate, 10, s.fetchEcbRate)
	if err != nil {
		if de, ok := domain.IsDomainError(err); ok && de.Code == "MissingEcbRate" {
			return domain.MarketPrice{}, domain.MissingValuationPrice(baseCommodityID, quoteCommodityID)
		}
		return domain.MarketPrice{}, err
	}
	quoteObs, err := FetchEcbRatePerEurWithLookback(ctx, quote.ID, baseObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		if de, ok := domain.IsDomainError(err); ok && de.Code == "MissingEcbRate" {
			return domain.MarketPrice{}, domain.MissingValuationPrice(baseCommodityID, quoteCommodityID)
		}
		return domain.MarketPrice{}, err
	}
	quotePerBase, err := domain.CrossRateBPerA(baseObs.Rate, quoteObs.Rate)
	if err != nil {
		return domain.MarketPrice{}, err
	}
	return domain.MarketPrice{
		BaseCommodityID:  base.ID,
		QuoteCommodityID: quote.ID,
		Price:            quotePerBase,
		ObservedAt:       domain.ClockOnUTCDate(asOfDate, time.Now()),
		Source:           "ecb",
	}, nil
}

func (s *Service) marketPricesForBalances(ctx context.Context, balances []domain.AccountBalance, reportingCommodityID, asOfDate string) ([]domain.MarketPrice, error) {
	needed := map[string]struct{}{}
	for _, b := range balances {
		if b.CommodityID != reportingCommodityID {
			needed[b.CommodityID] = struct{}{}
		}
	}
	prices := []domain.MarketPrice{}
	for commodityID := range needed {
		p, err := s.quoteMarketPrice(ctx, commodityID, reportingCommodityID, asOfDate)
		if err != nil {
			return nil, err
		}
		if p.Source == "identity" {
			continue
		}
		prices = append(prices, p)
	}
	return prices, nil
}

func (s *Service) ComputeNetWorth(ctx context.Context) (domain.NetWorthReport, error) {
	reportingCommodityID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	asOf := nowISO()
	asOfDate := domain.UTCDate(asOf)
	if s.cache != nil {
		report, hit, err := s.cache.GetNetWorth(ctx, reportingCommodityID, asOfDate)
		if err != nil {
			log.Printf("networth cache get: %v", err)
		} else if hit {
			return report, nil
		}
	}
	report, err := s.computeNetWorthUncached(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	if s.cache != nil {
		if putErr := s.cache.PutNetWorth(ctx, report); putErr != nil {
			log.Printf("networth cache put: %v", putErr)
		}
	}
	return report, nil
}

func (s *Service) computeNetWorthUncached(ctx context.Context) (domain.NetWorthReport, error) {
	reportingCommodityID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	balances, err := s.GetAccountBalances(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return domain.NetWorthReport{}, err
	}

	accountByID := map[string]domain.Account{}
	for _, a := range accounts {
		accountByID[a.ID] = a
	}
	var balanceSheet []domain.AccountBalance
	for _, b := range balances {
		account, ok := accountByID[b.AccountID]
		if !ok {
			continue
		}
		if account.AccountType == domain.AccountAsset || account.AccountType == domain.AccountLiability || account.AccountType == domain.AccountEquity {
			balanceSheet = append(balanceSheet, b)
		}
	}
	prices, err := s.marketPricesForBalances(ctx, balanceSheet, reportingCommodityID, todayISO())
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	return domain.ComputeNetWorth(accounts, balances, reportingCommodityID, prices, commodities, nowISO())
}

func (s *Service) RecordAudit(ctx context.Context, eventType string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO audit_events (id, event_type, payload_json, created_at) VALUES ($1,$2,$3,$4)`,
		newID(), eventType, string(raw), nowISO())
	return err
}

func (s *Service) ListAuditEvents(ctx context.Context) ([]domain.AuditEvent, error) {
	rows, err := s.db.Query(ctx, `SELECT id, event_type, payload_json, created_at FROM audit_events ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AuditEvent
	for rows.Next() {
		var e domain.AuditEvent
		var payload string
		if err := rows.Scan(&e.ID, &e.EventType, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(payload), &e.Payload)
		out = append(out, e)
	}
	if out == nil {
		out = []domain.AuditEvent{}
	}
	return out, rows.Err()
}

func (s *Service) loadPostings(ctx context.Context, journalIDs []string) (map[string][]domain.ResolvedPosting, error) {
	if len(journalIDs) == 0 {
		return map[string][]domain.ResolvedPosting{}, nil
	}

	out := map[string][]domain.ResolvedPosting{}
	missing := journalIDs
	if s.cache != nil {
		found, err := s.cache.GetPostings(ctx, journalIDs)
		if err != nil {
			log.Printf("postings cache get: %v", err)
		} else if len(found) > 0 {
			out = found
			missing = make([]string, 0, len(journalIDs))
			for _, id := range journalIDs {
				if _, ok := found[id]; !ok {
					missing = append(missing, id)
				}
			}
		}
	}
	if len(missing) == 0 {
		return out, nil
	}

	loaded, err := s.loadPostingsFromDB(ctx, missing)
	if err != nil {
		return nil, err
	}
	for id, postings := range loaded {
		out[id] = postings
		if s.cache != nil {
			if putErr := s.cache.PutPostings(ctx, id, postings); putErr != nil {
				log.Printf("postings cache put: %v", putErr)
			}
		}
	}
	return out, nil
}

func (s *Service) loadPostingsFromDB(ctx context.Context, journalIDs []string) (map[string][]domain.ResolvedPosting, error) {
	rows, err := s.db.Query(ctx, `
		SELECT journal_entry_id, line_order, account_id, units_minor, commodity_id,
			cost_per_unit_numerator, cost_per_unit_denominator, cost_commodity_id, cost_date, cost_label,
			price_numerator, price_denominator, price_commodity_id, price_source, price_observed_date,
			weight_minor, weight_commodity_id, memo
		FROM postings WHERE journal_entry_id = ANY($1)
		ORDER BY journal_entry_id, line_order
	`, journalIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.ResolvedPosting{}
	for rows.Next() {
		var journalID, accountID, commodityID, weightCommodityID string
		var lineOrder int
		var unitsMinor, weightMinor int64
		var costNum, costDen, costComm, costDate, costLabel *string
		var priceNum, priceDen, priceComm, priceSource, priceObserved, memo *string
		if err := rows.Scan(&journalID, &lineOrder, &accountID, &unitsMinor, &commodityID,
			&costNum, &costDen, &costComm, &costDate, &costLabel,
			&priceNum, &priceDen, &priceComm, &priceSource, &priceObserved,
			&weightMinor, &weightCommodityID, &memo); err != nil {
			return nil, err
		}
		p := domain.ResolvedPosting{
			PostingInput: domain.PostingInput{
				AccountID: accountID,
				Units:     domain.Units{Minor: domain.Int(unitsMinor), CommodityID: commodityID},
				Memo:      memo,
			},
			LineOrder: lineOrder,
			Weight:    domain.Weight{Minor: domain.Int(weightMinor), CommodityID: weightCommodityID},
		}
		if r := parseRational(costNum, costDen); r != nil && costComm != nil {
			p.Cost = &domain.CostBasis{PerUnit: *r, CommodityID: *costComm, Date: costDate, Label: costLabel}
		}
		if r := parseRational(priceNum, priceDen); r != nil && priceComm != nil {
			tp := domain.TransactionPrice{PerUnit: *r, CommodityID: *priceComm}
			if priceSource != nil && *priceSource != "" {
				tp.Source = domain.PriceSource(*priceSource)
			}
			if priceObserved != nil && *priceObserved != "" {
				d := *priceObserved
				tp.ObservedDate = &d
			}
			p.Price = &tp
		}
		out[journalID] = append(out[journalID], p)
	}
	// Cache empty slices for IDs with no postings so we don't re-query forever.
	for _, id := range journalIDs {
		if _, ok := out[id]; !ok {
			out[id] = []domain.ResolvedPosting{}
		}
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

type journalRow struct {
	ID            string
	EffectiveDate string
	Description   *string
	Status        string
	CreatedAt     string
	PostedAt      *string
}

func mapJournal(row journalRow, postings []domain.ResolvedPosting) domain.JournalEntry {
	if postings == nil {
		postings = []domain.ResolvedPosting{}
	}
	effective := row.EffectiveDate
	if canonical, err := domain.CanonicalUTC(row.EffectiveDate); err == nil {
		effective = canonical
	}
	createdAt := row.CreatedAt
	if canonical, err := domain.CanonicalUTC(row.CreatedAt); err == nil {
		createdAt = canonical
	}
	postedAt := row.PostedAt
	if row.PostedAt != nil {
		if canonical, err := domain.CanonicalUTC(*row.PostedAt); err == nil {
			postedAt = &canonical
		}
	}
	return domain.JournalEntry{
		ID:            row.ID,
		EffectiveDate: effective,
		Description:   row.Description,
		Status:        domain.JournalEntryStatus(row.Status),
		CreatedAt:     createdAt,
		PostedAt:      postedAt,
		Postings:      postings,
	}
}

func scanAccount(row scanner) (domain.Account, error) {
	var a domain.Account
	var typ string
	if err := row.Scan(&a.ID, &a.Code, &a.Name, &typ, &a.ParentID, &a.NativeCommodityID, &a.CreatedAt); err != nil {
		return domain.Account{}, err
	}
	a.AccountType = domain.AccountType(typ)
	return a, nil
}

func scanCommodity(row scanner) (domain.Commodity, error) {
	var c domain.Commodity
	var kind string
	if err := row.Scan(&c.ID, &c.Code, &c.Name, &c.MinorUnits, &kind); err != nil {
		return domain.Commodity{}, err
	}
	c.Kind = domain.CommodityKind(kind)
	return c, nil
}

func insertPostings(ctx context.Context, tx pgx.Tx, entryID string, resolved []domain.ResolvedPosting) error {
	for _, posting := range resolved {
		var costNum, costDen, costComm, costDate, costLabel any
		if posting.Cost != nil {
			costNum = posting.Cost.PerUnit.Numerator.String()
			costDen = posting.Cost.PerUnit.Denominator.String()
			costComm = posting.Cost.CommodityID
			costDate = strOrNil(posting.Cost.Date)
			costLabel = strOrNil(posting.Cost.Label)
		}
		var priceNum, priceDen, priceComm, priceSource, priceObserved any
		if posting.Price != nil {
			priceNum = posting.Price.PerUnit.Numerator.String()
			priceDen = posting.Price.PerUnit.Denominator.String()
			priceComm = posting.Price.CommodityID
			if posting.Price.Source != "" {
				priceSource = string(posting.Price.Source)
			}
			priceObserved = strOrNil(posting.Price.ObservedDate)
		}
		units, err := toInt64(posting.Units.Minor)
		if err != nil {
			return err
		}
		weight, err := toInt64(posting.Weight.Minor)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO postings (
				id, journal_entry_id, line_order, account_id,
				units_minor, commodity_id,
				cost_per_unit_numerator, cost_per_unit_denominator, cost_commodity_id, cost_date, cost_label,
				price_numerator, price_denominator, price_commodity_id, price_source, price_observed_date,
				weight_minor, weight_commodity_id, memo
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		`, newID(), entryID, posting.LineOrder, posting.AccountID,
			units, posting.Units.CommodityID,
			costNum, costDen, costComm, costDate, costLabel,
			priceNum, priceDen, priceComm, priceSource, priceObserved,
			weight, posting.Weight.CommodityID, posting.Memo,
		); err != nil {
			return err
		}
	}
	return nil
}

func toInt64(n *big.Int) (int64, error) {
	if n == nil {
		return 0, nil
	}
	if !n.IsInt64() {
		return 0, domain.InvalidAmount("amount exceeds int64: " + n.String())
	}
	return n.Int64(), nil
}
