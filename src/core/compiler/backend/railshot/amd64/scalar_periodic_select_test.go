//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestScalarPeriodicSelectProof(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, kind := range []string{"add", "sub", "mask2", "mask3", "mask4", "mask128", "sign", "variable", "lea", "clobber", "changed-flags", "no-update", "select-writes-counter"} {
			var a x64.Asm
			mask := byte(1)
			if kind == "mask2" {
				mask = 2
			}
			if kind == "mask3" {
				mask = 3
			}
			if kind == "mask4" {
				mask = 4
			}
			if kind == "mask128" {
				mask = 128
			}
			if kind == "sign" {
				a.TestSelf(R9, wide)
			} else if wide {
				a.EmitBytes([]byte{0x49, 0xf7, 0xc1, mask, 0, 0, 0})
			} else {
				a.EmitBytes([]byte{0x41, 0xf6, 0xc1, mask})
			}
			if kind == "changed-flags" {
				a.TestSelf(RDX, wide)
			}
			dst := RAX
			if kind == "select-writes-counter" {
				dst = R9
			}
			a.Cmovcc(condNE, dst, RSI, wide)
			switch kind {
			case "variable":
				a.AluRR(0x01, R9, R10, wide)
			case "lea":
				a.LeaScaledW(R9, R9, R9, 2, 1, wide)
			case "no-update":
			default:
				digit := byte(0)
				if kind == "sub" {
					digit = 5
				}
				a.AluRI(digit, R9, 1, wide)
			}
			if kind == "clobber" {
				a.MovReg64(R9, R10)
			}
			var selects []scalarLoopSelect
			if _, _, _, ok := scalarLoopBodyEffectsWithSelects(a.B, 0, &selects); !ok || len(selects) != 1 {
				t.Fatalf("wide=%t kind=%s fixture rejected %x", wide, kind, a.B)
			}
			want := kind == "add" || kind == "sub" || kind == "mask2" || kind == "mask3"
			if selects[0].periodic != want {
				t.Fatalf("wide=%t kind=%s periodic=%t want=%t", wide, kind, selects[0].periodic, want)
			}
		}
	}
}

func TestScalarPeriodicSelectDefaultExecution(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = false
	defer func() { pureSelectEnabled = saved }()
	for _, wide := range []bool{false, true} {
		typ, bt, k, and, eqz, add, sub, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x71), byte(0x45), byte(0x6a), byte(0x6b), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, and, eqz, add, sub, mul = wasm.I64, 0x7e, 0x42, 0x83, 0x50, 0x7c, 0x7d, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		for _, mask := range []uint64{1, 2, 3, 4, 128} {
			for _, subtract := range []bool{false, true} {
				update := add
				if subtract {
					update = sub
				}
				body := []byte{1, 1, bt, 0x03, 0x40, 0x20, 2, 0x20, 0, k}
				body = append(body, wasmtest.SLEB64(int64(mask))...)
				body = append(body, and, eqz, 0x04, bt, 0x20, 0, k, 3, mul, 0x05, 0x20, 0, k, 7, add, 0x0b, add, 0x21, 2,
					0x20, 0, k, 1, update, 0x21, 0, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x41, 0, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}, body})
				for _, enabled := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "canonical-i32": false}}
					c, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					for _, count := range []uint32{1, 2, 3, 4, 5, 7, 8, 9, 17} {
						for _, seed := range []uint64{0, 13, ^uint64(0) - 2} {
							x, sum := fold(seed), uint64(0)
							for i := uint32(0); i < count; i++ {
								v := fold(x + 7)
								if x&mask == 0 {
									v = fold(x * 3)
								}
								sum = fold(sum + v)
								if subtract {
									x = fold(x - 1)
								} else {
									x = fold(x + 1)
								}
							}
							got := runCompiledAmd64u(t, c, seed, uint64(count)|0xdeadbeef00000000)
							if got != sum {
								t.Fatalf("wide=%t mask=%d sub=%t enabled=%t seed=%x count=%d got=%x want=%x", wide, mask, subtract, enabled, seed, count, got, sum)
							}
						}
					}
					if diagnosticsEnabled && !profileEnabled && (nativeCompactionDisabled || !nativeCompactionEnabled) {
						got := stats.Funcs[0].Peephole["scalar-periodic-select-unroll4"] != 0
						if got != (enabled && mask <= 3) {
							t.Fatalf("wide=%t mask=%d sub=%t enabled=%t stats=%v", wide, mask, subtract, enabled, stats.Funcs[0].Peephole)
						}
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
				}
			}
		}
	}
}
