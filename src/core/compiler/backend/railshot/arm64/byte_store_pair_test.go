//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestByteStorePairMemoryAndPartialTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, shift, store := wasm.I32, byte(0x41), byte(0x76), byte(0x3a)
		locals := []byte{2, 1, 0x7f, 2, 0x7f}
		if wide {
			typ, c, shift, store = wasm.I64, 0x42, 0x88, 0x3c
			locals = []byte{2, 1, 0x7f, 2, 0x7e}
		}
		for _, tc := range []struct {
			bias                int32
			gap, shift          int32
			unknown, other, nop bool
		}{
			{8, 1, 8, false, false, false}, {9, 1, 8, false, false, false}, {4095, 1, 8, false, false, false}, {65534, 1, 8, false, false, false}, {65535, 1, 8, false, false, false}, {65536, 1, 8, false, false, false}, {-1, 1, 8, false, false, false},
			{8, 2, 8, false, false, false}, {8, 1, 7, false, false, false}, {8, 1, 8, true, false, false}, {8, 1, 8, false, true, false}, {8, 1, 8, false, false, true},
		} {
			b := append([]byte(nil), locals...)
			if tc.unknown {
				b = append(b, 0x20, 0)
				if wide {
					b = append(b, 0xa7)
				}
				b = append(b, 0x21, 1)
			}
			b = append(b, 0x20, 1, 0x41)
			b = append(b, wasmtest.SLEB32(tc.bias)...)
			b = append(b, 0x6a, 0x20, 0, 0x22, 2, store, 0, 0)
			if tc.nop {
				b = append(b, 0x01)
			}
			b = append(b, 0x20, 1, 0x41)
			b = append(b, wasmtest.SLEB32(tc.bias+tc.gap)...)
			b = append(b, 0x6a, 0x20)
			if tc.other {
				b = append(b, 3)
			} else {
				b = append(b, 2)
			}
			b = append(b, c)
			if wide {
				b = append(b, wasmtest.SLEB64(int64(tc.shift))...)
			} else {
				b = append(b, wasmtest.SLEB32(tc.shift)...)
			}
			b = append(b, shift, store, 0, 0, 0x41, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, b)
			for _, on := range []bool{false, true} {
				for _, arg := range []uint64{0, 1, 0x1122334455667788, 0xffffffffffffffff, 0x8877665500000100} {
					want := make([]byte, 65536)
					base := uint32(0)
					if tc.unknown {
						base = uint32(arg)
					}
					address := base + uint32(tc.bias)
					traps := uint64(address) >= uint64(len(want))
					if !traps {
						want[address] = byte(arg)
						address = base + uint32(tc.bias+tc.gap)
						traps = uint64(address) >= uint64(len(want))
						if !traps {
							v := arg
							if !wide {
								v = uint64(uint32(v))
							}
							if tc.other {
								v = 0
							}
							want[address] = byte(v >> uint32(tc.shift))
						}
					}
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"byte-store-pair": on}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runAlignedStoreMemory(t, m, opts, arg)
					if (err != nil) != traps || !bytes.Equal(got, want) {
						t.Fatalf("wide=%v tc=%+v on=%v arg=%x err=%v wantTrap=%v memoryEqual=%v", wide, tc, on, arg, err, traps, bytes.Equal(got, want))
					}
					if diagnosticsEnabled {
						hit := on && tc.bias >= 0 && tc.bias <= 65534 && tc.gap == 1 && tc.shift == 8 && !tc.unknown && !tc.other && !tc.nop
						if (stats.Funcs[0].Peephole["byte-store-pair"] != 0) != hit {
							t.Fatalf("wide=%v tc=%+v on=%v hits=%v want=%v", wide, tc, on, stats.Funcs[0].Peephole, hit)
						}
					}
				}
			}
		}
	}
}

func TestByteStorePairDefaultSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	b := []byte{1, 1, 0x7f, 0x41, 0, 0x21, 1,
		0x20, 1, 0x41, 8, 0x6a, 0x20, 0, 0x3a, 0, 0,
		0x20, 1, 0x41, 9, 0x6a, 0x20, 0, 0x41, 8, 0x76, 0x3a, 0, 0,
		0x41, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, b)
	for _, shared := range []bool{false, true} {
		m.Memories[0].Shared = shared
		m.Memories[0].Limits.HasMax = true
		m.Memories[0].Limits.Max = 1
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		want := 1
		if shared {
			want = 0
		}
		if got := stats.Funcs[0].Peephole["byte-store-pair"]; got != want {
			t.Fatalf("shared=%v selections=%d want=%d", shared, got, want)
		}
	}
}
