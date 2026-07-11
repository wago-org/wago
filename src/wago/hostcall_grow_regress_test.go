//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import "testing"

// hostCallThenGrowWasm: a module with an imported host function and a memory
// that grows. _start calls the import, then does memory.grow(1) and stores the
// result at address 0.
//
//	(import "env" "f" (func $f (param i32)))
//	(memory (export "memory") 18)
//	(func (export "_start")
//	  (call $f (i32.const 123))
//	  i32.const 0 i32.const 1 memory.grow i32.store)
var hostCallThenGrowWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x60,
	0x01, 0x7f, 0x00, 0x60, 0x00, 0x00, 0x02, 0x09, 0x01, 0x03, 0x65, 0x6e,
	0x76, 0x01, 0x66, 0x00, 0x00, 0x03, 0x02, 0x01, 0x01, 0x05, 0x03, 0x01,
	0x00, 0x12, 0x07, 0x13, 0x02, 0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79,
	0x02, 0x00, 0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00, 0x01, 0x0a,
	0x12, 0x01, 0x10, 0x00, 0x41, 0xfb, 0x00, 0x10, 0x00, 0x41, 0x00, 0x41,
	0x01, 0x40, 0x00, 0x36, 0x02, 0x00, 0x0b,
}

// TestHostCallThenGrow is a regression test for an arm64 resumeNative bug: it
// stored the host-call continuation PC at basedata offset -16, whose upper half
// overlaps the memory.grow max-pages cache at -12, so the PC's high word
// clobbered the grow ceiling. Every host call then broke the next memory.grow —
// real Rust/WASI programs, which allocate after their first WASI call, aborted
// with "memory allocation failed". memory.grow(1) from 18 pages must return the
// old size (18), not -1.
func TestHostCallThenGrow(t *testing.T) {
	c, err := Compile(nil, hostCallThenGrowWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, _, _ []uint64) {}),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("_start"); err != nil {
		t.Fatalf("_start: %v", err)
	}
	got, ok := in.ReadUint32Le(0)
	if !ok {
		t.Fatal("read mem[0] failed")
	}
	if got != 18 {
		t.Fatalf("memory.grow(1) after a host call returned %d, want 18 (old page count); "+
			"0xffffffff means the host-call continuation-PC store corrupted the max-pages cache", int32(got))
	}
}
