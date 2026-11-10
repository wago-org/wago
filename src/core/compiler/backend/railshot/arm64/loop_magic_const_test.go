//go:build (linux || darwin) && arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestLoopReciprocalLeaseDecoding(t *testing.T) {
	for _, divisor := range []uint32{3, 7, 17, 37, 0x80000001, 0xffffffff} {
		for _, op := range []byte{0x6e, 0x70} {
			magic, _, add := railshot.MagicU(uint64(divisor), 32)
			if add {
				magic = wideReciprocalU32(uint64(divisor))
			}
			value, typ, needed := loopIntConstRegisterValue(op, int64(int32(divisor)), 1)
			if !needed || value != int64(magic) || typ != 2 {
				t.Fatalf("d=%x op=%x value=%x type=%d needed=%v", divisor, op, value, typ, needed)
			}
			h := funcHintView{loopIntConstCount: 1, loopIntConstTypes: 2, loopIntConst: [4]int64{int64(magic)}}
			selected := scopedConstHintsFrom(&h)
			uses := scopedLoopConstUses{hints: &selected}
			reader := wasm.ReaderFrom(append(wasmtest.SLEB32(int32(divisor)), op))
			uses.note(0x41, &reader)
			if uses.mask != 1 || reader.Offset() != 0 {
				t.Fatal("reciprocal lease missing or source cursor advanced")
			}
		}
	}
	for _, divisor := range []int64{0, 1, 2, 16, -2147483648} {
		if _, _, needed := loopIntConstRegisterValue(0x70, divisor, 1); needed {
			t.Fatalf("non-magic divisor %x leased a reciprocal", divisor)
		}
	}
}

func TestLoopReciprocalLeaseExecution(t *testing.T) {
	for _, divisor := range []uint32{7, 17, 0x80000001, 0xffffffff} {
		for _, op := range []byte{0x6e, 0x70} {
			for _, calls := range []bool{false, true} {
				t.Run(fmt.Sprintf("%x/%x/calls%v", divisor, op, calls), func(t *testing.T) {
					body := []byte{1, 2, 0x7f}
					if calls {
						body = append(body, 0x20, 0, 0x10, 1, 0x1a)
					}
					body = append(body, 0x41, 17, 0x21, 2, 0x03, 0x40, 0x20, 0, 0x41)
					body = append(body, wasmtest.SLEB32(1664525)...)
					body = append(body, 0x6c, 0x41)
					body = append(body, wasmtest.SLEB32(1013904223)...)
					body = append(body, 0x6a, 0x21, 0, 0x20, 1, 0x20, 0, 0x41)
					body = append(body, wasmtest.SLEB32(int32(divisor))...)
					body = append(body, op, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
					m := modFuncs(t,
						funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
						funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x41, 17, 0x6a, 0x0b}})
					for _, cache := range []bool{false, true} {
						for _, magic := range []bool{false, true} {
							for _, input := range []uint64{0, 1, 0x7fffffff, 0x80000000, ^uint64(0)} {
								n, want := uint32(input), uint32(0)
								for range 17 {
									n = n*1664525 + 1013904223
									if op == 0x6e {
										want += n / divisor
									} else {
										want += n % divisor
									}
								}
								var stats ModuleStats
								opts := CompileOptions{Optimizations: map[string]bool{"inline": false, "loop-int-const": cache, "scoped-loop-int-const": cache, "magic-div": magic}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, input|0xa5a5a5a500000000)
								if err != nil || uint32(got) != want {
									t.Fatalf("cache=%v magic=%v n=%x got=%x want=%x err=%v", cache, magic, input, got, want, err)
								}
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-loop-int-const"] > 0) != (cache && magic) {
									t.Fatalf("cache=%v magic=%v stats=%v", cache, magic, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				})
			}
		}
	}
}
