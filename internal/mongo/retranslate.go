package mongo

import (
	"context"
	"errors"
	"fmt"

	"github.com/FinancyOrg/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func retranslateCacheID(presentationCommodityID, asOfDate string) string {
	return fmt.Sprintf("retranslate:%s:%s", presentationCommodityID, asOfDate)
}

type retranslateCacheDoc struct {
	ID                   string `bson:"_id"`
	Kind                 string `bson:"kind"`
	AsOfDate             string `bson:"asOfDate"`
	ReportingCommodityID string `bson:"reportingCommodityId"`
	RetranslationMinor   string `bson:"retranslationMinor"`
	NetWorthMinor        string `bson:"netWorthMinor"`
	IncomeExpenseMinor   string `bson:"incomeExpenseMinor"`
	AsOf                 string `bson:"asOf"`
}

func encodeRetranslation(p domain.RetranslationPreview) retranslateCacheDoc {
	asOfDate := domain.UTCDate(p.AsOf)
	return retranslateCacheDoc{
		ID:                   retranslateCacheID(p.ReportingCommodityID, asOfDate),
		Kind:                 "retranslate",
		AsOfDate:             asOfDate,
		ReportingCommodityID: p.ReportingCommodityID,
		RetranslationMinor:   minorString(p.RetranslationMinor),
		NetWorthMinor:        minorString(p.NetWorthMinor),
		IncomeExpenseMinor:   minorString(p.IncomeExpenseMinor),
		AsOf:                 p.AsOf,
	}
}

func decodeRetranslation(doc retranslateCacheDoc) (domain.RetranslationPreview, error) {
	retranslation, err := parseMinor(doc.RetranslationMinor)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	netWorth, err := parseMinor(doc.NetWorthMinor)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	incomeExpense, err := parseMinor(doc.IncomeExpenseMinor)
	if err != nil {
		return domain.RetranslationPreview{}, err
	}
	return domain.RetranslationPreview{
		ReportingCommodityID: doc.ReportingCommodityID,
		RetranslationMinor:   retranslation,
		NetWorthMinor:        netWorth,
		IncomeExpenseMinor:   incomeExpense,
		AsOf:                 doc.AsOf,
	}, nil
}

// GetRetranslation returns the cached retranslation preview for presentation P on asOfDate.
func (c *Client) GetRetranslation(ctx context.Context, presentationCommodityID, asOfDate string) (domain.RetranslationPreview, bool, error) {
	coll := c.cacheColl()
	if coll == nil || presentationCommodityID == "" || asOfDate == "" {
		return domain.RetranslationPreview{}, false, nil
	}
	var doc retranslateCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": retranslateCacheID(presentationCommodityID, asOfDate)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.RetranslationPreview{}, false, nil
	}
	if err != nil {
		return domain.RetranslationPreview{}, false, err
	}
	if doc.ReportingCommodityID != presentationCommodityID || doc.AsOfDate != asOfDate {
		return domain.RetranslationPreview{}, false, nil
	}
	preview, err := decodeRetranslation(doc)
	if err != nil {
		return domain.RetranslationPreview{}, false, err
	}
	return preview, true, nil
}

// PutRetranslation upserts the retranslation preview snapshot.
func (c *Client) PutRetranslation(ctx context.Context, preview domain.RetranslationPreview) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	doc := encodeRetranslation(preview)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}
