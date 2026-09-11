package ledger

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/FinancyOrg/backend/internal/domain"
)

// ViewAccount is the account shape used by the read-only frontend views.
// Hidden and liquid are Mongo-backed presentation flags. Missing values
// default to false and never change CRDB ledger facts.
type ViewAccount struct {
	Account   domain.Account
	Commodity domain.Commodity
	Balance   *big.Int
	IsHidden  bool
	IsLiquid  bool
	Deletable bool
}

type ViewAccountRef struct {
	AccountID   string
	Code        string
	Name        string
	AccountType domain.AccountType
	Commodity   domain.Commodity
}

type ViewTransactionCategory string

const (
	ViewTransactionIncome       ViewTransactionCategory = "income"
	ViewTransactionExpense      ViewTransactionCategory = "expense"
	ViewTransactionCapitalGains ViewTransactionCategory = "capital_gains"
)

type ViewPosting struct {
	Account    ViewAccountRef
	UnitsMinor *big.Int
	Commodity  domain.Commodity
	Memo       *string
}

type ViewTransaction struct {
	ID            string
	Date          string
	Description   *string
	Status        domain.JournalEntryStatus
	Category      *ViewTransactionCategory
	From          *ViewAccountRef
	To            *ViewAccountRef
	AmountMinor   *big.Int
	Commodity     *domain.Commodity
	ToAmountMinor *big.Int
	ToCommodity   *domain.Commodity
	Postings      []ViewPosting
}

type ViewExpense struct {
	AccountID string
	Name      string
	Commodity domain.Commodity
	Amount    *big.Int
}

type ViewTransactionsResult struct {
	Entries     []ViewTransaction
	TopExpenses []ViewExpense
	TopIncomes  []ViewExpense
	HasMore     bool
	Limit       int
	Offset      int
}

// ViewPeriod contains the metrics rendered by Finease's month and year cards.
// Monetary values remain integers in minor units until the HTTP boundary.
type ViewPeriod struct {
	Key               string
	Label             string
	StartDate         string
	EndDate           string
	CurrencyID        string
	IncomeMinor       *big.Int
	ExpenseMinor      *big.Int
	CapitalGainsMinor *big.Int
	EffectMinor       *big.Int
	NetSavingsMinor   *big.Int
	NetWorthMinor     *big.Int
	Factor            float64
	Good              bool
}

type ViewPeriodsResult struct {
	Currency domain.Commodity
	Months   []ViewPeriod
	Years    []ViewPeriod
	Timezone string
}

type ViewDashboard struct {
	Report      domain.NetWorthReport
	Currency    domain.Commodity
	LiquidMinor *big.Int
	Trend       []ViewPeriod
	Accounts    []ViewAccount
	Timezone    string
}

type periodAccumulator struct {
	income       *big.Int
	expense      *big.Int
	capitalGains *big.Int
}

func newPeriodAccumulator() *periodAccumulator {
	return &periodAccumulator{
		income:       big.NewInt(0),
		expense:      big.NewInt(0),
		capitalGains: big.NewInt(0),
	}
}

func (s *Service) ViewAccounts(ctx context.Context) ([]ViewAccount, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return nil, err
	}
	balances, err := s.GetAccountBalances(ctx)
	if err != nil {
		return nil, err
	}

	commodityByID := make(map[string]domain.Commodity, len(commodities))
	for _, commodity := range commodities {
		commodityByID[commodity.ID] = commodity
	}
	balanceByAccount := make(map[string]*big.Int, len(balances))
	for _, balance := range balances {
		balanceByAccount[balance.AccountID] = cloneBig(balance.Minor)
	}
	postedAccounts, err := s.accountsWithPostings(ctx)
	if err != nil {
		return nil, err
	}
	flagsByAccount, err := s.listAccountFlags(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]ViewAccount, 0, len(accounts))
	for _, account := range accounts {
		commodity, ok := commodityByID[account.NativeCommodityID]
		if !ok {
			continue
		}
		balance := balanceByAccount[account.ID]
		if balance == nil {
			balance = big.NewInt(0)
		}
		flags := flagsByAccount[account.ID]
		out = append(out, ViewAccount{
			Account:   account,
			Commodity: commodity,
			Balance:   balance,
			IsHidden:  flags.Hidden,
			IsLiquid:  flags.Liquid,
			Deletable: !postedAccounts[account.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Account.Name) < strings.ToLower(out[j].Account.Name)
	})
	return out, nil
}

func (s *Service) ViewDashboard(ctx context.Context) (ViewDashboard, error) {
	report, err := s.ComputeNetWorth(ctx)
	if err != nil {
		return ViewDashboard{}, err
	}
	currency, err := s.GetCommodity(ctx, report.ReportingCommodityID)
	if err != nil {
		return ViewDashboard{}, err
	}
	if currency == nil {
		return ViewDashboard{}, domain.InvalidCurrency(report.ReportingCommodityID)
	}
	accounts, err := s.ViewAccounts(ctx)
	if err != nil {
		return ViewDashboard{}, err
	}
	valuedByID := make(map[string]*big.Int, len(report.Assets))
	for _, valued := range report.Assets {
		valuedByID[valued.AccountID] = valued.ReportingMinor
	}
	for _, valued := range report.Liabilities {
		valuedByID[valued.AccountID] = valued.ReportingMinor
	}
	liquid := big.NewInt(0)
	for _, account := range accounts {
		if !account.IsLiquid {
			continue
		}
		if value := valuedByID[account.Account.ID]; value != nil {
			liquid.Add(liquid, value)
		}
	}

	periods, err := s.ViewPeriods(ctx)
	if err != nil {
		return ViewDashboard{}, err
	}
	return ViewDashboard{
		Report:      report,
		Currency:    *currency,
		LiquidMinor: liquid,
		Trend:       periods.Months,
		Accounts:    accounts,
		Timezone:    periods.Timezone,
	}, nil
}

func (s *Service) ViewTransactions(ctx context.Context, input ListJournalEntriesInput) (ViewTransactionsResult, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return ViewTransactionsResult{}, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return ViewTransactionsResult{}, err
	}
	accountByID := make(map[string]domain.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	commodityByID := make(map[string]domain.Commodity, len(commodities))
	for _, commodity := range commodities {
		commodityByID[commodity.ID] = commodity
	}

	result, err := s.ListJournalEntries(ctx, input)
	if err != nil {
		return ViewTransactionsResult{}, err
	}
	entries := make([]ViewTransaction, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, viewTransaction(entry, accountByID, commodityByID))
	}

	topExpenses := []ViewExpense{}
	topIncomes := []ViewExpense{}
	firstPage := input.Offset == nil || *input.Offset == 0
	if firstPage && (input.From != nil || input.To != nil) {
		var err error
		topExpenses, topIncomes, err = s.viewTopIncomeExpense(ctx, input, commodityByID)
		if err != nil {
			return ViewTransactionsResult{}, err
		}
	}

	return ViewTransactionsResult{
		Entries:     entries,
		TopExpenses: topExpenses,
		TopIncomes:  topIncomes,
		HasMore:     result.HasMore,
		Limit:       result.Limit,
		Offset:      result.Offset,
	}, nil
}

func (s *Service) ViewTransaction(ctx context.Context, id string) (*ViewTransaction, error) {
	entry, err := s.GetJournalEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return nil, err
	}
	accountByID := make(map[string]domain.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	commodityByID := make(map[string]domain.Commodity, len(commodities))
	for _, commodity := range commodities {
		commodityByID[commodity.ID] = commodity
	}
	transaction := viewTransaction(*entry, accountByID, commodityByID)
	return &transaction, nil
}

func viewTransaction(
	entry domain.JournalEntry,
	accountByID map[string]domain.Account,
	commodityByID map[string]domain.Commodity,
) ViewTransaction {
	var from, to *ViewAccountRef
	var fromPosting, toPosting *domain.ResolvedPosting
	postings := make([]ViewPosting, 0, len(entry.Postings))
	for i := range entry.Postings {
		posting := entry.Postings[i]
		account, accountOK := accountByID[posting.AccountID]
		commodity, commodityOK := commodityByID[posting.Units.CommodityID]
		if !accountOK || !commodityOK {
			continue
		}
		ref := viewAccountRef(account, commodity)
		postings = append(postings, ViewPosting{
			Account:    ref,
			UnitsMinor: cloneBig(posting.Units.Minor),
			Commodity:  commodity,
			Memo:       posting.Memo,
		})
		if posting.Units.Minor.Sign() < 0 && fromPosting == nil {
			fromPosting = &posting
			fromRef := ref
			from = &fromRef
		}
		if posting.Units.Minor.Sign() > 0 && toPosting == nil {
			toPosting = &posting
			toRef := ref
			to = &toRef
		}
	}

	var amountMinor, toAmountMinor *big.Int
	var commodity, toCommodity *domain.Commodity
	if fromPosting != nil {
		amountMinor = new(big.Int).Abs(fromPosting.Units.Minor)
		value := commodityByID[fromPosting.Units.CommodityID]
		commodity = &value
	}
	if toPosting != nil {
		toAmountMinor = new(big.Int).Abs(toPosting.Units.Minor)
		value := commodityByID[toPosting.Units.CommodityID]
		toCommodity = &value
	}
	return ViewTransaction{
		ID:            entry.ID,
		Date:          entry.EffectiveDate,
		Description:   entry.Description,
		Status:        entry.Status,
		Category:      classifyViewTransaction(postings),
		From:          from,
		To:            to,
		AmountMinor:   amountMinor,
		Commodity:     commodity,
		ToAmountMinor: toAmountMinor,
		ToCommodity:   toCommodity,
		Postings:      postings,
	}
}

func classifyViewTransaction(postings []ViewPosting) *ViewTransactionCategory {
	var (
		hasNegativeBalance bool
		hasPositiveBalance bool
		hasNegativeIncome  bool
		hasPositiveIncome  bool
		hasNegativeExpense bool
		hasPositiveExpense bool
		hasCapitalGains    bool
	)
	for _, posting := range postings {
		isNegative := posting.UnitsMinor.Sign() < 0
		isPositive := posting.UnitsMinor.Sign() > 0
		if IsCapitalGainsAccountCode(posting.Account.Code) {
			hasCapitalGains = true
		}
		switch posting.Account.AccountType {
		case domain.AccountAsset, domain.AccountLiability, domain.AccountEquity:
			hasNegativeBalance = hasNegativeBalance || isNegative
			hasPositiveBalance = hasPositiveBalance || isPositive
		case domain.AccountIncome:
			hasNegativeIncome = hasNegativeIncome || isNegative
			hasPositiveIncome = hasPositiveIncome || isPositive
		case domain.AccountExpense:
			hasNegativeExpense = hasNegativeExpense || isNegative
			hasPositiveExpense = hasPositiveExpense || isPositive
		}
	}

	if hasCapitalGains {
		category := ViewTransactionCapitalGains
		return &category
	}
	if (hasNegativeIncome || hasNegativeExpense) && hasPositiveBalance {
		category := ViewTransactionIncome
		return &category
	}
	if hasNegativeBalance && (hasPositiveIncome || hasPositiveExpense) {
		category := ViewTransactionExpense
		return &category
	}
	return nil
}

func viewAccountRef(account domain.Account, commodity domain.Commodity) ViewAccountRef {
	return ViewAccountRef{
		AccountID:   account.ID,
		Code:        account.Code,
		Name:        account.Name,
		AccountType: account.AccountType,
		Commodity:   commodity,
	}
}

func (s *Service) ViewPeriods(ctx context.Context) (ViewPeriodsResult, error) {
	loc := s.LedgerLocation(ctx)
	tz := loc.String()
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	var ledgerRevision int64
	if s.cache != nil {
		result, hit, err := s.cache.GetPeriods(ctx, reportingID, tz, ledgerRevision)
		if err != nil {
			log.Printf("periods cache get: %v", err)
		} else if hit && result.Timezone == tz {
			return result, nil
		}
	}
	result, err := s.viewPeriodsUncached(ctx, loc)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	result.Timezone = tz
	if s.cache != nil {
		if putErr := s.cache.PutPeriods(ctx, result, ledgerRevision); putErr != nil {
			log.Printf("periods cache put: %v", putErr)
		}
	}
	return result, nil
}

func (s *Service) viewPeriodsUncached(ctx context.Context, loc *time.Location) (ViewPeriodsResult, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	reporting, err := s.GetCommodity(ctx, reportingID)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	if reporting == nil {
		return ViewPeriodsResult{}, domain.InvalidCurrency(reportingID)
	}
	entries, err := s.listAllPostedEntries(ctx)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	if len(entries) == 0 {
		return ViewPeriodsResult{
			Currency: *reporting,
			Months:   []ViewPeriod{},
			Years:    []ViewPeriod{},
		}, nil
	}
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return ViewPeriodsResult{}, err
	}
	accountByID := make(map[string]domain.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	commodityByID := make(map[string]domain.Commodity, len(commodities))
	for _, commodity := range commodities {
		commodityByID[commodity.ID] = commodity
	}
	prices, err := s.ensureViewPrices(ctx, entries, reportingID, commodityByID)
	if err != nil {
		return ViewPeriodsResult{}, err
	}

	accumulators := map[string]*periodAccumulator{}
	var firstMonth, lastMonth time.Time
	for _, entry := range entries {
		date, err := viewEntryDate(entry.EffectiveDate, loc)
		if err != nil {
			return ViewPeriodsResult{}, err
		}
		month := monthStart(date)
		if firstMonth.IsZero() || month.Before(firstMonth) {
			firstMonth = month
		}
		if lastMonth.IsZero() || month.After(lastMonth) {
			lastMonth = month
		}
		key := month.Format("2006-01")
		accumulator := accumulators[key]
		if accumulator == nil {
			accumulator = newPeriodAccumulator()
			accumulators[key] = accumulator
		}
		for _, posting := range entry.Postings {
			account, ok := accountByID[posting.AccountID]
			if !ok {
				continue
			}
			value, err := domain.ReportingMinor(
				posting.Units,
				posting.Cost,
				reportingID,
				prices,
				commodityByID,
			)
			if err != nil {
				return ViewPeriodsResult{}, err
			}
			switch account.AccountType {
			case domain.AccountIncome:
				if IsCapitalGainsAccountCode(account.Code) {
					accumulator.capitalGains.Sub(accumulator.capitalGains, value)
				} else if value.Sign() > 0 {
					accumulator.expense.Add(accumulator.expense, value)
				} else {
					accumulator.income.Sub(accumulator.income, value)
				}
			case domain.AccountExpense:
				if value.Sign() < 0 {
					accumulator.income.Sub(accumulator.income, value)
				} else {
					accumulator.expense.Add(accumulator.expense, value)
				}
			}
		}
	}
	nowMonth := monthStart(time.Now().In(loc))
	if nowMonth.After(lastMonth) {
		lastMonth = nowMonth
	}
	months := make([]ViewPeriod, 0)
	cumulative := big.NewInt(0)
	for current := firstMonth; !current.After(lastMonth); current = current.AddDate(0, 1, 0) {
		key := current.Format("2006-01")
		accumulator := accumulators[key]
		if accumulator == nil {
			accumulator = newPeriodAccumulator()
		}
		effect := new(big.Int).Add(accumulator.income, accumulator.capitalGains)
		effect.Sub(effect, accumulator.expense)
		netSavings := new(big.Int).Sub(effect, accumulator.capitalGains)
		cumulative.Add(cumulative, effect)
		months = append(months, makeViewPeriod(
			current,
			current.AddDate(0, 1, -1),
			reporting.ID,
			accumulator.income,
			accumulator.expense,
			accumulator.capitalGains,
			effect,
			netSavings,
			cumulative,
		))
	}
	years := aggregateViewYears(months, reporting.ID, loc)
	sort.Slice(months, func(i, j int) bool {
		return months[i].Key > months[j].Key
	})
	sort.Slice(years, func(i, j int) bool {
		return years[i].Key > years[j].Key
	})
	return ViewPeriodsResult{Currency: *reporting, Months: months, Years: years}, nil
}

func (s *Service) listAllPostedEntries(ctx context.Context) ([]domain.JournalEntry, error) {
	limit := 200
	offset := 0
	var entries []domain.JournalEntry
	for {
		result, err := s.ListJournalEntries(ctx, ListJournalEntriesInput{
			Limit:  &limit,
			Offset: &offset,
			Status: string(domain.JournalPosted),
		})
		if err != nil {
			return nil, err
		}
		entries = append(entries, result.Entries...)
		if !result.HasMore {
			return entries, nil
		}
		offset += len(result.Entries)
	}
}

type pnlPostingRow struct {
	AccountID   string
	Name        string
	Code        string
	AccountType domain.AccountType
	UnitsMinor  *big.Int
	CommodityID string
}

func (s *Service) viewTopIncomeExpense(
	ctx context.Context,
	input ListJournalEntriesInput,
	commodityByID map[string]domain.Commodity,
) ([]ViewExpense, []ViewExpense, error) {
	reportingID, err := s.RequireDefaultCommodityID(ctx)
	if err != nil {
		return nil, nil, err
	}
	reporting, err := s.GetCommodity(ctx, reportingID)
	if err != nil {
		return nil, nil, err
	}
	if reporting == nil {
		return nil, nil, domain.InvalidCurrency(reportingID)
	}

	rows, err := s.listPnLPostings(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	needed := map[string]struct{}{}
	for _, row := range rows {
		needed[row.CommodityID] = struct{}{}
	}
	prices, err := s.spotMarketPrices(ctx, needed, *reporting, commodityByID)
	if err != nil {
		return nil, nil, err
	}

	expenses := map[string]*rankedTotal{}
	incomes := map[string]*rankedTotal{}
	for _, row := range rows {
		commodity, ok := commodityByID[row.CommodityID]
		if !ok {
			continue
		}
		reportingValue, err := domain.ConvertMinorToReporting(
			row.UnitsMinor,
			row.CommodityID,
			reportingID,
			prices,
			commodityByID,
		)
		if err != nil {
			// Skip legs we cannot convert at spot (e.g. non-currency commodities).
			continue
		}
		key := row.AccountID + "|" + row.CommodityID
		switch row.AccountType {
		case domain.AccountExpense:
			if row.UnitsMinor.Sign() <= 0 || reportingValue.Sign() <= 0 {
				continue
			}
			addRankedTotal(expenses, key, row.AccountID, row.Name, commodity, new(big.Int).Abs(row.UnitsMinor), reportingValue)
		case domain.AccountIncome:
			if IsCapitalGainsAccountCode(row.Code) || row.UnitsMinor.Sign() >= 0 || reportingValue.Sign() >= 0 {
				continue
			}
			addRankedTotal(incomes, key, row.AccountID, row.Name, commodity, new(big.Int).Abs(row.UnitsMinor), new(big.Int).Abs(reportingValue))
		}
	}
	return rankViewTotals(expenses, 10), rankViewTotals(incomes, 10), nil
}

func (s *Service) listPnLPostings(ctx context.Context, input ListJournalEntriesInput) ([]pnlPostingRow, error) {
	var where []string
	var args []any
	n := 1
	where = append(where, `j.status = 'posted'`)
	where = append(where, `a.account_type IN ('expense', 'income')`)
	loc := s.LedgerLocation(ctx)
	if input.From != nil && *input.From != "" {
		from := *input.From
		if start, err := domain.RangeStartIn(from, loc); err == nil {
			from = start
		}
		where = append(where, fmt.Sprintf(`j.effective_date >= $%d`, n))
		args = append(args, from)
		n++
	}
	if input.To != nil && *input.To != "" {
		to := *input.To
		if end, err := domain.RangeEndIn(to, loc); err == nil {
			to = end
		}
		where = append(where, fmt.Sprintf(`j.effective_date <= $%d`, n))
		args = append(args, to)
		n++
	}
	if input.AccountID != nil && *input.AccountID != "" {
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM postings px
			WHERE px.journal_entry_id = j.id AND px.account_id = $%d
		)`, n))
		args = append(args, *input.AccountID)
		n++
	}

	query := fmt.Sprintf(`
		SELECT a.id, a.name, a.code, a.account_type, p.units_minor, p.commodity_id
		FROM postings p
		JOIN journal_entries j ON j.id = p.journal_entry_id
		JOIN accounts a ON a.id = p.account_id
		WHERE %s
	`, strings.Join(where, " AND "))

	rs, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var out []pnlPostingRow
	for rs.Next() {
		var row pnlPostingRow
		var units int64
		var accountType string
		if err := rs.Scan(&row.AccountID, &row.Name, &row.Code, &accountType, &units, &row.CommodityID); err != nil {
			return nil, err
		}
		row.AccountType = domain.AccountType(accountType)
		row.UnitsMinor = domain.Int(units)
		out = append(out, row)
	}
	return out, rs.Err()
}

// spotMarketPrices builds in-memory FX quotes from ECB spots (Mongo/HTTP cache)
// without writing the ledger.
func (s *Service) spotMarketPrices(
	ctx context.Context,
	commodityIDs map[string]struct{},
	reporting domain.Commodity,
	commodityByID map[string]domain.Commodity,
) ([]domain.MarketPrice, error) {
	asOf := todayISO()
	prices := make([]domain.MarketPrice, 0, len(commodityIDs))
	for commodityID := range commodityIDs {
		if commodityID == reporting.ID {
			continue
		}
		commodity, ok := commodityByID[commodityID]
		if !ok || commodity.Kind != domain.CommodityCurrency {
			continue
		}
		baseObs, err := FetchEcbRatePerEurWithLookback(ctx, commodity.ID, asOf, 10, s.fetchEcbRate)
		if err != nil {
			if de, ok := domain.IsDomainError(err); ok && de.Code == "MissingEcbRate" {
				continue
			}
			return nil, err
		}
		quoteObs, err := FetchEcbRatePerEurWithLookback(ctx, reporting.ID, baseObs.ObservedDate, 10, s.fetchEcbRate)
		if err != nil {
			if de, ok := domain.IsDomainError(err); ok && de.Code == "MissingEcbRate" {
				continue
			}
			return nil, err
		}
		quotePerBase, err := domain.CrossRateBPerA(baseObs.Rate, quoteObs.Rate)
		if err != nil {
			return nil, err
		}
		prices = append(prices, domain.MarketPrice{
			BaseCommodityID:  commodity.ID,
			QuoteCommodityID: reporting.ID,
			Price:            quotePerBase,
			ObservedAt:       domain.ClockOnUTCDate(baseObs.ObservedDate, time.Now()),
			Source:           "ecb",
		})
	}
	return prices, nil
}

func addRankedTotal(
	totals map[string]*rankedTotal,
	key, accountID, name string,
	commodity domain.Commodity,
	nativeAmount, reportingAmount *big.Int,
) {
	total, ok := totals[key]
	if !ok {
		total = &rankedTotal{
			AccountID:       accountID,
			Name:            name,
			Commodity:       commodity,
			NativeAmount:    big.NewInt(0),
			ReportingAmount: big.NewInt(0),
		}
		totals[key] = total
	}
	total.NativeAmount.Add(total.NativeAmount, nativeAmount)
	total.ReportingAmount.Add(total.ReportingAmount, reportingAmount)
}

type rankedTotal struct {
	AccountID       string
	Name            string
	Commodity       domain.Commodity
	NativeAmount    *big.Int
	ReportingAmount *big.Int
}

func rankViewTotals(totals map[string]*rankedTotal, limit int) []ViewExpense {
	ordered := make([]*rankedTotal, 0, len(totals))
	for _, total := range totals {
		ordered = append(ordered, total)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if cmp := ordered[i].ReportingAmount.Cmp(ordered[j].ReportingAmount); cmp != 0 {
			return cmp > 0
		}
		return ordered[i].Name < ordered[j].Name
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	ranked := make([]ViewExpense, 0, len(ordered))
	for _, total := range ordered {
		ranked = append(ranked, ViewExpense{
			AccountID: total.AccountID,
			Name:      total.Name,
			Commodity: total.Commodity,
			Amount:    cloneBig(total.NativeAmount),
		})
	}
	return ranked
}

func (s *Service) ensureViewPrices(
	ctx context.Context,
	entries []domain.JournalEntry,
	reportingID string,
	commodities map[string]domain.Commodity,
) ([]domain.MarketPrice, error) {
	needed := map[string]struct{}{reportingID: {}}
	for _, entry := range entries {
		for _, posting := range entry.Postings {
			needed[posting.Units.CommodityID] = struct{}{}
		}
	}
	reporting, ok := commodities[reportingID]
	if !ok {
		return nil, domain.InvalidCurrency(reportingID)
	}
	return s.spotMarketPrices(ctx, needed, reporting, commodities)
}

func viewEntryDate(raw string, loc *time.Location) (time.Time, error) {
	t, err := domain.ParseUTC(raw)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.UTC
	}
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc), nil
}

func monthStart(value time.Time) time.Time {
	loc := value.Location()
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, loc)
}

func makeViewPeriod(
	start, end time.Time,
	currencyID string,
	income, expense, capitalGains, effect, netSavings, netWorth *big.Int,
) ViewPeriod {
	income = cloneBig(income)
	expense = cloneBig(expense)
	capitalGains = cloneBig(capitalGains)
	effect = cloneBig(effect)
	netSavings = cloneBig(netSavings)
	netWorth = cloneBig(netWorth)
	incomeSquared := new(big.Int).Mul(income, income)
	expenseSquared := new(big.Int).Mul(expense, expense)
	denominator := new(big.Int).Add(incomeSquared, expenseSquared)
	factor := 0.5
	if denominator.Sign() != 0 {
		numerator, _ := new(big.Float).SetInt(incomeSquared).Float64()
		denominatorFloat, _ := new(big.Float).SetInt(denominator).Float64()
		factor = numerator / denominatorFloat
	}
	return ViewPeriod{
		Key:               start.Format("2006-01"),
		Label:             start.Format("January 2006"),
		StartDate:         start.Format("2006-01-02"),
		EndDate:           end.Format("2006-01-02"),
		CurrencyID:        currencyID,
		IncomeMinor:       income,
		ExpenseMinor:      expense,
		CapitalGainsMinor: capitalGains,
		EffectMinor:       effect,
		NetSavingsMinor:   netSavings,
		NetWorthMinor:     netWorth,
		Factor:            factor,
		Good:              factor > 0.5,
	}
}

func aggregateViewYears(months []ViewPeriod, currencyID string, loc *time.Location) []ViewPeriod {
	type yearAccumulator struct {
		year         int
		income       *big.Int
		expense      *big.Int
		capitalGains *big.Int
		effect       *big.Int
		netWorth     *big.Int
		lastKey      string
	}
	byYear := map[int]*yearAccumulator{}
	for _, month := range months {
		date, err := time.Parse("2006-01", month.Key)
		if err != nil {
			continue
		}
		accumulator := byYear[date.Year()]
		if accumulator == nil {
			accumulator = &yearAccumulator{
				year:         date.Year(),
				income:       big.NewInt(0),
				expense:      big.NewInt(0),
				capitalGains: big.NewInt(0),
				effect:       big.NewInt(0),
				netWorth:     big.NewInt(0),
			}
			byYear[date.Year()] = accumulator
		}
		accumulator.income.Add(accumulator.income, month.IncomeMinor)
		accumulator.expense.Add(accumulator.expense, month.ExpenseMinor)
		accumulator.capitalGains.Add(accumulator.capitalGains, month.CapitalGainsMinor)
		accumulator.effect.Add(accumulator.effect, month.EffectMinor)
		if month.Key > accumulator.lastKey {
			accumulator.netWorth.Set(month.NetWorthMinor)
			accumulator.lastKey = month.Key
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	years := make([]ViewPeriod, 0, len(byYear))
	for _, accumulator := range byYear {
		start := time.Date(accumulator.year, 1, 1, 0, 0, 0, 0, loc)
		end := time.Date(accumulator.year, 12, 31, 0, 0, 0, 0, loc)
		netSavings := new(big.Int).Sub(accumulator.effect, accumulator.capitalGains)
		period := makeViewPeriod(
			start,
			end,
			currencyID,
			accumulator.income,
			accumulator.expense,
			accumulator.capitalGains,
			accumulator.effect,
			netSavings,
			accumulator.netWorth,
		)
		period.Key = start.Format("2006")
		period.Label = period.Key
		years = append(years, period)
	}
	return years
}
