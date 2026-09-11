package ledger

import (
	"math/big"

	"github.com/FinancyOrg/backend/internal/domain"
	"github.com/google/uuid"
)

func newID() string {
	return uuid.NewString()
}

func nowISO() string {
	return domain.NowUTC()
}

func todayISO() string {
	return domain.UTCDate(nowISO())
}

func parseBigInt(v *int64) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return big.NewInt(*v)
}

func parseRational(num, den *string) *domain.Rational {
	if num == nil || den == nil {
		return nil
	}
	n, ok1 := new(big.Int).SetString(*num, 10)
	d, ok2 := new(big.Int).SetString(*den, 10)
	if !ok1 || !ok2 {
		return nil
	}
	return &domain.Rational{Numerator: n, Denominator: d}
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func bigString(n *big.Int) string {
	if n == nil {
		return "0"
	}
	return n.String()
}

func ptr[T any](v T) *T {
	return &v
}
