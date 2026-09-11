package mongo

import (
	"context"
	"errors"
	"log"
	"math/big"
	"strings"

	"github.com/FinancyOrg/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type ecbCacheDoc struct {
	ID          string `bson:"_id"`
	Kind        string `bson:"kind"`
	Currency    string `bson:"currency"`
	Date        string `bson:"date"`
	Numerator   string `bson:"numerator,omitempty"`
	Denominator string `bson:"denominator,omitempty"`
	Missing     bool   `bson:"missing,omitempty"`
}

type ecbStore interface {
	GetEcbRate(ctx context.Context, currency, date string) (rate domain.Rational, missing, ok bool, err error)
	PutEcbRate(ctx context.Context, currency, date string, rate domain.Rational, missing bool) error
}

// WrapEcbRateFetcher returns inner unchanged when c is nil. Otherwise each
// currency+UTC-date pair is served from the cache collection after the first
// ECB HTTP response (including past-day misses, which never change).
func (c *Client) WrapEcbRateFetcher(inner func(context.Context, string, string) (domain.Rational, error)) func(context.Context, string, string) (domain.Rational, error) {
	if c == nil {
		return inner
	}
	return wrapEcbFetch(c, inner, func() string { return domain.UTCDate(domain.NowUTC()) })
}

func wrapEcbFetch(
	cache ecbStore,
	inner func(context.Context, string, string) (domain.Rational, error),
	today func() string,
) func(context.Context, string, string) (domain.Rational, error) {
	return func(ctx context.Context, currency, date string) (domain.Rational, error) {
		if date > today() {
			return domain.Rational{}, domain.InvalidEcbDate(date)
		}
		if strings.EqualFold(strings.TrimSpace(currency), "EUR") {
			return inner(ctx, currency, date)
		}
		rate, missing, ok, err := cache.GetEcbRate(ctx, currency, date)
		if err != nil {
			log.Printf("ecb cache get: %v", err)
		} else if ok {
			if missing {
				return domain.Rational{}, domain.MissingEcbRate(currency, date)
			}
			return rate, nil
		}
		rate, err = inner(ctx, currency, date)
		if err == nil {
			if putErr := cache.PutEcbRate(ctx, currency, date, rate, false); putErr != nil {
				log.Printf("ecb cache put: %v", putErr)
			}
			return rate, nil
		}
		if de, isDom := domain.IsDomainError(err); isDom && de.Code == "MissingEcbRate" && date < today() {
			if putErr := cache.PutEcbRate(ctx, currency, date, domain.Rational{}, true); putErr != nil {
				log.Printf("ecb cache put: %v", putErr)
			}
		}
		return rate, err
	}
}

func ecbCacheID(currency, date string) string {
	return "ecb:" + strings.ToUpper(strings.TrimSpace(currency)) + ":" + date
}

func (c *Client) GetEcbRate(ctx context.Context, currency, date string) (domain.Rational, bool, bool, error) {
	coll := c.cacheColl()
	if coll == nil {
		return domain.Rational{}, false, false, nil
	}
	var doc ecbCacheDoc
	err := coll.FindOne(ctx, bson.M{"_id": ecbCacheID(currency, date)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return domain.Rational{}, false, false, nil
	}
	if err != nil {
		return domain.Rational{}, false, false, err
	}
	if doc.Missing {
		return domain.Rational{}, true, true, nil
	}
	rate, err := parseCachedRational(doc.Numerator, doc.Denominator)
	if err != nil {
		return domain.Rational{}, false, false, err
	}
	return rate, false, true, nil
}

func (c *Client) PutEcbRate(ctx context.Context, currency, date string, rate domain.Rational, missing bool) error {
	coll := c.cacheColl()
	if coll == nil {
		return nil
	}
	code := strings.ToUpper(strings.TrimSpace(currency))
	doc := ecbCacheDoc{
		ID:       ecbCacheID(code, date),
		Kind:     "ecb",
		Currency: code,
		Date:     date,
		Missing:  missing,
	}
	if !missing {
		if rate.Numerator != nil {
			doc.Numerator = rate.Numerator.String()
		}
		if rate.Denominator != nil {
			doc.Denominator = rate.Denominator.String()
		}
	}
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

func parseCachedRational(num, den string) (domain.Rational, error) {
	n, ok1 := new(big.Int).SetString(num, 10)
	d, ok2 := new(big.Int).SetString(den, 10)
	if !ok1 || !ok2 {
		return domain.Rational{}, errors.New("mongo: invalid cached ECB rate")
	}
	return domain.RationalFromPair(n, d)
}
