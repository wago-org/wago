//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestMemRefSpillKeepsLoad is a regression test for a deferred-load (stMemRef)
// eviction bug in spill(). A deferred integer load holds its effective ADDRESS in
// a register with the actual mov not yet emitted. When that owned address register
// was reclaimed by spillIfUsed for a fixed role (RAX/RDX for a div, RCX for a
// shift), spill() stored the address register and marked the value as a plain
// slot — silently dropping the load, so a later reload used the address as if it
// were the loaded value.
//
// This miscompiled AssemblyScript's Unicode casemap() (~lib/util/casemap, used by
// String#toLowerCase): the case-table lookup multiplied a table ADDRESS instead of
// the loaded byte, producing wrong case mappings and, on some inputs, an infinite
// loop in the AS module-init path (every AS module that lowercases a string during
// global init hung on load).
//
// The function below is the minimal shape: the load's address (96 / x) is produced
// by a div so it lands in RAX; the second div (x / 3) reclaims RAX, spilling the
// still-pending load. For x = 12: mem[8] = 200, addr = 96/12 = 8, load8(8) = 200,
// x/3 = 4, result = 200 * 4 = 800. The bug multiplied the address 8 instead: 32.
func TestMemRefSpillKeepsLoad(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
		0x00,       // 0 local decls
		0x41, 0x08, // i32.const 8
		0x41, 0xc8, 0x01, // i32.const 200
		0x3a, 0x00, 0x00, // i32.store8 (mem[8] = 200)
		0x41, 0xe0, 0x00, // i32.const 96
		0x20, 0x00, // local.get 0 (x)
		0x6e,             // i32.div_u  -> 96/x, result in RAX
		0x2d, 0x00, 0x00, // i32.load8_u [96/x]  (deferred; address owns RAX)
		0x20, 0x00, // local.get 0 (x)
		0x41, 0x03, // i32.const 3
		0x6e, // i32.div_u  -> x/3 (reclaims RAX, spilling the pending load)
		0x6c, // i32.mul   (loaded byte * (x/3))
		0x0b, // end
	})
	if got := runAmd64(t, m, 12); got != 800 {
		t.Fatalf("deferred load spilled as its address instead of its value: got %d, want 800", got)
	}
}
