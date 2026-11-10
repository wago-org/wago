//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func maskedAddressModule(t testing.TB, mask, shift, offset uint32) *wasm.Module {
	body := []byte{0, 0x20, 0, 0x41}
	body = append(body, wasmtest.SLEB32(int32(mask))...)
	body = append(body, 0x71, 0x41)
	body = append(body, wasmtest.SLEB32(int32(shift))...)
	body = append(body, 0x74, 0x28, 2)
	body = append(body, wasmtest.ULEB(offset)...)
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestStaticMaskedAddressBounds(t *testing.T) {
	for _, tc := range []struct {
		mask, shift, offset uint32
		elide               bool
	}{
		{255, 1, 65000, true}, {255, 2, 64000, true}, {255, 2, 65000, false},
		{255, 32, 65000, true}, {0xffffffff, 1, 0, false}, {0x7fffffff, 1, 0, false},
		{65535, 16, 0, false}, {1, 31, 0, false}, {0, 0, 65532, true}, {0, 0, 65533, false},
	} {
		m := maskedAddressModule(t, tc.mask, tc.shift, tc.offset)
		for _, facts := range []bool{false, true} {
			for _, arg := range []uint64{0, 1, 2, 255, 256, 0x7fffffff, 0x80000000, 0xffffffff, 1<<32 | 3} {
				addr := (uint32(arg) & tc.mask) << (tc.shift & 31)
				trapped := uint64(addr)+uint64(tc.offset)+4 > 65536
				var stats ModuleStats
				opts := CompileOptions{NoBoundsFacts: !facts}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, arg)
				if trapped {
					if err == nil {
						t.Fatalf("%+v arg%x facts%t: expected trap", tc, arg, facts)
					}
				} else if err != nil || got != 0 {
					t.Fatalf("%+v arg%x facts%t: got %x %v", tc, arg, facts, got, err)
				}
				if diagnosticsEnabled {
					n := stats.Funcs[0].Peephole["static-bounds-elide"]
					if (n == 1) != (facts && tc.elide) {
						t.Fatalf("%+v facts%t: elided %d", tc, facts, n)
					}
				}
			}
		}
	}
}

func TestStaticBoundsPreservesNestedLoadTraps(t *testing.T) {
	for _, signed := range []bool{false, true} {
		op := byte(0x2d)
		if signed {
			op = 0x2c
		}
		body := []byte{0, 0x20, 0, op, 0, 0, 0x28, 2, 0, 0x0b}
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, arg := range []uint32{0, 1, 65536} {
			got, err := runArm64WrapperMem(t, m, arg, func(mem []byte) { mem[0] = 255 })
			trapped := arg == 65536 || signed && arg == 0
			if trapped {
				if err == nil {
					t.Fatalf("signed%t arg%d: expected trap", signed, arg)
				}
			} else if err != nil {
				t.Fatalf("signed%t arg%d: %x %v", signed, arg, got, err)
			}
		}
	}
}
