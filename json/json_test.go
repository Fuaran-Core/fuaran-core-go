package json

import "testing"

func TestEscapeString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "\"\""},
		{"plain", "\"plain\""},
		{"quote \" and backslash \\", "\"quote \\\" and backslash \\\\\""},
		// Control characters escape as lower-case four-digit \u00xx.
		{"tab\tnewline\ncr\rnul\x00", "\"tab\\u0009newline\\u000acr\\u000dnul\\u0000\""},
		// '/' and non-ASCII pass through literally.
		{"a/b — héllo €", "\"a/b — héllo €\""},
	}
	for _, c := range cases {
		if got := EscapeString(c.in); got != c.want {
			t.Errorf("EscapeString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestFormatFiniteDouble pins the canonical number form against the divergence-zone
// values of the shared wire corpus (its metric-float-* fixtures) plus a spread of
// fixed-point / scientific boundary cases.
func TestFormatFiniteDouble(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1e21, "1E+21"},
		{1e-7, "1E-07"},
		{0.30000000000000004, "0.30000000000000004"},
		{1.2345678901234568e17, "1.2345678901234568E+17"},
		{5e-324, "5E-324"},
		{-0.0, "0"},
		{0, "0"},
		{1, "1"},
		{100, "100"},
		{123.5, "123.5"},
		{0.5, "0.5"},
		{0.0001, "0.0001"},
		{-42.25, "-42.25"},
		{0.00001, "1E-05"},
	}
	for _, c := range cases {
		if got := FormatFiniteDouble(c.in); got != c.want {
			t.Errorf("FormatFiniteDouble(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOrdinalOrderIsUTF16(t *testing.T) {
	// An astral key (a surrogate pair, D83D DE00) sorts BELOW U+FF10 under UTF-16
	// and above it under code-point order.
	astral, bmp := "\U0001F600", "０"
	if !OrdinalLess(astral, bmp) {
		t.Fatalf("UTF-16 ordinal order puts the astral key first")
	}
	if astral < bmp {
		t.Fatalf("the probe is vacuous: Go's byte order already agrees")
	}
	keys := []string{bmp, astral, "a"}
	SortKeys(keys)
	if keys[0] != "a" || keys[1] != astral || keys[2] != bmp {
		t.Fatalf("SortKeys = %q", keys)
	}
}

func TestReaderRows(t *testing.T) {
	refused := func(input string, kind ErrorKind) {
		t.Helper()
		_, r := Parse(input)
		e, ok := r.(*Error)
		if !ok {
			t.Fatalf("%q: expected %s, got %v", input, kind, r)
		}
		if e.Kind != kind {
			t.Fatalf("%q: expected %s, got %s (%s)", input, kind, e.Kind, e.Message)
		}
	}
	refused(`{"a":1,"a":2}`, DuplicateMember)
	refused(`{} x`, TrailingCharacters)
	refused(`01`, MalformedNumber)
	refused(`+1`, UnexpectedChar)
	refused(`NaN`, UnexpectedChar)
	refused(`-Infinity`, MalformedNumber)
	refused("\"a\tb\"", BadEscape)
	refused(`"\ud800"`, BadEscape)
	refused(`"\udc00"`, BadEscape)
	refused(`1e999`, MalformedNumber)
	refused(`9007199254740993`, MalformedNumber)
	refused("\"\xed\xa0\x80\"", BadEscape) // a WTF-8 lone surrogate
	refused(`"\u12`, TruncatedUnicodeEscape)
	refused(`"\u12g4"`, BadHexDigit)
	refused(`"abc`, UnterminatedString)
	refused(`"abc\`, UnterminatedEscape)
	refused(`[1,`, UnexpectedEndOfInput)
	refused(`[1`, ExpectedToken)
	refused(`[1 2]`, ExpectedToken)
	refused(`tru`, ExpectedToken)

	accepted := func(input, canonical string) {
		t.Helper()
		v, r := Parse(input)
		if r != nil {
			t.Fatalf("%q: %v", input, r)
		}
		if got := Render(v); got != canonical {
			t.Fatalf("%q: rendered %s, want %s", input, got, canonical)
		}
	}
	accepted(`{"b":1,"a":[true,false,null,"xé"]}`, `{"a":[true,false,null,"xé"],"b":1}`)
	accepted(`1e-400`, `0`)
	accepted(`9007199254740992`, `9007199254740992`)
	accepted(`1E+21`, `1E+21`)
	accepted(`"😀"`, `"😀"`)
	accepted(` 2.50 `, `2.5`)
}

func TestLimitsAreEnforcedOnTheWayDown(t *testing.T) {
	_, r := Read(`[[1]]`, DepthOnly(1))
	b, ok := r.(*LimitBreach)
	if !ok || b.Limit != Depth || len(b.Path) != 1 || b.Path[0] != Index(0) {
		t.Fatalf("depth breach: %v", r)
	}
	_, r = Read(`{"k":"abcd"}`, Limits{Depth: 8, StringLength: 3})
	b, ok = r.(*LimitBreach)
	if !ok || b.Limit != StringLength || len(b.Path) != 1 || b.Path[0] != Key("k") {
		t.Fatalf("string breach: %v", r)
	}
	_, r = Read(`[1,2,3]`, Limits{Depth: 8, ArrayLength: 2})
	if b, ok = r.(*LimitBreach); !ok || b.Limit != ArrayLength || len(b.Path) != 0 {
		t.Fatalf("array breach: %v", r)
	}
	_, r = Read(`{"a":1,"b":2}`, Limits{Depth: 8, ObjectMembers: 1})
	if b, ok = r.(*LimitBreach); !ok || b.Limit != ObjectMembers {
		t.Fatalf("members breach: %v", r)
	}
	_, r = Read(`[1]`, Limits{Depth: 8, DocumentBytes: 2})
	if b, ok = r.(*LimitBreach); !ok || b.Limit != DocumentBytes {
		t.Fatalf("document breach: %v", r)
	}
	// A breach is never reported as malformed text, even when the text is also
	// malformed later: the first fault in document order wins.
	_, r = Read(`[[1]] x`, DepthOnly(1))
	if _, ok = r.(*LimitBreach); !ok {
		t.Fatalf("the breach comes first: %v", r)
	}
}
