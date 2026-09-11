package mongo

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// storeCollection holds Mongo-authoritative app settings.
// It is not the cache collection and is not wiped by ClearLedger.
const storeCollection = "store"

// storeConfigID is the single document: store.config.
const storeConfigID = "config"

const PresentationCommoditySettingKey = "presentationCommodityId"

type storeConfigDoc struct {
	ID                      string `bson:"_id"`
	PresentationCommodityID string `bson:"presentationCommodityId,omitempty"`
	Theme                   string `bson:"theme,omitempty"`
	Timezone                string `bson:"timezone,omitempty"`
	FunctionalChangeJob     string `bson:"functionalChangeJob,omitempty"`
	LedgerRevision          int64  `bson:"ledgerRevision,omitempty"`
}

func (c *Client) storeColl() *mongo.Collection {
	if c == nil || c.mongo == nil {
		return nil
	}
	return c.mongo.Database(c.db).Collection(storeCollection)
}

func (c *Client) getStoreConfig(ctx context.Context) (*storeConfigDoc, error) {
	coll := c.storeColl()
	if coll == nil {
		return nil, nil
	}
	var doc storeConfigDoc
	err := coll.FindOne(ctx, bson.M{"_id": storeConfigID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// GetSetting returns a field on store.config. Missing keys return (nil, nil).
func (c *Client) GetSetting(ctx context.Context, key string) (*string, error) {
	doc, err := c.getStoreConfig(ctx)
	if err != nil || doc == nil {
		return nil, err
	}
	var value string
	switch key {
	case PresentationCommoditySettingKey:
		value = doc.PresentationCommodityID
	case "theme":
		value = doc.Theme
	case "timezone":
		value = doc.Timezone
	case "functionalChangeJob":
		value = doc.FunctionalChangeJob
	default:
		return nil, nil
	}
	if value == "" {
		return nil, nil
	}
	return &value, nil
}

// GetLedgerRevision returns store.config.ledgerRevision (0 when unset).
func (c *Client) GetLedgerRevision(ctx context.Context) (int64, error) {
	doc, err := c.getStoreConfig(ctx)
	if err != nil || doc == nil {
		return 0, err
	}
	return doc.LedgerRevision, nil
}

// PutSetting upserts one field on store.config. Survives ClearLedger (cache wipe).
func (c *Client) PutSetting(ctx context.Context, key, value string) error {
	coll := c.storeColl()
	if coll == nil {
		return nil
	}
	switch key {
	case PresentationCommoditySettingKey, "theme", "timezone", "functionalChangeJob":
	default:
		return nil
	}
	_, err := coll.UpdateOne(
		ctx,
		bson.M{"_id": storeConfigID},
		bson.M{"$set": bson.M{key: value}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}
