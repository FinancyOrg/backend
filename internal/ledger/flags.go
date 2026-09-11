package ledger

import (
	"context"
	"log"
)

// AccountFlags are presentation-only toggles stored outside CRDB.
// Missing flags are treated as Hidden=false and Liquid=false.
type AccountFlags struct {
	Hidden bool
	Liquid bool
}

// AccountFlagsStore persists account view flags. A nil store on Service
// (tests, no Mongo) is a no-op: every account is treated as not hidden
// and not liquid.
type AccountFlagsStore interface {
	ListAccountFlags(ctx context.Context) (map[string]AccountFlags, error)
	PutAccountFlags(ctx context.Context, accountID string, flags AccountFlags) error
	DeleteAccountFlags(ctx context.Context, accountID string) error
}

func (s *Service) WithAccountFlags(store AccountFlagsStore) *Service {
	if store != nil {
		s.flags = store
	}
	return s
}

func (s *Service) listAccountFlags(ctx context.Context) (map[string]AccountFlags, error) {
	if s.flags == nil {
		return map[string]AccountFlags{}, nil
	}
	out, err := s.flags.ListAccountFlags(ctx)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]AccountFlags{}, nil
	}
	return out, nil
}

func (s *Service) putAccountFlags(ctx context.Context, accountID string, flags AccountFlags) error {
	if s.flags == nil {
		return nil
	}
	return s.flags.PutAccountFlags(ctx, accountID, flags)
}

func (s *Service) deleteAccountFlags(ctx context.Context, accountID string) {
	if s.flags == nil {
		return
	}
	if err := s.flags.DeleteAccountFlags(ctx, accountID); err != nil {
		log.Printf("account flags delete: %v", err)
	}
}
