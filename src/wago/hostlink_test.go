//go:build linux && amd64 && !tinygo

package wago

import (
	"os"
	"testing"
)

// TestHostImportsCompileUpFront verifies host-only imports compile directly to
// the synchronous host-call path. Cross-instance imports still relink from the
// retained wasm bytes, but host-only Instantiate should not re-run the backend.
func TestHostImportsCompileUpFront(t *testing.T) {
	src, err := os.ReadFile("../../bench/corpus/jsonproc.wasm")
	if err != nil {
		t.Skip("jsonproc.wasm not present")
	}
	c, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	if c.needsLink || c.hostLink != nil {
		t.Fatalf("host-only module should not use deferred codegen/cache")
	}
	if len(c.Code) == 0 || len(c.Entry) == 0 {
		t.Fatalf("host-only module should have compiled code")
	}
	l1, err := c.linkModule(WASI(WASIConfig{}))
	if err != nil {
		t.Fatalf("link 1: %v", err)
	}
	l2, err := c.linkModule(WASI(WASIConfig{}))
	if err != nil {
		t.Fatalf("link 2: %v", err)
	}
	if l1 != c || l2 != c {
		t.Fatal("host-only link should reuse the compiled module")
	}
}
