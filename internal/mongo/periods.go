package mongo

import (
	"context"
	"errors"
	"fmt"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/FinancyOrg/backend/internal/ledger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func periodsCacheID(presentationCommodityID, timezone string, ledgerRevision int64) string {
	return fmt.Sprintf("periods:%s:%s:%d", presentationCommodityID, timezone, ledgerRevision)
}

type periodsCacheDoc struct {
	ID             string          `bson:"_id"`
	Kind           string          `bson:"kind"`
	PresentationID string          `bson:"presentationCommodityId"`
	Timezone       string          `bson:"timezone"`
	LedgerRevision int64           `bson:"ledgerRevision,omitempty"`
	Currency       commodityDoc    `bson:"currency"`
	Months         []viewPeriodDoc `bson:"months"`
	Years          []viewPeriodDoc `bson:"years"`
}

type viewPeriodDoc struct {
	Key               string  `bson:"key"`
	Label             string  `bson:"label"`
	StartDate         string  `bson:"startDate"`
	EndDate           string  `bson:"endDate"`
	CurrencyID        string  `bson:"currencyId"`
	IncomeMinor       string  `bson:"incomeMinor"`
	ExpenseMinor      string  `bson:"expenseMinor"`
	CapitalGainsMinor string  `bson:"capitalGainsMinor"`
	EffectMinor       string  `bson:"effectMinor"`
	NetSavingsMinor   string  `bson:"netSavingsMinor"`
	NetWorthMinor     string  `bson:"netWorthMinor"`
	Factor            float64 `bson:"factor"`
	Good              bool    `bson:"good"`
}

func encodePeriods(r ledger.ViewPeriodsResult, ledgerRevision int64) periodsCacheDoc {
	return periodsCacheDoc{
		ID:             periodsCacheID(r.Currency.ID, r.Timezone, ledgerRevision),
		Kind:           "periods",
		PresentationID: r.Currency.ID,
		Timezone:       r.Timezone,
		LedgerRevision: ledgerRevision,
		Currency: commodityDoc{
			ID: r.Currency.ID, Code: r.Currency.Code, Name: r.Currency.Name,
			MinorUnits: r.Currency.MinorUnits, Kind: string(r.Currency.Kind),
		},
		Months: encodeViewPeriods(r.Months),
		Years:  encodeViewPeriods(r.Years),
	}
}

func decodePeriods(doc periodsCacheDoc) (ledger.ViewPeriodsResult, error) {
	months, err := decodeViewPeriods(doc.Months)
	if err != nil {
		return ledger.ViewPeriodsResult{}, err
	}
	years, err := decodeViewPeriods(doc.Years)
	if err != nil {
		return ledger.ViewPeriodsResult{}, err
	}
	return ledger.ViewPeriodsResult{
		Currency: domain.Commodity{
			ID: doc.Currency.ID, Code: doc.Currency.Code, Name: doc.Currency.Name,
			MinorUnits: doc.Currency.MinorUnits, Kind: domain.CommodityKind(doc.Currency.Kind),
		},
		Months:   months,
		Years:    years,
		Timezone: doc.Timezone,
	}, nil
}

func encodeViewPeriods(list []ledger.ViewPeriod) []viewPeriodDoc {
	if list == nil {
		return []viewPeriodDoc{}
	}
	out := make([]viewPeriodDoc, len(list))
	for i, p := range list {
		out[i] = viewPeriodDoc{
			Key:               p.Key,
			Label:             p.Label,
			StartDate:         p.StartDate,
			EndDate:           p.EndDate,
			CurrencyID:        p.CurrencyID,
			IncomeMinor:       minorString(p.IncomeMinor),
			ExpenseMinor:      minorString(p.ExpenseMinor),
			CapitalGainsMinor: minorString(p.CapitalGainsMinor),
			EffectMinor:       minorString(p.EffectMinor),
			NetSavingsMinor:   minorString(p.NetSavingsMinor),
			NetWorthMinor:     minorString(p.NetWorthMinor),
			Factor:            p.Factor,
			Good:              p.Good,
		}
	}
	return out
}

func decodeViewPeriods(list []viewPeriodDoc) ([]ledger.ViewPeriod, error) {
	out := make([]ledger.ViewPeriod, 0, len(list))
	for _, d := range list {
		income, err := parseMinor(d.IncomeMinor)
		if err != nil {
			return nil, err
		}
		expense, err := parseMinor(d.ExpenseMinor)
		if err != nil {
			return nil, err
		}
		capitalGains, err := parseMinor(d.CapitalGainsMinor)
		if err != nil {
			return nil, err
		}
		effect, err := parseMinor(d.EffectMinor)
		if err != nil {
			return nil, err
		}
		netSavings, err := parseMinor(d.NetSavingsMinor)
		if err != nil {
			return nil, err
		}
		netWorth, err := parseMinor(d.NetWorthMinor)
		if err != nil {
			return nil, err
		}
		out = append(out, ledger.ViewPeriod{
			Key:               d.Key,
			Label:             d.Label,
			StartDate:         d.StartDate,
			EndDate:           d.EndDate,
			CurrencyID:        d.CurrencyID,
			IncomeMinor:       income,
			ExpenseMinor:      expense,
			CapitalGainsMinor: capitalGains,
			EffectMinor:       effect,
			NetSavingsMinor:   netSavings,
			NetWorthMinor:     netWorth,
			Factor:            d.Factor,
			Good:              d.Good,
		})
	}
	return out, nil
}

// GetPeriods returns the cached months/years rollup for presentation P.
// hit is false on a miss or when Mongo is not configured.
func (c *Client) GetPeriods(ctx context.Context, presentationCommodityID, timezone string, ledgerRevision int64) (ledger.ViewPeriodsResult, bool, error) {
	coll := c.cacheColl()
	if coll == nil || presentationCommodityID == "" || timezone == "" {
		return ledger.ViewPeriodsResult{}, false, nil
	}
	var doc periodsCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": periodsCacheID(presentationCommodityID, timezone, ledgerRevision)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ledger.ViewPeriodsResult{}, false, nil
	}
	if err != nil {
		return ledger.ViewPeriodsResult{}, false, err
	}
	if doc.PresentationID != presentationCommodityID || doc.Timezone != timezone || doc.LedgerRevision != ledgerRevision {
		return ledger.ViewPeriodsResult{}, false, nil
	}
	result, err := decodePeriods(doc)
	if err != nil {
		return ledger.ViewPeriodsResult{}, false, err
	}
	return result, true, nil
}

// PutPeriods upserts the months/years rollup snapshot.
func (c *Client) PutPeriods(ctx context.Context, result ledger.ViewPeriodsResult, ledgerRevision int64) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	doc := encodePeriods(result, ledgerRevision)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}
