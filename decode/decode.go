// Package decode is the decode error contract: the closed set of decode codes, each
// at the path it names, and the reading combinators a host builds its decoders
// from.
//
// A decode of JSON text has two halves. The text is read by Parse, under a
// json.Limits record: malformed text is InvalidJson at the root, and a passed bound
// is LimitExceeded at the path of the container it was raised in. The value is then
// read through a Cursor, whose every refusal names its code and the path from the
// document root — so a decoder written over it answers exactly as the reference's
// decoder of the same shape does. Certified against the reference's
// decode/decode-rejects.json.
//
// The value model is the reference's null-free one, Value: an integer token within
// int32 is Int, every other number Float, and a null token is refused as malformed
// (json.NullNotRepresentable), as the reference's strict reading refuses it. The
// distinction between the two number cases is the TOKEN's, which is why this layer
// reads through its own value model rather than over json.Value: 2.0 is not an int.
package decode

import (
	"math"
	"strconv"
	"strings"

	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Code is one of the reference's closed set of decode codes.
type Code int

// The codes.
const (
	// InvalidJson: the text is not one well-formed JSON document (at the root).
	InvalidJson Code = iota
	// MissingField: a required member is absent (at the absent member's path).
	MissingField
	// WrongKind: a value of the wrong JSON kind.
	WrongKind
	// UnknownTag: a discriminator names no case the reader knows (at the discriminator).
	UnknownTag
	// OutOfRange: a number outside its declared range.
	OutOfRange
	// UndeclaredMember: a member a closed object does not declare (at that member).
	UndeclaredMember
	// LimitExceeded: a bound passed (at the container it guards).
	LimitExceeded
	// NotAdmitted: a known case this position does not admit (at the discriminator).
	NotAdmitted
	// SchemaFault: the decoder itself is ill-formed (an unresolved reference to a shape).
	SchemaFault
)

var codeNames = [...]string{
	"InvalidJson", "MissingField", "WrongKind", "UnknownTag", "OutOfRange",
	"UndeclaredMember", "LimitExceeded", "NotAdmitted", "SchemaFault",
}

// String is the reference's spelling (InvalidJson, ...).
func (c Code) String() string { return codeNames[c] }

// Error is a refusal: its code, the path from the document root, and a sentence.
type Error struct {
	Code    Code
	Path    []json.PathSeg
	Message string
}

func (e *Error) Error() string { return e.Code.String() + " at " + PathJSON(e.Path) + ": " + e.Message }

// PathJSON renders a path as the reference's vectors carry it: a JSON array of
// member names and item indices.
func PathJSON(path []json.PathSeg) string {
	items := make([]string, len(path))
	for i, seg := range path {
		switch s := seg.(type) {
		case json.Key:
			items[i] = json.EscapeString(string(s))
		case json.Index:
			items[i] = strconv.Itoa(int(s))
		}
	}
	return json.RenderArray(items)
}

// OfRefusal is the decode refusal of a refused read: malformed text is InvalidJson
// at the root, naming the parser's class; a breach is LimitExceeded at its path,
// naming the bound and its declared value.
func OfRefusal(r json.Refusal) *Error {
	switch x := r.(type) {
	case *json.Error:
		return &Error{Code: InvalidJson, Message: x.Error()}
	case *json.LimitBreach:
		return &Error{Code: LimitExceeded, Path: x.Path, Message: x.Error()}
	}
	panic("decode.OfRefusal: not a json.Refusal")
}

// Value is a read value in the reference's null-free model: one of Str, Int, Float,
// Bool, Arr or Obj.
type Value interface{ isValue() }

// Str is a string.
type Str string

// Int is an integer token (no point, no exponent) within int32.
type Int int32

// Float is any other number: a point or an exponent, or an integer past int32.
type Float float64

// Bool is a boolean.
type Bool bool

// Arr is an array.
type Arr []Value

// Obj is an object, in document order.
type Obj []Pair

// Pair is one member of an Obj.
type Pair struct {
	Key   string
	Value Value
}

func (Str) isValue()   {}
func (Int) isValue()   {}
func (Float) isValue() {}
func (Bool) isValue()  {}
func (Arr) isValue()   {}
func (Obj) isValue()   {}

// KindName is the JSON kind, as every decode error names it.
func KindName(v Value) string {
	switch v.(type) {
	case Str:
		return "string"
	case Int:
		return "int"
	case Float:
		return "float"
	case Bool:
		return "bool"
	case Arr:
		return "array"
	case Obj:
		return "object"
	}
	return "unknown"
}

type valueBuilder struct{}

func (valueBuilder) Null() (Value, bool) { return nil, false }
func (valueBuilder) Bool(b bool) Value   { return Bool(b) }
func (valueBuilder) Number(n float64, integerToken bool) Value {
	if integerToken && n >= math.MinInt32 && n <= math.MaxInt32 {
		return Int(int32(n))
	}
	return Float(n)
}
func (valueBuilder) String(s string) Value     { return Str(s) }
func (valueBuilder) Array(items []Value) Value { return Arr(items) }
func (valueBuilder) Object(members []json.Pair[Value]) Value {
	out := make(Obj, len(members))
	for i, m := range members {
		out[i] = Pair{Key: m.Key, Value: m.Value}
	}
	return out
}

// ReadValue reads JSON text under limits into the decode layer's value model,
// keeping the reader's refusal whole, for a codec that projects it its own way.
func ReadValue(input string, limits json.Limits) (Value, json.Refusal) {
	return json.Scan[Value](input, limits, valueBuilder{})
}

// Parse reads JSON text under limits into the decode layer's value model; a refusal
// is the decode contract's (OfRefusal).
func Parse(input string, limits json.Limits) (Value, *Error) {
	v, r := ReadValue(input, limits)
	if r != nil {
		return nil, OfRefusal(r)
	}
	return v, nil
}

// Cursor is a value and the path that reached it. Every reading answers an *Error
// at that path, or at a member or item below it.
type Cursor struct {
	value Value
	path  []json.PathSeg
}

// Root is a cursor at the document root.
func Root(v Value) Cursor { return Cursor{value: v} }

// Value is the value under the cursor.
func (c Cursor) Value() Value { return c.value }

// Path is the path from the document root.
func (c Cursor) Path() []json.PathSeg { return c.path }

func (c Cursor) below(seg json.PathSeg, v Value) Cursor {
	path := make([]json.PathSeg, len(c.path)+1)
	copy(path, c.path)
	path[len(c.path)] = seg
	return Cursor{value: v, path: path}
}

// Fail is a refusal at this cursor's path.
func (c Cursor) Fail(code Code, message string) *Error {
	path := make([]json.PathSeg, len(c.path))
	copy(path, c.path)
	return &Error{Code: code, Path: path, Message: message}
}

func (c Cursor) wrongKind(expected string) *Error {
	return c.Fail(WrongKind, "expected "+expected+", found "+KindName(c.value))
}

// String reads a string.
func (c Cursor) String() (string, *Error) {
	if s, ok := c.value.(Str); ok {
		return string(s), nil
	}
	return "", c.wrongKind("a string")
}

// Int reads an integer token within int32 only: 2.0, 1e3 and an integer past int32
// are refused as WrongKind.
func (c Cursor) Int() (int32, *Error) {
	if i, ok := c.value.(Int); ok {
		return int32(i), nil
	}
	return 0, c.wrongKind("an int")
}

// Float reads any number.
func (c Cursor) Float() (float64, *Error) {
	switch x := c.value.(type) {
	case Int:
		return float64(x), nil
	case Float:
		return float64(x), nil
	}
	return 0, c.wrongKind("a number")
}

// Bool reads a boolean.
func (c Cursor) Bool() (bool, *Error) {
	if b, ok := c.value.(Bool); ok {
		return bool(b), nil
	}
	return false, c.wrongKind("a bool")
}

// IntRange reads an int within [lo, hi]; outside it is OutOfRange.
func (c Cursor) IntRange(lo, hi int32) (int32, *Error) {
	i, err := c.Int()
	if err != nil {
		return 0, err
	}
	if i < lo || i > hi {
		return 0, c.Fail(OutOfRange, strconv.Itoa(int(i))+" is outside ["+strconv.Itoa(int(lo))+", "+strconv.Itoa(int(hi))+"]")
	}
	return i, nil
}

// Items reads the items of an array, each at its index.
func (c Cursor) Items() ([]Cursor, *Error) {
	arr, ok := c.value.(Arr)
	if !ok {
		return nil, c.wrongKind("an array")
	}
	out := make([]Cursor, len(arr))
	for i, v := range arr {
		out[i] = c.below(json.Index(i), v)
	}
	return out, nil
}

// ItemsAtMost reads the items of an array of at most max; more is LimitExceeded at
// the array.
func (c Cursor) ItemsAtMost(max int) ([]Cursor, *Error) {
	items, err := c.Items()
	if err != nil {
		return nil, err
	}
	if len(items) > max {
		return nil, c.Fail(LimitExceeded, strconv.Itoa(len(items))+" items where at most "+strconv.Itoa(max)+" are admitted")
	}
	return items, nil
}

func (c Cursor) members() (Obj, *Error) {
	if o, ok := c.value.(Obj); ok {
		return o, nil
	}
	return nil, c.wrongKind("an object")
}

// Optional reads an optional member: ok false when absent.
func (c Cursor) Optional(name string) (Cursor, bool, *Error) {
	members, err := c.members()
	if err != nil {
		return Cursor{}, false, err
	}
	for _, m := range members {
		if m.Key == name {
			return c.below(json.Key(m.Key), m.Value), true, nil
		}
	}
	return Cursor{}, false, nil
}

// Field reads a required member; absent is MissingField at the absent member's path.
func (c Cursor) Field(name string) (Cursor, *Error) {
	at, ok, err := c.Optional(name)
	if err != nil {
		return Cursor{}, err
	}
	if !ok {
		return Cursor{}, c.below(json.Key(name), nil).Fail(MissingField, "the required member '"+name+"' is absent")
	}
	return at, nil
}

// Closed reads a closed object: the first member, in document order, that declared
// does not name is UndeclaredMember at that member. The reference checks this
// before it reads any declared member.
func (c Cursor) Closed(declared []string) *Error {
	members, err := c.members()
	if err != nil {
		return err
	}
	for _, m := range members {
		if !contains(declared, m.Key) {
			return c.below(json.Key(m.Key), m.Value).Fail(UndeclaredMember, "the member '"+m.Key+"' is not declared here")
		}
	}
	return nil
}

// Tag reads the discriminator of a tagged union off the SAME object as its case:
// absent is MissingField and a non-string WrongKind, both at the key, and a tag
// outside known is UnknownTag there.
func (c Cursor) Tag(key string, known []string) (string, *Error) {
	at, err := c.Field(key)
	if err != nil {
		return "", err
	}
	tag, err := at.String()
	if err != nil {
		return "", err
	}
	if !contains(known, tag) {
		return "", at.Fail(UnknownTag, "'"+tag+"' is not one of "+strings.Join(known, ", "))
	}
	return tag, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
