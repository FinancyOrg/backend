package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	version "github.com/FinancyOrg/backend"
	"github.com/FinancyOrg/backend/internal/auth"
	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/ledger"
)

var (
	isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	intStrRe  = regexp.MustCompile(`^-?\d+$`)
	posIntRe  = regexp.MustCompile(`^\d+$`)
	uuidRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Firestore is the Mongo client ping target for GET /api/db/firestore (legacy route name).
type Firestore interface {
	Ping(ctx context.Context) error
}

type Server struct {
	Ledger    *ledger.Service
	Auth      *Auth
	Firestore Firestore
}

// Auth bundles the auth dependencies the handlers and middleware need.
type Auth struct {
	Verifier  auth.TokenVerifier
	Sessions  *auth.SessionManager
	Allowlist *auth.Allowlist
	// DevLogin enables POST /api/auth/dev. Set only by local make targets.
	DevLogin bool
}

// route is one registered endpoint. public routes skip session auth and must
// be exact paths (no wildcards) — enforced by a panic in New and by tests.
type route struct {
	method  string
	pattern string
	handler http.HandlerFunc
	public  bool
}

// routes is the single source of truth for the API surface. The mux and the
// auth middleware are both built from this table, and a test walks it to
// prove every non-public route rejects unauthenticated requests.
func (s *Server) routes() []route {
	return []route{
		{"GET", "/api/health", s.health, true},
		{"GET", "/api/db/firestore", s.firestore, true},
		{"GET", "/api/db/mongo", s.firestore, true},
		{"POST", "/api/auth/google", s.postAuthGoogle, true},
		{"POST", "/api/auth/dev", s.postAuthDev, true},
		{"POST", "/api/auth/logout", s.postAuthLogout, true},
		{"GET", "/api/auth/me", s.getAuthMe, false},
		{"GET", "/api/config", s.getConfig, false},
		{"PUT", "/api/config", s.putConfig, false},
		{"GET", "/api/config/default-currency", s.getDefaultCurrency, false},
		{"PUT", "/api/config/default-currency", s.setDefaultCurrency, false},
		{"GET", "/api/config/theme", s.getTheme, false},
		{"PUT", "/api/config/theme", s.setTheme, false},
		{"GET", "/api/config/timezone", s.getTimezone, false},
		{"PUT", "/api/config/timezone", s.setTimezone, false},
		{"GET", "/api/commodities", s.listCommodities, false},
		{"POST", "/api/commodities", s.createCommodity, false},
		{"GET", "/api/accounts", s.listAccounts, false},
		{"POST", "/api/accounts", s.createAccount, false},
		{"GET", "/api/accounts/{id}", s.getAccount, false},
		{"PATCH", "/api/accounts/{id}", s.patchAccount, false},
		{"DELETE", "/api/accounts/{id}", s.deleteAccount, false},
		{"GET", "/api/accounts/{id}/balance", s.getAccountBalance, false},
		{"GET", "/api/journal-entries", s.listJournalEntries, false},
		{"GET", "/api/journal-entries/{id}", s.getJournalEntry, false},
		{"POST", "/api/journal-entries", s.postJournalEntry, false},
		{"DELETE", "/api/journal-entries/{id}", s.deleteJournalEntry, false},
		{"POST", "/api/exchanges", s.exchange, false}, // disabled; use POST /api/v1/transaction
		{"GET", "/api/balances", s.listBalances, false},
		{"GET", "/api/prices", s.listPrices, false},
		{"PUT", "/api/prices/{commodity}/{quote}", s.upsertPrice, false},
		{"GET", "/api/net-worth", s.netWorth, false},
		{"POST", "/api/v1/transaction", s.postTransaction, false},
		{"GET", "/api/v1/transaction/{id}", s.getTransaction, false},
		{"PATCH", "/api/v1/transaction/{id}", s.patchTransaction, false},
		{"DELETE", "/api/v1/transaction/{id}", s.deleteTransaction, false},
		{"GET", "/api/v1/fx/rate", s.getFxRate, false},
		{"GET", "/api/v1/retranslate", s.getRetranslate, false},
		{"POST", "/api/v1/retranslate", s.postRetranslate, false},
		{"GET", "/api/v1/view/dashboard", s.viewDashboard, false},
		{"GET", "/api/v1/view/accounts", s.viewAccounts, false},
		{"GET", "/api/v1/view/transactions", s.viewTransactions, false},
		{"GET", "/api/v1/view/transactions/{id}", s.viewTransaction, false},
		{"GET", "/api/v1/view/months", s.viewMonths, false},
		{"GET", "/api/v1/view/years", s.viewYears, false},
		{"GET", "/api/v1/view/retranslation", s.viewRetranslation, false},
		{"DELETE", "/api/v1/cache", s.clearCache, false},
	}
}

// New builds the full handler chain: security headers, then rate limiting,
// then session auth, then the mux. Auth is mandatory — there is no
// unauthenticated mode.
func New(svc *ledger.Service, a *Auth) http.Handler {
	return (&Server{Ledger: svc, Auth: a}).Handler()
}

// Handler builds the full handler chain from s. Auth is mandatory.
func (s *Server) Handler() http.Handler {
	if s == nil || s.Auth == nil || s.Auth.Verifier == nil || s.Auth.Sessions == nil || s.Auth.Allowlist == nil {
		panic("httpapi.New: auth dependencies are required")
	}

	mux := http.NewServeMux()
	public := map[string]bool{}
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.method+" "+rt.pattern, rt.handler)
		if rt.public {
			if strings.Contains(rt.pattern, "{") {
				panic("httpapi.New: public route must be an exact path: " + rt.pattern)
			}
			public[rt.method+" "+rt.pattern] = true
		}
	}

	return auth.SecureHeaders(
		auth.NewRateLimiter(10, 20).Wrap(
			auth.RequireSession(s.Auth.Sessions, public)(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.String(),
	})
}

func (s *Server) firestore(w http.ResponseWriter, r *http.Request) {
	if s.Firestore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	if err := s.Firestore.Ping(r.Context()); err != nil {
		log.Printf("firestore ping: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Ledger.AppConfig(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, configJSON(cfg))
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CommodityID string `json:"commodityId"`
		Theme       string `json:"theme"`
		Timezone    string `json:"timezone"`
		Confirm     bool   `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.CommodityID) == "" {
		http.Error(w, "commodityId is required", http.StatusBadRequest)
		return
	}
	if body.Theme != "light" && body.Theme != "dark" {
		http.Error(w, "theme must be light or dark", http.StatusBadRequest)
		return
	}
	cfg, err := s.Ledger.SetAppConfig(r.Context(), body.CommodityID, ledger.UiTheme(body.Theme), body.Timezone, body.Confirm)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, configJSON(cfg))
}

func configJSON(cfg ledger.AppConfig) map[string]any {
	return map[string]any{
		"commodityId":                 cfg.PresentationCommodityID,
		"theme":                       cfg.Theme,
		"timezone":                    cfg.Timezone,
		"postedJournalCount":          cfg.PostedJournalCount,
		"functionalChangeEstimatedMs": cfg.FunctionalChangeEstimatedMs,
		"functionalChangePollMs":      cfg.FunctionalChangePollMs,
		"functionalChange":            functionalChangeJSON(cfg.FunctionalChange),
	}
}

func functionalChangeJSON(job *ledger.FunctionalChangeJob) any {
	if job == nil || job.Status == "" {
		return nil
	}
	out := map[string]any{
		"status": job.Status,
		"from":   job.From,
		"to":     job.To,
	}
	if job.Error != "" {
		out["error"] = job.Error
	}
	return out
}

func (s *Server) getDefaultCurrency(w http.ResponseWriter, r *http.Request) {
	id, err := s.Ledger.GetDefaultCommodityID(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	if id == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "FunctionalCurrencyNotSet", "message": "Functional currency is not set", "commodityId": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commodityId": *id})
}

func (s *Server) setDefaultCurrency(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CommodityID string `json:"commodityId"`
		Confirm     bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.CommodityID) == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	existing, err := s.Ledger.GetDefaultCommodityID(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	if body.Confirm && existing != nil {
		if _, err := s.Ledger.StartFunctionalCurrencyChange(r.Context(), body.CommodityID); err != nil {
			mapError(w, err)
			return
		}
		cfg, err := s.Ledger.AppConfig(r.Context())
		if err != nil {
			mapError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, configJSON(cfg))
		return
	}
	commodityID, txnCost, err := s.Ledger.SetDefaultCurrency(r.Context(), body.CommodityID)
	if err != nil {
		mapError(w, err)
		return
	}
	gains, err := s.Ledger.GetAccountByCode(r.Context(), ledger.CapitalGainsAccountCodeFor(commodityID))
	if err != nil {
		mapError(w, err)
		return
	}
	resp := map[string]any{
		"commodityId":            commodityID,
		"transactionCostAccount": accountJSON(txnCost),
	}
	if gains != nil {
		resp["capitalGainsAccount"] = accountJSON(*gains)
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) getTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := s.Ledger.GetUiTheme(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": theme})
}

func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Theme != "light" && body.Theme != "dark" {
		http.Error(w, "theme must be light or dark", http.StatusBadRequest)
		return
	}
	theme, err := s.Ledger.SetUiTheme(r.Context(), ledger.UiTheme(body.Theme))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": theme})
}

func (s *Server) getTimezone(w http.ResponseWriter, r *http.Request) {
	loc := s.Ledger.LedgerLocation(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"timezone": loc.String()})
}

func (s *Server) setTimezone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	loc, err := s.Ledger.SetLedgerTimezone(r.Context(), body.Timezone)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timezone": loc.String()})
}

func (s *Server) listCommodities(w http.ResponseWriter, r *http.Request) {
	list, err := s.Ledger.ListCommodities(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]any, len(list))
	for i, c := range list {
		out[i] = commodityJSON(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"commodities": out})
}

func (s *Server) createCommodity(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		MinorUnits int    `json:"minorUnits"`
		Kind       string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Code) < 2 || len(body.Code) > 16 || body.Name == "" {
		http.Error(w, "invalid commodity", http.StatusBadRequest)
		return
	}
	kind := domain.CommodityKind(body.Kind)
	c, err := s.Ledger.CreateCommodity(r.Context(), ledger.CreateCommodityInput{
		Code: body.Code, Name: body.Name, MinorUnits: body.MinorUnits, Kind: kind,
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"commodity": commodityJSON(c)})
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Ledger.ListAccounts(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]any, len(list))
	for i, a := range list {
		out[i] = accountJSON(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code                string  `json:"code"`
		Name                string  `json:"name"`
		AccountType         string  `json:"accountType"`
		ParentID            *string `json:"parentId"`
		NativeCommodityID   *string `json:"nativeCommodityId"`
		OpeningBalanceMinor *string `json:"openingBalanceMinor"`
		Hidden              bool    `json:"hidden"`
		Liquid              bool    `json:"liquid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	input := ledger.CreateAccountInput{
		Code:              body.Code,
		Name:              body.Name,
		AccountType:       body.AccountType,
		ParentID:          body.ParentID,
		NativeCommodityID: body.NativeCommodityID,
		Hidden:            body.Hidden,
		Liquid:            body.Liquid,
	}
	if body.OpeningBalanceMinor != nil && strings.TrimSpace(*body.OpeningBalanceMinor) != "" {
		if !intStrRe.MatchString(strings.TrimSpace(*body.OpeningBalanceMinor)) {
			http.Error(w, "invalid openingBalanceMinor", http.StatusBadRequest)
			return
		}
		input.OpeningBalanceMinor = domain.MustInt(strings.TrimSpace(*body.OpeningBalanceMinor))
	}
	a, err := s.Ledger.CreateAccount(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": accountJSON(a)})
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var body struct {
		Name              *string `json:"name"`
		AccountType       *string `json:"accountType"`
		NativeCommodityID *string `json:"nativeCommodityId"`
		Hidden            *bool   `json:"hidden"`
		Liquid            *bool   `json:"liquid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	a, err := s.Ledger.UpdateAccount(r.Context(), id, ledger.UpdateAccountInput{
		Name:              body.Name,
		AccountType:       body.AccountType,
		NativeCommodityID: body.NativeCommodityID,
		Hidden:            body.Hidden,
		Liquid:            body.Liquid,
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(a)})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.Ledger.DeleteAccount(r.Context(), id); err != nil {
		mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.Ledger.GetAccount(r.Context(), r.PathValue("id"))
	if err != nil {
		mapError(w, err)
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "AccountNotFound"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountJSON(*a)})
}

func (s *Server) getAccountBalance(w http.ResponseWriter, r *http.Request) {
	b, err := s.Ledger.GetAccountBalance(r.Context(), r.PathValue("id"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": balanceJSON(b)})
}

func (s *Server) listJournalEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	input := ledger.ListJournalEntriesInput{Status: "all"}
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		input.Limit = &n
	}
	if v := q.Get("offset"); v != "" {
		n, _ := strconv.Atoi(v)
		input.Offset = &n
	}
	if v := q.Get("account"); v != "" {
		input.AccountID = &v
	}
	if v := q.Get("from"); v != "" {
		input.From = &v
	}
	if v := q.Get("to"); v != "" {
		input.To = &v
	}
	if v := q.Get("q"); v != "" {
		input.Q = &v
	}
	if v := q.Get("status"); v == "posted" || v == "draft" || v == "all" {
		input.Status = v
	}
	result, err := s.Ledger.ListJournalEntries(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	entries := make([]any, len(result.Entries))
	for i, e := range result.Entries {
		entries[i] = journalJSON(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries, "hasMore": result.HasMore, "limit": result.Limit, "offset": result.Offset,
	})
}

func (s *Server) getJournalEntry(w http.ResponseWriter, r *http.Request) {
	e, err := s.Ledger.GetJournalEntry(r.Context(), r.PathValue("id"))
	if err != nil {
		mapError(w, err)
		return
	}
	if e == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "JournalEntryNotFound"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": journalJSON(*e)})
}

type postingBody struct {
	AccountID   string `json:"accountId"`
	UnitsMinor  string `json:"unitsMinor"`
	CommodityID string `json:"commodityId"`
	Cost        *struct {
		PerUnitNumerator   string  `json:"perUnitNumerator"`
		PerUnitDenominator string  `json:"perUnitDenominator"`
		CommodityID        string  `json:"commodityId"`
		Date               *string `json:"date"`
		Label              *string `json:"label"`
	} `json:"cost"`
	Price *struct {
		PerUnitNumerator   string `json:"perUnitNumerator"`
		PerUnitDenominator string `json:"perUnitDenominator"`
		CommodityID        string `json:"commodityId"`
	} `json:"price"`
	Memo *string `json:"memo"`
}

func (s *Server) postJournalEntry(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeJournalInput(w, r)
	if !ok {
		return
	}
	entry, err := s.Ledger.PostJournalEntry(r.Context(), input)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry": journalJSON(entry)})
}

func (s *Server) deleteJournalEntry(w http.ResponseWriter, r *http.Request) {
	if err := s.Ledger.DeleteJournalEntry(r.Context(), r.PathValue("id")); err != nil {
		mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJournalInput(w http.ResponseWriter, r *http.Request) (ledger.PostJournalEntryInput, bool) {
	var body struct {
		EffectiveDate string        `json:"effectiveDate"`
		PostedAt      *string       `json:"postedAt"`
		Description   *string       `json:"description"`
		Postings      []postingBody `json:"postings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return ledger.PostJournalEntryInput{}, false
	}
	if !isoDateRe.MatchString(body.EffectiveDate) || len(body.Postings) < 2 {
		http.Error(w, "invalid journal entry", http.StatusBadRequest)
		return ledger.PostJournalEntryInput{}, false
	}
	postings := make([]domain.PostingInput, 0, len(body.Postings))
	for _, p := range body.Postings {
		if !uuidRe.MatchString(p.AccountID) || !intStrRe.MatchString(p.UnitsMinor) {
			http.Error(w, "invalid posting", http.StatusBadRequest)
			return ledger.PostJournalEntryInput{}, false
		}
		in := domain.PostingInput{
			AccountID: p.AccountID,
			Units:     domain.Units{Minor: domain.MustInt(p.UnitsMinor), CommodityID: p.CommodityID},
			Memo:      p.Memo,
		}
		if p.Cost != nil {
			in.Cost = &domain.CostBasis{
				PerUnit: domain.Rational{
					Numerator: domain.MustInt(p.Cost.PerUnitNumerator), Denominator: domain.MustInt(p.Cost.PerUnitDenominator),
				},
				CommodityID: p.Cost.CommodityID, Date: p.Cost.Date, Label: p.Cost.Label,
			}
		}
		if p.Price != nil {
			in.Price = &domain.TransactionPrice{
				PerUnit: domain.Rational{
					Numerator: domain.MustInt(p.Price.PerUnitNumerator), Denominator: domain.MustInt(p.Price.PerUnitDenominator),
				},
				CommodityID: p.Price.CommodityID,
			}
		}
		postings = append(postings, in)
	}
	return ledger.PostJournalEntryInput{
		EffectiveDate: body.EffectiveDate, PostedAt: body.PostedAt, Description: body.Description, Postings: postings,
	}, true
}

func (s *Server) exchange(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusGone, map[string]string{
		"error":   "Gone",
		"message": "POST /api/exchanges is disabled; use POST /api/v1/transaction",
	})
}

func (s *Server) listBalances(w http.ResponseWriter, r *http.Request) {
	list, err := s.Ledger.GetAccountBalances(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]any, len(list))
	for i, b := range list {
		out[i] = balanceJSON(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"balances": out})
}

func (s *Server) listPrices(w http.ResponseWriter, r *http.Request) {
	list, err := s.Ledger.ListMarketPrices(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	out := make([]any, len(list))
	for i, p := range list {
		out[i] = priceJSON(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": out})
}

func (s *Server) upsertPrice(w http.ResponseWriter, r *http.Request) {
	if err := s.Ledger.UpsertMarketPrice(r.Context()); err != nil {
		mapError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) netWorth(w http.ResponseWriter, r *http.Request) {
	report, err := s.Ledger.ComputeNetWorth(r.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": netWorthJSON(report)})
}

func (s *Server) clearCache(w http.ResponseWriter, r *http.Request) {
	if err := s.Ledger.ClearCache(r.Context()); err != nil {
		log.Printf("cache clear: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mapError(w http.ResponseWriter, err error) {
	de, ok := domain.IsDomainError(err)
	if !ok {
		// Internal errors may embed SQL or driver details — log them, but
		// return a generic message so internals are not disclosed to clients.
		log.Printf("internal error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	status := http.StatusBadRequest
	if strings.HasSuffix(de.Code, "NotFound") {
		status = http.StatusNotFound
	}
	switch de.Code {
	case "PresentationCurrencyNotSet", "DefaultCurrencyNotSet", "FunctionalCurrencyNotSet", "FunctionalCurrencyChangeNotConfirmed", "FunctionalCurrencyChangeInProgress", "TransactionCostAccountMissing":
		status = http.StatusConflict
	case "UnsupportedCurrency":
		status = http.StatusBadRequest
	case "MissingEcbRate", "MissingValuationPrice", "MissingFunctionalCost":
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]string{"error": de.Code, "message": de.Message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func commodityJSON(c domain.Commodity) map[string]any {
	return map[string]any{"id": c.ID, "code": c.Code, "name": c.Name, "minorUnits": c.MinorUnits, "kind": c.Kind}
}

func accountJSON(a domain.Account) map[string]any {
	return map[string]any{
		"id": a.ID, "code": a.Code, "name": a.Name, "accountType": a.AccountType,
		"parentId": a.ParentID, "nativeCommodityId": a.NativeCommodityID,
		"createdAt": a.CreatedAt,
	}
}

func balanceJSON(b domain.AccountBalance) map[string]any {
	return map[string]any{"accountId": b.AccountID, "commodityId": b.CommodityID, "minor": b.Minor.String()}
}

func journalJSON(e domain.JournalEntry) map[string]any {
	postings := make([]any, len(e.Postings))
	for i, p := range e.Postings {
		item := map[string]any{
			"accountId": p.AccountID,
			"units":     map[string]any{"minor": p.Units.Minor.String(), "commodityId": p.Units.CommodityID},
			"weight":    map[string]any{"minor": p.Weight.Minor.String(), "commodityId": p.Weight.CommodityID},
			"lineOrder": p.LineOrder,
			"memo":      p.Memo,
		}
		if p.Cost != nil {
			item["cost"] = map[string]any{
				"perUnit":     map[string]string{"numerator": p.Cost.PerUnit.Numerator.String(), "denominator": p.Cost.PerUnit.Denominator.String()},
				"commodityId": p.Cost.CommodityID, "date": p.Cost.Date, "label": p.Cost.Label,
			}
		}
		if p.Price != nil {
			item["price"] = map[string]any{
				"perUnit":     map[string]string{"numerator": p.Price.PerUnit.Numerator.String(), "denominator": p.Price.PerUnit.Denominator.String()},
				"commodityId": p.Price.CommodityID,
			}
		}
		postings[i] = item
	}
	return map[string]any{
		"id": e.ID, "effectiveDate": domain.UTCDate(e.EffectiveDate), "description": e.Description, "status": e.Status,
		"createdAt": e.CreatedAt, "postedAt": e.PostedAt,
		"postings": postings,
	}
}

func priceJSON(p domain.MarketPrice) map[string]any {
	return map[string]any{
		"baseCommodityId": p.BaseCommodityID, "quoteCommodityId": p.QuoteCommodityID,
		"price":      map[string]string{"numerator": p.Price.Numerator.String(), "denominator": p.Price.Denominator.String()},
		"observedAt": p.ObservedAt, "source": p.Source,
	}
}

func netWorthJSON(r domain.NetWorthReport) map[string]any {
	mapVB := func(list []domain.ValuedBalance) []any {
		out := make([]any, len(list))
		for i, v := range list {
			out[i] = map[string]any{
				"accountId": v.AccountID, "accountType": v.AccountType,
				"reportingMinor": v.ReportingMinor.String(), "reportingCommodityId": v.ReportingCommodityID,
				"native": balanceJSON(v.Native),
			}
		}
		return out
	}
	return map[string]any{
		"reportingCommodityId": r.ReportingCommodityID, "asOf": r.AsOf,
		"assets": mapVB(r.Assets), "liabilities": mapVB(r.Liabilities),
		"totalAssetsMinor": r.TotalAssetsMinor.String(), "totalLiabilitiesMinor": r.TotalLiabilitiesMinor.String(),
		"netWorthMinor": r.NetWorthMinor.String(),
	}
}
