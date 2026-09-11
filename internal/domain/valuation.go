package domain

import (
	"math/big"
	"time"
)

type BalancePosting struct {
	AccountID   string
	UnitsMinor  *big.Int
	CommodityID string
}

func SumAccountBalances(postings []BalancePosting) []AccountBalance {
	type key struct{ accountID, commodityID string }
	m := map[key]*AccountBalance{}
	for _, posting := range postings {
		k := key{posting.AccountID, posting.CommodityID}
		existing := m[k]
		if existing != nil {
			existing.Minor.Add(existing.Minor, posting.UnitsMinor)
			continue
		}
		m[k] = &AccountBalance{
			AccountID:   posting.AccountID,
			CommodityID: posting.CommodityID,
			Minor:       cloneInt(posting.UnitsMinor),
		}
	}
	out := make([]AccountBalance, 0, len(m))
	for _, b := range m {
		if b.Minor.Sign() != 0 {
			out = append(out, *b)
		}
	}
	return out
}

func GetAccountNativeBalance(accountID, nativeCommodityID string, balances []AccountBalance) AccountBalance {
	for _, b := range balances {
		if b.AccountID == accountID && b.CommodityID == nativeCommodityID {
			return b
		}
	}
	return AccountBalance{
		AccountID:   accountID,
		CommodityID: nativeCommodityID,
		Minor:       big.NewInt(0),
	}
}

func findDirectPrice(prices []MarketPrice, baseCommodityID, quoteCommodityID string) *MarketPrice {
	for i := range prices {
		if prices[i].BaseCommodityID == baseCommodityID && prices[i].QuoteCommodityID == quoteCommodityID {
			p := prices[i]
			return &p
		}
	}
	return nil
}

func findInvertedPrice(prices []MarketPrice, baseCommodityID, quoteCommodityID string) *MarketPrice {
	for i := range prices {
		if prices[i].BaseCommodityID == quoteCommodityID && prices[i].QuoteCommodityID == baseCommodityID {
			inv, err := InvertRational(prices[i].Price)
			if err != nil {
				return nil
			}
			return &MarketPrice{
				BaseCommodityID:  baseCommodityID,
				QuoteCommodityID: quoteCommodityID,
				Price:            inv,
				ObservedAt:       prices[i].ObservedAt,
				Source:           prices[i].Source,
			}
		}
	}
	return nil
}

func FindMarketPrice(prices []MarketPrice, baseCommodityID, quoteCommodityID string) *MarketPrice {
	if baseCommodityID == quoteCommodityID {
		r, _ := RationalFromPair(big.NewInt(1), big.NewInt(1))
		return &MarketPrice{
			BaseCommodityID:  baseCommodityID,
			QuoteCommodityID: quoteCommodityID,
			Price:            r,
			ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			Source:           "identity",
		}
	}
	if direct := findDirectPrice(prices, baseCommodityID, quoteCommodityID); direct != nil {
		return direct
	}
	if inverted := findInvertedPrice(prices, baseCommodityID, quoteCommodityID); inverted != nil {
		return inverted
	}
	return nil
}

func LookupMarketPrice(prices []MarketPrice, baseCommodityID, quoteCommodityID string) (MarketPrice, error) {
	found := FindMarketPrice(prices, baseCommodityID, quoteCommodityID)
	if found == nil {
		return MarketPrice{}, MissingValuationPrice(baseCommodityID, quoteCommodityID)
	}
	return *found, nil
}

func ConvertMinorToReporting(
	minor *big.Int,
	fromCommodityID, toCommodityID string,
	prices []MarketPrice,
	commodities map[string]Commodity,
) (*big.Int, error) {
	if fromCommodityID == toCommodityID {
		return cloneInt(minor), nil
	}
	price, err := LookupMarketPrice(prices, fromCommodityID, toCommodityID)
	if err != nil {
		return nil, err
	}
	fromMinorUnits := 2
	toMinorUnits := 2
	if from, ok := commodities[fromCommodityID]; ok {
		fromMinorUnits = from.MinorUnits
	}
	if to, ok := commodities[toCommodityID]; ok {
		toMinorUnits = to.MinorUnits
	}
	return ConvertMinorAtRate(minor, price.Price, fromMinorUnits, toMinorUnits)
}

func ValueBalanceSheetAccount(
	account Account,
	balance AccountBalance,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities map[string]Commodity,
) (ValuedBalance, error) {
	reportingMinor, err := ConvertMinorToReporting(
		balance.Minor,
		balance.CommodityID,
		reportingCommodityID,
		prices,
		commodities,
	)
	if err != nil {
		return ValuedBalance{}, err
	}
	return ValuedBalance{
		AccountID:            account.ID,
		AccountType:          account.AccountType,
		Native:               balance,
		ReportingMinor:       reportingMinor,
		ReportingCommodityID: reportingCommodityID,
	}, nil
}

// IncomeExpenseMinor is income minus expenses at current market prices.
// Retranslation uses IncomeExpenseFromPostings (lot / functional cost) instead;
// that path does not fall back to market for foreign lines.
func IncomeExpenseMinor(
	accounts []Account,
	balances []AccountBalance,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities []Commodity,
	excludeCodes map[string]bool,
) (*big.Int, error) {
	pnl, err := sumReportingForTypes(accounts, balances, reportingCommodityID, prices, commodities, map[AccountType]bool{
		AccountIncome:  true,
		AccountExpense: true,
	}, excludeCodes)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Neg(pnl), nil
}

// RetranslationExpenseMinor is the expense (positive = loss, negative = gain)
// that makes income − expense equal net worth.
func RetranslationExpenseMinor(incomeExpenseMinor, netWorthMinor *big.Int) *big.Int {
	ie := incomeExpenseMinor
	if ie == nil {
		ie = big.NewInt(0)
	}
	nw := netWorthMinor
	if nw == nil {
		nw = big.NewInt(0)
	}
	return new(big.Int).Sub(ie, nw)
}

func sumReportingForTypes(
	accounts []Account,
	balances []AccountBalance,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities []Commodity,
	types map[AccountType]bool,
	excludeCodes map[string]bool,
) (*big.Int, error) {
	accountMap := map[string]Account{}
	for _, a := range accounts {
		accountMap[a.ID] = a
	}
	commodityMap := map[string]Commodity{}
	for _, c := range commodities {
		commodityMap[c.ID] = c
	}
	total := big.NewInt(0)
	for _, balance := range balances {
		account, ok := accountMap[balance.AccountID]
		if !ok || !types[account.AccountType] || excludeCodes[account.Code] {
			continue
		}
		reporting, err := ConvertMinorToReporting(
			balance.Minor, balance.CommodityID, reportingCommodityID, prices, commodityMap,
		)
		if err != nil {
			return nil, err
		}
		total.Add(total, reporting)
	}
	return total, nil
}

func ComputeNetWorth(
	accounts []Account,
	balances []AccountBalance,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities []Commodity,
	asOf string,
) (NetWorthReport, error) {
	accountMap := map[string]Account{}
	for _, a := range accounts {
		accountMap[a.ID] = a
	}
	commodityMap := map[string]Commodity{}
	for _, c := range commodities {
		commodityMap[c.ID] = c
	}

	var valued []ValuedBalance
	for _, balance := range balances {
		account, ok := accountMap[balance.AccountID]
		if !ok {
			continue
		}
		if !isBalanceSheetType(account.AccountType) {
			continue
		}
		vb, err := ValueBalanceSheetAccount(account, balance, reportingCommodityID, prices, commodityMap)
		if err != nil {
			return NetWorthReport{}, err
		}
		valued = append(valued, vb)
	}

	var assets, liabilities []ValuedBalance
	totalAssets := big.NewInt(0)
	totalLiabilities := big.NewInt(0)
	for _, v := range valued {
		switch v.AccountType {
		case AccountAsset:
			assets = append(assets, v)
			totalAssets.Add(totalAssets, v.ReportingMinor)
		case AccountLiability:
			liabilities = append(liabilities, v)
			totalLiabilities.Add(totalLiabilities, v.ReportingMinor)
		}
	}
	if assets == nil {
		assets = []ValuedBalance{}
	}
	if liabilities == nil {
		liabilities = []ValuedBalance{}
	}

	return NetWorthReport{
		ReportingCommodityID:  reportingCommodityID,
		AsOf:                  asOf,
		Assets:                assets,
		Liabilities:           liabilities,
		TotalAssetsMinor:      totalAssets,
		TotalLiabilitiesMinor: totalLiabilities,
		NetWorthMinor:         new(big.Int).Add(totalAssets, totalLiabilities),
	}, nil
}

func isBalanceSheetType(t AccountType) bool {
	return t == AccountAsset || t == AccountLiability || t == AccountEquity
}

func ValidateAccountCurrency(account Account, unitsCommodityID string) error {
	if account.NativeCommodityID != unitsCommodityID {
		return CurrencyMismatch(
			"Account " + account.Code + " expects commodity " + account.NativeCommodityID + ", got " + unitsCommodityID,
		)
	}
	return nil
}

type PnLPosting struct {
	AccountCode string
	AccountType AccountType
	Units       Units
	Cost        *CostBasis
}

// ReportingMinor is the posting amount in F: stored lot / functional cost when
// present (including F-native lines that inherited FIFO), else native if already
// F. Foreign lines without cost are an error — not today's close.
func ReportingMinor(
	units Units,
	cost *CostBasis,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities map[string]Commodity,
) (*big.Int, error) {
	_ = prices
	_ = commodities
	if cost != nil && cost.CommodityID == reportingCommodityID {
		got, err := MultiplyUnitsByRational(units.Minor, cost.PerUnit)
		if err != nil {
			return nil, err
		}
		return got, nil
	}
	if units.CommodityID == reportingCommodityID {
		return cloneInt(units.Minor), nil
	}
	return nil, MissingFunctionalCost(units.CommodityID)
}

func IncomeExpenseFromPostings(
	lines []PnLPosting,
	reportingCommodityID string,
	prices []MarketPrice,
	commodities []Commodity,
	excludeCodes map[string]bool,
) (*big.Int, error) {
	commodityMap := map[string]Commodity{}
	for _, c := range commodities {
		commodityMap[c.ID] = c
	}
	if excludeCodes == nil {
		excludeCodes = map[string]bool{}
	}
	total := big.NewInt(0)
	for _, line := range lines {
		if line.AccountType != AccountIncome && line.AccountType != AccountExpense {
			continue
		}
		if excludeCodes[line.AccountCode] {
			continue
		}
		reporting, err := ReportingMinor(line.Units, line.Cost, reportingCommodityID, prices, commodityMap)
		if err != nil {
			return nil, err
		}
		total.Add(total, reporting)
	}
	return new(big.Int).Neg(total), nil
}
