package mongo

import (
	"context"
	"errors"

	"github.com/FinancyOrg/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	balancesCacheID = "balances"
)

type balancesCacheDoc struct {
	ID    string       `bson:"_id"`
	Kind  string       `bson:"kind"`
	Items []balanceDoc `bson:"items"`
}

type balanceDoc struct {
	AccountID   string `bson:"accountId"`
	CommodityID string `bson:"commodityId"`
	Minor       string `bson:"minor"`
}

func encodeBalances(items []domain.AccountBalance) balancesCacheDoc {
	docs := make([]balanceDoc, 0, len(items))
	for _, b := range items {
		docs = append(docs, balanceDoc{
			AccountID: b.AccountID, CommodityID: b.CommodityID, Minor: minorString(b.Minor),
		})
	}
	return balancesCacheDoc{ID: balancesCacheID, Kind: "balances", Items: docs}
}

func decodeBalances(doc balancesCacheDoc) ([]domain.AccountBalance, error) {
	out := make([]domain.AccountBalance, 0, len(doc.Items))
	for _, d := range doc.Items {
		minor, err := parseMinor(d.Minor)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.AccountBalance{
			AccountID: d.AccountID, CommodityID: d.CommodityID, Minor: minor,
		})
	}
	return out, nil
}

// GetBalances returns the cached non-zero account balances snapshot.
func (c *Client) GetBalances(ctx context.Context) ([]domain.AccountBalance, bool, error) {
	coll := c.cacheColl()
	if coll == nil {
		return nil, false, nil
	}
	var doc balancesCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": balancesCacheID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	items, err := decodeBalances(doc)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

// PutBalances upserts the balances snapshot.
func (c *Client) PutBalances(ctx context.Context, items []domain.AccountBalance) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	if items == nil {
		items = []domain.AccountBalance{}
	}
	doc := encodeBalances(items)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}
