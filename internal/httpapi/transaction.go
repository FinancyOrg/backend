package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/ledger"
)

func (s *Server) postTransaction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CreditAccountID   string  `json:"creditAccountId"`
		DebitAccountID    string  `json:"debitAccountId"`
		CreditAmountMinor string  `json:"creditAmountMinor"`
		DebitAmountMinor  *string `json:"debitAmountMinor"`
		Datetime          *string `json:"datetime"`
		Description       *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !uuidRe.MatchString(body.CreditAccountID) || !uuidRe.MatchString(body.DebitAccountID) || !posIntRe.MatchString(body.CreditAmountMinor) {
		http.Error(w, "invalid transaction", http.StatusBadRequest)
		return
	}
	if body.DebitAmountMinor != nil && strings.TrimSpace(*body.DebitAmountMinor) != "" && !posIntRe.MatchString(*body.DebitAmountMinor) {
		http.Error(w, "invalid transaction", http.StatusBadRequest)
		return
	}

	input := ledger.PostTransactionInput{
		CreditAccountID:   body.CreditAccountID,
		DebitAccountID:    body.DebitAccountID,
		CreditAmountMinor: domain.MustInt(body.CreditAmountMinor),
		Datetime:          body.Datetime,
		Description:       body.Description,
	}
	if body.DebitAmountMinor != nil && strings.TrimSpace(*body.DebitAmountMinor) != "" {
		input.DebitAmountMinor = domain.MustInt(*body.DebitAmountMinor)
	}

	result, err := s.Ledger.PostTransaction(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	entry := journalJSON(result.Entry)
	entry["datetime"] = result.Entry.EffectiveDate
	writeJSON(w, http.StatusCreated, map[string]any{
		"entry":                entry,
		"transactionCostMinor": result.TransactionCostMinor,
	})
}

func (s *Server) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.Ledger.DeleteJournalEntry(r.Context(), id); err != nil {
		mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getTransaction(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{"entry": viewTransactionJSON(*transaction)})
}

func (s *Server) patchTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Datetime    *string `json:"datetime"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	entry, err := s.Ledger.UpdateJournalEntry(r.Context(), id, ledger.UpdateJournalEntryInput{
		Datetime:    body.Datetime,
		Description: body.Description,
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": journalJSON(entry)})
}

func (s *Server) getFxRate(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if from == "" || to == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}
	quote, err := s.Ledger.QuoteEcbCrossRate(r.Context(), from, to, date)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fromCommodityId": quote.FromCommodityID,
		"toCommodityId":   quote.ToCommodityID,
		"rateNumerator":   quote.Rate.Numerator.String(),
		"rateDenominator": quote.Rate.Denominator.String(),
		"observedDate":    quote.ObservedDate,
		"asOfDate":        quote.AsOfDate,
	})
}

func retranslationJSON(p domain.RetranslationPreview) map[string]any {
	return map[string]any{
		"retranslationMinor":   p.RetranslationMinor.String(),
		"netWorthMinor":        p.NetWorthMinor.String(),
		"incomeExpenseMinor":   p.IncomeExpenseMinor.String(),
		"reportingCommodityId": p.ReportingCommodityID,
	}
}

func (s *Server) getRetranslate(w http.ResponseWriter, r *http.Request) {
	preview, err := s.Ledger.PreviewRetranslation(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, retranslationJSON(preview))
}

func (s *Server) postRetranslate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Datetime    *string `json:"datetime"`
		AmountMinor *string `json:"amountMinor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	input := ledger.PostRetranslationInput{Datetime: body.Datetime}
	if body.AmountMinor != nil && strings.TrimSpace(*body.AmountMinor) != "" {
		if !intStrRe.MatchString(strings.TrimSpace(*body.AmountMinor)) {
			http.Error(w, "invalid amountMinor", http.StatusBadRequest)
			return
		}
		input.AmountMinor = domain.MustInt(strings.TrimSpace(*body.AmountMinor))
	}
	result, err := s.Ledger.PostRetranslation(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	resp := retranslationJSON(result.Preview)
	resp["bookedMinor"] = "0"
	if result.BookedMinor != nil {
		resp["bookedMinor"] = result.BookedMinor.String()
	}
	if result.Entry == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["entry"] = journalJSON(*result.Entry)
	resp["expenseAccount"] = accountJSON(*result.ExpenseAccount)
	if result.OffsetAccount != nil {
		resp["offsetAccount"] = accountJSON(*result.OffsetAccount)
	}
	writeJSON(w, http.StatusCreated, resp)
}
