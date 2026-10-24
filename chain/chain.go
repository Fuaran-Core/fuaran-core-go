// Package chain is the op-stream chain: an append-only sequence of records, each
// hashing its predecessor's hash with its own pre-image, so a tamper to any
// historical record breaks the chain at that record and the walk names the first
// break.
//
// The chain reads no op: every op is a string spliced into the pre-image unread,
// and an actor is its canonical text (EncodeActor). So one chain serves every op
// vocabulary, and a host certifies it with no op codec.
//
//	payload = {"seq":<seq>,"actor":<actor>,"op":<op>}      (exactly these bytes)
//	hash    = sha256Hex(prevHash ++ "|" ++ payload)          (lower-case hex)
//
// Under the canonical profile the first record hangs from Genesis, the empty
// string, at sequence 0; a stream that keeps another genesis (a wire's own fixed
// anchor) starts from an Anchor naming it. Certified against the reference's
// chain/skeleton-chain.json, and the actor and pre-image spelling against
// escape/string-escape.json.
package chain

import (
	"strconv"

	"github.com/Fuaran-Core/fuaran-core-go/address"
	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Genesis is the canonical profile's genesis: the empty previous hash.
const Genesis = ""

// Actor is who authored an op — folded into every pre-image, so attribution is
// tamper-evident. Closed: Human or Agent.
type Actor interface{ isActor() }

// Human is a person or account id — the load-bearing accountability case.
type Human struct{ ID string }

// Agent is an AI author; Model and Version double as provenance metadata.
type Agent struct {
	Model   string
	Version string
	ID      string
}

func (Human) isActor() {}
func (Agent) isActor() {}

// EncodeActor is the actor's canonical text. The member order is PINNED (kind
// first, then the case's members in declaration order), not sorted: the same bytes
// are folded into a chain payload and a DAG node id, so there is one spelling to
// drift.
func EncodeActor(a Actor) string {
	switch t := a.(type) {
	case Human:
		return `{"kind":"human","id":` + json.EscapeString(t.ID) + "}"
	case Agent:
		return `{"kind":"agent","model":` + json.EscapeString(t.Model) +
			`,"version":` + json.EscapeString(t.Version) +
			`,"id":` + json.EscapeString(t.ID) + "}"
	}
	panic("chain.EncodeActor: not an Actor")
}

// Payload is the pre-image of the record at seq: the actor and the op spliced
// verbatim.
func Payload(seq uint64, actor, op string) string {
	return `{"seq":` + strconv.FormatUint(seq, 10) + `,"actor":` + actor + `,"op":` + op + "}"
}

// Link is the hash a record hangs from prevHash with payload.
func Link(prevHash, payload string) string {
	return address.SHA256Hex(prevHash + "|" + payload)
}

// Anchor is where the next record hangs: its sequence number and the hash it links
// to.
type Anchor struct {
	PrevHash string
	Seq      uint64
}

// GenesisAnchor is the canonical profile's start: Genesis at sequence 0.
func GenesisAnchor() Anchor { return Anchor{PrevHash: Genesis} }

// Record is one chained record. Actor and Op are canonical text, carried verbatim.
type Record struct {
	Seq      uint64
	Actor    string
	Op       string
	PrevHash string
	Hash     string
}

// Payload is the record's pre-image, recomputed from its own members.
func (r Record) Payload() string { return Payload(r.Seq, r.Actor, r.Op) }

// Append chains one record onto anchor; answers the record and the anchor after it.
func Append(anchor Anchor, actor, op string) (Record, Anchor) {
	hash := Link(anchor.PrevHash, Payload(anchor.Seq, actor, op))
	return Record{Seq: anchor.Seq, Actor: actor, Op: op, PrevHash: anchor.PrevHash, Hash: hash},
		Anchor{PrevHash: hash, Seq: anchor.Seq + 1}
}

// BreakReason is why a walk stopped.
type BreakReason int

const (
	// SequenceMismatch: the record's sequence number is not the next one.
	SequenceMismatch BreakReason = iota
	// PrevHashBroken: the record does not link to its predecessor's hash.
	PrevHashBroken
	// HashMismatch: the record's hash does not recompute from its members.
	HashMismatch
)

var breakReasons = [...]string{
	"sequence-number mismatch",
	"prev-hash link broken",
	"hash mismatch (tampered op/actor/seq)",
}

// String is the reason's canonical spelling.
func (r BreakReason) String() string { return breakReasons[r] }

// Break is the first break a walk finds: the record's index in the run, the reason,
// and the value the walk expected beside the one it found.
type Break struct {
	Index    int
	Reason   BreakReason
	Expected string
	Got      string
}

func (b *Break) Error() string {
	return "chain break at record " + strconv.Itoa(b.Index) + ": " + b.Reason.String() +
		" (expected " + b.Expected + ", got " + b.Got + ")"
}

// Walk checks records from anchor: each record's sequence number, then its link,
// then its hash; the first fault is reported. An intact run answers the anchor the
// next record would hang from.
func Walk(anchor Anchor, records []Record) (Anchor, *Break) {
	at := anchor
	for index, r := range records {
		if r.Seq != at.Seq {
			return Anchor{}, &Break{index, SequenceMismatch, strconv.FormatUint(at.Seq, 10), strconv.FormatUint(r.Seq, 10)}
		}
		if r.PrevHash != at.PrevHash {
			return Anchor{}, &Break{index, PrevHashBroken, at.PrevHash, r.PrevHash}
		}
		recomputed := Link(r.PrevHash, r.Payload())
		if recomputed != r.Hash {
			return Anchor{}, &Break{index, HashMismatch, recomputed, r.Hash}
		}
		at = Anchor{PrevHash: r.Hash, Seq: r.Seq + 1}
	}
	return at, nil
}
