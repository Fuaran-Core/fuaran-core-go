package conformance

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Fuaran-Core/fuaran-core-go/chain"
	"github.com/Fuaran-Core/fuaran-core-go/decode"
	"github.com/Fuaran-Core/fuaran-core-go/json"
	"github.com/Fuaran-Core/fuaran-core-go/ops"
	"github.com/Fuaran-Core/fuaran-core-go/oracle"
	"github.com/Fuaran-Core/fuaran-core-go/tree"
)

// The oracle differential: the reference's extracted proof models, run as a process
// (its docs/oracle-protocol.md), asked about inputs nobody chose and compared with
// the spine's own answer. Two harnesses over one generator, and the pairing is the
// point (the Go UI host's decoder fuzz has the same shape):
//
//   - TestOracleDifferential* drive a deterministic SplitMix64 stream from
//     DifferentialSeed over DifferentialInputs inputs per function, so the
//     differential runs reproducibly on every `go test` and its stated seed and input
//     count are what host-compliance.json declares as oracle-differential evidence;
//   - FuzzOracle* are native testing.F targets over the same comparison, seeded from
//     the same stream and from every vector of the function's family, so
//     `go test -fuzz=FuzzOracleJSONParse -fuzztime=20s ./conformance` adds
//     coverage-guided mutation — the bounded run run.ps1 makes.
//
// The oracle is found through FUARAN_CORE_ORACLE (a command line, such as
// `dotnet <path>/fuaran-core-oracle.dll`), or built under the Core checkout's
// tools/Fuaran.Core.Oracle.Cli/bin/Release. Like the vectors, it is not optional: a
// differential that skipped its oracle would read green having compared nothing.

// DifferentialSeed is the SplitMix64 seed the deterministic differential draws from.
const DifferentialSeed uint64 = 0x5eed485

// DifferentialInputs is the number of inputs the deterministic differential asks
// the oracle about, per function.
const DifferentialInputs = 2000

func oracleClient(t testing.TB) *oracle.Client {
	t.Helper()
	line := os.Getenv("FUARAN_CORE_ORACLE")
	if line == "" {
		dll := filepath.Join(coreDir(t), "tools", "Fuaran.Core.Oracle.Cli", "bin", "Release", "net10.0", "fuaran-core-oracle.dll")
		if _, err := os.Stat(dll); err != nil {
			t.Fatalf("no oracle: build it from the Core checkout (dotnet build tools/Fuaran.Core.Oracle.Cli -c Release) or set FUARAN_CORE_ORACLE to a command line that serves the protocol")
		}
		line = "dotnet " + dll
	}
	c, err := oracle.StartCommandLine(line)
	if err != nil {
		t.Fatalf("starting the oracle: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	protocol, err := c.Describe()
	if err != nil {
		t.Fatalf("oracle.describe: %v", err)
	}
	if !strings.HasPrefix(protocol, "1.") {
		t.Fatalf("the oracle serves protocol %s; this harness pins major 1", protocol)
	}
	return c
}

// splitMix64 is the deterministic stream (Steele, Lea and Flood, 2014).
type splitMix64 struct{ state uint64 }

func (r *splitMix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *splitMix64) below(n int) int { return int(r.next() % uint64(n)) }

func (r *splitMix64) pick(xs []string) string { return xs[r.below(len(xs))] }

// ─── json.parse ──────────────────────────────────────────────────────────────

// jsonAtoms are the pieces a generated document is assembled and mutated from: the
// grammar's tokens, every row's trigger, and the number form's edges.
var jsonAtoms = []string{
	"{", "}", "[", "]", ",", ":", " ", "\n", "\t",
	`"a"`, `"b"`, `""`, `"é"`, `"😀"`, `"\ud800"`, `"\udc00"`, `"\x"`, `"\u12"`, `"\u12g4"`, "\"\t\"", `"abc`,
	"0", "-0", "1", "-1", "01", "1.5", ".5", "5.", "1e3", "1E+21", "1e999", "1e-400", "+1",
	"9007199254740992", "9007199254740993", "18014398509481984", "2147483647", "2147483648", "-2147483648", "-2147483649",
	"true", "false", "null", "tru", "nul", "NaN", "Infinity", "-Infinity",
	"é", "€", "😀",
}

// genJSON draws one document: a well-formed one with probability about a half, else
// a sequence of atoms, else a well-formed one with one byte-level mutation.
func genJSON(r *splitMix64, depth int) string {
	switch r.below(4) {
	case 0, 1:
		return genWellFormed(r, depth)
	case 2:
		n := 1 + r.below(8)
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(r.pick(jsonAtoms))
		}
		return b.String()
	default:
		s := genWellFormed(r, depth)
		if s == "" {
			return s
		}
		i := r.below(len(s))
		switch r.below(3) {
		case 0:
			return s[:i] + s[i+1:]
		case 1:
			return s[:i] + r.pick(jsonAtoms) + s[i:]
		default:
			return s[:i] + s[i:] + s[:i]
		}
	}
}

func genWellFormed(r *splitMix64, depth int) string {
	if depth <= 0 || r.below(3) == 0 {
		return r.pick([]string{`"a"`, `"b"`, `"é"`, `"😀"`, "0", "1", "-1", "1.5", "1e3", "1E+21", "9007199254740992", "2147483648", "true", "false", "null", `"😀"`})
	}
	n := r.below(4)
	items := make([]string, n)
	if r.below(2) == 0 {
		for i := range items {
			items[i] = genWellFormed(r, depth-1)
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	keys := []string{`"a"`, `"b"`, `"c"`, `"é"`}
	for i := range items {
		items[i] = r.pick(keys) + ":" + genWellFormed(r, depth-1)
	}
	return "{" + strings.Join(items, ",") + "}"
}

// oracleValue converts the oracle's tagged value form to the decode layer's model.
func oracleValue(t testing.TB, v json.Value) decode.Value {
	t.Helper()
	o, ok := v.(json.Object)
	if !ok || len(o) != 1 {
		t.Fatalf("a tagged value is a one-member object: %s", json.Render(v))
	}
	m := o[0]
	switch m.Key {
	case "string":
		return decode.Str(m.Value.(json.String))
	case "int":
		i, err := strconv.ParseInt(string(m.Value.(json.String)), 10, 32)
		if err != nil {
			t.Fatalf("an int token: %v", err)
		}
		return decode.Int(int32(i))
	case "float":
		f, err := strconv.ParseFloat(string(m.Value.(json.String)), 64)
		if err != nil && !math.IsInf(f, 0) {
			t.Fatalf("a float token: %v", err)
		}
		return decode.Float(f)
	case "bool":
		return decode.Bool(m.Value.(json.Bool))
	case "array":
		items := m.Value.(json.Array)
		out := make(decode.Arr, len(items))
		for i, item := range items {
			out[i] = oracleValue(t, item)
		}
		return out
	case "object":
		members := m.Value.(json.Array)
		out := make(decode.Obj, len(members))
		for i, member := range members {
			out[i] = decode.Pair{Key: string(json.Field(member, "key").(json.String)), Value: oracleValue(t, json.Field(member, "value"))}
		}
		return out
	}
	t.Fatalf("an unknown tag %s", m.Key)
	return nil
}

func sameValue(a, b decode.Value) bool {
	switch x := a.(type) {
	case decode.Str:
		y, ok := b.(decode.Str)
		return ok && x == y
	case decode.Int:
		y, ok := b.(decode.Int)
		return ok && x == y
	case decode.Float:
		y, ok := b.(decode.Float)
		return ok && math.Float64bits(float64(x)) == math.Float64bits(float64(y))
	case decode.Bool:
		y, ok := b.(decode.Bool)
		return ok && x == y
	case decode.Arr:
		y, ok := b.(decode.Arr)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case decode.Obj:
		y, ok := b.(decode.Obj)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i].Key != y[i].Key || !sameValue(x[i].Value, y[i].Value) {
				return false
			}
		}
		return true
	}
	return false
}

// compareJSONParse asks json.parse about text under the oracle's default limits
// (depth 512, the reader's own Strict) and compares the answer with the spine's
// null-free reading: the same verdict, the same refusal class and bound, and for an
// ASCII text the same position; the same value when both accept. A text that is
// not UTF-8 cannot be sent, and is not compared.
func compareJSONParse(t testing.TB, c *oracle.Client, text string) {
	t.Helper()
	if !utf8.ValidString(text) {
		return
	}
	answer, err := c.Call("json.parse", `{"text":`+json.EscapeString(text)+`}`)
	if err != nil {
		t.Fatalf("json.parse %q: %v", text, err)
	}
	mine, refusal := decode.ReadValue(text, json.Strict)
	ascii := isASCIIText(text)
	switch {
	case json.Field(answer, "value") != nil:
		if refusal != nil {
			t.Fatalf("json.parse %q: the oracle accepts, the spine refuses: %v", text, refusal)
		}
		if theirs := oracleValue(t, json.Field(answer, "value")); !sameValue(mine, theirs) {
			t.Fatalf("json.parse %q: values differ", text)
		}
	case json.Field(answer, "parseRefused") != nil:
		r := json.Field(answer, "parseRefused")
		e, ok := refusal.(*json.Error)
		if !ok {
			t.Fatalf("json.parse %q: the oracle refuses %s, the spine answers %v", text, json.Render(r), refusal)
		}
		if e.Kind.String() != s(t, r, "kind") {
			t.Fatalf("json.parse %q: the oracle refuses %s, the spine %s (%s)", text, s(t, r, "kind"), e.Kind, e.Message)
		}
		if ascii && e.At != int(n(t, r, "at")) {
			t.Fatalf("json.parse %q: refused at %d, the oracle at %d", text, e.At, int(n(t, r, "at")))
		}
	case json.Field(answer, "parseLimit") != nil:
		l := json.Field(answer, "parseLimit")
		b, ok := refusal.(*json.LimitBreach)
		if !ok || b.Limit.String() != s(t, l, "limit") {
			t.Fatalf("json.parse %q: the oracle refuses at bound %s, the spine %v", text, s(t, l, "limit"), refusal)
		}
		if ascii && b.At != int(n(t, l, "at")) {
			t.Fatalf("json.parse %q: breach at %d, the oracle at %d", text, b.At, int(n(t, l, "at")))
		}
	default:
		t.Fatalf("json.parse %q: an unknown answer %s", text, json.Render(answer))
	}
}

func isASCIIText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func jsonParseSeeds(t testing.TB) []string {
	var seeds []string
	for _, v := range vectors(t, read(t, "refusals/codec-refusals.json")) {
		if s(t, v, "codec") == "json" {
			seeds = append(seeds, s(t, v, "input"))
		}
	}
	for _, v := range vectors(t, read(t, "decode/decode-rejects.json")) {
		if !has(v, "limits") {
			seeds = append(seeds, s(t, v, "input"))
		}
	}
	for _, v := range vectors(t, read(t, "premises/int53.json")) {
		seeds = append(seeds, s(t, v, "token"))
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < 64; i++ {
		seeds = append(seeds, genJSON(r, 4))
	}
	return seeds
}

func TestOracleDifferentialJSONParse(t *testing.T) {
	c := oracleClient(t)
	for _, seed := range jsonParseSeeds(t) {
		compareJSONParse(t, c, seed)
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < DifferentialInputs; i++ {
		compareJSONParse(t, c, genJSON(r, 5))
	}
}

func FuzzOracleJSONParse(f *testing.F) {
	for _, seed := range jsonParseSeeds(f) {
		f.Add(seed)
	}
	c := oracleClient(f)
	f.Fuzz(func(t *testing.T, text string) { compareJSONParse(t, c, text) })
}

// ─── tree.apply ──────────────────────────────────────────────────────────────

var treeIDs = []string{"root", "a", "a1", "a2", "b", "b1", "c", "n1", "n2", "zz"}
var treeKinds = []string{"doc", "section", "para"}

func genTree(r *splitMix64) tree.Node {
	root := tree.Node{ID: "root", Kind: "doc"}
	used := map[string]bool{"root": true}
	var grow func(n *tree.Node, depth int)
	grow = func(n *tree.Node, depth int) {
		if depth == 0 {
			return
		}
		for i := r.below(4); i > 0; i-- {
			id := r.pick(treeIDs)
			if used[id] {
				continue
			}
			used[id] = true
			n.Children = append(n.Children, tree.Node{ID: id, Kind: r.pick(treeKinds)})
			grow(&n.Children[len(n.Children)-1], depth-1)
		}
	}
	grow(&root, 3)
	return root
}

func genOp(r *splitMix64, depth int) ops.Op {
	switch r.below(6) {
	case 0:
		return ops.InsertChild{Parent: r.pick(treeIDs), Node: tree.Node{ID: r.pick(treeIDs), Kind: r.pick(treeKinds)}}
	case 1:
		return ops.RemoveNode{Target: r.pick(treeIDs)}
	case 2:
		return ops.MoveNode{Target: r.pick(treeIDs), NewParent: r.pick(treeIDs)}
	case 3:
		order := make([]string, r.below(4))
		for i := range order {
			order[i] = r.pick(treeIDs)
		}
		return ops.ReorderChildren{Parent: r.pick(treeIDs), Order: order}
	case 4:
		if depth > 0 {
			members := make([]ops.Op, r.below(4))
			for i := range members {
				members[i] = genOp(r, depth-1)
			}
			return ops.Batch{Ops: members}
		}
		return ops.RemoveNode{Target: r.pick(treeIDs)}
	default:
		return ops.UpdateNode{Node: tree.Node{ID: r.pick(treeIDs), Kind: r.pick(treeKinds)}}
	}
}

// oracleTree renders a tree in the oracle's own form, {"id","kind","children"}.
func oracleTree(n *tree.Node) string {
	items := make([]string, len(n.Children))
	for i := range n.Children {
		items[i] = oracleTree(&n.Children[i])
	}
	return `{"id":` + json.EscapeString(n.ID) + `,"kind":` + json.EscapeString(n.Kind) + `,"children":` + json.RenderArray(items) + "}"
}

// oracleOp renders an op in the oracle's own form.
func oracleOp(op ops.Op) string {
	switch o := op.(type) {
	case ops.InsertChild:
		return `{"op":"insertChild","parent":` + json.EscapeString(o.Parent) + `,"node":` + oracleTree(&o.Node) + "}"
	case ops.RemoveNode:
		return `{"op":"removeNode","node":` + json.EscapeString(o.Target) + "}"
	case ops.MoveNode:
		return `{"op":"moveNode","node":` + json.EscapeString(o.Target) + `,"newParent":` + json.EscapeString(o.NewParent) + "}"
	case ops.ReorderChildren:
		items := make([]string, len(o.Order))
		for i, id := range o.Order {
			items[i] = json.EscapeString(id)
		}
		return `{"op":"reorderChildren","parent":` + json.EscapeString(o.Parent) + `,"order":` + json.RenderArray(items) + "}"
	case ops.Batch:
		items := make([]string, len(o.Ops))
		for i, member := range o.Ops {
			items[i] = oracleOp(member)
		}
		return `{"op":"batch","ops":` + json.RenderArray(items) + "}"
	case ops.UpdateNode:
		return `{"op":"updateNode","node":` + oracleTree(&o.Node) + "}"
	}
	panic("not an op")
}

// treeOfOracle reads the oracle's tree form.
func treeOfOracle(t testing.TB, v json.Value) tree.Node {
	t.Helper()
	n := tree.Node{ID: s(t, v, "id"), Kind: s(t, v, "kind")}
	if children, ok := json.Field(v, "children").(json.Array); ok {
		for _, c := range children {
			n.Children = append(n.Children, treeOfOracle(t, c))
		}
	}
	return n
}

// rejectionOfOracle spells the oracle's rejection as the spine's rejection bytes.
func rejectionOfOracle(t testing.TB, r json.Value) string {
	t.Helper()
	switch kind := s(t, r, "rejection"); kind {
	case "unknownNode":
		return ops.UnknownNode{Target: s(t, r, "target")}.Encode()
	case "duplicateId":
		return ops.DuplicateID{ID: s(t, r, "id")}.Encode()
	case "cannotRemoveRoot":
		return ops.CannotRemoveRoot{}.Encode()
	case "wouldNestUnderSelf":
		return ops.WouldNestUnderSelf{Target: s(t, r, "node")}.Encode()
	case "reorderMismatch":
		return ops.ReorderMismatch{Parent: s(t, r, "parent")}.Encode()
	default:
		return kind
	}
}

func compareTreeApply(t testing.TB, c *oracle.Client, base tree.Node, op ops.Op) {
	t.Helper()
	answer, err := c.Call("tree.apply", `{"tree":`+oracleTree(&base)+`,"op":`+oracleOp(op)+`}`)
	if err != nil {
		t.Fatalf("tree.apply %s on %s: %v", op.Encode(), base.Encode(), err)
	}
	mine, rejection := ops.Apply(base, op)
	switch {
	case json.Field(answer, "applied") != nil:
		if rejection != nil {
			t.Fatalf("tree.apply %s on %s: the oracle applies, the spine rejects %s", op.Encode(), base.Encode(), rejection.Encode())
		}
		theirs := treeOfOracle(t, json.Field(answer, "applied"))
		if !mine.Equal(theirs) {
			t.Fatalf("tree.apply %s on %s: %s, the oracle %s", op.Encode(), base.Encode(), mine.Encode(), theirs.Encode())
		}
	case json.Field(answer, "rejected") != nil:
		want := rejectionOfOracle(t, json.Field(answer, "rejected"))
		if rejection == nil {
			t.Fatalf("tree.apply %s on %s: the oracle rejects %s, the spine applies", op.Encode(), base.Encode(), want)
		}
		if rejection.Encode() != want {
			t.Fatalf("tree.apply %s on %s: %s, the oracle %s", op.Encode(), base.Encode(), rejection.Encode(), want)
		}
	default:
		t.Fatalf("tree.apply: an unknown answer %s", json.Render(answer))
	}
}

// compareTreeApplyText is compareTreeApply over the skeleton texts of a tree and an
// op, as a fuzz target receives them; a text that is not a tree or an op is no
// comparison.
func compareTreeApplyText(t testing.TB, c *oracle.Client, treeText, opText string) {
	t.Helper()
	base, err := tree.Parse(treeText)
	if err != nil {
		return
	}
	op, err := ops.Parse(opText)
	if err != nil {
		return
	}
	compareTreeApply(t, c, base, op)
}

func TestOracleDifferentialTreeApply(t *testing.T) {
	c := oracleClient(t)
	for _, v := range vectors(t, read(t, "apply/skeleton-apply.json")) {
		compareTreeApplyText(t, c, s(t, get(t, v, "input"), "tree"), s(t, get(t, v, "input"), "op"))
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < DifferentialInputs; i++ {
		compareTreeApply(t, c, genTree(r), genOp(r, 2))
	}
}

func FuzzOracleTreeApply(f *testing.F) {
	for _, v := range vectors(f, read(f, "apply/skeleton-apply.json")) {
		f.Add(s(f, get(f, v, "input"), "tree"), s(f, get(f, v, "input"), "op"))
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < 64; i++ {
		base, op := genTree(r), genOp(r, 2)
		f.Add(base.Encode(), op.Encode())
	}
	c := oracleClient(f)
	f.Fuzz(func(t *testing.T, treeText, opText string) { compareTreeApplyText(t, c, treeText, opText) })
}

// ─── chain.verify ────────────────────────────────────────────────────────────

var chainActors = []string{`{"kind":"human","id":"alice"}`, `{"kind":"human","id":"bob"}`, `{"kind":"agent","model":"m","version":"1","id":"x"}`}
var chainOps = []string{"{}", `{"$type":"removeNode","target":"a1"}`, `{"a":1}`, `[]`}

// genChain is an intact run of records from the genesis anchor, with probability
// about a half tampered in one member of one record.
func genChain(r *splitMix64) []chain.Record {
	at := chain.GenesisAnchor()
	records := make([]chain.Record, r.below(6))
	for i := range records {
		records[i], at = chain.Append(at, r.pick(chainActors), r.pick(chainOps))
	}
	if len(records) > 0 && r.below(2) == 0 {
		i := r.below(len(records))
		switch r.below(5) {
		case 0:
			records[i].Seq += uint64(1 + r.below(3))
		case 1:
			records[i].PrevHash = "deadbeef"
		case 2:
			records[i].Hash = records[i].Hash[1:] + "0"
		case 3:
			records[i].Op = `{"tampered":true}`
		default:
			records[i].Actor = `{"kind":"human","id":"mallory"}`
		}
	}
	return records
}

func oracleRecords(records []chain.Record) string {
	items := make([]string, len(records))
	for i, rec := range records {
		items[i] = `{"seq":` + strconv.FormatUint(rec.Seq, 10) + `,"actor":` + json.EscapeString(rec.Actor) + `,"op":` + json.EscapeString(rec.Op) +
			`,"prev":` + json.EscapeString(rec.PrevHash) + `,"hash":` + json.EscapeString(rec.Hash) + "}"
	}
	return json.RenderArray(items)
}

func compareChainVerify(t testing.TB, c *oracle.Client, records []chain.Record) {
	t.Helper()
	answer, err := c.Call("chain.verify", `{"hash":"sha256","records":`+oracleRecords(records)+`}`)
	if err != nil {
		t.Fatalf("chain.verify: %v", err)
	}
	_, brk := chain.Walk(chain.GenesisAnchor(), records)
	switch {
	case json.Field(answer, "intact") != nil:
		if brk != nil {
			t.Fatalf("chain.verify %s: the oracle finds it intact, the spine %v", oracleRecords(records), brk)
		}
	case json.Field(answer, "broken") != nil:
		b := json.Field(answer, "broken")
		if brk == nil {
			t.Fatalf("chain.verify %s: the oracle finds %s, the spine finds it intact", oracleRecords(records), json.Render(b))
		}
		if brk.Index != int(n(t, b, "index")) || brk.Reason.String() != s(t, b, "reason") || brk.Expected != s(t, b, "expected") || brk.Got != s(t, b, "got") {
			t.Fatalf("chain.verify %s: the spine finds %+v, the oracle %s", oracleRecords(records), brk, json.Render(b))
		}
	default:
		t.Fatalf("chain.verify: an unknown answer %s", json.Render(answer))
	}
}

// chainOfText reads a run of records from the chain vectors' record form (a JSON
// array of {seq, actor, op, prevHash, hash}); not a run is no comparison.
func chainOfText(text string) ([]chain.Record, bool) {
	v, r := json.Parse(text)
	items, ok := v.(json.Array)
	if r != nil || !ok {
		return nil, false
	}
	records := make([]chain.Record, 0, len(items))
	for _, item := range items {
		seq, okSeq := json.Field(item, "seq").(json.Number)
		actor, okActor := json.Field(item, "actor").(json.String)
		op, okOp := json.Field(item, "op").(json.String)
		prev, okPrev := json.Field(item, "prevHash").(json.String)
		hash, okHash := json.Field(item, "hash").(json.String)
		if !(okSeq && okActor && okOp && okPrev && okHash) || seq < 0 || seq > 1<<20 || seq != json.Number(math.Trunc(float64(seq))) {
			return nil, false
		}
		records = append(records, chain.Record{Seq: uint64(seq), Actor: string(actor), Op: string(op), PrevHash: string(prev), Hash: string(hash)})
	}
	return records, true
}

func chainSeeds(t testing.TB) []string {
	var seeds []string
	for _, v := range vectors(t, read(t, "chain/skeleton-chain.json")) {
		if s(t, v, "case") == "walk" {
			seeds = append(seeds, json.Render(get(t, get(t, v, "input"), "records")))
		}
	}
	return seeds
}

func TestOracleDifferentialChainVerify(t *testing.T) {
	c := oracleClient(t)
	for _, seed := range chainSeeds(t) {
		if records, ok := chainOfText(seed); ok {
			compareChainVerify(t, c, records)
		}
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < DifferentialInputs; i++ {
		compareChainVerify(t, c, genChain(r))
	}
}

func FuzzOracleChainVerify(f *testing.F) {
	for _, seed := range chainSeeds(f) {
		f.Add(seed)
	}
	r := &splitMix64{state: DifferentialSeed}
	for i := 0; i < 32; i++ {
		f.Add(strings.ReplaceAll(strings.ReplaceAll(oracleRecords(genChain(r)), `"prev":`, `"prevHash":`), `"hash":`, `"hash":`))
	}
	c := oracleClient(f)
	f.Fuzz(func(t *testing.T, text string) {
		if records, ok := chainOfText(text); ok {
			compareChainVerify(t, c, records)
		}
	})
}
