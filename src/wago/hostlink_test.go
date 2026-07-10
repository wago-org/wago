//go:build linux && amd64 && !tinygo

package wago

import (
	"os"
	"testing"
)

// TestHostLinkCached verifies the host-only link recompile is memoized: a
// needsLink module (WASI, returning imports) links once and every later host
// Instantiate reuses that linked module + its code mapping instead of re-running
// the backend. Guards the large-module instantiate optimization.
func TestHostLinkCached(t *testing.T) {
	src, err := os.ReadFile("../../bench/corpus/jsonproc.wasm")
	if err != nil {
		t.Skip("jsonproc.wasm not present")
	}
	c, err := Compile(nil, src)
	if err != nil {
		t.Fatal(err)
	}
	if !c.needsLink || c.hostLink == nil {
		t.Fatalf("expected a deferred-codegen (needsLink) module with a host-link cache")
	}
	// Satisfy the module's imports with bare stubs; this test exercises link
	// caching, not WASI behavior, so the imports need only bind.
	stubs := Imports{}
	for _, name := range c.Imports {
		stubs[name] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	l1, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("link 1: %v", err)
	}
	l2, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("link 2: %v", err)
	}
	if l1 == c {
		t.Fatal("host link should recompile to a fresh linked module")
	}
	if l1 != l2 {
		t.Fatal("host link not cached: repeated linkModule produced different linked modules")
	}
}
