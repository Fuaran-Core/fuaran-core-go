package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Fuaran-Core/fuaran-core-go/decode"
	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// The premise legs certify the spine's runtime premises against the probes the
// reference emits (conformance/premises/): each family is the probe a host-premise
// row of the reference's claims ladder names, and reproducing every vector
// discharges that premise on this module's own runtime (the reference's
// docs/host-premises.md). Each leg asserts it ran exactly the count the family's
// manifest declares.

func family(t testing.TB, file, id string) (json.Value, int) {
	t.Helper()
	doc := read(t, "premises/"+file)
	want := declared(t, "premises", id)
	if got := len(vectors(t, doc)); got != want {
		t.Fatalf("%s: %d vectors, the manifest declares %d", file, got, want)
	}
	return doc, want
}

// ─── premises/string-model.json ──────────────────────────────────────────────

// utf8OfUnits is the mapping by which a sequence of UTF-16 code units reaches a Go
// string: a well-formed sequence becomes its UTF-8 bytes, and the first unpaired
// surrogate refuses at its index. unicode/utf16.Decode would substitute U+FFFD, so
// the pairing is checked here.
func utf8OfUnits(units []uint16) (bytes []byte, index int, unit uint16, ok bool) {
	var out []byte
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
				r := 0x10000 + (rune(u)-0xD800)<<10 + (rune(units[i+1]) - 0xDC00)
				out = utf8.AppendRune(out, r)
				i++
				continue
			}
			return nil, i, u, false
		case u >= 0xDC00 && u <= 0xDFFF:
			return nil, i, u, false
		default:
			out = utf8.AppendRune(out, rune(u))
		}
	}
	return out, 0, 0, true
}

func TestPremiseStringModel(t *testing.T) {
	doc, want := family(t, "string-model.json", "premiseStringModel")
	ran := 0
	for _, v := range vectors(t, doc) {
		id, expected := s(t, v, "id"), get(t, v, "expected")
		switch {
		case strings.HasPrefix(id, "encode/"):
			var units []uint16
			for _, u := range arr(t, v, "units") {
				units = append(units, uint16(u.(json.Number)))
			}
			bytes, index, unit, ok := utf8OfUnits(units)
			if ok {
				if got := hex.EncodeToString(bytes); got != s(t, expected, "utf8") {
					t.Errorf("%s: %s, want %s", id, got, s(t, expected, "utf8"))
				}
			} else {
				refused := get(t, expected, "refused")
				if index != int(n(t, refused, "index")) || unit != uint16(n(t, refused, "unit")) {
					t.Errorf("%s: refused at %d (unit %04X), want %v", id, index, unit, json.Render(refused))
				}
			}
		case strings.HasPrefix(id, "parse/"):
			v, r := json.Parse(s(t, v, "text"))
			switch x := v.(type) {
			case nil:
				if got := refusalClass(r); got != s(t, expected, "refused") {
					t.Errorf("%s: %s, want %s", id, got, s(t, expected, "refused"))
				}
			case json.String:
				if got := hex.EncodeToString([]byte(x)); got != s(t, expected, "utf8") {
					t.Errorf("%s: %s, want %s", id, got, s(t, expected, "utf8"))
				}
			case json.Object:
				if len(x) != 1 {
					t.Fatalf("%s: one member", id)
				}
				if got := hex.EncodeToString([]byte(x[0].Key)); got != s(t, get(t, expected, "key"), "utf8") {
					t.Errorf("%s: key %s", id, got)
				}
			default:
				t.Errorf("%s: read %s", id, json.Render(v))
			}
		default:
			t.Fatalf("%s: an unknown probe", id)
		}
		ran++
	}
	if ran != want {
		t.Fatalf("ran %d, want %d", ran, want)
	}
}

// ─── premises/float-digits.json ──────────────────────────────────────────────

func TestPremiseFloatDigits(t *testing.T) {
	doc, want := family(t, "float-digits.json", "premiseFloatDigits")
	ran := 0
	for _, v := range vectors(t, doc) {
		id := s(t, v, "id")
		bits, err := strconv.ParseUint(s(t, v, "bits"), 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		canonical := s(t, get(t, v, "expected"), "canonical")
		f := math.Float64frombits(bits)
		if got := json.FormatFiniteDouble(f); got != canonical {
			t.Errorf("%s: written %s, want %s", id, got, canonical)
		}
		// Read back: the same double, the negative zero reading as zero.
		back, r := json.Parse(canonical)
		x, ok := back.(json.Number)
		if r != nil || !ok {
			t.Fatalf("%s: %s read back as %v %v", id, canonical, back, r)
		}
		wanted := bits
		if f == 0 {
			wanted = 0
		}
		if math.Float64bits(float64(x)) != wanted {
			t.Errorf("%s: read back %016x, want %016x", id, math.Float64bits(float64(x)), wanted)
		}
		ran++
	}
	if ran != want {
		t.Fatalf("ran %d, want %d", ran, want)
	}
}

// ─── premises/int53.json ─────────────────────────────────────────────────────

func TestPremiseInt53(t *testing.T) {
	doc, want := family(t, "int53.json", "premiseInt53")
	ran := 0
	for _, v := range vectors(t, doc) {
		id, expected := s(t, v, "id"), get(t, v, "expected")
		value, r := decode.ReadValue(s(t, v, "token"), json.Strict)
		switch x := value.(type) {
		case decode.Int:
			if float64(x) != n(t, expected, "int") {
				t.Errorf("%s: int %d", id, x)
			}
		case decode.Float:
			if got := fmt.Sprintf("%016x", math.Float64bits(float64(x))); got != s(t, expected, "float") {
				t.Errorf("%s: float %s, want %s", id, got, s(t, expected, "float"))
			}
		case nil:
			if got := refusalClass(r); got != s(t, expected, "refused") {
				t.Errorf("%s: %s, want %s", id, got, s(t, expected, "refused"))
			}
		default:
			t.Errorf("%s: read %v", id, value)
		}
		ran++
	}
	if ran != want {
		t.Fatalf("ran %d, want %d", ran, want)
	}
}

// ─── premises/sha256.json ────────────────────────────────────────────────────

func TestPremiseSha256(t *testing.T) {
	doc, want := family(t, "sha256.json", "premiseSha256")
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input := s(t, v, "id"), get(t, v, "input")
		unit, err := hex.DecodeString(s(t, input, "hex"))
		if err != nil {
			t.Fatal(err)
		}
		message := make([]byte, 0, len(unit)*int(n(t, input, "repeat")))
		for i := 0; i < int(n(t, input, "repeat")); i++ {
			message = append(message, unit...)
		}
		sum := sha256.Sum256(message)
		if got := hex.EncodeToString(sum[:]); got != s(t, get(t, v, "expected"), "sha256") {
			t.Errorf("%s: %s", id, got)
		}
		ran++
	}
	if ran != want {
		t.Fatalf("ran %d, want %d", ran, want)
	}
}

// ─── premises/recursion-depth.json ───────────────────────────────────────────

// The reader descends once per level and refuses past 512 before it descends, so a
// document never overflows a stack that holds 512 levels; a goroutine's stack grows
// on demand up to the runtime's maximum (1 GB on 64-bit), which holds it with a wide
// margin. The probes run on an ordinary goroutine for that reason: the stack is the
// runtime's, stated here rather than inherited unknowingly.
func TestPremiseRecursionDepth(t *testing.T) {
	doc, want := family(t, "recursion-depth.json", "premiseRecursionDepth")
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input, expected := s(t, v, "id"), get(t, v, "input"), get(t, v, "expected")
		depth := int(n(t, input, "depth"))
		text := strings.Repeat(s(t, input, "open"), depth) + s(t, input, "inner") + strings.Repeat(s(t, input, "close"), depth)
		value, r := json.Parse(text)
		if r == nil {
			rendered := json.Render(value)
			if rendered != text {
				t.Errorf("%s: the canonical rendering differs", id)
			}
			sum := sha256.Sum256([]byte(rendered))
			if got := hex.EncodeToString(sum[:]); got != s(t, expected, "accepted") {
				t.Errorf("%s: %s", id, got)
			}
		} else if got := refusalClass(r); got != s(t, expected, "refused") {
			t.Errorf("%s: %s, want %s", id, got, s(t, expected, "refused"))
		}
		ran++
	}
	if ran != want {
		t.Fatalf("ran %d, want %d", ran, want)
	}
}
