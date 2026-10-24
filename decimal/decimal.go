// Package decimal is the exact decimal: a value carried as its CANONICAL TEXT,
// never a host decimal or float — the digits are the value, and text is the one
// representation every host agrees on. The twin of the reference's Decimal
// (its D72), certified against the reference's laws/decimal-laws.json.
//
// THE GRAMMAR READ is -?[0-9]+(\.[0-9]+)? over ASCII digits: a leading '+', a bare
// point ('.5', '5.'), an exponent, a separator and white space are refused. THE
// CANONICAL FORM WRITTEN has no leading zero on the integer part, no trailing zero
// on the fraction, no point where the fraction is zero and no sign on zero, so two
// texts denote one number exactly when their canonical forms are one string.
//
// The arithmetic is exact over math/big integers scaled by a power of ten; the one
// rounding kernel (roundQuotient) rounds the EXACT value once, under one of the
// seven modes, and nothing else here rounds. The only approximations are the two
// conversions to and from float, each of which a caller asks for by name.
package decimal

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// RoundingMode is one of the seven modes, by its wire spelling.
type RoundingMode string

// The rounding modes — the wire spellings of the seven-mode set (closed: an
// unknown spelling is refused by ParseRoundingMode).
const (
	RoundHalfEven RoundingMode = "half-even"
	RoundHalfUp   RoundingMode = "half-up"
	RoundHalfDown RoundingMode = "half-down"
	RoundUp       RoundingMode = "up"
	RoundDown     RoundingMode = "down"
	RoundCeiling  RoundingMode = "ceiling"
	RoundFloor    RoundingMode = "floor"
)

// RoundingModes is the closed set, in declaration order.
var RoundingModes = []RoundingMode{RoundHalfEven, RoundHalfUp, RoundHalfDown, RoundUp, RoundDown, RoundCeiling, RoundFloor}

// ParseRoundingMode reads a wire spelling; ok is false for a spelling outside the set.
func ParseRoundingMode(s string) (RoundingMode, bool) {
	for _, m := range RoundingModes {
		if string(m) == s {
			return m, true
		}
	}
	return "", false
}

// MaxScale is the largest scale a rounding may name. A quotient is computed to the
// scale it names, so the bound is a resource limit rather than a statement about
// decimals.
const MaxScale = 1000

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parts reads decimal text as (negative, integer digits, fraction digits) with the
// leading integer zeros and trailing fraction zeros stripped, or ok=false outside
// the grammar.
func parts(s string) (neg bool, ip, fp string, ok bool) {
	if s == "" {
		return false, "", "", false
	}
	neg = s[0] == '-'
	body := s
	if neg {
		body = s[1:]
	}
	if dot := strings.IndexByte(body, '.'); dot < 0 {
		ip = body
		if !isASCIIDigits(ip) {
			return false, "", "", false
		}
	} else {
		ip, fp = body[:dot], body[dot+1:]
		if !isASCIIDigits(ip) || !isASCIIDigits(fp) {
			return false, "", "", false
		}
	}
	ip = strings.TrimLeft(ip, "0")
	fp = strings.TrimRight(fp, "0")
	if ip == "" && fp == "" {
		neg = false
	}
	return neg, ip, fp, true
}

// Canonical is the canonical form of s, or ok=false where s is not decimal text.
func Canonical(s string) (string, bool) {
	neg, ip, fp, ok := parts(s)
	if !ok {
		return "", false
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if ip == "" {
		b.WriteByte('0')
	} else {
		b.WriteString(ip)
	}
	if fp != "" {
		b.WriteByte('.')
		b.WriteString(fp)
	}
	return b.String(), true
}

// IsCanonical reports whether s is decimal text already in its canonical form.
func IsCanonical(s string) bool {
	c, ok := Canonical(s)
	return ok && c == s
}

// dec is an exact value: unscaled × 10^-scale, scale >= 0.
type dec struct {
	unscaled *big.Int
	scale    int
}

func parseDec(s string) (dec, bool) {
	neg, ip, fp, ok := parts(s)
	if !ok {
		return dec{}, false
	}
	u, _ := new(big.Int).SetString("0"+ip+fp, 10)
	if neg {
		u.Neg(u)
	}
	return dec{unscaled: u, scale: len(fp)}, true
}

var bigTen = big.NewInt(10)

func pow10(n int) *big.Int { return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil) }

// text renders the value in canonical form.
func (d dec) text() string {
	digits := new(big.Int).Abs(d.unscaled).String()
	var raw string
	if d.scale == 0 {
		raw = digits
	} else {
		if len(digits) <= d.scale {
			digits = strings.Repeat("0", d.scale-len(digits)+1) + digits
		}
		raw = digits[:len(digits)-d.scale] + "." + digits[len(digits)-d.scale:]
	}
	if d.unscaled.Sign() < 0 {
		raw = "-" + raw
	}
	c, _ := Canonical(raw)
	return c
}

// aligned brings two values to their common scale.
func aligned(a, b dec) (*big.Int, *big.Int, int) {
	scale := a.scale
	if b.scale > scale {
		scale = b.scale
	}
	x := new(big.Int).Mul(a.unscaled, pow10(scale-a.scale))
	y := new(big.Int).Mul(b.unscaled, pow10(scale-b.scale))
	return x, y, scale
}

// Compare is the numeric order of two decimal texts (-1 / 0 / 1), or ok=false where
// either is not decimal text. Canonical or not: 1.50 = 1.5.
func Compare(a, b string) (int, bool) {
	x, okx := parseDec(a)
	y, oky := parseDec(b)
	if !okx || !oky {
		return 0, false
	}
	p, q, _ := aligned(x, y)
	return p.Cmp(q), true
}

// Add is the exact sum in canonical form, or ok=false where either is not decimal text.
func Add(a, b string) (string, bool) {
	return binary(a, b, func(x, y dec) dec {
		p, q, s := aligned(x, y)
		return dec{unscaled: p.Add(p, q), scale: s}
	})
}

// Sub is the exact difference in canonical form.
func Sub(a, b string) (string, bool) {
	return binary(a, b, func(x, y dec) dec {
		p, q, s := aligned(x, y)
		return dec{unscaled: p.Sub(p, q), scale: s}
	})
}

// Mul is the exact product in canonical form.
func Mul(a, b string) (string, bool) {
	return binary(a, b, func(x, y dec) dec {
		return dec{unscaled: new(big.Int).Mul(x.unscaled, y.unscaled), scale: x.scale + y.scale}
	})
}

// Rem is the remainder of a truncating division, with the dividend's sign — the
// integer mod's convention, and exact. ok=false for a zero divisor as well as for
// text that is not decimal (callers check the divisor first).
func Rem(a, b string) (string, bool) {
	x, okx := parseDec(a)
	y, oky := parseDec(b)
	if !okx || !oky || y.unscaled.Sign() == 0 {
		return "", false
	}
	p, q, s := aligned(x, y)
	return dec{unscaled: new(big.Int).Rem(p, q), scale: s}.text(), true
}

func binary(a, b string, f func(x, y dec) dec) (string, bool) {
	x, okx := parseDec(a)
	y, oky := parseDec(b)
	if !okx || !oky {
		return "", false
	}
	return f(x, y).text(), true
}

// Abs is the magnitude in canonical form.
func Abs(a string) (string, bool) {
	x, ok := parseDec(a)
	if !ok {
		return "", false
	}
	return dec{unscaled: new(big.Int).Abs(x.unscaled), scale: x.scale}.text(), true
}

// Negate is the negation in canonical form.
func Negate(a string) (string, bool) {
	x, ok := parseDec(a)
	if !ok {
		return "", false
	}
	return dec{unscaled: new(big.Int).Neg(x.unscaled), scale: x.scale}.text(), true
}

// IsZero reports decimal text denoting zero.
func IsZero(a string) bool {
	x, ok := parseDec(a)
	return ok && x.unscaled.Sign() == 0
}

// roundQuotient is numerator / denominator (magnitudes, denominator non-zero)
// rounded to an integer under mode, where negative is the sign of the exact
// quotient: the remainder decides the last digit — twice the remainder against the
// divisor for the three half-way modes, its being non-zero for the four directed
// ones. ONE rounding, of the exact value.
func roundQuotient(mode RoundingMode, negative bool, num, den *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if r.Sign() == 0 {
		return q
	}
	half := new(big.Int).Lsh(r, 1).Cmp(den)
	bump := func() *big.Int { return q.Add(q, big.NewInt(1)) }
	switch mode {
	case RoundDown:
		return q
	case RoundUp:
		return bump()
	case RoundCeiling:
		if negative {
			return q
		}
		return bump()
	case RoundFloor:
		if negative {
			return bump()
		}
		return q
	case RoundHalfUp:
		if half >= 0 {
			return bump()
		}
		return q
	case RoundHalfDown:
		if half > 0 {
			return bump()
		}
		return q
	default: // half-even
		if half > 0 || (half == 0 && q.Bit(0) == 1) {
			return bump()
		}
		return q
	}
}

func signed(negative bool, mag *big.Int) *big.Int {
	if negative {
		return mag.Neg(mag)
	}
	return mag
}

// Quantize brings a decimal to scale places under mode — the value itself where it
// already has no more places than that.
func Quantize(mode RoundingMode, scale int, a string) (string, bool) {
	x, ok := parseDec(a)
	if !ok {
		return "", false
	}
	if x.scale <= scale {
		return x.text(), true
	}
	negative := x.unscaled.Sign() < 0
	mag := new(big.Int).Abs(x.unscaled)
	q := roundQuotient(mode, negative, mag, pow10(x.scale-scale))
	return dec{unscaled: signed(negative, q), scale: scale}.text(), true
}

// Divide is dividend / divisor correctly rounded to scale places under mode.
// isNull reports a zero divisor; ok=false where an operand is not decimal text.
func Divide(mode RoundingMode, scale int, a, b string) (q string, isNull bool, ok bool) {
	x, okx := parseDec(a)
	y, oky := parseDec(b)
	if !okx || !oky {
		return "", false, false
	}
	if y.unscaled.Sign() == 0 {
		return "", true, true
	}
	// a / b = (ua / ub) × 10^(sb - sa); scaled by 10^scale, the integer to round is
	// ua × 10^e / ub with e = sb - sa + scale.
	e := y.scale - x.scale + scale
	num := new(big.Int).Abs(x.unscaled)
	den := new(big.Int).Abs(y.unscaled)
	if e >= 0 {
		num.Mul(num, pow10(e))
	} else {
		den.Mul(den, pow10(-e))
	}
	negative := (x.unscaled.Sign() < 0) != (y.unscaled.Sign() < 0) && x.unscaled.Sign() != 0
	r := roundQuotient(mode, negative, num, den)
	return dec{unscaled: signed(negative, r), scale: scale}.text(), false, true
}

// ToFloat is the nearest float64 to a decimal text, or ok=false where it is not
// decimal text OR its magnitude is past the float range — an infinity is not the
// nearest float to any decimal. The one place the type rounds, for the results
// that are a float by declaration (mean, median, stddev, a cast).
func ToFloat(s string) (float64, bool) {
	c, ok := Canonical(s)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(c, 64)
	if err != nil && (math.IsInf(f, 0) || math.IsNaN(f)) {
		return 0, false
	}
	return f, true
}

// OfFloat is the decimal text of a FINITE float — its shortest round-trip digits
// laid out without an exponent. ok=false for NaN and the infinities.
func OfFloat(f float64) (string, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", false
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	e := strings.IndexByte(s, 'e')
	exponent, _ := strconv.Atoi(s[e+1:])
	m, ok := parseDec(s[:e])
	if !ok {
		return "", false
	}
	scale := m.scale - exponent
	if scale >= 0 {
		return dec{unscaled: m.unscaled, scale: scale}.text(), true
	}
	return dec{unscaled: new(big.Int).Mul(m.unscaled, pow10(-scale)), scale: 0}.text(), true
}

// TruncateToInt64 is the integer part (toward zero), or ok=false past int64.
func TruncateToInt64(a string) (int64, bool) {
	x, ok := parseDec(a)
	if !ok {
		return 0, false
	}
	q := new(big.Int).Quo(x.unscaled, pow10(x.scale))
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}
