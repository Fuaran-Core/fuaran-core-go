// Package json is canonical JSON: the value model, the strict reader every wire's
// JSON layer cites, and the canonical writer.
//
// Every Fuaran wire is JSON below its own vocabulary, and the reference states once
// what that JSON is (its "JSON-syntax layer"). A conformant document is one RFC 8259
// text, read strictly; each row below is a refusal of the TEXT, and no conformant
// encoder can reach one:
//
//	| Row | Input class                                                       | Refused as                                                      |
//	| 1   | a repeated member name in one object, compared after unescaping   | DuplicateMember                                                 |
//	| 2   | content after the root value                                      | TrailingCharacters                                              |
//	| 3   | a number outside the RFC 8259 grammar                             | MalformedNumber (UnexpectedChar where the token cannot start a value: +1, .5) |
//	| 4   | a bare NaN / Infinity / -Infinity                                 | UnexpectedChar (MalformedNumber for -Infinity, whose sign starts a number)   |
//	| 5   | a raw C0 control character inside a string                        | BadEscape                                                       |
//	| 6   | an unpaired surrogate, raw or escaped                             | BadEscape                                                       |
//	| 7   | an overflowing exponent (1e999), or an integer token past 2^53 that is not the canonical layout of the double it reads as | MalformedNumber |
//
// A quoted sentinel ("NaN") is a string: what a typed float slot makes of it is the
// citing wire's question, not this layer's.
//
// Limits are a mechanism, never values. Every read takes a Limits record and
// enforces each bound ON THE WAY DOWN, before the container, code point, item or
// member it guards is read, so nothing past a bound is ever read or built. A breach
// is a *LimitBreach at the path of the container it was raised in, and is never
// reported as malformed text; the first fault in document order wins, whichever
// kind it is. The values belong to each wire: a host passes its specification's
// numbers and maps the breach to its own envelope. Strict bounds the depth at 512,
// the reader's own stack guard, and nothing else.
//
// A Go string is a sequence of bytes, so this reader also meets the one input the
// reference's UTF-16 string model cannot carry: ill-formed UTF-8. A byte sequence
// that is not UTF-8 inside a string literal is refused as BadEscape, the byte-level
// form of row 6 (an unpaired surrogate encoded WTF-8 style is exactly such a
// sequence); outside a string every non-ASCII byte is already UnexpectedChar.
//
// The model and its answers are certified against the reference's refusals/ (the
// json vectors), decode/ and premises/ families.
package json

// Value is a parsed JSON value: one of Null, Bool, Number, String, Array or Object.
// The set is closed by the unexported method.
type Value interface{ isValue() }

// Null is the JSON null.
type Null struct{}

// Bool is a JSON boolean.
type Bool bool

// Number is a JSON number, read as an IEEE-754 double.
//
// That is conformant rather than a limitation, and the reasoning is worth keeping
// because the obvious remedy is unnecessary: integer identity on the wire stops at
// ±2^53, and the bound was chosen precisely because the integer and float canonical
// layouts AGREE over that range — a double holds every integer in it, and the
// fixed-point window of the number form (base-10 exponent <= 16) covers every one
// of them, so re-encoding produces the integer spelling with no integer type in
// play. Past the bound row 7 admits only a token that IS the canonical layout of
// its double, so there is nothing an int64 arm could preserve that a host is
// required to preserve.
type Number float64

// String is a JSON string, well-formed UTF-8 by construction (rows 5 and 6).
type String string

// Array is a JSON array, in document order.
type Array []Value

// Object is a JSON object. It keeps parse order (a reader looks members up by
// name; Render re-sorts on emit). A repeated member name never reaches this type:
// row 1 refuses it at the reader.
type Object []Member

// Member is one object member.
type Member struct {
	Key   string
	Value Value
}

func (Null) isValue()   {}
func (Bool) isValue()   {}
func (Number) isValue() {}
func (String) isValue() {}
func (Array) isValue()  {}
func (Object) isValue() {}

// Field is member lookup by name, in any member order. A repeated name cannot
// occur — row 1 refuses it — so the first match is the only match. Nil on a
// non-object or an absent member.
func (o Object) Field(key string) Value {
	for _, m := range o {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

// Field is Object.Field on any value: nil unless v is an object holding key.
func Field(v Value, key string) Value {
	if o, ok := v.(Object); ok {
		return o.Field(key)
	}
	return nil
}
