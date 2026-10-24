// Package dag is the op-DAG: content-addressed nodes, each an op by an actor over a
// set of parents. A node's id is
//
//	sha256Hex( <parent ids sorted ordinally, joined by ","> ++ "|" ++ <actor> ++ "|" ++ <op> )
//
// over the actor's canonical text and the op's (Op.Canonical), so the id is a
// function of the parent SET and the same write deduplicates to one node. The DAG
// holds no state: a state is what replaying an order of its ops from an initial
// state gives, with the domain's apply passed in.
//
// Orders are Kahn drains, the ready node with the smallest id first, so every order
// is total and a function of the node set; a parent the DAG does not hold
// constrains nothing. Certified against the reference's dag/skeleton-dag.json.
package dag

import (
	"sort"
	"strings"

	"github.com/Fuaran-Core/fuaran-core-go/address"
	"github.com/Fuaran-Core/fuaran-core-go/chain"
	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Op is an op a DAG can address: its canonical text is what the id hashes.
type Op interface{ Canonical() string }

// Text is an op held as its canonical text and nothing else.
type Text string

// Canonical is the text itself.
func (t Text) Canonical() string { return string(t) }

// Node is one node: its id, its parents in the order first written, its actor and op.
type Node[O Op] struct {
	ID      string
	Parents []string
	Actor   chain.Actor
	Op      O
}

// NodeID is the content id of a node of parents, actor and an op of canonical text op.
func NodeID(parents []string, actor chain.Actor, op string) string {
	sorted := sortedOrdinal(parents)
	return address.SHA256Hex(strings.Join(sorted, ",") + "|" + chain.EncodeActor(actor) + "|" + op)
}

// AppendFault is why a write was refused; nothing is added. One of EmptyParentID,
// CommaInParentID, UnknownParent or ContentIDCollision.
type AppendFault interface {
	error
	// Encode is the fault's canonical bytes: $type, and the offending id as parentId
	// or nodeId (the reference exporter's faultJ).
	Encode() string
	isAppendFault()
}

// EmptyParentID: a parent is "" — the genesis marker, not a node id (genesis is no parents).
type EmptyParentID struct{}

// CommaInParentID: a parent carries the "," the id's pre-image joins parents with.
type CommaInParentID struct{ Parent string }

// UnknownParent: a parent the DAG does not hold.
type UnknownParent struct{ Parent string }

// ContentIDCollision: the id is held by a node of different content.
type ContentIDCollision struct{ NodeID string }

func (EmptyParentID) isAppendFault()      {}
func (CommaInParentID) isAppendFault()    {}
func (UnknownParent) isAppendFault()      {}
func (ContentIDCollision) isAppendFault() {}

// Encode is the fault's canonical bytes.
func (EmptyParentID) Encode() string { return `{"$type":"emptyParentId"}` }

// Encode is the fault's canonical bytes.
func (f CommaInParentID) Encode() string {
	return `{"$type":"commaInParentId","parentId":` + json.EscapeString(f.Parent) + "}"
}

// Encode is the fault's canonical bytes.
func (f UnknownParent) Encode() string {
	return `{"$type":"unknownParent","parentId":` + json.EscapeString(f.Parent) + "}"
}

// Encode is the fault's canonical bytes.
func (f ContentIDCollision) Encode() string {
	return `{"$type":"contentIdCollision","nodeId":` + json.EscapeString(f.NodeID) + "}"
}

func (f EmptyParentID) Error() string      { return f.Encode() }
func (f CommaInParentID) Error() string    { return f.Encode() }
func (f UnknownParent) Error() string      { return f.Encode() }
func (f ContentIDCollision) Error() string { return f.Encode() }

// Cyclic is the nodes a drain could not place, because they lie on or below a
// cycle, in id order. Only a hand-built or unverified load can hold one.
type Cyclic struct{ Unplaced []string }

func (c *Cyclic) Error() string { return "cyclic: " + strings.Join(c.Unplaced, ", ") }

// ReplayFault is why a replay refused: UnknownHead (a replay of one head only), a
// *Cyclic, or Rejected — the domain refused the op of this node, the first in
// replay order.
type ReplayFault[R any] struct {
	UnknownHead string
	Cyclic      *Cyclic
	// Rejected names the node whose op the domain refused, with its reason.
	Rejected  string
	Rejection R
	kind      int
}

const (
	faultUnknownHead = iota + 1
	faultCyclic
	faultRejected
)

// IsUnknownHead reports a replay of a head the DAG does not hold.
func (f *ReplayFault[R]) IsUnknownHead() bool { return f.kind == faultUnknownHead }

// IsCyclic reports a drain that could not place every node.
func (f *ReplayFault[R]) IsCyclic() bool { return f.kind == faultCyclic }

// IsRejected reports a domain refusal, at Rejected with Rejection.
func (f *ReplayFault[R]) IsRejected() bool { return f.kind == faultRejected }

func (f *ReplayFault[R]) Error() string {
	switch f.kind {
	case faultUnknownHead:
		return "unknown head " + f.UnknownHead
	case faultCyclic:
		return f.Cyclic.Error()
	}
	return "rejected at " + f.Rejected
}

// BreakReason is why an integrity walk stopped.
type BreakReason int

const (
	// ContentIDMismatch: a node whose id is not its content hash.
	ContentIDMismatch BreakReason = iota
	// MissingParent: a parent the DAG does not hold.
	MissingParent
)

var breakReasons = [...]string{"content-id mismatch (tampered node)", "missing parent"}

// String is the reason's canonical spelling.
func (r BreakReason) String() string { return breakReasons[r] }

// Break is the first integrity fault: the node, the reason, and the expected and
// found values (the recomputed id and the stored one; "" and the absent parent).
type Break struct {
	Node     string
	Reason   BreakReason
	Expected string
	Got      string
}

// Dag is a DAG of ops.
type Dag[O Op] struct {
	nodes map[string]Node[O]
}

// New is an empty DAG.
func New[O Op]() *Dag[O] { return &Dag[O]{nodes: map[string]Node[O]{}} }

// FromNodes is a DAG of stored nodes, their ids trusted as stored — what a load
// does before it verifies. FirstBreak says whether they hold.
func FromNodes[O Op](nodes []Node[O]) *Dag[O] {
	d := New[O]()
	for _, n := range nodes {
		d.nodes[n.ID] = n
	}
	return d
}

// Len is the number of nodes.
func (d *Dag[O]) Len() int { return len(d.nodes) }

// Get is the node with this id, if held.
func (d *Dag[O]) Get(id string) (Node[O], bool) {
	n, ok := d.nodes[id]
	return n, ok
}

// AppendOn appends op as a child of every id in parents (nil is a genesis node).
// The parents are judged in the order given, the first refusal returned: "", then
// a comma-bearing id, then an id the DAG does not hold. They are stored as given;
// the id sorts them, so their order changes no id. A write identical to a held node
// records nothing and answers that node's id.
func (d *Dag[O]) AppendOn(actor chain.Actor, op O, parents []string) (string, AppendFault) {
	for _, p := range parents {
		if p == "" {
			return "", EmptyParentID{}
		}
		if strings.Contains(p, ",") {
			return "", CommaInParentID{Parent: p}
		}
		if _, held := d.nodes[p]; !held {
			return "", UnknownParent{Parent: p}
		}
	}
	text := op.Canonical()
	id := NodeID(parents, actor, text)
	if existing, held := d.nodes[id]; held {
		same := equalStrings(sortedOrdinal(existing.Parents), sortedOrdinal(parents)) &&
			existing.Actor == actor && existing.Op.Canonical() == text
		if same {
			return id, nil
		}
		return "", ContentIDCollision{NodeID: id}
	}
	stored := make([]string, len(parents))
	copy(stored, parents)
	d.nodes[id] = Node[O]{ID: id, Parents: stored, Actor: actor, Op: op}
	return id, nil
}

// MergeAll is AppendOn over heads deduplicated and sorted ordinally.
func (d *Dag[O]) MergeAll(actor chain.Actor, op O, heads []string) (string, AppendFault) {
	return d.AppendOn(actor, op, sortedOrdinal(dedup(heads)))
}

// Heads is the nodes that are no node's parent, in id order.
func (d *Dag[O]) Heads() []string {
	parents := map[string]struct{}{}
	for _, n := range d.nodes {
		for _, p := range n.Parents {
			parents[p] = struct{}{}
		}
	}
	var out []string
	for id := range d.nodes {
		if _, isParent := parents[id]; !isParent {
			out = append(out, id)
		}
	}
	return sortedOrdinal(out)
}

// drain is the Kahn drain over the nodes keep admits, the ready node with the
// smallest id (UTF-16 ordinal) first; a parent keep refuses constrains nothing.
// (placed, unplaced), the second in id order. Twin of drainNodes.
func (d *Dag[O]) drain(keep func(string) bool) (placed, unplaced []string) {
	indeg := map[string]int{}
	children := map[string][]string{}
	for id, n := range d.nodes {
		if !keep(id) {
			continue
		}
		held := 0
		for _, p := range n.Parents {
			if _, ok := d.nodes[p]; ok && keep(p) {
				held++
				children[p] = append(children[p], id)
			}
		}
		indeg[id] = held
	}
	var ready []string
	for id, deg := range indeg {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	ready = sortedOrdinal(ready)
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		placed = append(placed, id)
		kids := sortedOrdinal(children[id])
		for _, k := range kids {
			indeg[k]--
			if indeg[k] == 0 {
				ready = insertOrdinal(ready, k)
			}
		}
	}
	done := map[string]struct{}{}
	for _, id := range placed {
		done[id] = struct{}{}
	}
	for id := range indeg {
		if _, ok := done[id]; !ok {
			unplaced = append(unplaced, id)
		}
	}
	return placed, sortedOrdinal(unplaced)
}

func insertOrdinal(sorted []string, id string) []string {
	i := sort.Search(len(sorted), func(i int) bool { return !json.OrdinalLess(sorted[i], id) })
	sorted = append(sorted, "")
	copy(sorted[i+1:], sorted[i:])
	sorted[i] = id
	return sorted
}

func ordered(placed, unplaced []string) ([]string, *Cyclic) {
	if len(unplaced) > 0 {
		return nil, &Cyclic{Unplaced: unplaced}
	}
	return placed, nil
}

// TotalOrder is the whole DAG in one total order: parents first, the smallest ready
// id next.
func (d *Dag[O]) TotalOrder() ([]string, *Cyclic) {
	placed, unplaced := d.drain(func(string) bool { return true })
	return ordered(placed, unplaced)
}

// Ranks is every node's Lamport depth: 0 with no held parent, else one past its
// deepest held parent.
func (d *Dag[O]) Ranks() (map[string]int, *Cyclic) {
	order, cyc := d.TotalOrder()
	if cyc != nil {
		return nil, cyc
	}
	ranks := map[string]int{}
	for _, id := range order {
		r := 0
		for _, p := range d.nodes[id].Parents {
			if rp, ok := ranks[p]; ok && rp+1 > r {
				r = rp + 1
			}
		}
		ranks[id] = r
	}
	return ranks, nil
}

// closureOf is the ancestor closure of roots: each root plus every transitive parent
// the DAG holds; an id it does not hold contributes nothing. Twin of closureOf.
func (d *Dag[O]) closureOf(roots []string) map[string]struct{} {
	seen := map[string]struct{}{}
	stack := append([]string(nil), roots...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, done := seen[id]; done {
			continue
		}
		if n, ok := d.nodes[id]; ok {
			seen[id] = struct{}{}
			stack = append(stack, n.Parents...)
		}
	}
	return seen
}

// AncestorsOf is id and every node it descends from; empty for an id the DAG does
// not hold. Twin of Dag.ancestorsOf.
func (d *Dag[O]) AncestorsOf(id string) map[string]struct{} { return d.closureOf([]string{id}) }

// deepest is, of a non-empty down-closed set, the member with the largest ancestor
// closure, the larger id (ordinal) on a tie (twin of deepest). Over an acyclic set
// only a maximal member can win, so only those are sized.
func (d *Dag[O]) deepest(common map[string]struct{}) string {
	keep := func(id string) bool { _, ok := common[id]; return ok }
	candidates := common
	if _, unplaced := d.drain(keep); len(unplaced) == 0 {
		var parents []string
		for id := range common {
			if n, ok := d.nodes[id]; ok {
				parents = append(parents, n.Parents...)
			}
		}
		below := d.closureOf(parents)
		candidates = map[string]struct{}{}
		for id := range common {
			if _, under := below[id]; !under {
				candidates[id] = struct{}{}
			}
		}
	}
	best, bestSize := "", -1
	for id := range candidates {
		size := len(d.AncestorsOf(id))
		if size > bestSize || (size == bestSize && json.OrdinalLess(best, id)) {
			best, bestSize = id, size
		}
	}
	return best
}

// CommonBase is the common base of N heads: of the nodes in every head's ancestor
// closure, the one with the largest closure, the larger id on a tie (twin of
// Dag.commonBase). "" and false for no heads, or when nothing is common to all. A
// function of the head set; a head the DAG does not hold makes it absent.
func (d *Dag[O]) CommonBase(heads []string) (string, bool) {
	distinct := dedup(heads)
	if len(distinct) == 0 {
		return "", false
	}
	common := d.AncestorsOf(distinct[0])
	for _, h := range distinct[1:] {
		c := d.AncestorsOf(h)
		for id := range common {
			if _, ok := c[id]; !ok {
				delete(common, id)
			}
		}
	}
	if len(common) == 0 {
		return "", false
	}
	return d.deepest(common), true
}

// fold applies the ops of order (ids the DAG holds) from state0; the first refusal
// is named by its node id. Twin of fold.
func fold[O Op, S any, R any](d *Dag[O], state0 S, order []string, apply func(O, S) (S, R)) (S, string, R, bool) {
	state := state0
	var none R
	for _, id := range order {
		next, r := apply(d.nodes[id].Op, state)
		if any(r) != nil {
			return state, id, r, true
		}
		state = next
	}
	return state, "", none, false
}

// ReplayTo replays head's history from state0: its closure in drain order, each op
// applied once; the first node the domain refuses is named. The domain's apply
// answers its refusal as R, an interface or pointer type whose nil is acceptance.
func ReplayTo[O Op, S any, R any](d *Dag[O], state0 S, head string, apply func(O, S) (S, R)) (S, *ReplayFault[R]) {
	var zero S
	if _, ok := d.nodes[head]; !ok {
		return zero, &ReplayFault[R]{UnknownHead: head, kind: faultUnknownHead}
	}
	closure := d.AncestorsOf(head)
	placed, unplaced := d.drain(func(id string) bool { _, ok := closure[id]; return ok })
	order, cyc := ordered(placed, unplaced)
	if cyc != nil {
		return zero, &ReplayFault[R]{Cyclic: cyc, kind: faultCyclic}
	}
	state, at, r, refused := fold(d, state0, order, apply)
	if refused {
		return zero, &ReplayFault[R]{Rejected: at, Rejection: r, kind: faultRejected}
	}
	return state, nil
}

// ReplayAll replays the whole DAG from state0 in TotalOrder; apply is as for ReplayTo.
func ReplayAll[O Op, S any, R any](d *Dag[O], state0 S, apply func(O, S) (S, R)) (S, *ReplayFault[R]) {
	var zero S
	order, cyc := d.TotalOrder()
	if cyc != nil {
		return zero, &ReplayFault[R]{Cyclic: cyc, kind: faultCyclic}
	}
	state, at, r, refused := fold(d, state0, order, apply)
	if refused {
		return zero, &ReplayFault[R]{Rejected: at, Rejection: r, kind: faultRejected}
	}
	return state, nil
}

// FirstBreak is the first integrity fault, walking the drain order then any node on
// or below a cycle in id order: a node whose id is not its content hash, then a
// parent the DAG does not hold. Nil for an intact DAG.
func (d *Dag[O]) FirstBreak() *Break {
	placed, unplaced := d.drain(func(string) bool { return true })
	for _, id := range append(placed, unplaced...) {
		n := d.nodes[id]
		h := NodeID(n.Parents, n.Actor, n.Op.Canonical())
		if id != h {
			return &Break{Node: id, Reason: ContentIDMismatch, Expected: h, Got: id}
		}
		for _, p := range n.Parents {
			if _, ok := d.nodes[p]; !ok {
				return &Break{Node: id, Reason: MissingParent, Expected: "", Got: p}
			}
		}
	}
	return nil
}

// dedup is ids with every repeat after the first dropped, in the order given.
func dedup(ids []string) []string {
	var out []string
	for _, id := range ids {
		if !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsString(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sortedOrdinal(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.SliceStable(out, func(i, j int) bool { return json.OrdinalLess(out[i], out[j]) })
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
