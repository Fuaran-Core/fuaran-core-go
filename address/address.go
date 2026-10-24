// Package address is the digest and the content address: SHA-256 as lower-case hex,
// and `sha256:` with the hex SHA-256 of a value's canonical bytes — the digest
// convention every reference vector and record uses.
//
// The digest is the standard library's crypto/sha256, FIPS 180-4 on every input
// length; the host-premise probe premises/sha256.json discharges that on this
// runtime rather than assuming it.
package address

import (
	"crypto/sha256"
	"encoding/hex"
	"math"

	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Prefix names the digest algorithm.
const Prefix = "sha256:"

// SHA256Hex is the SHA-256 of the UTF-8 bytes of payload as 64 lower-case hex digits.
func SHA256Hex(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// Fault is why a value or a text has no content address.
type Fault int

const (
	// NonFinite: the value holds a non-finite number, which has no canonical form.
	NonFinite Fault = iota
	// NotCanonical: the text is not the canonical form of the value it reads as, so
	// its digest would not recompute from that value.
	NotCanonical
)

func (f Fault) Error() string {
	if f == NonFinite {
		return "the value holds a non-finite number, which has no canonical form"
	}
	return "the text is not the canonical form of the value it reads as"
}

// OfBytes is the address of exactly these bytes, which the caller vouches are
// canonical.
func OfBytes(canonical string) string { return Prefix + SHA256Hex(canonical) }

// OfValue is the address of a value: the digest of its canonical bytes.
func OfValue(v json.Value) (string, error) {
	if !finite(v) {
		return "", NonFinite
	}
	return OfBytes(json.Render(v)), nil
}

// OfCanonicalText is the address of a text that must already be canonical: refused
// when it is not, rather than silently re-rendered, because a re-rendered digest
// names bytes the caller never held.
func OfCanonicalText(text string) (string, error) {
	v, r := json.Parse(text)
	if r != nil || json.Render(v) != text {
		return "", NotCanonical
	}
	return OfBytes(text), nil
}

func finite(v json.Value) bool {
	switch x := v.(type) {
	case json.Number:
		return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0)
	case json.Array:
		for _, item := range x {
			if !finite(item) {
				return false
			}
		}
	case json.Object:
		for _, m := range x {
			if !finite(m.Value) {
				return false
			}
		}
	}
	return true
}
