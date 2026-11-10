//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarPosttestLoopEveryExit(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, signed := range []bool{false, true} {
			typ, bt, add, lt := wasm.I32, byte(0x7f), byte(0x6a), byte(0x49)
			if wide {
				typ, bt, add, lt = wasm.I64, 0x7e, 0x7c, 0x54
			}
			if signed {
				if wide {
					lt = 0x53
				} else {
					lt = 0x48
				}
			}
			k := byte(0x41)
			if wide {
				k = 0x42
			}
			// do { sum += i; ++i } while (i < bound); both values remain live.
			m := modFuncs(t, funcDef{params: []wasm.ValType{typ, typ}, results: []wasm.ValType{typ}, body: []byte{
				1, 1, bt, 0x03, 0x40,
				0x20, 2, 0x20, 0, add, 0x21, 2,
				0x20, 0, k, 1, add, 0x22, 0, 0x20, 1, lt, 0x0d, 0,
				0x0b, 0x20, 2, 0x20, 0, add, 0x0b,
			}})
			if diagnosticsEnabled {
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats, Interruptible: true})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
				if stats.Funcs[0].Peephole["scalar-posttest-unroll4"] != 0 {
					t.Fatal("unrolled interruptible loop")
				}
			}

			for _, enabled := range []bool{false, true} {
				for _, input := range [][2]uint64{{0, 0}, {7, 3}, {0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}, {0, 6}, {0, 7}, {0, 8}, {0, 9}, {0xffffffff, 1}, {^uint64(0), 1}, {0xfffffffe, 1}, {^uint64(0) - 1, 1}} {
					fold := func(v uint64) uint64 {
						if !wide {
							return uint64(uint32(v))
						}
						return v
					}
					i, b, sum := fold(input[0]), fold(input[1]), uint64(0)
					for {
						sum = fold(sum + i)
						i = fold(i + 1)
						more := i < b
						if signed {
							if wide {
								more = int64(i) < int64(b)
							} else {
								more = int32(i) < int32(b)
							}
						}
						if !more {
							break
						}
					}
					want := fold(sum + i)
					a, barg := input[0], input[1]
					if !wide {
						a |= 0xdeadbeef00000000
						barg |= 0xcafebabe00000000
					}
					var stats ModuleStats
					opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, a, barg)
					if err != nil || got != want {
						t.Fatalf("wide=%v on=%v input=%x got=%x want=%x err=%v", wide, enabled, input, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-posttest-unroll4"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("wide=%v on=%v unroll=%d", wide, enabled, stats.Funcs[0].Peephole["scalar-posttest-unroll4"])
					}
				}
			}
		}
	}
}

func TestScalarLoopBodyAdmission(t *testing.T) {
	for _, b := range [][]byte{
		{0x43, 0x8d, 0x3c, 0x49, 0x41, 0x01, 0xfa, 0x41, 0x83, 0xc1, 1, 0x45, 0x39, 0xd9},
		{0x4d, 0x01, 0xc1, 0x4d, 0x39, 0xd1},
		{0x81, 0xc0, 0xe8, 0x0f, 0x85, 0xff}, // branch-like immediate bytes
		{0x48, 0x8d, 0x44, 0x8d, 0xff},
		{0x48, 0x8d, 0x85, 0, 0, 0, 0},
	} {
		if !scalarLoopBody(b) {
			t.Fatalf("rejected safe body %x", b)
		}
	}
	for _, b := range [][]byte{
		nil, {0x48}, {0x48, 0x89}, {0x48, 0x83, 0xc0}, {0x48, 0x81, 0xc0, 1},
		{0x48, 0x8b, 0x00}, {0x48, 0x89, 0x00}, {0x48, 0x8d, 0x05, 0, 0, 0, 0},
		{0x48, 0x8d, 0x04}, {0x48, 0x8d, 0x04, 0x25, 0, 0, 0, 0},
		{0xe8, 0, 0, 0, 0}, {0xeb, 0}, {0x0f, 0x85, 0, 0, 0, 0}, {0xc3}, {0x90},
		{0x48, 0x83, 0xc4, 8}, {0x48, 0x89, 0xc5}, {0x48, 0x83, 0xd0, 1},
		{0x48, 0x83, 0xd8, 1}, {0x66, 0x01, 0xc0}, {0x67, 0x8d, 0x00},
		{0x48, 0xf7, 0xf1}, {0x48, 0x8d, 0x80, 0, 0, 0},
	} {
		if scalarLoopBody(b) {
			t.Fatalf("accepted unsafe/truncated body %x", b)
		}
	}
}
