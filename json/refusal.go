package json

import "strconv"

// ErrorKind is the class of a malformed text — the reference's closed JsonErrorKind,
// every case of it, so a refusal names the class the reference names. The sentence
// beside a refusal is this host's own.
type ErrorKind int

// The classes, in the reference's order.
const (
	UnexpectedChar ErrorKind = iota
	UnexpectedEndOfInput
	ExpectedToken
	UnterminatedString
	UnterminatedEscape
	TruncatedUnicodeEscape
	BadEscape
	BadHexDigit
	MalformedNumber
	// NullNotRepresentable is a null where the reading admits none. Parse and Read
	// build Null and never raise it; the decode layer (package decode) reads the
	// reference's null-free model and does.
	NullNotRepresentable
	// MaxDepthExceeded is the reference's own name for its depth cap. This reader
	// raises a depth breach as a *LimitBreach with Depth, as the reference does
	// under a limits record; the case is kept so the vocabulary is the reference's,
	// whole.
	MaxDepthExceeded
	TrailingCharacters
	DuplicateMember
)

var errorKindNames = [...]string{
	"UnexpectedChar",
	"UnexpectedEndOfInput",
	"ExpectedToken",
	"UnterminatedString",
	"UnterminatedEscape",
	"TruncatedUnicodeEscape",
	"BadEscape",
	"BadHexDigit",
	"MalformedNumber",
	"NullNotRepresentable",
	"MaxDepthExceeded",
	"TrailingCharacters",
	"DuplicateMember",
}

// String is the reference's spelling of the class (MalformedNumber, ...).
func (k ErrorKind) String() string { return errorKindNames[k] }

// Refusal is why a read refused: the text is malformed (*Error), or it is
// well-formed and passes a bound (*LimitBreach). The two are never conflated — a
// breach reported as malformed sends the author to repair the wrong thing. Both
// implement error.
type Refusal interface {
	error
	// Offset is the byte offset the refusal was raised at.
	Offset() int
	isRefusal()
}

// Error is malformed text: the class, the byte offset it was detected at, and a
// sentence.
type Error struct {
	Kind    ErrorKind
	At      int
	Message string
}

func (e *Error) Error() string {
	return e.Kind.String() + " at byte " + strconv.Itoa(e.At) + ": " + e.Message
}

// Offset is the byte offset the refusal was raised at.
func (e *Error) Offset() int { return e.At }
func (*Error) isRefusal()    {}

// PathSeg is one step of a path from the document root: a Key (a member name) or
// an Index (an item index).
type PathSeg interface{ isPathSeg() }

// Key is a member name on a path.
type Key string

// Index is an item index on a path.
type Index int

func (Key) isPathSeg()   {}
func (Index) isPathSeg() {}

// Limit names a bound of Limits.
type Limit int

// The bounds.
const (
	// Depth is the nested objects and arrays admitted, the root counting as one.
	Depth Limit = iota
	// StringLength is the Unicode code points in ONE decoded string or member name —
	// a surrogate pair is one, whichever spelling it arrived in.
	StringLength
	// ArrayLength is the items in one array.
	ArrayLength
	// ObjectMembers is the members in one object.
	ObjectMembers
	// DocumentBytes is the UTF-8 bytes of the whole input.
	DocumentBytes
)

var limitNames = [...]string{"depth", "stringLength", "arrayLength", "objectMembers", "documentBytes"}

// String is the reference's member name for the bound (depth, stringLength, ...).
func (l Limit) String() string { return limitNames[l] }

// LimitBreach is a bound passed: which, its declared value, the path of the
// container it was raised in (the string's own path for a string value, the
// object's for a member name, the root for the document size), and the byte
// offset. Never the input.
type LimitBreach struct {
	Limit Limit
	Value int
	Path  []PathSeg
	At    int
}

func (b *LimitBreach) Error() string {
	return "limit " + b.Limit.String() + " of " + strconv.Itoa(b.Value) + " exceeded"
}

// Offset is the byte offset the refusal was raised at.
func (b *LimitBreach) Offset() int { return b.At }
func (*LimitBreach) isRefusal()    {}

// Limits is the bounds a read enforces on the way down. A zero bound is unbounded;
// the depth is always bounded, because it is the reader's stack guard.
type Limits struct {
	Depth         int
	StringLength  int
	ArrayLength   int
	ObjectMembers int
	DocumentBytes int
}

// Strict is the reader's own stack guard: nesting past 512 is refused, and no other
// bound applies. Deliberately looser than any wire's bound, so the reader never
// refuses a document a wire says a host must accept.
var Strict = DepthOnly(512)

// DepthOnly is a record bounding the depth alone.
func DepthOnly(depth int) Limits { return Limits{Depth: depth} }
