//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDivisorBorrowPreservesPinnedLocal(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { sharedScalarEnabled = old }()
	for _, enabled := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			typ, add := wasm.I32, byte(0x6a)
			fold := func(v uint64) uint64 { return uint64(uint32(v)) }
			if wide {
				typ, add = wasm.I64, 0x7c
				fold = func(v uint64) uint64 { return v }
			}
			for _, op := range divOps(wide) {
				t.Run(op.name, func(t *testing.T) {
					// The divisor is used again after DIV/IDIV and may also be the result home.
					for _, alias := range []bool{false, true} {
						body := []byte{0, 0x20, 0, 0x20, 1, op.opcode, 0x20, 1, add}
						if alias {
							body = append(body, 0x21, 1, 0x20, 1)
						}
						body = append(body, 0x0b)
						m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body})
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"borrowed-div-rem": enabled}})
						if err != nil {
							t.Fatal(err)
						}
						for _, n := range []uint64{0, 1, 31, 0x7fffffff, 0x80000000, 0xffffffff, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)} {
							for _, d := range []uint64{1, 2, 7, 0x7fffffff, 0x80000000, 0xffffffff, 0x8000000000000001} {
								nn, dd := fold(n), fold(d)
								if dd == 0 || op.signed && !op.rem && (wide && nn == 1<<63 && dd == ^uint64(0) || !wide && nn == 1<<31 && dd == 0xffffffff) {
									continue
								}
								want := fold(refDivRem(op, wide, nn, dd) + dd)
								if !wide {
									nn |= 0xdeadbeef00000000
									dd |= 0xcafebabe00000000
								}
								if got := runCompiledAmd64u(t, cm, nn, dd); got != want {
									t.Fatalf("alias=%t n=%x d=%x got=%x want=%x", alias, n, d, got, want)
								}
							}
						}
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["divisor-local-borrow"] != 0) != enabled {
							t.Fatalf("enabled=%t unexpected divisor borrowing", enabled)
						}
					}
				})
			}
		}
	}
}
