package json

import (
	"sort"
	"strings"
	"unicode/utf16"
)

// EscapeString quotes and escapes a string the one canonical way: `"` and `\`
// escaped, every control character U+0000–U+001F as a lower-case `\u00xx` with no
// short form, and nothing else — `/` and non-ASCII pass through literally. The
// result includes the surrounding quotes.
//
// Byte-wise iteration is safe here: every byte of a multi-byte UTF-8 sequence is
// >= 0x80, so only ASCII bytes can match an escape case. Certified against the
// reference's escape/string-escape.json.
func EscapeString(s string) string {
	const hex = "0123456789abcdef"
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == '"':
			sb.WriteString(`\"`)
		case b == '\\':
			sb.WriteString(`\\`)
		case b < 0x20:
			sb.WriteString(`\u00`)
			sb.WriteByte(hex[b>>4])
			sb.WriteByte(hex[b&0xF])
		default:
			sb.WriteByte(b)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// OrdinalLess reports whether a sorts before b in UTF-16 code-unit order — the
// reference's StringComparer.Ordinal, the member order every host sorts by.
//
// "Ordinal" means UTF-16 code-unit order, and the distinction is observable exactly
// above the BMP. An astral key encodes to a surrogate pair beginning in
// U+D800–U+DBFF, so under UTF-16 it sorts BELOW a key starting in U+E000–U+FFFF
// that it sorts ABOVE under code-point or UTF-8-byte comparison. Go's string order
// is byte-wise over UTF-8, which equals code-point order — so it disagrees with the
// canonical order on precisely the objects that carry both kinds of key, and one
// document then canonicalises to two different byte sequences, and two different
// digests, depending on which host encoded it.
//
// The common case is paid for: a key of ASCII bytes needs no conversion, so the
// comparison falls back to byte order without allocating.
func OrdinalLess(a, b string) bool { return OrdinalCompare(a, b) < 0 }

// OrdinalCompare is the three-way form of OrdinalLess: -1, 0 or 1.
func OrdinalCompare(a, b string) int {
	if isASCII(a) && isASCII(b) {
		return strings.Compare(a, b)
	}
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// SortKeys orders object keys as the canonical encoder requires: ordinally.
func SortKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool { return OrdinalLess(keys[i], keys[j]) })
}

// Rendered is one (name, rendered value) pair RenderObject takes.
type Rendered struct {
	Key   string
	Value string
}

// RenderObject is an object from (name, rendered value) pairs: names sorted
// ordinally, no whitespace. Each value is passed through verbatim. The slice is
// sorted in place.
func RenderObject(fields []Rendered) string {
	sort.SliceStable(fields, func(i, j int) bool { return OrdinalLess(fields[i].Key, fields[j].Key) })
	var out strings.Builder
	out.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(EscapeString(f.Key))
		out.WriteByte(':')
		out.WriteString(f.Value)
	}
	out.WriteByte('}')
	return out.String()
}

// RenderArray is an array from rendered items, in source order.
func RenderArray(items []string) string {
	return "[" + strings.Join(items, ",") + "]"
}

// Render is a value's canonical bytes: names sorted ordinally, the canonical number
// form, the canonical escapes. For a canonical input, Render(Parse(x)) == x.
func Render(v Value) string {
	switch x := v.(type) {
	case Null:
		return "null"
	case Bool:
		if x {
			return "true"
		}
		return "false"
	case Number:
		return FormatNumber(float64(x))
	case String:
		return EscapeString(string(x))
	case Array:
		items := make([]string, len(x))
		for i, item := range x {
			items[i] = Render(item)
		}
		return RenderArray(items)
	case Object:
		fields := make([]Rendered, len(x))
		for i, m := range x {
			fields[i] = Rendered{Key: m.Key, Value: Render(m.Value)}
		}
		return RenderObject(fields)
	}
	panic("json.Render: not a Value")
}
