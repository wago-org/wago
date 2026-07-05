//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// brTableIndexInRAX builds a one-function module whose br_table dispatches on an
// index that x86 forces into RAX: `p0 / p1` (i32.div_u) leaves its quotient in
// RAX. The jump-table lowering (opBrTable) uses RAX as the table base, so an index
// already living in RAX aliases the base — the LeaRip(RAX) that loads the table
// address clobbers the live index before the table read, dispatching through a
// corrupted address (SIGSEGV / wrong target). This is the regex-crate crash's root
// cause, reduced to a self-contained module: no pins or memory needed, just the
// index-in-RAX aliasing. Six nested blocks give the br_table 5 labels + a default
// (≥ brTableJumpMin, so the jump-table form fires); each arm returns 1000+label.
func brTableIndexInRAX(t *testing.T) *wasm.Module {
	t.Helper()
	params := []wasm.ValType{i32, i32}
	body := []byte{0x00} // no locals
	for i := 0; i < 6; i++ {
		body = append(body, 0x02, 0x40) // block (void)
	}
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0x6e) // local.get 0; local.get 1; i32.div_u -> RAX
	// br_table with 5 case labels [0..4] + default 5 (count 5 >= brTableJumpMin).
	body = append(body, 0x0e, 0x05, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05)
	for i := 0; i < 6; i++ {
		body = append(body, 0x0b) // end block i
		body = append(body, 0x41) // i32.const
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f) // return
	}
	body = append(body, 0x0b) // end func

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{i32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestExecBrTableIndexInRAX is the regression for the br_table jump-table
// register-allocation bug that crashed the Rust regex crate: the dispatch index
// must survive the table-base load even when it lives in RAX. Pre-fix, an
// index in RAX was overwritten by the `lea rax,[table]` and the module faulted.
func TestExecBrTableIndexInRAX(t *testing.T) {
	m := brTableIndexInRAX(t)
	// index = a/b, clamped to the default (label 5) when >= 5; arm returns 1000+idx.
	for _, c := range []struct{ a, b uint64 }{
		{0, 1}, {1, 1}, {4, 1}, {5, 1}, {9, 1}, {8, 4}, {100, 1},
	} {
		idx := c.a / c.b
		want := uint64(1000) + idx
		if idx >= 5 {
			want = 1005
		}
		if got := runAmd64u(t, m, c.a, c.b); got != want {
			t.Fatalf("f(%d,%d): idx=%d got=%d want=%d", c.a, c.b, idx, got, want)
		}
	}
}
