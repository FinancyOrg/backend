package domain

import "fmt"

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func newError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func AccountNotFound(id string) *Error {
	return newError("AccountNotFound", "Account not found: "+id)
}

func AccountAlreadyExists(code string) *Error {
	return newError("AccountAlreadyExists", "Account already exists: "+code)
}

func AccountNotEmpty(id string) *Error {
	return newError("AccountNotEmpty", "Account still has journal activity and cannot be changed that way: "+id)
}

func JournalEntryNotFound(id string) *Error {
	return newError("JournalEntryNotFound", "Journal entry not found: "+id)
}

func InvalidAccountType(typ string) *Error {
	return newError("InvalidAccountType", "Invalid account type: "+typ)
}

func InvalidAccount(message string) *Error {
	return newError("InvalidAccount", message)
}

func InvalidCurrency(message string) *Error {
	return newError("InvalidCurrency", message)
}

func InvalidSetting(message string) *Error {
	return newError("InvalidSetting", message)
}

func CommodityAlreadyExists(code string) *Error {
	return newError("CommodityAlreadyExists", "Commodity already exists: "+code)
}

func CurrencyMismatch(message string) *Error {
	return newError("CurrencyMismatch", message)
}

func UnbalancedJournalEntry(details string) *Error {
	return newError("UnbalancedJournalEntry", "Journal entry is unbalanced: "+details)
}

func InvalidAmount(message string) *Error {
	return newError("InvalidAmount", message)
}

func InvalidPrice(message string) *Error {
	return newError("InvalidPrice", message)
}

func MissingValuationPrice(base, quote string) *Error {
	return newError("MissingValuationPrice", fmt.Sprintf("Missing valuation price for %s/%s", base, quote))
}

func MissingFunctionalCost(commodityID string) *Error {
	return newError(
		"MissingFunctionalCost",
		"Posting in "+commodityID+" has no functional cost; IE does not convert at today's close",
	)
}

func FunctionalCurrencyNotSet() *Error {
	return newError("FunctionalCurrencyNotSet", "Functional currency is not set. Set it before posting or viewing the ledger.")
}

func PresentationCurrencyNotSet() *Error {
	return FunctionalCurrencyNotSet()
}

// DefaultCurrencyNotSet is a legacy alias for FunctionalCurrencyNotSet.
func DefaultCurrencyNotSet() *Error {
	return FunctionalCurrencyNotSet()
}

func FunctionalCurrencyChangeNotConfirmed(from, to string) *Error {
	return newError(
		"FunctionalCurrencyChangeNotConfirmed",
		"Changing functional currency from "+from+" to "+to+" restates lot costs at today's ECB rate. Retry with confirm=true.",
	)
}

func FunctionalCurrencyChangeInProgress(from, to string) *Error {
	return newError(
		"FunctionalCurrencyChangeInProgress",
		"A functional currency change from "+from+" to "+to+" is already restating lot costs.",
	)
}

func UnsupportedCurrency(code string) *Error {
	return newError("UnsupportedCurrency", "Currency is not supported by ECB reference rates: "+code)
}

func DefaultCurrencyAlreadySet(commodityID string) *Error {
	return newError("DefaultCurrencyAlreadySet", "Default currency is already set to "+commodityID)
}

func MissingEcbRate(currency, date string) *Error {
	return newError("MissingEcbRate", fmt.Sprintf("No ECB reference rate for %s/EUR on %s", currency, date))
}

func InvalidEcbDate(date string) *Error {
	return newError("InvalidEcbDate", "date cannot be in the future")
}

func TransactionCostAccountMissing() *Error {
	return newError(
		"TransactionCostAccountMissing",
		"Expenses:Transaction Cost account is missing for the fee currency; create Expenses:Transaction Cost:{CCY} before posting",
	)
}

func InvalidExchange(message string) *Error {
	return newError("InvalidExchange", message)
}

func InvalidTransaction(message string) *Error {
	return newError("InvalidTransaction", message)
}

func InvalidJournalEntry(message string) *Error {
	return newError("InvalidJournalEntry", message)
}

var AccountTypes = []AccountType{
	AccountAsset,
	AccountLiability,
	AccountEquity,
	AccountIncome,
	AccountExpense,
}

func IsAccountType(value string) bool {
	for _, t := range AccountTypes {
		if string(t) == value {
			return true
		}
	}
	return false
}

// NormalBalanceSign is the sign convention for display/reporting.
func NormalBalanceSign(accountType AccountType) int {
	switch accountType {
	case AccountAsset, AccountExpense:
		return 1
	case AccountLiability, AccountEquity, AccountIncome:
		return -1
	default:
		return 0
	}
}

func IsDomainError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	de, ok := err.(*Error)
	return de, ok
}
