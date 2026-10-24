package json

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Builder is what a reading builds from the tokens. One reader serves both value
// models — Value here and the null-free, integer-aware model of package decode — so
// the grammar, the rows and the limits are stated once.
type Builder[V any] interface {
	// Null answers false when the reading admits no null (NullNotRepresentable).
	Null() (V, bool)
	Bool(b bool) V
	// Number takes integerToken true for a token with no point and no exponent.
	Number(n float64, integerToken bool) V
	String(s string) V
	Array(items []V) V
	Object(members []Pair[V]) V
}

// Pair is one object member as a Builder receives it.
type Pair[V any] struct {
	Key   string
	Value V
}

type valueBuilder struct{}

func (valueBuilder) Null() (Value, bool)            { return Null{}, true }
func (valueBuilder) Bool(b bool) Value              { return Bool(b) }
func (valueBuilder) Number(n float64, _ bool) Value { return Number(n) }
func (valueBuilder) String(s string) Value          { return String(s) }
func (valueBuilder) Array(items []Value) Value      { return Array(items) }
func (valueBuilder) Object(members []Pair[Value]) Value {
	out := make(Object, len(members))
	for i, m := range members {
		out[i] = Member{Key: m.Key, Value: m.Value}
	}
	return out
}

// Parse reads a document under the reader's own stack guard (Strict).
func Parse(input string) (Value, Refusal) {
	return Scan[Value](input, Strict, valueBuilder{})
}

// Read reads a document under limits, each bound enforced on the way down.
func Read(input string, limits Limits) (Value, Refusal) {
	return Scan[Value](input, limits, valueBuilder{})
}

// Scan reads the whole document under limits through build: the document bound
// before anything is read, then exactly ONE value and nothing after it but
// whitespace (row 2).
func Scan[V any](input string, limits Limits, build Builder[V]) (V, Refusal) {
	var zero V
	if limits.DocumentBytes > 0 && len(input) > limits.DocumentBytes {
		return zero, &LimitBreach{Limit: DocumentBytes, Value: limits.DocumentBytes}
	}
	p := &parser[V]{src: input, limits: limits, build: build}
	v, err := p.parseValue()
	if err != nil {
		if b, ok := err.(*LimitBreach); ok {
			// Pushed inner-first on the way out; reversed once here.
			for i, j := 0, len(b.Path)-1; i < j; i, j = i+1, j-1 {
				b.Path[i], b.Path[j] = b.Path[j], b.Path[i]
			}
		}
		return zero, err
	}
	p.skipWS()
	if p.pos < len(p.src) {
		return zero, p.fail(TrailingCharacters, "content after the root value (the JSON-syntax layer, row 2)")
	}
	return v, nil
}

type parser[V any] struct {
	src string
	pos int
	// depth counts the containers open, on the way DOWN, before the recursion that
	// would pass the bound. Without it the reader is unbounded mutual recursion.
	depth  int
	limits Limits
	build  Builder[V]
}

func (p *parser[V]) fail(kind ErrorKind, message string) Refusal {
	return &Error{Kind: kind, At: p.pos, Message: message}
}

func (p *parser[V]) failAt(kind ErrorKind, at int, message string) Refusal {
	return &Error{Kind: kind, At: at, Message: message}
}

func (p *parser[V]) breach(limit Limit, value int) Refusal {
	return &LimitBreach{Limit: limit, Value: value, At: p.pos}
}

// within adds the step the refusal passed through on its way out. Only a breach
// carries a path; malformed text is refused at the root by every reading.
func within(r Refusal, seg PathSeg) Refusal {
	if b, ok := r.(*LimitBreach); ok {
		b.Path = append(b.Path, seg)
	}
	return r
}

func (p *parser[V]) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// expect consumes ch, or refuses ExpectedToken where it stands — at the end of the
// input too, as the reference's `expect` does; UnexpectedEndOfInput is raised only
// where a VALUE was expected and the input ended.
func (p *parser[V]) expect(ch byte) Refusal {
	if p.pos >= len(p.src) {
		return p.fail(ExpectedToken, "expected '"+string(ch)+"' but the input ended")
	}
	if c := p.src[p.pos]; c != ch {
		return p.fail(ExpectedToken, "expected '"+string(ch)+"' but found '"+string(c)+"'")
	}
	p.pos++
	return nil
}

func (p *parser[V]) parseValue() (V, Refusal) {
	var zero V
	p.skipWS()
	if p.pos >= len(p.src) {
		return zero, p.fail(UnexpectedEndOfInput, "unexpected end of input")
	}
	c := p.src[p.pos]
	switch {
	case c == '{' || c == '[':
		// Refused BEFORE descending: a check after the walk has already paid the cost
		// it exists to refuse, and here it would never run at all.
		if p.depth >= p.limits.Depth {
			return zero, p.breach(Depth, p.limits.Depth)
		}
		p.depth++
		var v V
		var err Refusal
		if c == '{' {
			v, err = p.parseObject()
		} else {
			v, err = p.parseArray()
		}
		p.depth--
		return v, err
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return zero, err
		}
		return p.build.String(s), nil
	case c == 't':
		return p.literal("true", func() (V, Refusal) { return p.build.Bool(true), nil })
	case c == 'f':
		return p.literal("false", func() (V, Refusal) { return p.build.Bool(false), nil })
	case c == 'n':
		// A reading that admits no null refuses at the `n`, before the literal is read,
		// as the reference's strict reading does.
		v, ok := p.build.Null()
		if !ok {
			return zero, p.fail(NullNotRepresentable, "null is not representable in this reading")
		}
		return p.literal("null", func() (V, Refusal) { return v, nil })
	case c == '-' || isDigit(c):
		return p.parseNumber()
	default:
		return zero, p.fail(UnexpectedChar, "unexpected character '"+string(c)+"'")
	}
}

func (p *parser[V]) literal(word string, value func() (V, Refusal)) (V, Refusal) {
	if strings.HasPrefix(p.src[p.pos:], word) {
		p.pos += len(word)
		return value()
	}
	var zero V
	return zero, p.fail(ExpectedToken, "expected '"+word+"'")
}

// Surrogate classes of one appended unit.
const (
	noSur = iota // a code point of its own
	hiSur        // a high half, waiting for its low half
	loSur        // a low half, completing a high half
)

// parseString reads one string literal, as the reference's parser does, unit by unit:
//
//   - Row 6 — an unpaired surrogate is refused. A high half must be followed
//     IMMEDIATELY by a low half, and a low half must complete a pending high; the
//     refusal fires AFTER the offending unit is consumed (a character after it, an
//     escape after its letter or its four hex digits, the closing quote after it),
//     which is where the reference's position stands. A byte sequence that is not
//     UTF-8 (how a raw surrogate reaches a Go string) is refused where it stands.
//   - Row 5 — a raw C0 control character is refused AT the character, before the
//     surrogate test. A conformant encoder escapes them, so passing one through
//     admits input no encoder produces.
//   - The string bound, counted in CODE POINTS as the literal is built, tested after
//     the surrogate tests and before the unit is appended: a low half completing a
//     pending high is not a new code point. Measuring afterwards has already paid
//     the allocation the bound exists to refuse.
func (p *parser[V]) parseString() (string, Refusal) {
	if err := p.expect('"'); err != nil {
		return "", err
	}
	var out strings.Builder
	codePoints := 0
	pending := false // the last unit was a high surrogate waiting for its low half
	var high rune    // that high half
	// unit appends one unit of class cls — the reference's `append`: the two surrogate
	// tests, the bound, then the unit itself.
	unit := func(r rune, cls int) Refusal {
		if pending && cls != loSur {
			return p.fail(BadEscape, "an unpaired high surrogate (the JSON-syntax layer, row 6): a high half must be followed immediately by a low half")
		}
		if !pending && cls == loSur {
			return p.fail(BadEscape, "an unpaired low surrogate (the JSON-syntax layer, row 6): a low half completes a high half before it")
		}
		if !pending {
			if p.limits.StringLength > 0 && codePoints >= p.limits.StringLength {
				return p.breach(StringLength, p.limits.StringLength)
			}
			codePoints++
		}
		switch cls {
		case hiSur:
			pending, high = true, r
		case loSur:
			out.WriteRune(0x10000 + ((high - 0xD800) << 10) + (r - 0xDC00))
			pending = false
		default:
			out.WriteRune(r)
		}
		return nil
	}
	for {
		if p.pos >= len(p.src) {
			return "", p.fail(UnterminatedString, "unterminated string")
		}
		c := p.src[p.pos]
		switch {
		case c == '"':
			p.pos++
			if pending {
				return "", p.fail(BadEscape, "an unpaired high surrogate (the JSON-syntax layer, row 6): the string ends while a high half waits for its low half")
			}
			return out.String(), nil
		case c == '\\':
			if p.pos+1 >= len(p.src) {
				return "", p.failAt(UnterminatedEscape, p.pos+1, "unterminated escape")
			}
			esc := p.src[p.pos+1]
			p.pos += 2
			if esc == 'u' {
				u, err := p.parseHex4()
				if err != nil {
					return "", err
				}
				cls := noSur
				switch {
				case u >= 0xD800 && u <= 0xDBFF:
					cls = hiSur
				case u >= 0xDC00 && u <= 0xDFFF:
					cls = loSur
				}
				if err := unit(rune(u), cls); err != nil {
					return "", err
				}
				continue
			}
			var simple rune
			switch esc {
			case '"':
				simple = '"'
			case '\\':
				simple = '\\'
			case '/':
				simple = '/'
			case 'b':
				simple = '\b'
			case 'f':
				simple = '\f'
			case 'n':
				simple = '\n'
			case 'r':
				simple = '\r'
			case 't':
				simple = '\t'
			default:
				return "", p.fail(BadEscape, "unknown escape '\\"+string(esc)+"'")
			}
			if err := unit(simple, noSur); err != nil {
				return "", err
			}
		case c < 0x20:
			return "", p.fail(BadEscape, "a raw control character U+"+hex4(uint16(c))+" inside a string (the JSON-syntax layer, row 5): it must be escaped, and a conformant encoder escapes it")
		default:
			r, size := utf8.DecodeRuneInString(p.src[p.pos:])
			if r == utf8.RuneError && size <= 1 {
				return "", p.fail(BadEscape, "a byte sequence that is not UTF-8 inside a string (the JSON-syntax layer, row 6): text is well-formed Unicode, and an unpaired surrogate has no encoding")
			}
			p.pos += size
			if err := unit(r, noSur); err != nil {
				return "", err
			}
		}
	}
}

func hex4(u uint16) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[u>>12], digits[(u>>8)&0xF], digits[(u>>4)&0xF], digits[u&0xF]})
}

// parseHex4 reads the four hex digits of a \u escape, p.pos just past the `\u`. The
// reference counts the units left in UTF-16: fewer than four is
// TruncatedUnicodeEscape, and four or more with one that is not a hex digit is
// BadHexDigit, both refused at the first digit's position. Counting bytes would
// call `\u0é0` a bad digit where the reference, seeing three units, calls it
// truncated.
func (p *parser[V]) parseHex4() (uint16, Refusal) {
	units := 0
	for i := p.pos; i < len(p.src) && units < 4; {
		r, size := utf8.DecodeRuneInString(p.src[i:])
		units += utf16.RuneLen(r)
		i += size
	}
	if units < 4 {
		return 0, p.fail(TruncatedUnicodeEscape, "truncated \\u escape")
	}
	var value uint16
	for i := 0; i < 4; i++ {
		b := p.src[p.pos+i]
		var digit byte
		switch {
		case isDigit(b):
			digit = b - '0'
		case b >= 'a' && b <= 'f':
			digit = b - 'a' + 10
		case b >= 'A' && b <= 'F':
			digit = b - 'A' + 10
		default:
			return 0, p.fail(BadHexDigit, "bad hex digit in a \\u escape")
		}
		value = value*16 + uint16(digit)
	}
	p.pos += 4
	return value, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func (p *parser[V]) takeDigits() {
	for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
		p.pos++
	}
}

// scanNumber is the reference's token scan: an optional sign, integer digits, an
// optional fraction, an optional exponent — each part taken as far as its characters
// run, whether or not the grammar is then met. The token ends where no part
// continues, so `1e5-0` is the token `1e5` and then trailing text, and `01` is one
// token the grammar refuses. Answers whether the token is a FLOAT token (a point or
// an exponent).
func (p *parser[V]) scanNumber() (isFloat bool) {
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
	}
	p.takeDigits()
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		p.takeDigits()
		isFloat = true
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		p.takeDigits()
		isFloat = true
	}
	return isFloat
}

// parseNumber reads one number token, held to the RFC 8259 grammar before the
// platform reader (row 3). Asking strconv.ParseFloat "is this a number" asks about
// Go, not about this format: it accepts +1, .5, 5., 01, NaN and inf, none of which
// RFC 8259 permits. Then the two VALUE rules of row 7: a value outside the finite
// double range is refused, and an integer token past 2^53 is read only when it is
// the canonical layout of the double it reads as — judged on the TOKEN, since
// 2^53 + 1 reads as 2^53. Every refusal is at the END of the token, where the
// reference's position stands once the token is scanned.
func (p *parser[V]) parseNumber() (V, Refusal) {
	var zero V
	start := p.pos
	isFloat := p.scanNumber()
	slice := p.src[start:p.pos]
	if !IsRFC8259Number(slice) {
		return zero, p.fail(MalformedNumber, "malformed number: '"+slice+"' is not in the RFC 8259 grammar")
	}
	n, err := strconv.ParseFloat(slice, 64)
	if err != nil {
		var numErr *strconv.NumError
		// An underflow reads as the nearest double (zero or a subnormal), as every
		// other host's reader reads it; only an overflow, which answers an infinity,
		// is outside the finite range.
		if !(errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) && !math.IsInf(n, 0)) {
			if math.IsInf(n, 0) {
				return zero, p.fail(MalformedNumber, "malformed number: '"+slice+"' is outside the finite double range")
			}
			return zero, p.fail(MalformedNumber, "malformed number: '"+slice+"'")
		}
	}
	if math.IsInf(n, 0) {
		return zero, p.fail(MalformedNumber, "malformed number: '"+slice+"' is outside the finite double range")
	}
	if !isFloat && !int53SafeOrCanonical(slice, n) {
		return zero, p.fail(MalformedNumber, "malformed number: the integer '"+slice+"' is past 2^53 and is not the canonical layout of a double, so it cannot round-trip")
	}
	return p.build.Number(n, !isFloat), nil
}

// int53SafeOrCanonical is an integer token within 2^53 in magnitude, or one past it
// that is the canonical float layout of n, the double it reads as. Every other
// integer token past 2^53 is refused (row 7).
func int53SafeOrCanonical(slice string, n float64) bool {
	digits := strings.TrimPrefix(slice, "-")
	safe := len(digits) < 16 || (len(digits) == 16 && digits <= "9007199254740992")
	return safe || FormatNumber(n) == slice
}

// IsRFC8259Number is the RFC 8259 number production, exactly:
//
//	number = [ "-" ] int [ frac ] [ exp ]
//	int    = "0" / ( digit1-9 *DIGIT )
//	frac   = "." 1*DIGIT
//	exp    = ("e" / "E") [ "+" / "-" ] 1*DIGIT
//
// Written out rather than delegated, because delegating is the defect: every
// platform's float reader accepts a different superset, so a host that reaches for
// one accepts a set that is a property of its runtime rather than of JSON.
func IsRFC8259Number(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}

func (p *parser[V]) parseObject() (V, Refusal) {
	var zero V
	if err := p.expect('{'); err != nil {
		return zero, err
	}
	p.skipWS()
	var members []Pair[V]
	// The names seen so far: a set keeps the row-1 refusal linear in width, where
	// scanning every earlier member made a wide object quadratic.
	seen := map[string]struct{}{}
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return p.build.Object(members), nil
	}
	for {
		p.skipWS()
		if p.limits.ObjectMembers > 0 && len(members) >= p.limits.ObjectMembers {
			return zero, p.breach(ObjectMembers, p.limits.ObjectMembers)
		}
		keyAt := p.pos
		key, err := p.parseString()
		if err != nil {
			return zero, err
		}
		p.skipWS()
		if err := p.expect(':'); err != nil {
			return zero, err
		}
		value, err := p.parseValue()
		if err != nil {
			return zero, within(err, Key(key))
		}
		// Row 1 — refused when the member is accumulated, after its value is read, at
		// the repeated name's opening quote. It changes what a document MEANS rather
		// than whether it is accepted: hosts kept the first or the last occurrence, so
		// identical bytes read as different documents with no error raised anywhere.
		if _, dup := seen[key]; dup {
			return zero, p.failAt(DuplicateMember, keyAt, "the member '"+key+"' appears more than once in one object (the JSON-syntax layer, row 1)")
		}
		seen[key] = struct{}{}
		members = append(members, Pair[V]{Key: key, Value: value})
		p.skipWS()
		switch {
		case p.pos < len(p.src) && p.src[p.pos] == ',':
			p.pos++
		case p.pos < len(p.src) && p.src[p.pos] == '}':
			p.pos++
			return p.build.Object(members), nil
		default:
			return zero, p.fail(ExpectedToken, "expected ',' or '}'")
		}
	}
}

func (p *parser[V]) parseArray() (V, Refusal) {
	var zero V
	if err := p.expect('['); err != nil {
		return zero, err
	}
	p.skipWS()
	var items []V
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return p.build.Array(items), nil
	}
	for {
		// Where the next item would start, before a character of it is read.
		if p.limits.ArrayLength > 0 && len(items) >= p.limits.ArrayLength {
			p.skipWS()
			return zero, p.breach(ArrayLength, p.limits.ArrayLength)
		}
		index := len(items)
		item, err := p.parseValue()
		if err != nil {
			return zero, within(err, Index(index))
		}
		items = append(items, item)
		p.skipWS()
		switch {
		case p.pos < len(p.src) && p.src[p.pos] == ',':
			p.pos++
		case p.pos < len(p.src) && p.src[p.pos] == ']':
			p.pos++
			return p.build.Array(items), nil
		default:
			return zero, p.fail(ExpectedToken, "expected ',' or ']'")
		}
	}
}
