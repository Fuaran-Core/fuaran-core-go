// Package ops is the skeleton ops and their apply — the reference's tree-op
// contract over the tree.Node witness, with no domain policy: every node may hold
// children, and a rejection names its class and the id it is about.
//
//	| Op              | Refused when, in this order                                                                                              |
//	| insertChild     | an id of the graft is already in the tree or repeats inside the graft (duplicateId, the first in pre-order); the parent is absent (unknownNode) |
//	| removeNode      | the target is the root (cannotRemoveRoot); it is absent (unknownNode)                                                     |
//	| moveNode        | the target is the root (cannotRemoveRoot); the target, then the new parent, is absent (unknownNode); the new parent is the target or below it (wouldNestUnderSelf) |
//	| reorderChildren | the parent is absent (unknownNode); the order is not a permutation of its children's ids (reorderMismatch)                 |
//	| updateNode      | no node has the payload's id (unknownNode); the kind is rewritten in place and the children kept                          |
//	| batch           | any member is refused: all or nothing, and the member's own rejection is the answer                                        |
//
// An inserted node is appended to its parent's children, and a moved one to its
// new parent's. Ops and rejections are $type-discriminated canonical JSON.
// Certified against the reference's apply/skeleton-apply.json.
package ops

import (
	"sort"

	"github.com/Fuaran-Core/fuaran-core-go/decode"
	"github.com/Fuaran-Core/fuaran-core-go/json"
	"github.com/Fuaran-Core/fuaran-core-go/tree"
)

// Op is a skeleton op: one of InsertChild, RemoveNode, MoveNode, ReorderChildren,
// Batch or UpdateNode.
type Op interface {
	// Encode is the op's canonical bytes.
	Encode() string
	// Canonical is Encode, under the name a content-addressed DAG hashes an op by
	// (dag.Op), so a skeleton op can be a DAG's op with no adapter.
	Canonical() string
	isOp()
}

// InsertChild appends node under parent.
type InsertChild struct {
	Parent string
	Node   tree.Node
}

// RemoveNode detaches the target and its subtree.
type RemoveNode struct{ Target string }

// MoveNode detaches the target and appends it under NewParent.
type MoveNode struct {
	Target    string
	NewParent string
}

// ReorderChildren permutes the parent's children into Order.
type ReorderChildren struct {
	Parent string
	Order  []string
}

// Batch applies its members in order, all or nothing.
type Batch struct{ Ops []Op }

// UpdateNode rewrites the kind of the node with the payload's id, keeping its children.
type UpdateNode struct{ Node tree.Node }

func (InsertChild) isOp()     {}
func (RemoveNode) isOp()      {}
func (MoveNode) isOp()        {}
func (ReorderChildren) isOp() {}
func (Batch) isOp()           {}
func (UpdateNode) isOp()      {}

// Canonical is the op's canonical bytes.
func (o InsertChild) Canonical() string { return o.Encode() }

// Canonical is the op's canonical bytes.
func (o RemoveNode) Canonical() string { return o.Encode() }

// Canonical is the op's canonical bytes.
func (o MoveNode) Canonical() string { return o.Encode() }

// Canonical is the op's canonical bytes.
func (o ReorderChildren) Canonical() string { return o.Encode() }

// Canonical is the op's canonical bytes.
func (o Batch) Canonical() string { return o.Encode() }

// Canonical is the op's canonical bytes.
func (o UpdateNode) Canonical() string { return o.Encode() }

// Encode is the op's canonical bytes.
func (o InsertChild) Encode() string {
	return `{"$type":"insertChild","node":` + o.Node.Encode() + `,"parent":` + json.EscapeString(o.Parent) + "}"
}

// Encode is the op's canonical bytes.
func (o RemoveNode) Encode() string {
	return `{"$type":"removeNode","target":` + json.EscapeString(o.Target) + "}"
}

// Encode is the op's canonical bytes.
func (o MoveNode) Encode() string {
	return `{"$type":"moveNode","newParent":` + json.EscapeString(o.NewParent) + `,"target":` + json.EscapeString(o.Target) + "}"
}

// Encode is the op's canonical bytes.
func (o ReorderChildren) Encode() string {
	items := make([]string, len(o.Order))
	for i, id := range o.Order {
		items[i] = json.EscapeString(id)
	}
	return `{"$type":"reorderChildren","order":` + json.RenderArray(items) + `,"parent":` + json.EscapeString(o.Parent) + "}"
}

// Encode is the op's canonical bytes.
func (o Batch) Encode() string {
	items := make([]string, len(o.Ops))
	for i, op := range o.Ops {
		items[i] = op.Encode()
	}
	return `{"$type":"batch","ops":` + json.RenderArray(items) + "}"
}

// Encode is the op's canonical bytes.
func (o UpdateNode) Encode() string {
	return `{"$type":"updateNode","node":` + o.Node.Encode() + "}"
}

var opTags = []string{"insertChild", "removeNode", "moveNode", "reorderChildren", "batch", "updateNode"}

// Decode reads an op, each refusal at its path.
func Decode(at decode.Cursor) (Op, *decode.Error) {
	str := func(name string) (string, *decode.Error) {
		f, err := at.Field(name)
		if err != nil {
			return "", err
		}
		return f.String()
	}
	node := func(name string) (tree.Node, *decode.Error) {
		f, err := at.Field(name)
		if err != nil {
			return tree.Node{}, err
		}
		return tree.Decode(f)
	}
	tag, err := at.Tag("$type", opTags)
	if err != nil {
		return nil, err
	}
	switch tag {
	case "insertChild":
		parent, err := str("parent")
		if err != nil {
			return nil, err
		}
		n, err := node("node")
		if err != nil {
			return nil, err
		}
		return InsertChild{Parent: parent, Node: n}, nil
	case "removeNode":
		target, err := str("target")
		if err != nil {
			return nil, err
		}
		return RemoveNode{Target: target}, nil
	case "moveNode":
		target, err := str("target")
		if err != nil {
			return nil, err
		}
		newParent, err := str("newParent")
		if err != nil {
			return nil, err
		}
		return MoveNode{Target: target, NewParent: newParent}, nil
	case "reorderChildren":
		parent, err := str("parent")
		if err != nil {
			return nil, err
		}
		orderAt, err := at.Field("order")
		if err != nil {
			return nil, err
		}
		items, err := orderAt.Items()
		if err != nil {
			return nil, err
		}
		order := make([]string, 0, len(items))
		for _, item := range items {
			id, err := item.String()
			if err != nil {
				return nil, err
			}
			order = append(order, id)
		}
		return ReorderChildren{Parent: parent, Order: order}, nil
	case "batch":
		opsAt, err := at.Field("ops")
		if err != nil {
			return nil, err
		}
		items, err := opsAt.Items()
		if err != nil {
			return nil, err
		}
		members := make([]Op, 0, len(items))
		for _, item := range items {
			op, err := Decode(item)
			if err != nil {
				return nil, err
			}
			members = append(members, op)
		}
		return Batch{Ops: members}, nil
	case "updateNode":
		n, err := node("node")
		if err != nil {
			return nil, err
		}
		return UpdateNode{Node: n}, nil
	}
	return nil, at.Fail(decode.UnknownTag, "unknown op '"+tag+"'")
}

// Parse reads an op from its JSON text.
func Parse(text string) (Op, *decode.Error) {
	v, err := decode.Parse(text, json.Strict)
	if err != nil {
		return nil, err
	}
	return Decode(decode.Root(v))
}

// Rejection is why an op was refused: one of DuplicateID, UnknownNode,
// CannotRemoveRoot, WouldNestUnderSelf or ReorderMismatch.
type Rejection interface {
	error
	// Encode is the rejection's canonical bytes — its class and the id it is about.
	Encode() string
	isRejection()
}

// DuplicateID: an id of the graft is already in the tree or repeats inside the graft.
type DuplicateID struct{ ID string }

// UnknownNode: no node has the target's id.
type UnknownNode struct{ Target string }

// CannotRemoveRoot: the root is the target of a remove or a move.
type CannotRemoveRoot struct{}

// WouldNestUnderSelf: the new parent is the target or below it.
type WouldNestUnderSelf struct{ Target string }

// ReorderMismatch: the order is not a permutation of the parent's children.
type ReorderMismatch struct{ Parent string }

func (DuplicateID) isRejection()        {}
func (UnknownNode) isRejection()        {}
func (CannotRemoveRoot) isRejection()   {}
func (WouldNestUnderSelf) isRejection() {}
func (ReorderMismatch) isRejection()    {}

// Encode is the rejection's canonical bytes.
func (r DuplicateID) Encode() string {
	return `{"$type":"duplicateId","id":` + json.EscapeString(r.ID) + "}"
}

// Encode is the rejection's canonical bytes.
func (r UnknownNode) Encode() string {
	return `{"$type":"unknownNode","target":` + json.EscapeString(r.Target) + "}"
}

// Encode is the rejection's canonical bytes.
func (CannotRemoveRoot) Encode() string { return `{"$type":"cannotRemoveRoot"}` }

// Encode is the rejection's canonical bytes.
func (r WouldNestUnderSelf) Encode() string {
	return `{"$type":"wouldNestUnderSelf","target":` + json.EscapeString(r.Target) + "}"
}

// Encode is the rejection's canonical bytes.
func (r ReorderMismatch) Encode() string {
	return `{"$type":"reorderMismatch","parent":` + json.EscapeString(r.Parent) + "}"
}

func (r DuplicateID) Error() string        { return r.Encode() }
func (r UnknownNode) Error() string        { return r.Encode() }
func (r CannotRemoveRoot) Error() string   { return r.Encode() }
func (r WouldNestUnderSelf) Error() string { return r.Encode() }
func (r ReorderMismatch) Error() string    { return r.Encode() }

// firstDuplicate is the first id of graft, in pre-order, already in t or repeated
// inside the graft itself; "" and false when there is none.
func firstDuplicate(t, graft *tree.Node) (string, bool) {
	seen := map[string]struct{}{}
	for _, id := range t.IDs() {
		seen[id] = struct{}{}
	}
	for _, id := range graft.IDs() {
		if _, dup := seen[id]; dup {
			return id, true
		}
		seen[id] = struct{}{}
	}
	return "", false
}

// detach removes the node with id from below n; nil when it is not below n.
func detach(n *tree.Node, id string) *tree.Node {
	for i := range n.Children {
		if n.Children[i].ID == id {
			removed := n.Children[i]
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return &removed
		}
	}
	for i := range n.Children {
		if removed := detach(&n.Children[i], id); removed != nil {
			return removed
		}
	}
	return nil
}

// Apply applies op to t: the new tree, or the rejection. t is never changed.
func Apply(t tree.Node, op Op) (tree.Node, Rejection) {
	out := t.Clone()
	if r := applyInPlace(&out, op); r != nil {
		return tree.Node{}, r
	}
	return out, nil
}

// applyInPlace applies op to t in place. On a rejection the tree may be partly
// changed (a batch member applied before the refused one); Apply is the
// all-or-nothing form.
func applyInPlace(t *tree.Node, op Op) Rejection {
	switch o := op.(type) {
	case InsertChild:
		if d, dup := firstDuplicate(t, &o.Node); dup {
			return DuplicateID{ID: d}
		}
		p := t.Find(o.Parent)
		if p == nil {
			return UnknownNode{Target: o.Parent}
		}
		p.Children = append(p.Children, o.Node.Clone())
		return nil
	case RemoveNode:
		if o.Target == t.ID {
			return CannotRemoveRoot{}
		}
		if detach(t, o.Target) == nil {
			return UnknownNode{Target: o.Target}
		}
		return nil
	case MoveNode:
		if o.Target == t.ID {
			return CannotRemoveRoot{}
		}
		moved := t.Find(o.Target)
		if moved == nil {
			return UnknownNode{Target: o.Target}
		}
		if t.Find(o.NewParent) == nil {
			return UnknownNode{Target: o.NewParent}
		}
		if moved.Find(o.NewParent) != nil {
			return WouldNestUnderSelf{Target: o.Target}
		}
		node := detach(t, o.Target)
		t.Find(o.NewParent).Children = append(t.Find(o.NewParent).Children, *node)
		return nil
	case ReorderChildren:
		p := t.Find(o.Parent)
		if p == nil {
			return UnknownNode{Target: o.Parent}
		}
		current := make([]string, len(p.Children))
		for i := range p.Children {
			current[i] = p.Children[i].ID
		}
		wanted := append([]string(nil), o.Order...)
		sort.Strings(current)
		sort.Strings(wanted)
		if !equalStrings(current, wanted) {
			return ReorderMismatch{Parent: o.Parent}
		}
		old := p.Children
		p.Children = make([]tree.Node, 0, len(old))
		for _, id := range o.Order {
			for i := range old {
				if old[i].ID == id {
					p.Children = append(p.Children, old[i])
					old = append(old[:i], old[i+1:]...)
					break
				}
			}
		}
		return nil
	case Batch:
		for _, member := range o.Ops {
			if r := applyInPlace(t, member); r != nil {
				return r
			}
		}
		return nil
	case UpdateNode:
		target := t.Find(o.Node.ID)
		if target == nil {
			return UnknownNode{Target: o.Node.ID}
		}
		target.Kind = o.Node.Kind
		return nil
	}
	panic("ops.Apply: not an Op")
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

// Footprint is the read/write footprint of an op-script (twin of the reference's
// Footprint), over node ids. The reference's record also carries slot reads and
// writes; no skeleton op accesses a slot, so they are always empty here and are not
// carried.
type Footprint struct {
	// Reads: ids whose existence an op depends on.
	Reads map[string]struct{}
	// StructureWrites: parents whose child list an op changes.
	StructureWrites map[string]struct{}
	// ContentWrites: ids authored, destroyed, relocated or rewritten.
	ContentWrites map[string]struct{}
	// UnknownParentWrites: ids removed, moved or rewritten under a parent the script
	// cannot name.
	UnknownParentWrites map[string]struct{}
}

func newFootprint() Footprint {
	return Footprint{
		Reads:               map[string]struct{}{},
		StructureWrites:     map[string]struct{}{},
		ContentWrites:       map[string]struct{}{},
		UnknownParentWrites: map[string]struct{}{},
	}
}

func addAll(into map[string]struct{}, ids ...string) {
	for _, id := range ids {
		into[id] = struct{}{}
	}
}

func merge(into, from map[string]struct{}) {
	for id := range from {
		into[id] = struct{}{}
	}
}

// Union is the union of two footprints (twin of Footprint.union); f is extended in
// place and returned.
func (f Footprint) Union(other Footprint) Footprint {
	merge(f.Reads, other.Reads)
	merge(f.StructureWrites, other.StructureWrites)
	merge(f.ContentWrites, other.ContentWrites)
	merge(f.UnknownParentWrites, other.UnknownParentWrites)
	return f
}

// WritesStructure reports whether the footprint changes any child list, named or not.
func (f Footprint) WritesStructure() bool {
	return len(f.StructureWrites) > 0 || len(f.UnknownParentWrites) > 0
}

func footprintOf(op Op) Footprint {
	f := newFootprint()
	switch o := op.(type) {
	case InsertChild:
		inserted := o.Node.IDs()
		addAll(f.Reads, inserted...)
		addAll(f.Reads, o.Parent)
		addAll(f.StructureWrites, o.Parent)
		addAll(f.ContentWrites, inserted...)
	case RemoveNode:
		addAll(f.Reads, o.Target)
		addAll(f.ContentWrites, o.Target)
		addAll(f.UnknownParentWrites, o.Target)
	case MoveNode:
		addAll(f.Reads, o.Target, o.NewParent)
		addAll(f.StructureWrites, o.NewParent)
		addAll(f.ContentWrites, o.Target)
		addAll(f.UnknownParentWrites, o.Target)
	case ReorderChildren:
		addAll(f.Reads, o.Order...)
		addAll(f.Reads, o.Parent)
		addAll(f.StructureWrites, o.Parent)
	case Batch:
		return FootprintOf(o.Ops)
	case UpdateNode:
		addAll(f.Reads, o.Node.ID)
		addAll(f.ContentWrites, o.Node.ID)
		addAll(f.UnknownParentWrites, o.Node.ID)
	}
	return f
}

// FootprintOf is the footprint of an op-script: the union of each op's (twin of
// Ops.footprint Policy.none over the plain scope). Conservative by contract: a
// remove, a move and an update carry an unknown-parent write, so each is dependent
// on every concurrent structural write.
func FootprintOf(script []Op) Footprint {
	acc := newFootprint()
	for _, op := range script {
		acc = acc.Union(footprintOf(op))
	}
	return acc
}
