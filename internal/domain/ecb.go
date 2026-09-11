package domain

import "strings"

// EcbQuoteCurrency is the ECB EXR quote unit. Spots are units of CCY per 1 EUR.
// This is a rate-source convention, not a ledger home currency. A commodity
// row for EUR exists only if the user creates or selects it.
const EcbQuoteCurrency = "EUR"

// SupportedCurrencyCodes is the embedded ECB EXR allowlist (see docs/rebuild/backend/06-ecb.md).
var SupportedCurrencyCodes = map[string]struct{}{
	EcbQuoteCurrency: {}, // identity; no ECB request
	"AUD":            {}, "BRL": {}, "CAD": {}, "CHF": {}, "CNY": {},
	"CZK": {}, "DKK": {}, "GBP": {}, "HKD": {}, "HUF": {},
	"IDR": {}, "ILS": {}, "INR": {}, "ISK": {}, "JPY": {},
	"KRW": {}, "MXN": {}, "MYR": {}, "NOK": {}, "NZD": {},
	"PHP": {}, "PLN": {}, "RON": {}, "SEK": {}, "SGD": {},
	"THB": {}, "TRY": {}, "USD": {}, "ZAR": {},
}

func IsSupportedCurrency(code string) bool {
	_, ok := SupportedCurrencyCodes[code]
	return ok
}

func IsEcbQuoteCurrency(code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), EcbQuoteCurrency)
}
