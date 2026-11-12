// Package decimal is a small port of the rust_decimal behaviour Finglass uses.
//
// It stores a signed mantissa and a decimal scale. The mantissa is never
// shared and never mutated after construction: every operation returns a new
// value. Parse follows rust_decimal 1.43's grammar and limits (28 fraction
// digits, a 96-bit mantissa), and String prints like Rust's Display, including
// a negative zero.
package decimal

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// maxScale is rust_decimal's maximum number of fraction digits.
const maxScale = 28

var (
	// maxMantissa is rust_decimal's maximum unsigned mantissa, 2^96 - 1.
	maxMantissa = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 96), big.NewInt(1))
	// overflowMantissa is one past maxMantissa, 2^96.
	overflowMantissa = new(big.Int).Lsh(big.NewInt(1), 96)
)

// Decimal is a fixed-point decimal with a signed mantissa and a scale.
type Decimal struct {
	mantissa *big.Int
	scale    int32
	negZero  bool
}

// Zero is the decimal zero.
var Zero Decimal

// New returns a decimal with rust_decimal's Decimal::new semantics.
func New(mantissa int64, scale int32) Decimal {
	return Decimal{mantissa: big.NewInt(mantissa), scale: scale}
}

// FromInt returns the decimal representation of n.
func FromInt(n int64) Decimal {
	return Decimal{mantissa: big.NewInt(n)}
}

// Parse parses a decimal with rust_decimal's from_str rules.
func Parse(s string) (Decimal, error) {
	index := strings.IndexAny(s, "eE")
	if index < 0 {
		return parsePlain(s)
	}
	base, err := parsePlain(s[:index])
	if err != nil {
		return Decimal{}, err
	}
	return applyExponent(base, s[index+1:])
}

func parsePlain(s string) (Decimal, error) {
	if s == "" {
		return Decimal{}, errors.New("invalid decimal: empty")
	}

	index := 0
	negative := false
	switch s[0] {
	case '-':
		negative = true
		index++
	case '+':
		index++
	}
	if index == len(s) {
		return Decimal{}, errors.New("invalid decimal: no digits found")
	}

	mantissa := new(big.Int)
	scale := 0
	point := false
	has := false

	for index < len(s) {
		character := s[index]
		switch {
		case character >= '0' && character <= '9':
			digit := int64(character - '0')
			next := new(big.Int).Mul(mantissa, big.NewInt(10))
			next.Add(next, big.NewInt(digit))
			if next.Cmp(overflowMantissa) >= 0 {
				if !point {
					return Decimal{}, errors.New("invalid decimal: overflow from too many digits")
				}
				return roundAt(mantissa, scale, character, s[index+1:], point, negative)
			}
			mantissa = next
			has = true
			index++
			if point {
				scale++
				if scale >= maxScale && index < len(s) {
					// Rust skips underscores at the rounding position and
					// rounds on the next actual character.
					next := index
					for next < len(s) && s[next] == '_' {
						next++
					}
					if next == len(s) {
						return fromMagnitude(mantissa, scale, negative)
					}
					return roundAt(mantissa, scale, s[next], s[next+1:], point, negative)
				}
			}
		case character == '.':
			if point {
				return Decimal{}, errors.New("invalid decimal: two decimal points")
			}
			point = true
			index++
		case character == '_':
			if !has {
				return Decimal{}, errors.New("invalid decimal: must start lead with a number")
			}
			index++
		default:
			return Decimal{}, errors.New("invalid decimal: unknown character")
		}
	}

	if !has {
		return Decimal{}, errors.New("invalid decimal: no digits found")
	}
	return fromMagnitude(mantissa, scale, negative)
}

// roundAt mirrors rust_decimal's maybe_round: next is the first digit past the
// retained precision and remaining is everything after it.
func roundAt(mantissa *big.Int, scale int, next byte, remaining string, point bool, negative bool) (Decimal, error) {
	var digit int64
	switch {
	case next >= '0' && next <= '9':
		digit = int64(next - '0')
	case next == '_':
		digit = 0
	case next == '.' && !point:
		digit = 0
	default:
		return Decimal{}, errors.New("invalid decimal: unknown character")
	}

	rounded := new(big.Int).Set(mantissa)
	if digit >= 5 {
		rounded.Add(rounded, big.NewInt(1))
		if rounded.Cmp(overflowMantissa) >= 0 {
			if scale == 0 {
				return Decimal{}, errors.New("invalid decimal: overflow from mantissa after rounding")
			}
			rounded.Add(rounded, big.NewInt(4))
			rounded.Quo(rounded, big.NewInt(10))
			scale--
		}
	}

	seenDot := point || next == '.'
	for index := 0; index < len(remaining); index++ {
		character := remaining[index]
		switch {
		case character >= '0' && character <= '9', character == '_':
		case character == '.' && !seenDot:
			seenDot = true
		default:
			return Decimal{}, errors.New("invalid decimal: unknown character")
		}
	}

	return fromMagnitude(rounded, scale, negative)
}

func applyExponent(base Decimal, exponentPart string) (Decimal, error) {
	negative := false
	switch {
	case strings.HasPrefix(exponentPart, "-"):
		negative = true
		exponentPart = exponentPart[1:]
	case strings.HasPrefix(exponentPart, "+"):
		exponentPart = exponentPart[1:]
	}
	if exponentPart == "" {
		return Decimal{}, errors.New("invalid decimal: exponent")
	}
	for index := 0; index < len(exponentPart); index++ {
		if exponentPart[index] < '0' || exponentPart[index] > '9' {
			return Decimal{}, errors.New("invalid decimal: exponent")
		}
	}
	value, err := strconv.ParseUint(exponentPart, 10, 32)
	if err != nil {
		return Decimal{}, errors.New("invalid decimal: exponent")
	}
	if value > maxScale {
		return Decimal{}, errors.New("invalid decimal: scale exceeds maximum precision")
	}
	exponent := int32(value)

	mantissa := base.mantissaValue()
	if negative {
		if base.scale+exponent > maxScale {
			rescaled := base.Rescale(maxScale - exponent)
			return Decimal{mantissa: rescaled.mantissaValue(), scale: maxScale, negZero: rescaled.negZero}, nil
		}
		return Decimal{mantissa: mantissa, scale: base.scale + exponent, negZero: base.negZero}, nil
	}

	if exponent < base.scale {
		return Decimal{mantissa: mantissa, scale: base.scale - exponent, negZero: base.negZero}, nil
	}

	// Applying a non-negative exponent shift keeps the value and, once the
	// trailing zeros Rust's normalize would strip are gone, leaves scale 0.
	product := new(big.Int).Mul(mantissa, pow10(int64(exponent-base.scale)))
	if new(big.Int).Abs(product).Cmp(maxMantissa) > 0 {
		return Decimal{}, errors.New("invalid decimal: exceeds maximum possible value")
	}
	return Decimal{mantissa: product, scale: 0, negZero: base.negZero}, nil
}

func fromMagnitude(mantissa *big.Int, scale int, negative bool) (Decimal, error) {
	if scale < 0 || scale > maxScale {
		return Decimal{}, errors.New("invalid decimal: scale exceeds maximum precision")
	}
	if mantissa.Sign() == 0 {
		return Decimal{scale: int32(scale)}, nil
	}
	value := new(big.Int).Set(mantissa)
	if negative {
		value.Neg(value)
	}
	return Decimal{mantissa: value, scale: int32(scale)}, nil
}

func pow10(exponent int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(exponent), nil)
}

func (d Decimal) mantissaValue() *big.Int {
	if d.mantissa == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(d.mantissa)
}

// String formats the decimal like Rust's Display.
func (d Decimal) String() string {
	absolute := d.mantissaValue()
	absolute.Abs(absolute)
	negative := d.mantissaValue().Sign() < 0 || (d.IsZero() && d.negZero)

	digits := absolute.String()
	if d.scale > 0 {
		scale := int(d.scale)
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// Scale returns the number of fraction digits.
func (d Decimal) Scale() int32 {
	return d.scale
}

// Mantissa returns a copy of the signed mantissa.
func (d Decimal) Mantissa() *big.Int {
	return d.mantissaValue()
}

// Sign returns -1, 0 or 1. Every zero has sign 0.
func (d Decimal) Sign() int {
	return d.mantissaValue().Sign()
}

// IsZero reports whether the value is zero.
func (d Decimal) IsZero() bool {
	return d.mantissaValue().Sign() == 0
}

// Cmp compares two decimals by value, so 1.50 and 1.5 are equal.
func (d Decimal) Cmp(other Decimal) int {
	left := d.mantissaValue()
	right := other.mantissaValue()
	if d.scale < other.scale {
		left.Mul(left, pow10(int64(other.scale-d.scale)))
	} else if other.scale < d.scale {
		right.Mul(right, pow10(int64(d.scale-other.scale)))
	}
	return left.Cmp(right)
}

// Neg returns the negation, preserving a zero's sign flag like Rust.
func (d Decimal) Neg() Decimal {
	if d.IsZero() {
		return Decimal{scale: d.scale, negZero: !d.negZero}
	}
	return Decimal{mantissa: new(big.Int).Neg(d.mantissaValue()), scale: d.scale}
}

// Add adds two decimals. The result's scale is the larger of the two scales,
// except that a zero operand is returned unchanged, as Rust does. It does not
// check the 96-bit mantissa limit.
func (d Decimal) Add(other Decimal) Decimal {
	if d.IsZero() {
		return other
	}
	if other.IsZero() {
		return d
	}
	scale := d.scale
	if other.scale > scale {
		scale = other.scale
	}
	left := d.mantissaValue()
	left.Mul(left, pow10(int64(scale-d.scale)))
	right := other.mantissaValue()
	right.Mul(right, pow10(int64(scale-other.scale)))
	sum := new(big.Int).Add(left, right)
	return decimalFromSigned(sum, scale)
}

// Sub subtracts other from d. Like Add, it does not check the mantissa limit.
func (d Decimal) Sub(other Decimal) Decimal {
	if d.IsZero() {
		if other.IsZero() {
			return other
		}
		return other.Neg()
	}
	if other.IsZero() {
		return d
	}
	scale := d.scale
	if other.scale > scale {
		scale = other.scale
	}
	left := d.mantissaValue()
	left.Mul(left, pow10(int64(scale-d.scale)))
	right := other.mantissaValue()
	right.Mul(right, pow10(int64(scale-other.scale)))
	difference := new(big.Int).Sub(left, right)
	return decimalFromSigned(difference, scale)
}

// Mul multiplies two decimals; the scale is the sum of both scales. It does not
// check the mantissa limit. A zero product is the plain zero, like Rust.
func (d Decimal) Mul(other Decimal) Decimal {
	if d.IsZero() || other.IsZero() {
		return Zero
	}
	product := new(big.Int).Mul(d.mantissaValue(), other.mantissaValue())
	return Decimal{mantissa: product, scale: d.scale + other.scale}
}

// RoundDP rounds to dp fraction digits, halfway away from zero. It never raises
// the scale: a value with fewer digits is returned unchanged.
func (d Decimal) RoundDP(dp int32) Decimal {
	if d.scale <= dp {
		return d
	}
	if d.IsZero() {
		return Decimal{scale: dp, negZero: d.negZero}
	}
	difference := d.scale - dp
	divisor := pow10(int64(difference))
	quotient, remainder := new(big.Int).QuoRem(d.mantissaValue(), divisor, new(big.Int))
	if new(big.Int).Abs(remainder).Cmp(new(big.Int).Rsh(new(big.Int).Set(divisor), 1)) >= 0 {
		if d.mantissaValue().Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return decimalFromSigned(quotient, dp)
}

// Rescale returns the value at scale, padding with zeros or rounding halfway
// away from zero when the scale is lowered. A zero keeps its sign flag, as
// Rust's rescale does.
func (d Decimal) Rescale(scale int32) Decimal {
	if scale == d.scale {
		return d
	}
	if d.IsZero() {
		return Decimal{scale: scale, negZero: d.negZero}
	}
	if scale > d.scale {
		mantissa := d.mantissaValue()
		mantissa.Mul(mantissa, pow10(int64(scale-d.scale)))
		return Decimal{mantissa: mantissa, scale: scale}
	}

	difference := d.scale - scale
	divisor := pow10(int64(difference))
	quotient, remainder := new(big.Int).QuoRem(d.mantissaValue(), divisor, new(big.Int))
	negative := d.mantissaValue().Sign() < 0
	if new(big.Int).Abs(remainder).Cmp(new(big.Int).Rsh(new(big.Int).Set(divisor), 1)) >= 0 {
		if negative {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if quotient.Sign() == 0 {
		return Decimal{scale: scale, negZero: d.negZero || d.mantissaValue().Sign() < 0}
	}
	return Decimal{mantissa: quotient, scale: scale}
}

// Normalize strips trailing fraction zeros and turns every zero, including a
// negative zero, into the plain zero.
func (d Decimal) Normalize() Decimal {
	if d.IsZero() {
		return Zero
	}
	mantissa := d.mantissaValue()
	scale := d.scale
	ten := big.NewInt(10)
	quotient := new(big.Int)
	remainder := new(big.Int)
	for scale > 0 {
		quotient.QuoRem(mantissa, ten, remainder)
		if remainder.Sign() != 0 {
			break
		}
		mantissa.Set(quotient)
		scale--
	}
	return Decimal{mantissa: mantissa, scale: scale}
}

// Trunc returns the integer part towards zero.
func (d Decimal) Trunc() Decimal {
	quotient := new(big.Int).Quo(d.mantissaValue(), pow10(int64(d.scale)))
	if quotient.Sign() == 0 {
		return Decimal{negZero: d.negZero || d.mantissaValue().Sign() < 0}
	}
	return Decimal{mantissa: quotient}
}

// Int64 returns the truncated value when it fits in an int64.
func (d Decimal) Int64() (int64, bool) {
	quotient := new(big.Int).Quo(d.mantissaValue(), pow10(int64(d.scale)))
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

func decimalFromSigned(mantissa *big.Int, scale int32) Decimal {
	if mantissa.Sign() == 0 {
		return Decimal{scale: scale}
	}
	return Decimal{mantissa: mantissa, scale: scale}
}
