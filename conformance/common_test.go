// Package conformance certifies the spine against the conformance vectors the
// reference emits, read from a checkout of Core (Fuaran-Core/fuaran-core) at the
// commit core-pin.json names.
//
// The checkout is found at FUARAN_CORE_DIR, or as fuaran-core/ beside this
// repository or beside any directory above it (the layout CI checks it out in). It
// is NOT optional: a suite that skipped its vectors would read green while
// certifying nothing, so an absent checkout fails every leg here, naming how to
// provide it.
//
// Each leg asserts the vector count its family's manifest.json declares (where the
// family has one), and fails on a vector it did not run, so a leg that silently ran
// nothing cannot pass.
package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Fuaran-Core/fuaran-core-go/json"
)

func isCore(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "conformance", "apply", "manifest.json"))
	return err == nil
}

// coreDir locates the Core checkout, or fails the test naming the remedy.
func coreDir(t testing.TB) string {
	t.Helper()
	if d := os.Getenv("FUARAN_CORE_DIR"); d != "" {
		if !isCore(d) {
			t.Fatalf("FUARAN_CORE_DIR=%s is not a Core checkout (no conformance/apply/manifest.json)", d)
		}
		return d
	}
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := filepath.Dir(here); ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "fuaran-core")
		if isCore(candidate) {
			return candidate
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	t.Fatalf("no Core checkout found: clone https://github.com/Fuaran-Core/fuaran-core beside this repository (as fuaran-core/), checked out at the commit core-pin.json names, or set FUARAN_CORE_DIR to one")
	return ""
}

// read parses conformance/<rel> of the Core checkout.
func read(t testing.TB, rel string) json.Value {
	t.Helper()
	path := filepath.Join(coreDir(t), "conformance", filepath.FromSlash(rel))
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	v, r := json.Parse(string(text))
	if r != nil {
		t.Fatalf("%s: %v", path, r)
	}
	return v
}

// get is a required member.
func get(t testing.TB, v json.Value, k string) json.Value {
	t.Helper()
	m := json.Field(v, k)
	if m == nil {
		t.Fatalf("missing member '%s' in %s", k, json.Render(v))
	}
	return m
}

// has reports whether v holds the member.
func has(v json.Value, k string) bool { return json.Field(v, k) != nil }

// s is a required string member.
func s(t testing.TB, v json.Value, k string) string {
	t.Helper()
	x, ok := get(t, v, k).(json.String)
	if !ok {
		t.Fatalf("'%s' is not a string: %s", k, json.Render(get(t, v, k)))
	}
	return string(x)
}

// n is a required number member.
func n(t testing.TB, v json.Value, k string) float64 {
	t.Helper()
	x, ok := get(t, v, k).(json.Number)
	if !ok {
		t.Fatalf("'%s' is not a number: %s", k, json.Render(get(t, v, k)))
	}
	return float64(x)
}

// arr is a required array member.
func arr(t testing.TB, v json.Value, k string) json.Array {
	t.Helper()
	x, ok := get(t, v, k).(json.Array)
	if !ok {
		t.Fatalf("'%s' is not an array: %s", k, json.Render(get(t, v, k)))
	}
	return x
}

// strItems is a required array-of-strings member.
func strItems(t testing.TB, v json.Value, k string) []string {
	t.Helper()
	items := arr(t, v, k)
	out := make([]string, len(items))
	for i, item := range items {
		x, ok := item.(json.String)
		if !ok {
			t.Fatalf("'%s'[%d] is not a string", k, i)
		}
		out[i] = string(x)
	}
	return out
}

// declared is the family row's declared vector count in <dir>/manifest.json.
func declared(t testing.TB, dir, family string) int {
	t.Helper()
	m := read(t, dir+"/manifest.json")
	for _, row := range arr(t, m, "families") {
		if s(t, row, "id") == family {
			return int(n(t, row, "vectors"))
		}
	}
	t.Fatalf("%s/manifest.json declares no %s", dir, family)
	return 0
}

// vectors is the document's vectors member.
func vectors(t testing.TB, doc json.Value) json.Array { return arr(t, doc, "vectors") }
