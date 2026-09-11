package mongo

import (
	"context"

	"github.com/FinancyOrg/backend/internal/ledger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const flagsCollection = "account_flags"

type accountFlagsDoc struct {
	ID     string `bson:"_id"`
	Hidden bool   `bson:"hidden"`
	Liquid bool   `bson:"liquid"`
}

func (c *Client) flagsColl() *mongo.Collection {
	if c == nil || c.mongo == nil {
		return nil
	}
	return c.mongo.Database(c.db).Collection(flagsCollection)
}

// ListAccountFlags returns every stored hidden/liquid pair. Missing accounts
// are omitted so callers treat them as both-false.
func (c *Client) ListAccountFlags(ctx context.Context) (map[string]ledger.AccountFlags, error) {
	coll := c.flagsColl()
	if coll == nil {
		return map[string]ledger.AccountFlags{}, nil
	}
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := map[string]ledger.AccountFlags{}
	for cur.Next(ctx) {
		var doc accountFlagsDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out[doc.ID] = ledger.AccountFlags{Hidden: doc.Hidden, Liquid: doc.Liquid}
	}
	return out, cur.Err()
}

// PutAccountFlags upserts hidden/liquid for one account. This collection is
// source-of-truth metadata and is not cleared by cache invalidation.
func (c *Client) PutAccountFlags(ctx context.Context, accountID string, flags ledger.AccountFlags) error {
	coll := c.flagsColl()
	if coll == nil {
		return nil
	}
	doc := accountFlagsDoc{ID: accountID, Hidden: flags.Hidden, Liquid: flags.Liquid}
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": accountID}, doc, options.Replace().SetUpsert(true))
	return err
}

// DeleteAccountFlags removes the sidecar flags for a deleted account.
func (c *Client) DeleteAccountFlags(ctx context.Context, accountID string) error {
	coll := c.flagsColl()
	if coll == nil {
		return nil
	}
	_, err := coll.DeleteOne(ctx, bson.M{"_id": accountID})
	return err
}
