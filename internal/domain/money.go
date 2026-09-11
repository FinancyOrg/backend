package domain

import (
	"math/big"
	"strings"
)

func GCD(a, b *big.Int) *big.Int {
	x := new(big.Int).Abs(a)
	y := new(big.Int).Abs(b)
	return new(big.Int).GCD(nil, nil, x, y)
}

func NormalizeRational(r Rational) (Rational, error) {
	if r.Denominator == nil || r.Denominator.Sign() == 0 {
		return Rational{}, InvalidPrice("Denominator cannot be zero")
	}
	if r.Denominator.Sign() < 0 {
		return NormalizeRational(Rational{
			Numerator:   new(big.Int).Neg(r.Numerator),
			Denominator: new(big.Int).Neg(r.Denominator),
		})
	}
	g := GCD(r.Numerator, r.Denominator)
	return Rational{
		Numerator:   new(big.Int).Quo(r.Numerator, g),
		Denominator: new(big.Int).Quo(r.Denominator, g),
	}, nil
}

func RationalFromDecimal(value string, scale int) (Rational, error) {
	trimmed := strings.TrimSpace(value)
	if !isDecimal(trimmed) {
		return Rational{}, InvalidPrice("Invalid decimal: " + value)
	}
	negative := strings.HasPrefix(trimmed, "-")
	unsigned := trimmed
	if negative {
		unsigned = trimmed[1:]
	}
	whole, frac, _ := strings.Cut(unsigned, ".")
	padded := (frac + strings.Repeat("0", scale))[:scale]
	raw := whole + padded
	if negative {
		raw = "-" + raw
	}
	num, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return Rational{}, InvalidPrice("Invalid decimal: " + value)
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return NormalizeRational(Rational{Numerator: num, Denominator: den})
}

func RationalFromPair(numerator, denominator *big.Int) (Rational, error) {
	return NormalizeRational(Rational{
		Numerator:   cloneInt(numerator),
		Denominator: cloneInt(denominator),
	})
}

func MustRational(numerator, denominator int64) Rational {
	r, err := RationalFromPair(big.NewInt(numerator), big.NewInt(denominator))
	if err != nil {
		panic(err)
	}
	return r
}

func MultiplyUnitsByRational(unitsMinor *big.Int, rate Rational) (*big.Int, error) {
	norm, err := NormalizeRational(rate)
	if err != nil {
		return nil, err
	}
	product := new(big.Int).Mul(unitsMinor, norm.Numerator)
	if new(big.Int).Rem(product, norm.Denominator).Sign() != 0 {
		return nil, InvalidAmount(
			"Cannot convert " + unitsMinor.String() + " units by " +
				norm.Numerator.String() + "/" + norm.Denominator.String() + " without remainder",
		)
	}
	return new(big.Int).Quo(product, norm.Denominator), nil
}

// RoundHalfAwayFromZero rounds numerator/denominator to the nearest integer.
// Ties (exactly halfway) round away from zero (commercial / Kaufmännisch rounding).
// Used for valuation/reporting FX only — not for posting weights, which must remain exact.
func RoundHalfAwayFromZero(numerator, denominator *big.Int) (*big.Int, error) {
	if denominator.Sign() == 0 {
		return nil, InvalidAmount("Denominator cannot be zero")
	}
	if denominator.Sign() < 0 {
		return RoundHalfAwayFromZero(new(big.Int).Neg(numerator), new(big.Int).Neg(denominator))
	}

	quotient := new(big.Int).Quo(numerator, denominator)
	remainder := new(big.Int).Rem(numerator, denominator)
	if remainder.Sign() == 0 {
		return quotient, nil
	}

	absRemainder := new(big.Int).Abs(remainder)
	twice := new(big.Int).Mul(absRemainder, big.NewInt(2))
	if twice.Cmp(denominator) >= 0 {
		if numerator.Sign() >= 0 {
			return quotient.Add(quotient, big.NewInt(1)), nil
		}
		return quotient.Sub(quotient, big.NewInt(1)), nil
	}
	return quotient, nil
}

// ConvertMinorAtRate converts a native minor-unit amount through an FX/market
// rate into another commodity's minor units, rounding with RoundHalfAwayFromZero.
//
// Formula (before rounding):
//
//	toMinor = fromMinor × (rate.num/rate.den) × 10^(toMinorUnits − fromMinorUnits)
func ConvertMinorAtRate(fromMinor *big.Int, rate Rational, fromMinorUnits, toMinorUnits int) (*big.Int, error) {
	norm, err := NormalizeRational(rate)
	if err != nil {
		return nil, err
	}
	num := new(big.Int).Mul(fromMinor, norm.Numerator)
	den := cloneInt(norm.Denominator)
	scaleDiff := toMinorUnits - fromMinorUnits
	if scaleDiff > 0 {
		num.Mul(num, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scaleDiff)), nil))
	} else if scaleDiff < 0 {
		den.Mul(den, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scaleDiff)), nil))
	}
	return RoundHalfAwayFromZero(num, den)
}

func DivideRational(a, b Rational) (Rational, error) {
	if b.Numerator.Sign() == 0 {
		return Rational{}, InvalidPrice("Cannot divide by zero")
	}
	return NormalizeRational(Rational{
		Numerator:   new(big.Int).Mul(a.Numerator, b.Denominator),
		Denominator: new(big.Int).Mul(a.Denominator, b.Numerator),
	})
}

func InvertRational(r Rational) (Rational, error) {
	if r.Numerator.Sign() == 0 {
		return Rational{}, InvalidPrice("Cannot invert zero")
	}
	return NormalizeRational(Rational{
		Numerator:   cloneInt(r.Denominator),
		Denominator: cloneInt(r.Numerator),
	})
}

func CompareRational(a, b Rational) int {
	left := new(big.Int).Mul(a.Numerator, b.Denominator)
	right := new(big.Int).Mul(b.Numerator, a.Denominator)
	return left.Cmp(right)
}

func FormatMinor(minor *big.Int, minorUnits int) string {
	negative := minor.Sign() < 0
	abs := new(big.Int).Abs(minor)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(minorUnits)), nil)
	whole := new(big.Int).Quo(abs, scale)
	frac := new(big.Int).Rem(abs, scale)
	fracStr := frac.String()
	if minorUnits == 0 {
		// Match TS padStart(0): "0".padStart(0) stays "0".
		fracStr = "0"
	} else {
		for len(fracStr) < minorUnits {
			fracStr = "0" + fracStr
		}
	}
	sign := ""
	if negative {
		sign = "-"
	}
	return sign + whole.String() + "." + fracStr
}

func ParseMinor(value string, minorUnits int) *big.Int {
	trimmed := strings.TrimSpace(value)
	negative := strings.HasPrefix(trimmed, "-")
	unsigned := trimmed
	if negative {
		unsigned = trimmed[1:]
	}
	whole, frac, _ := strings.Cut(unsigned, ".")
	padded := (frac + strings.Repeat("0", minorUnits))
	if len(padded) > minorUnits {
		padded = padded[:minorUnits]
	}
	raw := whole + padded
	if negative {
		raw = "-" + raw
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return big.NewInt(0)
	}
	return n
}

func MinorToRational(minor *big.Int, minorUnits int) (Rational, error) {
	return NormalizeRational(Rational{
		Numerator:   cloneInt(minor),
		Denominator: new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(minorUnits)), nil),
	})
}

func RationalToMinor(r Rational, minorUnits int) (*big.Int, error) {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(minorUnits)), nil)
	scaled, err := NormalizeRational(Rational{
		Numerator:   new(big.Int).Mul(r.Numerator, scale),
		Denominator: cloneInt(r.Denominator),
	})
	if err != nil {
		return nil, err
	}
	if scaled.Denominator.Cmp(big.NewInt(1)) != 0 {
		return nil, InvalidAmount(
			"Rational " + r.Numerator.String() + "/" + r.Denominator.String() + " is not representable in minor units",
		)
	}
	return scaled.Numerator, nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		i++
		if i >= len(s) {
			return false
		}
	}
	sawDigit := false
	sawDot := false
	for ; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			sawDigit = true
			continue
		}
		if c == '.' && !sawDot {
			sawDot = true
			continue
		}
		return false
	}
	return sawDigit
}
