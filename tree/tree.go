// Package tree is the skeleton tree — the witness surface the reference's structural
// ops read: a node is an id, a kind and its children, and nothing else. A host with
// a richer node type runs the ops by mapping those three members onto its own node.
//
// Its canonical bytes are {"children":[...],"id":...,"kind":...} (members sorted
// ordinally), the envelope the reference's apply/, dag/, merge/ and diff/ families
// carry every tree in.
package tree

import (
	"strings"

	"github.com/Fuaran-Core/fuaran-core-go/decode"
	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Node is a skeleton node.
type Node struct {
	ID       string
	Kind     string
	Children []Node
}

// Leaf is a node with no children.
func Leaf(id, kind string) Node { return Node{ID: id, Kind: kind} }

// Find is the node with this id, anywhere in the tree (this node included); nil when
// there is none.
func (n *Node) Find(id string) *Node {
	if n.ID == id {
		return n
	}
	for i := range n.Children {
		if found := n.Children[i].Find(id); found != nil {
			return found
		}
	}
	return nil
}

// IDs is every id in the tree, in pre-order.
func (n *Node) IDs() []string {
	var out []string
	n.collectIDs(&out)
	return out
}

func (n *Node) collectIDs(out *[]string) {
	*out = append(*out, n.ID)
	for i := range n.Children {
		n.Children[i].collectIDs(out)
	}
}

// Clone is a deep copy.
func (n Node) Clone() Node {
	out := Node{ID: n.ID, Kind: n.Kind}
	if n.Children != nil {
		out.Children = make([]Node, len(n.Children))
		for i, c := range n.Children {
			out.Children[i] = c.Clone()
		}
	}
	return out
}

// Equal reports whether two trees are the same tree, member for member.
func (n Node) Equal(m Node) bool {
	if n.ID != m.ID || n.Kind != m.Kind || len(n.Children) != len(m.Children) {
		return false
	}
	for i := range n.Children {
		if !n.Children[i].Equal(m.Children[i]) {
			return false
		}
	}
	return true
}

// Encode is the node's canonical bytes.
func (n *Node) Encode() string {
	var out strings.Builder
	n.encodeInto(&out)
	return out.String()
}

func (n *Node) encodeInto(out *strings.Builder) {
	out.WriteString(`{"children":[`)
	for i := range n.Children {
		if i > 0 {
			out.WriteByte(',')
		}
		n.Children[i].encodeInto(out)
	}
	out.WriteString(`],"id":`)
	out.WriteString(json.EscapeString(n.ID))
	out.WriteString(`,"kind":`)
	out.WriteString(json.EscapeString(n.Kind))
	out.WriteByte('}')
}

// Decode reads a node: string id and kind members and a children array of nodes,
// each refusal at its path. Any other member is ignored.
func Decode(at decode.Cursor) (Node, *decode.Error) {
	var n Node
	idAt, err := at.Field("id")
	if err != nil {
		return n, err
	}
	if n.ID, err = idAt.String(); err != nil {
		return n, err
	}
	kindAt, err := at.Field("kind")
	if err != nil {
		return n, err
	}
	if n.Kind, err = kindAt.String(); err != nil {
		return n, err
	}
	childrenAt, err := at.Field("children")
	if err != nil {
		return n, err
	}
	items, err := childrenAt.Items()
	if err != nil {
		return n, err
	}
	n.Children = make([]Node, 0, len(items))
	for _, item := range items {
		child, err := Decode(item)
		if err != nil {
			return n, err
		}
		n.Children = append(n.Children, child)
	}
	return n, nil
}

// Parse reads a node from its JSON text under the reader's stack guard.
func Parse(text string) (Node, *decode.Error) {
	v, err := decode.Parse(text, json.Strict)
	if err != nil {
		return Node{}, err
	}
	return Decode(decode.Root(v))
}
