package ledger

import (
	"context"
	"math/big"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Service) stampReportingLots(ctx context.Context, postings []domain.PostingInput, reportingID, effectiveAt, postedAt string) ([]domain.PostingInput, error) {
	if len(postings) == 0 {
		return postings, nil
	}
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accountByID := map[string]domain.Account{}
	for _, a := range accounts {
		accountByID[a.ID] = a
	}
	commodities, err := s.ListCommodities(ctx)
	if err != nil {
		return nil, err
	}
	commodityByID := map[string]domain.Commodity{}
	for _, c := range commodities {
		commodityByID[c.ID] = c
	}
	reporting, ok := commodityByID[reportingID]
	if !ok {
		return nil, domain.InvalidCurrency("Unknown reporting commodity: " + reportingID)
	}

	var monetaryIDs []string
	needed := map[string]struct{}{}
	for _, p := range postings {
		account, ok := accountByID[p.AccountID]
		if !ok {
			continue
		}
		if !isMonetaryAccount(account) {
			continue
		}
		ccy, ok := commodityByID[p.Units.CommodityID]
		if !ok || !isForeignCurrency(ccy, reportingID) {
			continue
		}
		if _, seen := needed[account.ID]; seen {
			continue
		}
		needed[account.ID] = struct{}{}
		monetaryIDs = append(monetaryIDs, account.ID)
	}

	lots := map[string][]domain.Lot{}
	if len(monetaryIDs) > 0 {
		lots, err = s.loadMonetaryLots(ctx, monetaryIDs, reportingID, effectiveAt, postedAt, nowISO())
		if err != nil {
			return nil, err
		}
	}

	date := domain.UTCDate(effectiveAt)
	costDate := effectiveAt
	convert := func(minor *big.Int, commodityID string) (*big.Int, error) {
		return s.convertToReporting(ctx, minor, commodityID, reporting, commodityByID, date)
	}

	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isMonetaryAccount(account) {
			continue
		}
		ccy, ok := commodityByID[p.Units.CommodityID]
		if !ok || !isForeignCurrency(ccy, reportingID) || p.Units.Minor.Sign() >= 0 {
			continue
		}
		if p.Cost != nil {
			domain.EnsureNativeWeight(p, reportingID)
			lots[account.ID] = domain.ApplyLotDelta(lots[account.ID], p.Units.Minor, domain.CostReportingAmount(p.Units, p.Cost, reportingID))
			continue
		}
		qty := new(big.Int).Abs(p.Units.Minor)
		costMinor, remaining, shortfall := domain.ConsumeLots(lots[account.ID], qty)
		lots[account.ID] = remaining
		if shortfall.Sign() > 0 {
			spot, err := convert(shortfall, p.Units.CommodityID)
			if err != nil {
				return nil, err
			}
			spot = new(big.Int).Abs(spot)
			costMinor.Add(costMinor, spot)
			lots[account.ID] = domain.AddOverdraft(lots[account.ID], shortfall, spot)
		}
		if err := domain.AttachReportingCost(p, costMinor, reportingID, &costDate); err != nil {
			return nil, err
		}
	}

	var realizedCover *big.Int
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isMonetaryAccount(account) {
			continue
		}
		ccy, ok := commodityByID[p.Units.CommodityID]
		if !ok || !isForeignCurrency(ccy, reportingID) || p.Units.Minor.Sign() <= 0 {
			continue
		}
		if p.Cost != nil {
			domain.EnsureNativeWeight(p, reportingID)
			remaining, realized := domain.CoverInflow(lots[account.ID], p.Units.Minor, domain.CostReportingAmount(p.Units, p.Cost, reportingID))
			lots[account.ID] = remaining
			realizedCover = addRealized(realizedCover, realized)
			continue
		}
		if copied := counterpartReportingMinor(postings, p, reportingID); copied != nil {
			if err := domain.AttachReportingCost(p, copied, reportingID, &costDate); err != nil {
				return nil, err
			}
			remaining, realized := domain.CoverInflow(lots[account.ID], p.Units.Minor, copied)
			lots[account.ID] = remaining
			realizedCover = addRealized(realizedCover, realized)
			continue
		}
		inflowCost := functionalNetGiven(postings, accountByID, reportingID)
		if inflowCost == nil || inflowCost.Sign() <= 0 {
			var err error
			inflowCost, err = convert(p.Units.Minor, p.Units.CommodityID)
			if err != nil {
				return nil, err
			}
		}
		if err := domain.AttachReportingCost(p, inflowCost, reportingID, &costDate); err != nil {
			return nil, err
		}
		remaining, realized := domain.CoverInflow(lots[account.ID], p.Units.Minor, inflowCost)
		lots[account.ID] = remaining
		realizedCover = addRealized(realizedCover, realized)
	}

	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok {
			continue
		}
		if account.AccountType != domain.AccountIncome && account.AccountType != domain.AccountExpense {
			continue
		}
		ccy, ok := commodityByID[p.Units.CommodityID]
		if !ok || !isForeignCurrency(ccy, reportingID) {
			continue
		}
		if p.Cost != nil {
			domain.EnsureNativeWeight(p, reportingID)
			continue
		}
		if copied := counterpartReportingMinor(postings, p, reportingID); copied != nil {
			if err := domain.AttachReportingCost(p, copied, reportingID, &costDate); err != nil {
				return nil, err
			}
			continue
		}
		spot, err := convert(p.Units.Minor, p.Units.CommodityID)
		if err != nil {
			return nil, err
		}
		if err := domain.AttachReportingCost(p, spot, reportingID, &costDate); err != nil {
			return nil, err
		}
	}

	if err := stampFunctionalPnLFromLots(postings, accountByID, reportingID, &costDate); err != nil {
		return nil, err
	}
	if err := rewriteSameCurrencyResidual(postings, accountByID, reportingID, &costDate); err != nil {
		return nil, err
	}
	postings, err = rewriteForeignBalanceSheetExit(postings, accountByID, reportingID, &costDate)
	if err != nil {
		return nil, err
	}
	postings, err = rewriteForeignCurrencyExchange(postings, accountByID, reportingID, &costDate)
	if err != nil {
		return nil, err
	}
	if realizedCover != nil && realizedCover.Sign() != 0 {
		postings, err = rewriteOverdraftCover(postings, accountByID, reportingID, realizedCover, &costDate)
		if err != nil {
			return nil, err
		}
	}
	return postings, nil
}

func counterpartReportingMinor(postings []domain.PostingInput, dest *domain.PostingInput, reportingID string) *big.Int {
	if dest == nil || dest.Units.Minor == nil {
		return nil
	}
	for i := range postings {
		p := &postings[i]
		if p == dest || p.Cost == nil || p.Units.CommodityID != dest.Units.CommodityID || p.Cost.CommodityID != reportingID {
			continue
		}
		src := domain.CostReportingAmount(p.Units, p.Cost, reportingID)
		if src == nil {
			continue
		}
		return domain.ScaleReportingCost(p.Units.Minor, src, dest.Units.Minor)
	}
	return nil
}

func applyLotMovement(lots []domain.Lot, units, costMinor *big.Int) []domain.Lot {
	return domain.ApplyLotDelta(lots, units, costMinor)
}

func isTransactionCostAccount(account domain.Account) bool {
	return strings.HasPrefix(account.Code, TransactionCostAccountPrefix)
}

func addRealized(total, next *big.Int) *big.Int {
	if next == nil || next.Sign() == 0 {
		return total
	}
	if total == nil {
		return new(big.Int).Set(next)
	}
	return total.Add(total, next)
}

func functionalNetGiven(postings []domain.PostingInput, accountByID map[string]domain.Account, reportingID string) *big.Int {
	foreignIn := 0
	given := big.NewInt(0)
	txn := big.NewInt(0)
	foundGiven := false
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || p.Units.Minor == nil {
			continue
		}
		if isMonetaryAccount(account) && p.Units.CommodityID != reportingID && p.Units.Minor.Sign() > 0 {
			foreignIn++
		}
		if p.Units.CommodityID == reportingID && p.Units.Minor.Sign() < 0 &&
			(isMonetaryAccount(account) ||
				(account.AccountType == domain.AccountIncome || account.AccountType == domain.AccountExpense) &&
					!isTransactionCostAccount(account)) {
			given.Add(given, new(big.Int).Abs(p.Units.Minor))
			foundGiven = true
		}
		if isTransactionCostAccount(account) && p.Units.CommodityID == reportingID {
			txn.Add(txn, p.Units.Minor)
		}
	}
	if foreignIn != 1 || !foundGiven {
		return nil
	}
	return new(big.Int).Sub(given, txn)
}

func foreignMonetaryOutflowCost(postings []domain.PostingInput, accountByID map[string]domain.Account, reportingID string) *big.Int {
	total := big.NewInt(0)
	found := false
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isMonetaryAccount(account) {
			continue
		}
		if p.Units.CommodityID == reportingID || p.Units.Minor == nil || p.Units.Minor.Sign() >= 0 || p.Cost == nil {
			continue
		}
		amt := domain.CostReportingAmount(p.Units, p.Cost, reportingID)
		if amt == nil {
			continue
		}
		total.Add(total, new(big.Int).Abs(amt))
		found = true
	}
	if !found {
		return nil
	}
	return total
}

func stampFunctionalPnLFromLots(postings []domain.PostingInput, accountByID map[string]domain.Account, reportingID string, costDate *string) error {
	fifo := foreignMonetaryOutflowCost(postings, accountByID, reportingID)
	if fifo == nil {
		return nil
	}
	copied := false
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok {
			continue
		}
		if account.AccountType != domain.AccountIncome && account.AccountType != domain.AccountExpense {
			continue
		}
		if isTransactionCostAccount(account) || p.Units.CommodityID != reportingID || p.Cost != nil {
			continue
		}
		if err := domain.AttachReportingCost(p, fifo, reportingID, costDate); err != nil {
			return err
		}
		copied = true
		break
	}
	if !copied {
		return nil
	}
	return attachTxnReportingCost(postings, accountByID, reportingID, big.NewInt(0), costDate)
}

func attachTxnReportingCost(postings []domain.PostingInput, accountByID map[string]domain.Account, reportingID string, amount *big.Int, costDate *string) error {
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isTransactionCostAccount(account) {
			continue
		}
		if err := domain.AttachReportingCost(p, amount, reportingID, costDate); err != nil {
			return err
		}
	}
	return nil
}

func rewriteForeignBalanceSheetExit(
	postings []domain.PostingInput,
	accountByID map[string]domain.Account,
	reportingID string,
	costDate *string,
) ([]domain.PostingInput, error) {
	var out, in *domain.PostingInput
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || p.Units.Minor == nil {
			continue
		}
		if isMonetaryAccount(account) && p.Units.CommodityID != reportingID && p.Units.Minor.Sign() < 0 {
			out = p
			continue
		}
		if isMonetaryAccount(account) && p.Units.CommodityID == reportingID && p.Units.Minor.Sign() > 0 {
			in = p
		}
	}
	if out == nil || in == nil {
		return postings, nil
	}

	outCost := domain.CostReportingAmount(out.Units, out.Cost, reportingID)
	if outCost == nil {
		return postings, nil
	}
	desiredTxnValue := new(big.Int).Sub(
		new(big.Int).Abs(outCost),
		new(big.Int).Abs(in.Units.Minor),
	)
	currentTxnValue, _ := transactionCostValues(postings, accountByID, reportingID)
	adjustment := new(big.Int).Sub(desiredTxnValue, currentTxnValue)
	if adjustment.Sign() == 0 {
		return postings, nil
	}

	if err := replaceSourcePriceWithCarrying(out, outCost, reportingID); err != nil {
		return nil, err
	}
	return appendReportingAdjustment(
		postings,
		accountByID,
		reportingID,
		adjustment,
		nil,
		costDate,
		"Realized foreign-currency gain or loss",
	)
}

func rewriteForeignCurrencyExchange(
	postings []domain.PostingInput,
	accountByID map[string]domain.Account,
	reportingID string,
	costDate *string,
) ([]domain.PostingInput, error) {
	var out, in *domain.PostingInput
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isMonetaryAccount(account) || p.Units.Minor == nil {
			continue
		}
		if p.Units.CommodityID != reportingID && p.Units.Minor.Sign() < 0 {
			out = p
		}
		if p.Units.CommodityID != reportingID && p.Units.Minor.Sign() > 0 {
			in = p
		}
	}
	if out == nil || in == nil || out.Units.CommodityID == in.Units.CommodityID {
		return postings, nil
	}
	outCost := domain.CostReportingAmount(out.Units, out.Cost, reportingID)
	inCost := domain.CostReportingAmount(in.Units, in.Cost, reportingID)
	if outCost == nil || inCost == nil {
		return postings, nil
	}
	desiredTxnValue := new(big.Int).Sub(
		new(big.Int).Abs(outCost),
		new(big.Int).Abs(inCost),
	)
	currentTxnValue, _ := transactionCostValues(postings, accountByID, reportingID)
	adjustment := new(big.Int).Sub(desiredTxnValue, currentTxnValue)
	if adjustment.Sign() == 0 {
		return postings, nil
	}
	return appendReportingAdjustment(
		postings,
		accountByID,
		reportingID,
		adjustment,
		out,
		costDate,
		"Realized foreign-currency exchange gain or loss",
	)
}

func rewriteOverdraftCover(
	postings []domain.PostingInput,
	accountByID map[string]domain.Account,
	reportingID string,
	realizedCover *big.Int,
	costDate *string,
) ([]domain.PostingInput, error) {
	if realizedCover == nil || realizedCover.Sign() == 0 {
		return postings, nil
	}
	var source, foreignIn *domain.PostingInput
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || p.Units.Minor == nil {
			continue
		}
		if isMonetaryAccount(account) && p.Units.CommodityID == reportingID && p.Units.Minor.Sign() < 0 {
			source = p
			continue
		}
		if isMonetaryAccount(account) && p.Units.CommodityID != reportingID && p.Units.Minor.Sign() > 0 {
			foreignIn = p
		}
	}
	if source == nil || foreignIn == nil || source.Price == nil {
		return postings, nil
	}

	baseTxnValue, currentTxnValue := transactionCostValues(postings, accountByID, reportingID)
	desiredTxnValue := new(big.Int).Add(baseTxnValue, realizedCover)
	adjustment := new(big.Int).Sub(desiredTxnValue, currentTxnValue)
	if adjustment.Sign() == 0 {
		return postings, nil
	}
	return appendReportingAdjustment(
		postings,
		accountByID,
		reportingID,
		adjustment,
		source,
		costDate,
		"Realized overdraft-cover gain or loss",
	)
}

func transactionCostValues(
	postings []domain.PostingInput,
	accountByID map[string]domain.Account,
	reportingID string,
) (base, current *big.Int) {
	base = big.NewInt(0)
	current = big.NewInt(0)
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isTransactionCostAccount(account) || p.Units.CommodityID != reportingID {
			continue
		}
		base.Add(base, p.Units.Minor)
		if value := domain.CostReportingAmount(p.Units, p.Cost, reportingID); value != nil {
			current.Add(current, value)
		} else {
			current.Add(current, p.Units.Minor)
		}
	}
	return base, current
}

func replaceSourcePriceWithCarrying(posting *domain.PostingInput, cost *big.Int, reportingID string) error {
	if posting == nil || posting.Units.Minor == nil || posting.Units.Minor.Sign() == 0 || cost == nil {
		return nil
	}
	perUnit, err := domain.NormalizeRational(domain.Rational{
		Numerator:   new(big.Int).Abs(cost),
		Denominator: new(big.Int).Abs(posting.Units.Minor),
	})
	if err != nil {
		return err
	}
	source := domain.PriceSourceActual
	if posting.Price != nil && posting.Price.Source != "" {
		source = posting.Price.Source
	}
	posting.Price = &domain.TransactionPrice{
		PerUnit:      perUnit,
		CommodityID:  reportingID,
		Source:       source,
		ObservedDate: nil,
	}
	return nil
}

func appendReportingAdjustment(
	postings []domain.PostingInput,
	accountByID map[string]domain.Account,
	reportingID string,
	amount *big.Int,
	source *domain.PostingInput,
	costDate *string,
	memoText string,
) ([]domain.PostingInput, error) {
	if amount == nil || amount.Sign() == 0 {
		return postings, nil
	}
	txnAccount, ok := findReportingTransactionCostAccount(accountByID, reportingID)
	if !ok {
		return nil, domain.InvalidJournalEntry("Missing transaction-cost account for realized foreign-currency adjustment")
	}

	memo := memoText
	adjustment := domain.PostingInput{
		AccountID: txnAccount.ID,
		Units: domain.Units{
			Minor:       cloneBig(amount),
			CommodityID: reportingID,
		},
		Memo: &memo,
	}
	if err := domain.AttachReportingCost(&adjustment, amount, reportingID, costDate); err != nil {
		return nil, err
	}

	if source != nil {
		if source.Price == nil || source.Price.CommodityID == reportingID {
			return nil, domain.InvalidJournalEntry("Missing foreign price for realized overdraft-cover adjustment")
		}
		oldPrice := source.Price.PerUnit.Clone()
		oldWeight, err := domain.ComputeWeight(*source)
		if err != nil {
			return nil, err
		}
		adjustmentWeight, err := domain.RoundHalfAwayFromZero(
			new(big.Int).Mul(amount, oldPrice.Numerator),
			oldPrice.Denominator,
		)
		if err != nil {
			return nil, err
		}
		adjustmentPerUnit, err := domain.NormalizeRational(domain.Rational{
			Numerator:   cloneBig(adjustmentWeight),
			Denominator: cloneBig(amount),
		})
		if err != nil {
			return nil, err
		}
		adjustment.Price = &domain.TransactionPrice{
			PerUnit:      adjustmentPerUnit,
			CommodityID:  source.Price.CommodityID,
			Source:       source.Price.Source,
			ObservedDate: source.Price.ObservedDate,
		}
		newWeight := new(big.Int).Sub(oldWeight.Minor, adjustmentWeight)
		perUnit, err := domain.NormalizeRational(domain.Rational{
			Numerator:   newWeight,
			Denominator: cloneBig(source.Units.Minor),
		})
		if err != nil {
			return nil, err
		}
		source.Price.PerUnit = perUnit
	}
	return append(postings, adjustment), nil
}

func findReportingTransactionCostAccount(
	accountByID map[string]domain.Account,
	reportingID string,
) (domain.Account, bool) {
	for _, account := range accountByID {
		if isTransactionCostAccount(account) && account.NativeCommodityID == reportingID {
			return account, true
		}
	}
	return domain.Account{}, false
}

func rewriteSameCurrencyResidual(postings []domain.PostingInput, accountByID map[string]domain.Account, reportingID string, costDate *string) error {
	var out, in *domain.PostingInput
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || isTransactionCostAccount(account) || p.Units.CommodityID == reportingID || p.Units.Minor == nil {
			continue
		}
		foreignMonetary := isMonetaryAccount(account)
		foreignPnL := account.AccountType == domain.AccountIncome || account.AccountType == domain.AccountExpense
		if !foreignMonetary && !foreignPnL {
			continue
		}
		if p.Units.Minor.Sign() < 0 && foreignMonetary {
			out = p
		}
		if p.Units.Minor.Sign() > 0 {
			in = p
		}
	}
	if out == nil || in == nil || out.Units.CommodityID != in.Units.CommodityID {
		return nil
	}
	outQty := new(big.Int).Abs(out.Units.Minor)
	inQty := new(big.Int).Abs(in.Units.Minor)
	if outQty.Cmp(inQty) == 0 {
		return nil
	}
	outCost := domain.CostReportingAmount(out.Units, out.Cost, reportingID)
	inCost := domain.CostReportingAmount(in.Units, in.Cost, reportingID)
	if outCost == nil || inCost == nil {
		return nil
	}
	fifoResidual := new(big.Int).Sub(new(big.Int).Abs(outCost), new(big.Int).Abs(inCost))
	if fifoResidual.Sign() == 0 {
		return nil
	}
	nativeResidual := new(big.Int).Sub(outQty, inQty)
	if nativeResidual.Sign() == 0 {
		return nil
	}
	for i := range postings {
		p := &postings[i]
		account, ok := accountByID[p.AccountID]
		if !ok || !isTransactionCostAccount(account) {
			continue
		}
		units := new(big.Int).Set(fifoResidual)
		if p.Units.Minor.Sign() < 0 {
			units.Neg(units)
		}
		p.Units.Minor = units
		p.Units.CommodityID = reportingID
		price, err := domain.NormalizeRational(domain.Rational{
			Numerator:   new(big.Int).Set(nativeResidual),
			Denominator: new(big.Int).Abs(units),
		})
		if err != nil {
			return err
		}
		p.Price = &domain.TransactionPrice{PerUnit: price, CommodityID: out.Units.CommodityID}
		if err := domain.AttachReportingCost(p, fifoResidual, reportingID, costDate); err != nil {
			return err
		}
	}
	return nil
}

func isMonetaryAccount(account domain.Account) bool {
	return account.AccountType == domain.AccountAsset || account.AccountType == domain.AccountLiability
}

func isForeignCurrency(c domain.Commodity, reportingID string) bool {
	return c.Kind == domain.CommodityCurrency && c.ID != reportingID
}

func (s *Service) convertToReporting(
	ctx context.Context,
	minor *big.Int,
	fromID string,
	reporting domain.Commodity,
	commodities map[string]domain.Commodity,
	date string,
) (*big.Int, error) {
	if fromID == reporting.ID {
		return cloneBig(minor), nil
	}
	from, ok := commodities[fromID]
	if !ok {
		return nil, domain.InvalidCurrency("Unknown commodity: " + fromID)
	}
	fromObs, err := FetchEcbRatePerEurWithLookback(ctx, from.ID, date, 10, s.fetchEcbRate)
	if err != nil {
		return nil, err
	}
	toObs, err := FetchEcbRatePerEurWithLookback(ctx, reporting.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		return nil, err
	}
	rate, err := domain.CrossRateBPerA(fromObs.Rate, toObs.Rate)
	if err != nil {
		return nil, err
	}
	return domain.ConvertMinorAtRate(minor, rate, from.MinorUnits, reporting.MinorUnits)
}

func (s *Service) loadMonetaryLots(
	ctx context.Context,
	accountIDs []string,
	reportingID, effectiveAt, postedAt, createdAt string,
) (map[string][]domain.Lot, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.account_id, p.units_minor, p.commodity_id,
			p.cost_per_unit_numerator, p.cost_per_unit_denominator, p.cost_commodity_id, p.cost_date, p.cost_label
		FROM postings p
		JOIN journal_entries e ON e.id = p.journal_entry_id
		JOIN accounts a ON a.id = p.account_id
		WHERE e.status = 'posted'
		  AND p.account_id = ANY($1)
		  AND a.account_type IN ('asset', 'liability')
		  AND (
			e.effective_date < $2
			OR (e.effective_date = $2 AND e.posted_at < $3)
			OR (e.effective_date = $2 AND e.posted_at = $3 AND e.created_at < $4)
		  )
		ORDER BY e.effective_date, e.posted_at, e.created_at, p.line_order
	`, accountIDs, effectiveAt, postedAt, createdAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lots := map[string][]domain.Lot{}
	for rows.Next() {
		var accountID, commodityID string
		var unitsMinor int64
		var costNum, costDen, costComm, costDate, costLabel *string
		if err := rows.Scan(&accountID, &unitsMinor, &commodityID, &costNum, &costDen, &costComm, &costDate, &costLabel); err != nil {
			return nil, err
		}
		if commodityID == reportingID {
			continue
		}
		units := domain.Units{Minor: domain.Int(unitsMinor), CommodityID: commodityID}
		var cost *domain.CostBasis
		if r := parseRational(costNum, costDen); r != nil && costComm != nil {
			cost = &domain.CostBasis{PerUnit: *r, CommodityID: *costComm, Date: costDate, Label: costLabel}
		}
		lots[accountID] = applyLotMovement(lots[accountID], units.Minor, domain.CostReportingAmount(units, cost, reportingID))
	}
	return lots, rows.Err()
}

func (s *Service) ListPnLPostings(ctx context.Context) ([]domain.PnLPosting, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.code, a.account_type, p.units_minor, p.commodity_id,
			p.cost_per_unit_numerator, p.cost_per_unit_denominator, p.cost_commodity_id, p.cost_date, p.cost_label
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN journal_entries e ON e.id = p.journal_entry_id
		WHERE e.status = 'posted' AND a.account_type IN ('income', 'expense')
		ORDER BY e.effective_date, p.line_order
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PnLPosting
	for rows.Next() {
		var code, accountType, commodityID string
		var unitsMinor int64
		var costNum, costDen, costComm, costDate, costLabel *string
		if err := rows.Scan(&code, &accountType, &unitsMinor, &commodityID, &costNum, &costDen, &costComm, &costDate, &costLabel); err != nil {
			return nil, err
		}
		line := domain.PnLPosting{
			AccountCode: code,
			AccountType: domain.AccountType(accountType),
			Units:       domain.Units{Minor: domain.Int(unitsMinor), CommodityID: commodityID},
		}
		if r := parseRational(costNum, costDen); r != nil && costComm != nil {
			line.Cost = &domain.CostBasis{PerUnit: *r, CommodityID: *costComm, Date: costDate, Label: costLabel}
		}
		out = append(out, line)
	}
	if out == nil {
		out = []domain.PnLPosting{}
	}
	return out, rows.Err()
}

// restampFunctionalCosts converts stored old-F costs and seeds missing costs
// on old-F-native monetary/P&L postings, all at one change-date rate.
func (s *Service) restampFunctionalCosts(ctx context.Context, tx pgx.Tx, fromID, toID, changeDate string) (domain.Rational, error) {
	from, err := s.requireCommodity(ctx, fromID)
	if err != nil {
		return domain.Rational{}, err
	}
	to, err := s.requireCommodity(ctx, toID)
	if err != nil {
		return domain.Rational{}, err
	}
	fromObs, err := FetchEcbRatePerEurWithLookback(ctx, from.ID, changeDate, 10, s.fetchEcbRate)
	if err != nil {
		return domain.Rational{}, err
	}
	toObs, err := FetchEcbRatePerEurWithLookback(ctx, to.ID, fromObs.ObservedDate, 10, s.fetchEcbRate)
	if err != nil {
		return domain.Rational{}, err
	}
	rate, err := domain.CrossRateBPerA(fromObs.Rate, toObs.Rate)
	if err != nil {
		return domain.Rational{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT p.id, p.units_minor, p.commodity_id,
			p.cost_per_unit_numerator, p.cost_per_unit_denominator, p.cost_commodity_id,
			a.account_type
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		WHERE (
			p.cost_commodity_id = $1
			AND p.cost_per_unit_numerator IS NOT NULL
			AND p.cost_per_unit_denominator IS NOT NULL
		) OR (
			p.commodity_id = $1
			AND p.cost_per_unit_numerator IS NULL
			AND p.cost_per_unit_denominator IS NULL
			AND p.cost_commodity_id IS NULL
			AND a.account_type IN ('asset', 'liability', 'income', 'expense')
		)
	`, fromID)
	if err != nil {
		return domain.Rational{}, err
	}
	defer rows.Close()

	type restampRow struct {
		id         string
		units      *big.Int
		oldCost    *domain.CostBasis
		seedNative bool
	}
	var pending []restampRow
	for rows.Next() {
		var id, commodityID, accountType string
		var unitsMinor int64
		var costNum, costDen, costComm *string
		if err := rows.Scan(&id, &unitsMinor, &commodityID, &costNum, &costDen, &costComm, &accountType); err != nil {
			return domain.Rational{}, err
		}
		r := parseRational(costNum, costDen)
		if r != nil && costComm != nil {
			pending = append(pending, restampRow{
				id:      id,
				units:   domain.Int(unitsMinor),
				oldCost: &domain.CostBasis{PerUnit: *r, CommodityID: *costComm},
			})
			continue
		}
		if commodityID == fromID && (accountType == string(domain.AccountAsset) ||
			accountType == string(domain.AccountLiability) ||
			accountType == string(domain.AccountIncome) ||
			accountType == string(domain.AccountExpense)) {
			pending = append(pending, restampRow{
				id:         id,
				units:      domain.Int(unitsMinor),
				seedNative: true,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Rational{}, err
	}
	rows.Close()

	costDate := changeDate
	for _, row := range pending {
		oldAmount := row.units
		var err error
		if !row.seedNative {
			oldAmount, err = domain.MultiplyUnitsByRational(row.units, row.oldCost.PerUnit)
			if err != nil {
				return domain.Rational{}, err
			}
		}
		newAmount, err := domain.ConvertMinorAtRate(oldAmount, rate, from.MinorUnits, to.MinorUnits)
		if err != nil {
			return domain.Rational{}, err
		}
		if newAmount.Sign() == 0 && oldAmount.Sign() != 0 {
			if oldAmount.Sign() > 0 {
				newAmount = big.NewInt(1)
			} else {
				newAmount = big.NewInt(-1)
			}
		}
		cost, err := domain.CostFromReportingAmount(row.units, newAmount, toID, &costDate)
		if err != nil {
			return domain.Rational{}, err
		}
		if cost == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE postings SET
					cost_per_unit_numerator = NULL,
					cost_per_unit_denominator = NULL,
					cost_commodity_id = NULL,
					cost_date = NULL
				WHERE id = $1
			`, row.id); err != nil {
				return domain.Rational{}, err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE postings SET
				cost_per_unit_numerator = $2,
				cost_per_unit_denominator = $3,
				cost_commodity_id = $4,
				cost_date = $5
			WHERE id = $1
		`, row.id, cost.PerUnit.Numerator.String(), cost.PerUnit.Denominator.String(), toID, costDate); err != nil {
			return domain.Rational{}, err
		}
	}
	return rate, nil
}
