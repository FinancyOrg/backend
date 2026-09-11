package mongo

import (
	"context"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const cacheCollection = "cache"

// Client is a MongoDB-protocol connection to the Financy cache and settings store.
type Client struct {
	mongo *mongo.Client
	db    string
}

// Connect opens a client for uri. An empty uri returns (nil, nil) so tests
// and a backend started without MONGO_URI skip the cache.
func Connect(ctx context.Context, uri string) (*Client, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, nil
	}
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo: %w", err)
	}
	c := &Client{mongo: mongoClient, db: databaseFromURI(uri)}
	if err := c.Ping(ctx); err != nil {
		_ = c.Close(ctx)
		return nil, err
	}
	return c, nil
}

func databaseFromURI(uri string) string {
	s := strings.TrimSpace(uri)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	slash := strings.Index(s, "/")
	if slash < 0 {
		return ""
	}
	s = s[slash+1:]
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return strings.Trim(s, "/")
}

func (c *Client) cacheColl() *mongo.Collection {
	if c == nil || c.mongo == nil {
		return nil
	}
	return c.mongo.Database(c.db).Collection(cacheCollection)
}

// Ping confirms the MongoDB-compatible endpoint is reachable and authorized.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.mongo == nil {
		return fmt.Errorf("mongo: not configured")
	}
	if err := c.mongo.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongo: ping: %w", err)
	}
	return nil
}

// Close releases the underlying driver client.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.mongo == nil {
		return nil
	}
	return c.mongo.Disconnect(ctx)
}
