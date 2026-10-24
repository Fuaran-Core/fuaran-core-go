package decimal

import (
	"strings"
	"testing"
)

// The seven rounding modes over the well-known table (the one java.math.RoundingMode
// documents), at scale 0 — every mode, both signs, a tie on an even and an odd
// neighbour, and values either side of the half.
func TestRoundingModesTable(t *testing.T) {
	inputs := []string{"5.5", "2.5", "1.6", "1.1", "1.0", "-1.0", "-1.1", "-1.6", "-2.5", "-5.5"}
	want := map[RoundingMode][]string{
		RoundUp:       {"6", "3", "2", "2", "1", "-1", "-2", "-2", "-3", "-6"},
		RoundDown:     {"5", "2", "1", "1", "1", "-1", "-1", "-1", "-2", "-5"},
		RoundCeiling:  {"6", "3", "2", "2", "1", "-1", "-1", "-1", "-2", "-5"},
		RoundFloor:    {"5", "2", "1", "1", "1", "-1", "-2", "-2", "-3", "-6"},
		RoundHalfUp:   {"6", "3", "2", "1", "1", "-1", "-1", "-2", "-3", "-6"},
		RoundHalfDown: {"5", "2", "2", "1", "1", "-1", "-1", "-2", "-2", "-5"},
		RoundHalfEven: {"6", "2", "2", "1", "1", "-1", "-1", "-2", "-2", "-6"},
	}
	if len(want) != len(RoundingModes) {
		t.Fatalf("the table covers %d modes; the vocabulary has %d", len(want), len(RoundingModes))
	}
	for mode, outs := range want {
		if _, ok := ParseRoundingMode(string(mode)); !ok {
			t.Errorf("%s is not parsed", mode)
		}
		for i, in := range inputs {
			got, ok := Quantize(mode, 0, in)
			if !ok || got != outs[i] {
				t.Errorf("%s(%s) = %q, want %q", mode, in, got, outs[i])
			}
		}
	}
	if _, ok := ParseRoundingMode("half-odd"); ok {
		t.Fatalf("an unknown mode parsed")
	}
}

// A quotient is ONE rounding of the exact value: 2/3 at 4 places, -2/3, a zero
// divisor (null), and a result that rounds to zero carries no sign.
func TestDivide(t *testing.T) {
	cases := []struct {
		mode  RoundingMode
		a, b  string
		scale int
		want  string
		null  bool
	}{
		{RoundHalfEven, "2", "3", 4, "0.6667", false},
		{RoundDown, "2", "3", 4, "0.6666", false},
		{RoundHalfEven, "-2", "3", 4, "-0.6667", false},
		{RoundCeiling, "-2", "3", 0, "0", false},
		{RoundHalfUp, "1", "0", 2, "", true},
		{RoundHalfEven, "1.25", "0.5", 0, "2", false},
		{RoundHalfEven, "0.125", "1", 2, "0.12", false},
	}
	for _, c := range cases {
		got, isNull, ok := Divide(c.mode, c.scale, c.a, c.b)
		if !ok || isNull != c.null || got != c.want {
			t.Errorf("%s/%s @%d %s = (%q, null=%v, ok=%v), want %q null=%v", c.a, c.b, c.scale, c.mode, got, isNull, ok, c.want, c.null)
		}
	}
}

func TestCanonicalAndArithmetic(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"007.50", "7.5"}, {"-0.0", "0"}, {"0", "0"}, {"-12", "-12"}, {"1.", ""}, {".5", ""}, {"+1", ""}, {"1e3", ""},
	} {
		got, ok := Canonical(c.in)
		if (c.want == "") == ok || got != c.want {
			t.Errorf("Canonical(%q) = %q, %v", c.in, got, ok)
		}
	}
	if sum, _ := Add("0.1", "0.2"); sum != "0.3" {
		t.Errorf("0.1 + 0.2 = %s", sum)
	}
	if order, _ := Compare("1.50", "1.5"); order != 0 {
		t.Errorf("1.50 vs 1.5 = %d", order)
	}
	if f, ok := ToFloat("1e400"); ok || f != 0 {
		t.Errorf("1e400 is not decimal text")
	}
	if _, ok := ToFloat("1" + strings.Repeat("0", 309)); ok {
		t.Errorf("a decimal past the float range is refused, not an infinity")
	}
	if d, ok := OfFloat(1e21); !ok || d != "1000000000000000000000" {
		t.Errorf("OfFloat(1e21) = %s", d)
	}
	if n, ok := TruncateToInt64("-7.9"); !ok || n != -7 {
		t.Errorf("TruncateToInt64(-7.9) = %d", n)
	}
}
