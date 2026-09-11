package mongo

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/FinancyOrg/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func netWorthCacheID(presentationCommodityID, asOfDate string) string {
	return fmt.Sprintf("networth:%s:%s", presentationCommodityID, asOfDate)
}

type netWorthCacheDoc struct {
	ID                    string             `bson:"_id"`
	Kind                  string             `bson:"kind"`
	AsOfDate              string             `bson:"asOfDate"`
	ReportingCommodityID  string             `bson:"reportingCommodityId"`
	AsOf                  string             `bson:"asOf"`
	Assets                []valuedBalanceDoc `bson:"assets"`
	Liabilities           []valuedBalanceDoc `bson:"liabilities"`
	TotalAssetsMinor      string             `bson:"totalAssetsMinor"`
	TotalLiabilitiesMinor string             `bson:"totalLiabilitiesMinor"`
	NetWorthMinor         string             `bson:"netWorthMinor"`
}

type valuedBalanceDoc struct {
	AccountID            string `bson:"accountId"`
	AccountType          string `bson:"accountType"`
	ReportingMinor       string `bson:"reportingMinor"`
	ReportingCommodityID string `bson:"reportingCommodityId"`
	NativeAccountID      string `bson:"nativeAccountId"`
	NativeCommodityID    string `bson:"nativeCommodityId"`
	NativeMinor          string `bson:"nativeMinor"`
}

func encodeNetWorth(r domain.NetWorthReport) netWorthCacheDoc {
	asOfDate := domain.UTCDate(r.AsOf)
	return netWorthCacheDoc{
		ID:                    netWorthCacheID(r.ReportingCommodityID, asOfDate),
		Kind:                  "networth",
		AsOfDate:              asOfDate,
		ReportingCommodityID:  r.ReportingCommodityID,
		AsOf:                  r.AsOf,
		Assets:                encodeValued(r.Assets),
		Liabilities:           encodeValued(r.Liabilities),
		TotalAssetsMinor:      minorString(r.TotalAssetsMinor),
		TotalLiabilitiesMinor: minorString(r.TotalLiabilitiesMinor),
		NetWorthMinor:         minorString(r.NetWorthMinor),
	}
}

func decodeNetWorth(doc netWorthCacheDoc) (domain.NetWorthReport, error) {
	assets, err := decodeValued(doc.Assets)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	liabilities, err := decodeValued(doc.Liabilities)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	totalAssets, err := parseMinor(doc.TotalAssetsMinor)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	totalLiabilities, err := parseMinor(doc.TotalLiabilitiesMinor)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	netWorth, err := parseMinor(doc.NetWorthMinor)
	if err != nil {
		return domain.NetWorthReport{}, err
	}
	return domain.NetWorthReport{
		ReportingCommodityID:  doc.ReportingCommodityID,
		AsOf:                  doc.AsOf,
		Assets:                assets,
		Liabilities:           liabilities,
		TotalAssetsMinor:      totalAssets,
		TotalLiabilitiesMinor: totalLiabilities,
		NetWorthMinor:         netWorth,
	}, nil
}

func encodeValued(list []domain.ValuedBalance) []valuedBalanceDoc {
	if list == nil {
		return []valuedBalanceDoc{}
	}
	out := make([]valuedBalanceDoc, len(list))
	for i, v := range list {
		out[i] = valuedBalanceDoc{
			AccountID:            v.AccountID,
			AccountType:          string(v.AccountType),
			ReportingMinor:       minorString(v.ReportingMinor),
			ReportingCommodityID: v.ReportingCommodityID,
			NativeAccountID:      v.Native.AccountID,
			NativeCommodityID:    v.Native.CommodityID,
			NativeMinor:          minorString(v.Native.Minor),
		}
	}
	return out
}

func decodeValued(list []valuedBalanceDoc) ([]domain.ValuedBalance, error) {
	out := make([]domain.ValuedBalance, 0, len(list))
	for _, d := range list {
		reporting, err := parseMinor(d.ReportingMinor)
		if err != nil {
			return nil, err
		}
		native, err := parseMinor(d.NativeMinor)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.ValuedBalance{
			AccountID:            d.AccountID,
			AccountType:          domain.AccountType(d.AccountType),
			ReportingMinor:       reporting,
			ReportingCommodityID: d.ReportingCommodityID,
			Native: domain.AccountBalance{
				AccountID:   d.NativeAccountID,
				CommodityID: d.NativeCommodityID,
				Minor:       native,
			},
		})
	}
	return out, nil
}

func minorString(n *big.Int) string {
	if n == nil {
		return "0"
	}
	return n.String()
}

func parseMinor(s string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, errors.New("mongo: invalid cached minor amount")
	}
	return n, nil
}

// GetNetWorth returns the cached net-worth snapshot for presentation P on asOfDate.
// hit is false on a miss or when Mongo is not configured.
func (c *Client) GetNetWorth(ctx context.Context, presentationCommodityID, asOfDate string) (domain.NetWorthReport, bool, error) {
	coll := c.cacheColl()
	if coll == nil || presentationCommodityID == "" || asOfDate == "" {
		return domain.NetWorthReport{}, false, nil
	}
	var doc netWorthCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": netWorthCacheID(presentationCommodityID, asOfDate)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.NetWorthReport{}, false, nil
	}
	if err != nil {
		return domain.NetWorthReport{}, false, err
	}
	if doc.ReportingCommodityID != presentationCommodityID || doc.AsOfDate != asOfDate {
		return domain.NetWorthReport{}, false, nil
	}
	report, err := decodeNetWorth(doc)
	if err != nil {
		return domain.NetWorthReport{}, false, err
	}
	return report, true, nil
}

// PutNetWorth upserts the derived net-worth snapshot.
func (c *Client) PutNetWorth(ctx context.Context, report domain.NetWorthReport) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	doc := encodeNetWorth(report)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}
