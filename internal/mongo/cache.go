package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Invalidate deletes cache docs whose kind is in kinds. Empty kinds is a no-op.
// ECB docs are never matched unless "ecb" is passed explicitly (callers must not).
func (c *Client) Invalidate(ctx context.Context, kinds ...string) error {
	coll := c.cacheColl()
	if coll == nil || len(kinds) == 0 {
		return nil
	}
	_, err := coll.DeleteMany(ctx, bson.M{
		"kind": bson.M{"$in": kinds},
	})
	return err
}

// ClearLedger deletes CRDB-backed snapshot docs in the cache collection.
// ECB rate docs are left in place.
func (c *Client) ClearLedger(ctx context.Context) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	_, err := coll.DeleteMany(ctx, bson.M{
		"kind": bson.M{"$ne": "ecb"},
	})
	return err
}
