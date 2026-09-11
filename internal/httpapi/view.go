package httpapi

import (
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/ledger"
)

func (s *Server) viewDashboard(w http.ResponseWriter, r *http.Request) {
	result, err := s.Ledger.ViewDashboard(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency":              commodityJSON(result.Currency),
		"asOf":                  result.Report.AsOf,
		"timezone":              result.Timezone,
		"netWorthMinor":         minorString(result.Report.NetWorthMinor),
		"totalAssetsMinor":      minorString(result.Report.TotalAssetsMinor),
		"totalLiabilitiesMinor": minorString(result.Report.TotalLiabilitiesMinor),
		"liquidMinor":           minorString(result.LiquidMinor),
		"assets":                valuedBalancesJSON(result.Report.Assets, result.Accounts),
		"liabilities":           valuedBalancesJSON(result.Report.Liabilities, result.Accounts),
		"trend":                 periodsJSON(result.Trend),
	})
}

func (s *Server) viewAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.Ledger.ViewAccounts(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	reporting, err := s.Ledger.GetDefaultCommodityID(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	var currency any
	if reporting != nil {
		commodity, commodityErr := s.Ledger.GetCommodity(r.Context(), *reporting)
		if commodityErr != nil {
			mapError(w, commodityErr)
			return
		}
		if commodity != nil {
			currency = commodityJSON(*commodity)
		}
	}
	out := make([]any, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, viewAccountJSON(account))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": currency,
		"accounts": out,
	})
}

func (s *Server) viewTransactions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	input := ledger.ListJournalEntriesInput{
		Status: "posted",
	}
	if value := query.Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			input.Limit = &parsed
		}
	}
	if value := query.Get("offset"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			input.Offset = &parsed
		}
	}
	if value := query.Get("account"); value != "" {
		input.AccountID = &value
	}
	if value := query.Get("from"); value != "" {
		input.From = &value
	}
	if value := query.Get("to"); value != "" {
		input.To = &value
	}
	if value := query.Get("q"); value != "" {
		input.Q = &value
	}
	result, err := s.Ledger.ViewTransactions(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	entries := make([]any, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, viewTransactionJSON(entry))
	}
	expenses := make([]any, 0, len(result.TopExpenses))
	for _, expense := range result.TopExpenses {
		expenses = append(expenses, map[string]any{
			"accountId":   expense.AccountID,
			"name":        expense.Name,
			"commodity":   commodityJSON(expense.Commodity),
			"amountMinor": minorString(expense.Amount),
		})
	}
	incomes := make([]any, 0, len(result.TopIncomes))
	for _, income := range result.TopIncomes {
		incomes = append(incomes, map[string]any{
			"accountId":   income.AccountID,
			"name":        income.Name,
			"commodity":   commodityJSON(income.Commodity),
			"amountMinor": minorString(income.Amount),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":     entries,
		"topExpenses": expenses,
		"topIncomes":  incomes,
		"hasMore":     result.HasMore,
		"limit":       result.Limit,
		"offset":      result.Offset,
	})
}

func (s *Server) viewTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	transaction, err := s.Ledger.ViewTransaction(r.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	if transaction == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "JournalEntryNotFound"})
		return
	}
	writeJSON(w, http.StatusOK, viewTransactionJSON(*transaction))
}

func (s *Server) viewMonths(w http.ResponseWriter, r *http.Request) {
	result, err := s.Ledger.ViewPeriods(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	months := result.Months
	if year := r.URL.Query().Get("year"); year != "" {
		filtered := make([]ledger.ViewPeriod, 0, len(months))
		for _, month := range months {
			if strings.HasPrefix(month.Key, year+"-") {
				filtered = append(filtered, month)
			}
		}
		months = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": commodityJSON(result.Currency),
		"timezone": result.Timezone,
		"months":   periodsJSON(months),
	})
}

func (s *Server) viewYears(w http.ResponseWriter, r *http.Request) {
	result, err := s.Ledger.ViewPeriods(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": commodityJSON(result.Currency),
		"timezone": result.Timezone,
		"years":    periodsJSON(result.Years),
	})
}

func (s *Server) viewRetranslation(w http.ResponseWriter, r *http.Request) {
	preview, err := s.Ledger.PreviewRetranslation(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reportingCommodityId": preview.ReportingCommodityID,
		"retranslationMinor":   minorString(preview.RetranslationMinor),
		"netWorthMinor":        minorString(preview.NetWorthMinor),
		"incomeExpenseMinor":   minorString(preview.IncomeExpenseMinor),
		"asOf":                 preview.AsOf,
	})
}

func viewAccountJSON(account ledger.ViewAccount) map[string]any {
	return map[string]any{
		"id":                account.Account.ID,
		"code":              account.Account.Code,
		"name":              account.Account.Name,
		"accountType":       account.Account.AccountType,
		"parentId":          account.Account.ParentID,
		"nativeCommodityId": account.Account.NativeCommodityID,
		"commodity":         commodityJSON(account.Commodity),
		"balanceMinor":      minorString(account.Balance),
		"isHidden":          account.IsHidden,
		"isLiquid":          account.IsLiquid,
		"deletable":         account.Deletable,
		"createdAt":         account.Account.CreatedAt,
	}
}

func viewTransactionJSON(transaction ledger.ViewTransaction) map[string]any {
	return map[string]any{
		"id":            transaction.ID,
		"date":          transaction.Date,
		"description":   transaction.Description,
		"status":        transaction.Status,
		"category":      transaction.Category,
		"from":          viewAccountRefJSON(transaction.From),
		"to":            viewAccountRefJSON(transaction.To),
		"amountMinor":   nullableMinorString(transaction.AmountMinor),
		"commodity":     nullableCommodityJSON(transaction.Commodity),
		"toAmountMinor": nullableMinorString(transaction.ToAmountMinor),
		"toCommodity":   nullableCommodityJSON(transaction.ToCommodity),
		"postings":      viewPostingsJSON(transaction.Postings),
	}
}

func viewAccountRefJSON(account *ledger.ViewAccountRef) any {
	if account == nil {
		return nil
	}
	return map[string]any{
		"id":          account.AccountID,
		"code":        account.Code,
		"name":        account.Name,
		"accountType": account.AccountType,
		"commodity":   commodityJSON(account.Commodity),
	}
}

func viewPostingsJSON(postings []ledger.ViewPosting) []any {
	out := make([]any, 0, len(postings))
	for _, posting := range postings {
		out = append(out, map[string]any{
			"account":    viewAccountRefJSON(&posting.Account),
			"unitsMinor": minorString(posting.UnitsMinor),
			"commodity":  commodityJSON(posting.Commodity),
			"memo":       posting.Memo,
		})
	}
	return out
}

func valuedBalancesJSON(values []domain.ValuedBalance, accounts []ledger.ViewAccount) []any {
	accountByID := make(map[string]ledger.ViewAccount, len(accounts))
	for _, account := range accounts {
		accountByID[account.Account.ID] = account
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		account := accountByID[value.AccountID]
		out = append(out, map[string]any{
			"accountId":            value.AccountID,
			"accountCode":          account.Account.Code,
			"accountName":          account.Account.Name,
			"accountType":          value.AccountType,
			"reportingMinor":       minorString(value.ReportingMinor),
			"reportingCommodityId": value.ReportingCommodityID,
			"native":               balanceJSON(value.Native),
		})
	}
	return out
}

func periodsJSON(periods []ledger.ViewPeriod) []any {
	out := make([]any, 0, len(periods))
	for _, period := range periods {
		out = append(out, map[string]any{
			"key":               period.Key,
			"label":             period.Label,
			"startDate":         period.StartDate,
			"endDate":           period.EndDate,
			"currencyId":        period.CurrencyID,
			"incomeMinor":       minorString(period.IncomeMinor),
			"expenseMinor":      minorString(period.ExpenseMinor),
			"capitalGainsMinor": minorString(period.CapitalGainsMinor),
			"effectMinor":       minorString(period.EffectMinor),
			"netSavingsMinor":   minorString(period.NetSavingsMinor),
			"netWorthMinor":     minorString(period.NetWorthMinor),
			"factor":            period.Factor,
			"good":              period.Good,
		})
	}
	return out
}

func minorString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}

func nullableMinorString(value *big.Int) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullableCommodityJSON(value *domain.Commodity) any {
	if value == nil {
		return nil
	}
	return commodityJSON(*value)
}
