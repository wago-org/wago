//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBorrowedDivRemDeferredOperandsUseEstablishedCover(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, cons, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, cons, add = wasm.I64, 0x42, 0x7c
		}
		for _, op := range divOps(wide) {
			for _, deferRight := range []bool{false, true} {
				body := []byte{0, 0x20, 0}
				if !deferRight {
					body = append(body, cons, 7, add)
				}
				body = append(body, 0x20, 1)
				if deferRight {
					body = append(body, cons, 7, add)
				}
				body = append(body, op.opcode, 0x20, 0, add, 0x20, 1, add, 0x0b)
				m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
				for _, enabled := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						for _, pair := range [][2]uint64{{17, 5}, {1 << 63, 3}, {^uint64(0), 2}, {17, ^uint64(0)}} {
							n, d := pair[0], pair[1]
							numerator, denominator := n, d
							if deferRight {
								denominator += 7
							} else {
								numerator += 7
							}
							if !wide {
								numerator, denominator = uint64(uint32(numerator)), uint64(uint32(denominator))
							}
							want := refDivRem(op, wide, numerator, denominator) + n + d
							var stats ModuleStats
							opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"borrowed-div-rem": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, n, d)
							if !wide {
								want, got = uint64(uint32(want)), uint64(uint32(got))
							}
							if err != nil || got != want {
								t.Fatalf("%s deferRight=%v enabled=%v guard=%v n=%x d=%x got=%x want=%x err=%v", op.name, deferRight, enabled, guard, n, d, got, want, err)
							}
							if diagnosticsEnabled && stats.Funcs[0].Peephole["borrowed-div-rem"] != 0 {
								t.Fatal("deferred operand entered concrete borrow cover")
							}
						}
					}
				}
			}
		}
	}
}
