package mongo

import (
	"context"
	"errors"
	"math/big"

	"github.com/FinancyOrg/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const journalCacheKind = "journal"

func journalCacheID(journalEntryID string) string {
	return "journal:" + journalEntryID
}

type postingsCacheDoc struct {
	ID             string               `bson:"_id"`
	Kind           string               `bson:"kind"`
	JournalEntryID string               `bson:"journalEntryId"`
	Postings       []resolvedPostingDoc `bson:"postings"`
}

type resolvedPostingDoc struct {
	AccountID         string  `bson:"accountId"`
	UnitsMinor        string  `bson:"unitsMinor"`
	UnitsCommodityID  string  `bson:"unitsCommodityId"`
	CostNum           *string `bson:"costNum,omitempty"`
	CostDen           *string `bson:"costDen,omitempty"`
	CostCommodityID   *string `bson:"costCommodityId,omitempty"`
	CostDate          *string `bson:"costDate,omitempty"`
	CostLabel         *string `bson:"costLabel,omitempty"`
	PriceNum          *string `bson:"priceNum,omitempty"`
	PriceDen          *string `bson:"priceDen,omitempty"`
	PriceCommodityID  *string `bson:"priceCommodityId,omitempty"`
	WeightMinor       string  `bson:"weightMinor"`
	WeightCommodityID string  `bson:"weightCommodityId"`
	Memo              *string `bson:"memo,omitempty"`
	LineOrder         int     `bson:"lineOrder"`
}

func encodePostings(journalID string, postings []domain.ResolvedPosting) postingsCacheDoc {
	docs := make([]resolvedPostingDoc, 0, len(postings))
	for _, p := range postings {
		d := resolvedPostingDoc{
			AccountID:         p.AccountID,
			UnitsMinor:        minorString(p.Units.Minor),
			UnitsCommodityID:  p.Units.CommodityID,
			WeightMinor:       minorString(p.Weight.Minor),
			WeightCommodityID: p.Weight.CommodityID,
			Memo:              p.Memo,
			LineOrder:         p.LineOrder,
		}
		if p.Cost != nil {
			num := p.Cost.PerUnit.Numerator.String()
			den := p.Cost.PerUnit.Denominator.String()
			comm := p.Cost.CommodityID
			d.CostNum = &num
			d.CostDen = &den
			d.CostCommodityID = &comm
			d.CostDate = p.Cost.Date
			d.CostLabel = p.Cost.Label
		}
		if p.Price != nil {
			num := p.Price.PerUnit.Numerator.String()
			den := p.Price.PerUnit.Denominator.String()
			comm := p.Price.CommodityID
			d.PriceNum = &num
			d.PriceDen = &den
			d.PriceCommodityID = &comm
		}
		docs = append(docs, d)
	}
	if docs == nil {
		docs = []resolvedPostingDoc{}
	}
	return postingsCacheDoc{
		ID:             journalCacheID(journalID),
		Kind:           journalCacheKind,
		JournalEntryID: journalID,
		Postings:       docs,
	}
}

func decodePostings(doc postingsCacheDoc) ([]domain.ResolvedPosting, error) {
	out := make([]domain.ResolvedPosting, 0, len(doc.Postings))
	for _, d := range doc.Postings {
		units, err := parseMinor(d.UnitsMinor)
		if err != nil {
			return nil, err
		}
		weight, err := parseMinor(d.WeightMinor)
		if err != nil {
			return nil, err
		}
		p := domain.ResolvedPosting{
			PostingInput: domain.PostingInput{
				AccountID: d.AccountID,
				Units:     domain.Units{Minor: units, CommodityID: d.UnitsCommodityID},
				Memo:      d.Memo,
			},
			LineOrder: d.LineOrder,
			Weight:    domain.Weight{Minor: weight, CommodityID: d.WeightCommodityID},
		}
		if d.CostNum != nil && d.CostDen != nil && d.CostCommodityID != nil {
			r, err := parseRationalStrings(*d.CostNum, *d.CostDen)
			if err != nil {
				return nil, err
			}
			p.Cost = &domain.CostBasis{
				PerUnit: r, CommodityID: *d.CostCommodityID,
				Date: d.CostDate, Label: d.CostLabel,
			}
		}
		if d.PriceNum != nil && d.PriceDen != nil && d.PriceCommodityID != nil {
			r, err := parseRationalStrings(*d.PriceNum, *d.PriceDen)
			if err != nil {
				return nil, err
			}
			p.Price = &domain.TransactionPrice{PerUnit: r, CommodityID: *d.PriceCommodityID}
		}
		out = append(out, p)
	}
	return out, nil
}

func parseRationalStrings(num, den string) (domain.Rational, error) {
	n, ok := new(big.Int).SetString(num, 10)
	if !ok {
		return domain.Rational{}, errors.New("mongo: invalid rational numerator")
	}
	d, ok := new(big.Int).SetString(den, 10)
	if !ok {
		return domain.Rational{}, errors.New("mongo: invalid rational denominator")
	}
	return domain.Rational{Numerator: n, Denominator: d}, nil
}

// GetPostings returns cached postings for the given journal entry IDs.
// Missing IDs are omitted from the result map (not an error).
func (c *Client) GetPostings(ctx context.Context, journalIDs []string) (map[string][]domain.ResolvedPosting, error) {
	coll := c.cacheColl()
	if coll == nil || len(journalIDs) == 0 {
		return map[string][]domain.ResolvedPosting{}, nil
	}
	ids := make([]string, 0, len(journalIDs))
	for _, id := range journalIDs {
		if id != "" {
			ids = append(ids, journalCacheID(id))
		}
	}
	if len(ids) == 0 {
		return map[string][]domain.ResolvedPosting{}, nil
	}
	cur, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := map[string][]domain.ResolvedPosting{}
	for cur.Next(ctx) {
		var doc postingsCacheDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		postings, err := decodePostings(doc)
		if err != nil {
			return nil, err
		}
		out[doc.JournalEntryID] = postings
	}
	return out, cur.Err()
}

// PutPostings upserts postings for one journal entry.
func (c *Client) PutPostings(ctx context.Context, journalID string, postings []domain.ResolvedPosting) error {
	coll := c.cacheColl()
	if coll == nil || journalID == "" {
		return nil
	}
	doc := encodePostings(journalID, postings)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": doc.ID}, doc, options.Replace().SetUpsert(true))
	return err
}

// DeletePostings removes cached postings for the given journal entry IDs.
func (c *Client) DeletePostings(ctx context.Context, journalIDs ...string) error {
	coll := c.cacheColl()
	if coll == nil || len(journalIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(journalIDs))
	for _, id := range journalIDs {
		if id != "" {
			ids = append(ids, journalCacheID(id))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	return err
}
