//go:build amd64 && (linux || darwin || windows) && !tinygo && wago_codegenstats

package wago

import (
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func divRemPairCount(t *testing.T, f divRemPairFixture) int {
	t.Helper()
	m, e := wasm.DecodeModule(f.Module())
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats backend.ModuleStats
	c, e := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CodeImage.Close()
	count := 0
	for _, s := range stats.Funcs {
		count += s.Peephole["divrem-adjacent-pair"]
	}
	return count
}

func TestDivRemPairAdmissionAMD64(t *testing.T) {
	for _, f := range divRemPairFixtures() {
		if n := divRemPairCount(t, f); n != 1 {
			t.Fatalf("%+v: native pair count %d, want 1", f, n)
		}
	}
	for _, gap := range []string{"mutate", "effect", "callback", "trap", "boundary", "window", "distinct"} {
		f := divRemPairFixture{Loop: "throughput", Gap: gap}
		if n := divRemPairCount(t, f); n != 0 {
			t.Fatalf("gap=%s: unexpectedly fused %d pairs", gap, n)
		}
	}
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			f := divRemPairFixture{Wide: wide, Signed: signed, RemFirst: true, Loop: "throughput"}
			if n := divRemPairCount(t, f); n != 0 {
				t.Fatalf("remainder first: %+v count=%d", f, n)
			}
		}
	}
}

func TestDivRemPairPrefixAdmissionAMD64(t *testing.T) {
	m, e := wasm.DecodeModule(divRemPairPrefixModule())
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats backend.ModuleStats
	c, e := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CodeImage.Close()
	if n := stats.Funcs[0].Peephole["divrem-adjacent-pair"]; n != 1 {
		t.Fatalf("prefix fixture fused %d pairs, want 1", n)
	}
}
