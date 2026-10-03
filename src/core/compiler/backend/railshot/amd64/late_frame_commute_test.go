//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLateFrameCommute(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := lateFrameCommuteEnabled
	defer func() { lateFrameCommuteEnabled = saved }()
	for _, wide := range []bool{false, true} {
		localType := byte(0x7f)
		typ, add, xor, mul, and, or := wasm.I32, byte(0x6a), byte(0x73), byte(0x6c), byte(0x71), byte(0x72)
		if wide {
			localType = 0x7e
			typ, add, xor, mul, and, or = wasm.I64, 0x7c, 0x85, 0x7e, 0x83, 0x84
		}
		for _, op := range []byte{add, xor, mul, and, or} {
			inner := xor
			if op == xor {
				inner = add
			}
			// More than 64 locals, no loop: all locals have frame homes. The
			// local.set consumer requests no fixed destination. The inner tree
			// differs from the outer operation to avoid associative covering.
			body := []byte{1, 65, localType, 0x20, 0, 0x20, 1, 0x20, 2, inner, op, 0x21, 3, 0x20, 3, 0x20, 0, xor, 0x20, 1, xor, 0x20, 2, xor, 0x0b}
			m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body)
			lateFrameCommuteEnabled = false
			off := compileWithStats(t, m, false).Funcs[0]
			lateFrameCommuteEnabled = true
			on := compileWithStats(t, m, false).Funcs[0]
			if on.Peephole["late-frame-commute"] == 0 || off.Peephole["late-frame-commute"] != 0 {
				t.Fatalf("wide=%v op=%x: admission off=%v on=%v", wide, op, off.Peephole, on.Peephole)
			}
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("wide=%v op=%x: bytes on=%d off=%d", wide, op, on.CodeBytes, off.CodeBytes)
			}
			for _, args := range [][3]uint64{{0, 0, 0}, {1, 2, 3}, {^uint64(0), 1, 2}, {0xdeadbeef80000000, 0x1234567887654321, 0xfedcba9876543210}} {
				rhs := args[1] ^ args[2]
				if inner == add {
					rhs = args[1] + args[2]
				}
				var want uint64
				switch op {
				case add:
					want = args[0] + rhs
				case xor:
					want = args[0] ^ rhs
				case mul:
					want = args[0] * rhs
				case and:
					want = args[0] & rhs
				case or:
					want = args[0] | rhs
				}
				want ^= args[0] ^ args[1] ^ args[2]
				if !wide {
					want = uint64(uint32(want))
				}
				for _, enabled := range []bool{false, true} {
					lateFrameCommuteEnabled = enabled
					got := runAmd64u(t, m, args[:]...)
					if !wide {
						got = uint64(uint32(got))
					}
					if got != want {
						t.Fatalf("wide=%v op=%x enabled=%v args=%x: got=%x want=%x", wide, op, enabled, args, got, want)
					}
				}
			}
		}
	}
}
