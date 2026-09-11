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
	accountsCacheID    = "accounts"
	commoditiesCacheID = "commodities"
)

type accountsCacheDoc struct {
	ID    string       `bson:"_id"`
	Kind  string       `bson:"kind"`
	Items []accountDoc `bson:"items"`
}

type accountDoc struct {
	ID                string  `bson:"id"`
	Code              string  `bson:"code"`
	Name              string  `bson:"name"`
	AccountType       string  `bson:"accountType"`
	ParentID          *string `bson:"parentId,omitempty"`
	NativeCommodityID string  `bson:"nativeCommodityId"`
	CreatedAt         string  `bson:"createdAt"`
}

type commoditiesCacheDoc struct {
	ID    string         `bson:"_id"`
	Kind  string         `bson:"kind"`
	Items []commodityDoc `bson:"items"`
}

type commodityDoc struct {
	ID         string `bson:"id"`
	Code       string `bson:"code"`
	Name       string `bson:"name"`
	MinorUnits int    `bson:"minorUnits"`
	Kind       string `bson:"commodityKind"`
}

func encodeAccounts(items []domain.Account) accountsCacheDoc {
	docs := make([]accountDoc, 0, len(items))
	for _, a := range items {
		docs = append(docs, accountDoc{
			ID: a.ID, Code: a.Code, Name: a.Name, AccountType: string(a.AccountType),
			ParentID: a.ParentID, NativeCommodityID: a.NativeCommodityID,
			CreatedAt: a.CreatedAt,
		})
	}
	return accountsCacheDoc{ID: accountsCacheID, Kind: "accounts", Items: docs}
}

func decodeAccounts(doc accountsCacheDoc) []domain.Account {
	out := make([]domain.Account, 0, len(doc.Items))
	for _, d := range doc.Items {
		out = append(out, domain.Account{
			ID: d.ID, Code: d.Code, Name: d.Name, AccountType: domain.AccountType(d.AccountType),
			ParentID: d.ParentID, NativeCommodityID: d.NativeCommodityID,
			CreatedAt: d.CreatedAt,
		})
	}
	return out
}

func encodeCommodities(items []domain.Commodity) commoditiesCacheDoc {
	docs := make([]commodityDoc, 0, len(items))
	for _, c := range items {
		docs = append(docs, commodityDoc{
			ID: c.ID, Code: c.Code, Name: c.Name, MinorUnits: c.MinorUnits, Kind: string(c.Kind),
		})
	}
	return commoditiesCacheDoc{ID: commoditiesCacheID, Kind: "commodities", Items: docs}
}

func decodeCommodities(doc commoditiesCacheDoc) []domain.Commodity {
	out := make([]domain.Commodity, 0, len(doc.Items))
	for _, d := range doc.Items {
		out = append(out, domain.Commodity{
			ID: d.ID, Code: d.Code, Name: d.Name, MinorUnits: d.MinorUnits,
			Kind: domain.CommodityKind(d.Kind),
		})
	}
	return out
}

// GetAccounts returns the cached chart of accounts snapshot.
func (c *Client) GetAccounts(ctx context.Context) ([]domain.Account, bool, error) {
	coll := c.cacheColl()
	if coll == nil {
		return nil, false, nil
	}
	var doc accountsCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": accountsCacheID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeAccounts(doc), true, nil
}

// PutAccounts upserts the accounts snapshot.
func (c *Client) PutAccounts(ctx context.Context, items []domain.Account) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	if items == nil {
		items = []domain.Account{}
	}
	doc := encodeAccounts(items)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

// GetCommodities returns the cached commodities snapshot.
func (c *Client) GetCommodities(ctx context.Context) ([]domain.Commodity, bool, error) {
	coll := c.cacheColl()
	if coll == nil {
		return nil, false, nil
	}
	var doc commoditiesCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": commoditiesCacheID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeCommodities(doc), true, nil
}

// PutCommodities upserts the commodities snapshot.
func (c *Client) PutCommodities(ctx context.Context, items []domain.Commodity) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	if items == nil {
		items = []domain.Commodity{}
	}
	doc := encodeCommodities(items)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}
