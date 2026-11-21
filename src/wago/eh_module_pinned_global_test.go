//go:build (((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage) || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage)

package wago

import "testing"

// ARM64 keeps hot globals in module-wide registers whose memory cells are not
// updated while wasm runs. A throw published only dirty function-local pins
// before jumping to its handler, and the handler reloaded module pins from the
// stale cells, so every caught exception reset $g to its last published value.
// The dead loop after return only raises $g's read score so it becomes
// module-pinned.
func TestEHHandlerKeepsModulePinnedGlobal(t *testing.T) {
	if got := invokeI32(t, admissionModule(t, ehModulePinnedGlobalWasm), "f"); got != 95 {
		t.Fatalf("f = %d, want 95", got)
	}
}

// ehModulePinnedGlobalWasm encodes:
//
//	(module
//	  (tag $t)
//	  (global $g (mut i32) (i32.const 100))
//	  (func $f
//	    (block (try_table (catch_all 0) (throw $t))))
//	  (func (export "f") (result i32)
//	    (local $i i32)
//	    (loop
//	      (global.set $g (i32.sub (global.get $g) (i32.const 1)))
//	      (call $f)
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if 0 (i32.lt_u (local.get $i) (i32.const 5)))
//	      (return (global.get $g))
//	      (drop (loop (result i32) (global.get $g) (loop (if (global.get $g) (then))))))
//	    unreachable))
const ehModulePinnedGlobalWasm = "0061736d010000000108026000006000017f03030200010d030100000607017f0141e4000b070501016600010a40020d0002401f4001020008000b0b0b3001017f0340230041016b24001000200041016a210020004105490d0023000f037f23000340230004400b0b0b1a0b000b001f046e616d6501040100016602060101010001690704010001670b0401000174"
