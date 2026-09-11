package domain

import "math/big"

type AccountType string

const (
	AccountAsset     AccountType = "asset"
	AccountLiability AccountType = "liability"
	AccountEquity    AccountType = "equity"
	AccountIncome    AccountType = "income"
	AccountExpense   AccountType = "expense"
)

type JournalEntryStatus string

const (
	JournalDraft  JournalEntryStatus = "draft"
	JournalPosted JournalEntryStatus = "posted"
)

type CommodityKind string

const (
	CommodityCurrency CommodityKind = "currency"
	CommoditySecurity CommodityKind = "security"
	CommodityOther    CommodityKind = "other"
)

type Commodity struct {
	ID         string
	Code       string
	Name       string
	MinorUnits int
	Kind       CommodityKind
}

type Account struct {
	ID                string
	Code              string
	Name              string
	AccountType       AccountType
	ParentID          *string
	NativeCommodityID string
	CreatedAt         string
}

type Rational struct {
	Numerator   *big.Int
	Denominator *big.Int
}

func (r Rational) Clone() Rational {
	return Rational{
		Numerator:   cloneInt(r.Numerator),
		Denominator: cloneInt(r.Denominator),
	}
}

type CostBasis struct {
	PerUnit     Rational
	CommodityID string
	Date        *string
	Label       *string
}

type PriceSource string

const (
	PriceSourceActual   PriceSource = "actual"
	PriceSourceECB      PriceSource = "ecb"
	PriceSourceIdentity PriceSource = "identity"
)

type TransactionPrice struct {
	PerUnit      Rational
	CommodityID  string
	Source       PriceSource
	ObservedDate *string
}

// Units is a signed quantity in a commodity's minor units.
type Units struct {
	Minor       *big.Int
	CommodityID string
}

// Weight is used for journal balancing — stored at post time.
type Weight struct {
	Minor       *big.Int
	CommodityID string
}

type PostingInput struct {
	AccountID string
	Units     Units
	Cost      *CostBasis
	Price     *TransactionPrice
	Memo      *string
}

type ResolvedPosting struct {
	PostingInput
	Weight    Weight
	LineOrder int
}

type JournalEntry struct {
	ID            string
	EffectiveDate string
	Description   *string
	Status        JournalEntryStatus
	CreatedAt     string
	PostedAt      *string
	Postings      []ResolvedPosting
}

type MarketPrice struct {
	BaseCommodityID  string
	QuoteCommodityID string
	Price            Rational
	ObservedAt       string
	Source           string
}

type AccountBalance struct {
	AccountID   string
	CommodityID string
	Minor       *big.Int
}

type ValuedBalance struct {
	AccountID            string
	AccountType          AccountType
	Native               AccountBalance
	ReportingMinor       *big.Int
	ReportingCommodityID string
}

type NetWorthReport struct {
	ReportingCommodityID  string
	AsOf                  string
	Assets                []ValuedBalance
	Liabilities           []ValuedBalance
	TotalAssetsMinor      *big.Int
	TotalLiabilitiesMinor *big.Int
	NetWorthMinor         *big.Int
}

// RetranslationPreview is the read-only forex retranslation estimate
// (GET /api/v1/retranslate). AsOf is set when the preview is computed/cached.
type RetranslationPreview struct {
	ReportingCommodityID string
	RetranslationMinor   *big.Int
	NetWorthMinor        *big.Int
	IncomeExpenseMinor   *big.Int
	AsOf                 string
}

type AuditEvent struct {
	ID        string
	EventType string
	Payload   map[string]any
	CreatedAt string
}

func cloneInt(n *big.Int) *big.Int {
	if n == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(n)
}

func Int(n int64) *big.Int {
	return big.NewInt(n)
}

func MustInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("invalid integer: " + s)
	}
	return n
}
