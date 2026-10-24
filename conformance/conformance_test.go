package conformance

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Fuaran-Core/fuaran-core-go/address"
	"github.com/Fuaran-Core/fuaran-core-go/chain"
	"github.com/Fuaran-Core/fuaran-core-go/dag"
	"github.com/Fuaran-Core/fuaran-core-go/decimal"
	"github.com/Fuaran-Core/fuaran-core-go/decode"
	"github.com/Fuaran-Core/fuaran-core-go/json"
	"github.com/Fuaran-Core/fuaran-core-go/ops"
	"github.com/Fuaran-Core/fuaran-core-go/tree"
)

func actorOf(t testing.TB, text string) chain.Actor {
	t.Helper()
	v, r := json.Parse(text)
	if r != nil {
		t.Fatalf("an actor is JSON: %v", r)
	}
	var actor chain.Actor
	switch kind := s(t, v, "kind"); kind {
	case "human":
		actor = chain.Human{ID: s(t, v, "id")}
	case "agent":
		actor = chain.Agent{Model: s(t, v, "model"), Version: s(t, v, "version"), ID: s(t, v, "id")}
	default:
		t.Fatalf("unknown actor kind %s", kind)
	}
	if got := chain.EncodeActor(actor); got != text {
		t.Fatalf("the actor's canonical text: %s, want %s", got, text)
	}
	return actor
}

func treeOf(t testing.TB, text string) tree.Node {
	t.Helper()
	n, err := tree.Parse(text)
	if err != nil {
		t.Fatalf("a tree decodes: %v", err)
	}
	if got := n.Encode(); got != text {
		t.Fatalf("a tree's canonical bytes: %s, want %s", got, text)
	}
	return n
}

func opOf(t testing.TB, text string) ops.Op {
	t.Helper()
	op, err := ops.Parse(text)
	if err != nil {
		t.Fatalf("an op decodes: %v", err)
	}
	if got := op.Encode(); got != text {
		t.Fatalf("an op's canonical bytes: %s, want %s", got, text)
	}
	return op
}

// refusalClass is a refusal's class as the vectors spell it: the malformed-text
// kind, or LimitExceeded/<bound> for a well-formed document past a bound.
func refusalClass(r json.Refusal) string {
	switch x := r.(type) {
	case *json.Error:
		return x.Kind.String()
	case *json.LimitBreach:
		return "LimitExceeded/" + x.Limit.String()
	}
	return "?"
}

// ─── refusals/codec-refusals.json — the JSON reader ──────────────────────────

func TestCodecRefusalsJSONVectors(t *testing.T) {
	doc := read(t, "refusals/codec-refusals.json")
	if got, want := len(vectors(t, doc)), declared(t, "refusals", "codecRefusals"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		if s(t, v, "codec") != "json" {
			continue
		}
		name := s(t, v, "name")
		got, r := json.Parse(s(t, v, "input"))
		switch {
		case has(v, "refusal") && r != nil:
			e, ok := r.(*json.Error)
			if !ok || e.Kind.String() != s(t, v, "refusal") {
				t.Errorf("%s: expected %s, got %v", name, s(t, v, "refusal"), r)
			}
		case !has(v, "refusal") && r == nil:
			if rendered := json.Render(got); rendered != s(t, v, "canonical") {
				t.Errorf("%s: rendered %s, want %s", name, rendered, s(t, v, "canonical"))
			}
		default:
			t.Errorf("%s: expected refusal=%v, got %v", name, has(v, "refusal"), r)
		}
		ran++
	}
	// The column and profile vectors (45) need the columnar codec and the wire
	// profile, which the spine does not hold yet (DECISIONS.md D3).
	if ran != 56 {
		t.Fatalf("the json vectors run: %d, want 56", ran)
	}
}

// ─── decode/decode-rejects.json — the decode error contract ──────────────────

func strOf(v json.Value, k string) (string, bool) {
	x, ok := json.Field(v, k).(json.String)
	return string(x), ok
}

// readShape reads the vector's shape grammar with the spine's combinators exactly as
// the reference's shape decoder reads it.
func readShape(t testing.TB, defs, shape json.Value, at decode.Cursor) *decode.Error {
	t.Helper()
	any := json.Object{{Key: "$type", Value: json.String("any")}}
	fieldOr := func(v json.Value, k string) json.Value {
		if f := json.Field(v, k); f != nil {
			return f
		}
		return any
	}
	switch kind := s(t, shape, "$type"); kind {
	case "string":
		_, err := at.String()
		return err
	case "int":
		_, err := at.Int()
		return err
	case "float":
		_, err := at.Float()
		return err
	case "bool":
		_, err := at.Bool()
		return err
	case "any":
		return nil
	case "list":
		of := fieldOr(shape, "of")
		var items []decode.Cursor
		var err *decode.Error
		if max, ok := json.Field(shape, "maxItems").(json.Number); ok {
			items, err = at.ItemsAtMost(int(max))
		} else {
			items, err = at.Items()
		}
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := readShape(t, defs, of, item); err != nil {
				return err
			}
		}
		return nil
	case "object":
		fields, _ := json.Field(shape, "fields").(json.Array)
		if closed, ok := json.Field(shape, "closed").(json.Bool); ok && bool(closed) {
			names := make([]string, len(fields))
			for i, f := range fields {
				names[i] = s(t, f, "name")
			}
			if err := at.Closed(names); err != nil {
				return err
			}
		}
		for _, f := range fields {
			sub := fieldOr(f, "shape")
			if optional, ok := json.Field(f, "optional").(json.Bool); ok && bool(optional) {
				c, present, err := at.Optional(s(t, f, "name"))
				if err != nil {
					return err
				}
				if present {
					if err := readShape(t, defs, sub, c); err != nil {
						return err
					}
				}
			} else {
				c, err := at.Field(s(t, f, "name"))
				if err != nil {
					return err
				}
				if err := readShape(t, defs, sub, c); err != nil {
					return err
				}
			}
		}
		if len(fields) == 0 {
			if _, _, err := at.Optional(""); err != nil { // still an object
				return err
			}
		}
		return nil
	case "tagged":
		key, ok := strOf(shape, "key")
		if !ok {
			key = "kind"
		}
		cases := arr(t, shape, "cases")
		tags := make([]string, len(cases))
		for i, c := range cases {
			tags[i] = s(t, c, "tag")
		}
		tag, err := at.Tag(key, tags)
		if err != nil {
			return err
		}
		if admit, ok := json.Field(shape, "admit").(json.Array); ok {
			admitted := false
			for _, a := range admit {
				if a == json.String(tag) {
					admitted = true
				}
			}
			if !admitted {
				keyAt, err := at.Field(key)
				if err != nil {
					return err
				}
				return keyAt.Fail(decode.NotAdmitted, "'"+tag+"' is not admitted here")
			}
		}
		for _, c := range cases {
			if s(t, c, "tag") == tag {
				return readShape(t, defs, get(t, c, "shape"), at)
			}
		}
		t.Fatalf("no case for %s", tag)
		return nil
	case "intRange":
		_, err := at.IntRange(int32(n(t, shape, "min")), int32(n(t, shape, "max")))
		return err
	case "ref":
		def := json.Field(defs, s(t, shape, "name"))
		if def == nil {
			return at.Fail(decode.SchemaFault, "an unresolved shape")
		}
		return readShape(t, defs, def, at)
	default:
		t.Fatalf("unknown shape %s", kind)
		return nil
	}
}

func limitsOf(t testing.TB, v json.Value) json.Limits {
	l := json.Field(v, "limits")
	if l == nil {
		return json.Strict
	}
	return json.Limits{
		Depth:         int(n(t, l, "depth")),
		StringLength:  int(n(t, l, "stringLength")),
		ArrayLength:   int(n(t, l, "arrayLength")),
		ObjectMembers: int(n(t, l, "objectMembers")),
		DocumentBytes: int(n(t, l, "documentBytes")),
	}
}

func TestDecodeRejectsVectors(t *testing.T) {
	doc := read(t, "decode/decode-rejects.json")
	if got, want := len(vectors(t, doc)), declared(t, "decode", "decodeRejects"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		name := s(t, v, "name")
		defs := json.Field(v, "definitions")
		if defs == nil {
			defs = json.Object{}
		}
		var outcome *decode.Error
		value, outcome := decode.Parse(s(t, v, "input"), limitsOf(t, v))
		if outcome == nil {
			outcome = readShape(t, defs, get(t, v, "shape"), decode.Root(value))
		}
		switch r := json.Field(v, "refusal"); {
		case r == nil && outcome == nil:
			if json.Field(v, "accepted") != json.Bool(true) {
				t.Errorf("%s: accepted without saying so", name)
			}
		case r != nil && outcome != nil:
			if outcome.Code.String() != s(t, r, "code") {
				t.Errorf("%s: code %s, want %s (%s)", name, outcome.Code, s(t, r, "code"), outcome.Message)
			}
			if got, want := decode.PathJSON(outcome.Path), json.Render(get(t, r, "path")); got != want {
				t.Errorf("%s: path %s, want %s", name, got, want)
			}
		default:
			t.Errorf("%s: expected refusal=%v, got %v", name, r != nil, outcome)
		}
		ran++
	}
	if ran != 47 {
		t.Fatalf("ran %d, want 47", ran)
	}
}

// ─── laws/decimal-laws.json — the exact decimal ──────────────────────────────

func TestDecimalLawVectors(t *testing.T) {
	doc := read(t, "laws/decimal-laws.json")
	if got, want := len(vectors(t, doc)), declared(t, "laws", "decimal"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input, expected := s(t, v, "id"), get(t, v, "input"), get(t, v, "expected")
		accept := s(t, expected, "verdict") == "accept"
		var answer string
		var ok bool
		switch c := s(t, v, "case"); c {
		case "canonical":
			answer, ok = decimal.Canonical(s(t, input, "text"))
		case "compare":
			var order int
			order, ok = decimal.Compare(s(t, input, "a"), s(t, input, "b"))
			answer = strconv.Itoa(order)
		case "add":
			answer, ok = decimal.Add(s(t, input, "a"), s(t, input, "b"))
		case "toFloat":
			var f float64
			f, ok = decimal.ToFloat(s(t, input, "text"))
			answer = json.FormatFiniteDouble(f)
		default:
			// The column codec and the aggregates (22) need the columnar codec, which
			// the spine does not hold yet (DECISIONS.md D3).
			continue
		}
		if ok != accept {
			t.Errorf("%s: accepted=%v, want %v", id, ok, accept)
		} else if accept {
			var want string
			switch s(t, v, "case") {
			case "canonical":
				want = s(t, expected, "canonical")
			case "compare":
				want = strconv.Itoa(int(n(t, expected, "order")))
			case "add":
				want = s(t, expected, "sum")
			case "toFloat":
				want = s(t, expected, "float")
			}
			if answer != want {
				t.Errorf("%s: %s, want %s", id, answer, want)
			}
		}
		ran++
	}
	if ran != 53 {
		t.Fatalf("the canonical, compare, add and toFloat vectors run: %d, want 53", ran)
	}
}

// ─── escape/string-escape.json — the one string spelling ─────────────────────

func TestStringEscapeLines(t *testing.T) {
	doc := read(t, "escape/string-escape.json")
	ran := 0
	for _, line := range arr(t, doc, "lines") {
		text, ok := line.(json.String)
		if !ok {
			t.Fatalf("a line is a string")
		}
		fields := strings.Split(string(text), "\t")
		if len(fields) != 3 {
			t.Fatalf("%s: three fields", text)
		}
		if strings.HasPrefix(fields[2], `{"seq"`) {
			actor := actorOf(t, fields[1])
			if got := chain.Payload(0, chain.EncodeActor(actor), "{}"); got != fields[2] {
				t.Errorf("%s: payload %s, want %s", fields[0], got, fields[2])
			}
		} else {
			var ch string
			if hex, ok := strings.CutPrefix(fields[0], "U+"); ok {
				cp, err := strconv.ParseUint(hex, 16, 32)
				if err != nil {
					t.Fatal(err)
				}
				ch = string(rune(cp))
			} else {
				v, r := json.Parse(fields[2])
				if r != nil {
					t.Fatal(r)
				}
				ch = s(t, v, "id")
			}
			if got := json.EscapeString(ch); got != `"`+fields[1]+`"` {
				t.Errorf("%s: escaped %s, want \"%s\"", fields[0], got, fields[1])
			}
			if got := chain.EncodeActor(chain.Human{ID: ch}); got != fields[2] {
				t.Errorf("%s: actor %s, want %s", fields[0], got, fields[2])
			}
		}
		ran++
	}
	if ran != 38 {
		t.Fatalf("ran %d lines, want 38", ran)
	}
}

// ─── apply/skeleton-apply.json — the skeleton ops ────────────────────────────

func TestSkeletonApplyVectors(t *testing.T) {
	doc := read(t, "apply/skeleton-apply.json")
	if got, want := len(vectors(t, doc)), declared(t, "apply", "skeletonApply"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input, expected := s(t, v, "id"), get(t, v, "input"), get(t, v, "expected")
		base := treeOf(t, s(t, input, "tree"))
		op := opOf(t, s(t, input, "op"))
		result, rejection := ops.Apply(base, op)
		switch verdict := s(t, expected, "verdict"); {
		case verdict == "accept" && rejection == nil:
			bytes := result.Encode()
			if bytes != s(t, expected, "tree") {
				t.Errorf("%s: tree %s, want %s", id, bytes, s(t, expected, "tree"))
			}
			if got := address.OfBytes(bytes); got != s(t, expected, "hash") {
				t.Errorf("%s: hash %s, want %s", id, got, s(t, expected, "hash"))
			}
		case verdict == "reject" && rejection != nil:
			if got := rejection.Encode(); got != s(t, expected, "rejection") {
				t.Errorf("%s: rejection %s, want %s", id, got, s(t, expected, "rejection"))
			}
		default:
			t.Errorf("%s: expected %s, got rejection %v", id, verdict, rejection)
		}
		ran++
	}
	if ran != 25 {
		t.Fatalf("ran %d, want 25", ran)
	}
}

// ─── chain/skeleton-chain.json — the op-stream chain ─────────────────────────

func anchorOf(t testing.TB, v json.Value) chain.Anchor {
	return chain.Anchor{PrevHash: s(t, v, "prevHash"), Seq: uint64(n(t, v, "seq"))}
}

func TestSkeletonChainVectors(t *testing.T) {
	doc := read(t, "chain/skeleton-chain.json")
	if got, want := len(vectors(t, doc)), declared(t, "chain", "skeletonChain"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input, expected := s(t, v, "id"), get(t, v, "input"), get(t, v, "expected")
		anchor := anchorOf(t, get(t, input, "anchor"))
		switch c := s(t, v, "case"); c {
		case "append":
			at := anchor
			want := arr(t, expected, "records")
			entries := arr(t, input, "entries")
			if len(entries) != len(want) {
				t.Fatalf("%s: %d entries, %d records", id, len(entries), len(want))
			}
			for i, e := range entries {
				w := want[i]
				r, next := chain.Append(at, s(t, e, "actor"), s(t, e, "op"))
				if r.Seq != uint64(n(t, w, "seq")) || r.PrevHash != s(t, w, "prevHash") ||
					r.Payload() != s(t, w, "payload") || r.Hash != s(t, w, "hash") {
					t.Errorf("%s: record %d: %+v", id, i, r)
				}
				at = next
			}
			if at != anchorOf(t, get(t, expected, "tip")) {
				t.Errorf("%s: the tip %+v", id, at)
			}
		case "walk":
			var records []chain.Record
			for _, r := range arr(t, input, "records") {
				records = append(records, chain.Record{
					Seq: uint64(n(t, r, "seq")), Actor: s(t, r, "actor"), Op: s(t, r, "op"),
					PrevHash: s(t, r, "prevHash"), Hash: s(t, r, "hash"),
				})
			}
			tip, brk := chain.Walk(anchor, records)
			switch verdict := s(t, expected, "verdict"); {
			case verdict == "intact" && brk == nil:
				if tip != anchorOf(t, get(t, expected, "tip")) {
					t.Errorf("%s: the tip %+v", id, tip)
				}
			case verdict == "break" && brk != nil:
				if brk.Index != int(n(t, expected, "index")) || brk.Reason.String() != s(t, expected, "reason") ||
					brk.Expected != s(t, expected, "expected") || brk.Got != s(t, expected, "got") {
					t.Errorf("%s: break %+v", id, brk)
				}
			default:
				t.Errorf("%s: expected %s, got %v", id, verdict, brk)
			}
		default:
			t.Fatalf("%s: unknown case %s", id, c)
		}
		ran++
	}
	if ran != 16 {
		t.Fatalf("ran %d, want 16", ran)
	}
}

// ─── dag/skeleton-dag.json — the op-DAG ──────────────────────────────────────

func applyTree(op ops.Op, t tree.Node) (tree.Node, ops.Rejection) { return ops.Apply(t, op) }

func TestSkeletonDagVectors(t *testing.T) {
	doc := read(t, "dag/skeleton-dag.json")
	if got, want := len(vectors(t, doc)), declared(t, "dag", "skeletonDag"); got != want {
		t.Fatalf("the manifest's count: %d vectors, %d declared", got, want)
	}
	ran := 0
	for _, v := range vectors(t, doc) {
		id, input, expected := s(t, v, "id"), get(t, v, "input"), get(t, v, "expected")
		switch c := s(t, v, "case"); c {
		case "build":
			checkBuild(t, id, input, expected)
		case "verify":
			var nodes []dag.Node[ops.Op]
			for _, x := range arr(t, input, "nodes") {
				nodes = append(nodes, dag.Node[ops.Op]{
					ID: s(t, x, "id"), Parents: strItems(t, x, "parents"),
					Actor: actorOf(t, s(t, x, "actor")), Op: opOf(t, s(t, x, "op")),
				})
			}
			d := dag.FromNodes(nodes)
			brk := d.FirstBreak()
			switch verdict := s(t, expected, "verdict"); {
			case verdict == "intact" && brk == nil:
			case verdict == "break" && brk != nil:
				if brk.Node != s(t, expected, "node") || brk.Reason.String() != s(t, expected, "reason") ||
					brk.Expected != s(t, expected, "expected") || brk.Got != s(t, expected, "got") {
					t.Errorf("%s: break %+v", id, brk)
				}
			default:
				t.Errorf("%s: expected %s, got %v", id, verdict, brk)
			}
		default:
			t.Fatalf("%s: unknown case %s", id, c)
		}
		ran++
	}
	if ran != 11 {
		t.Fatalf("ran %d, want 11", ran)
	}
}

func checkBuild(t *testing.T, id string, input, expected json.Value) {
	t.Helper()
	state0 := treeOf(t, s(t, input, "state0"))
	d := dag.New[ops.Op]()
	ids := map[string]string{}   // label -> id
	names := map[string]string{} // id -> the first label that wrote it
	writes := arr(t, input, "writes")
	wantWrites := arr(t, expected, "writes")
	if len(writes) != len(wantWrites) {
		t.Fatalf("%s: %d writes, %d expected", id, len(writes), len(wantWrites))
	}
	for i, w := range writes {
		want := wantWrites[i]
		label := s(t, w, "label")
		var parents []string
		for _, p := range strItems(t, w, "parents") {
			if l, ok := strings.CutPrefix(p, "@"); ok {
				parents = append(parents, ids[l])
			} else {
				parents = append(parents, p)
			}
		}
		actor, op := actorOf(t, s(t, w, "actor")), opOf(t, s(t, w, "op"))
		var node string
		var fault dag.AppendFault
		switch verb := s(t, w, "verb"); verb {
		case "appendOn":
			node, fault = d.AppendOn(actor, op, parents)
		case "mergeAll":
			node, fault = d.MergeAll(actor, op, parents)
		default:
			t.Fatalf("%s: unknown verb %s", id, verb)
		}
		if s(t, want, "label") != label {
			t.Fatalf("%s: write %s expected under %s", id, label, s(t, want, "label"))
		}
		if fault == nil {
			if node != s(t, want, "id") {
				t.Errorf("%s: write %s: id %s, want %s", id, label, node, s(t, want, "id"))
			}
			ids[label] = node
			if _, named := names[node]; !named {
				names[node] = label
			}
		} else if got := fault.Encode(); got != s(t, want, "refused") {
			t.Errorf("%s: write %s: %s, want %s", id, label, got, s(t, want, "refused"))
		}
	}
	name := func(node string) string {
		l, ok := names[node]
		if !ok {
			t.Fatalf("%s: no label for %s", id, node)
		}
		return l
	}
	labels := func(nodes []string) []string {
		out := make([]string, len(nodes))
		for i, x := range nodes {
			out[i] = name(x)
		}
		return out
	}
	if got, want := labels(d.Heads()), strItems(t, expected, "heads"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: heads %v, want %v", id, got, want)
	}
	order, cyc := d.TotalOrder()
	if cyc != nil {
		t.Fatalf("%s: %v", id, cyc)
	}
	if got, want := labels(order), strItems(t, expected, "order"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: order %v, want %v", id, got, want)
	}
	ranks, _ := d.Ranks()
	var gotRanks []string
	for k, r := range ranks {
		gotRanks = append(gotRanks, name(k)+"="+strconv.Itoa(r))
	}
	var wantRanks []string
	for _, m := range get(t, expected, "ranks").(json.Object) {
		wantRanks = append(wantRanks, m.Key+"="+strconv.Itoa(int(m.Value.(json.Number))))
	}
	sort.Strings(gotRanks)
	sort.Strings(wantRanks)
	if fmt.Sprint(gotRanks) != fmt.Sprint(wantRanks) {
		t.Errorf("%s: ranks %v, want %v", id, gotRanks, wantRanks)
	}

	replays := arr(t, expected, "replays")
	heads := d.Heads()
	if len(replays) != len(heads) {
		t.Fatalf("%s: %d replays for %d heads", id, len(replays), len(heads))
	}
	for i, head := range heads {
		want := replays[i]
		if name(head) != s(t, want, "head") {
			t.Errorf("%s: replay %d is of %s, want %s", id, i, name(head), s(t, want, "head"))
		}
		got, fault := dag.ReplayTo(d, state0, head, applyTree)
		switch verdict := s(t, want, "verdict"); {
		case verdict == "accept" && fault == nil:
			if got.Encode() != s(t, want, "tree") || address.OfBytes(got.Encode()) != s(t, want, "hash") {
				t.Errorf("%s: replay %s: %s", id, name(head), got.Encode())
			}
		case verdict == "reject" && fault != nil && fault.IsRejected():
			if name(fault.Rejected) != s(t, want, "node") || fault.Rejection.Encode() != s(t, want, "rejection") {
				t.Errorf("%s: replay %s: rejected at %s with %s", id, name(head), name(fault.Rejected), fault.Rejection.Encode())
			}
		default:
			t.Errorf("%s: replay %s: expected %s, got %v", id, name(head), verdict, fault)
		}
	}
	all := get(t, expected, "replayAll")
	got, fault := dag.ReplayAll(d, state0, applyTree)
	switch verdict := s(t, all, "verdict"); {
	case verdict == "accept" && fault == nil:
		if got.Encode() != s(t, all, "tree") || address.OfBytes(got.Encode()) != s(t, all, "hash") {
			t.Errorf("%s: replayAll: %s", id, got.Encode())
		}
	case verdict == "reject" && fault != nil && fault.IsRejected():
		if name(fault.Rejected) != s(t, all, "node") || fault.Rejection.Encode() != s(t, all, "rejection") {
			t.Errorf("%s: replayAll: rejected at %s with %s", id, name(fault.Rejected), fault.Rejection.Encode())
		}
	default:
		t.Errorf("%s: replayAll: expected %s, got %v", id, verdict, fault)
	}
}
