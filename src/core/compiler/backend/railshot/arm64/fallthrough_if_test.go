//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestFallthroughIfPreservesCapturedPrefix(t *testing.T) {
	old := nestedFallthroughIfEnabled
	t.Cleanup(func() { nestedFallthroughIfEnabled = old })
	// The prefix captures x before either branch updates the same local.
	body := []byte{0, 0x20, 0, 0x02, 0x7f, 0x20, 1, 0x04, 0x7f, 0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x05, 0x20, 0, 0x41, 2, 0x6a, 0x22, 0, 0x0b, 0x0b, 0x6a, 0x20, 0, 0x73, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		nestedFallthroughIfEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, pure := range []bool{false, true} {
				for _, x := range []uint64{0, 1, 0x7fffffff, 0x88776655ffffffff} {
					for _, c := range []uint64{0, 1, 0xffffffffffffffff} {
						v := uint32(x) + 1
						if uint32(c) == 0 {
							v = uint32(x) + 2
						}
						want := uint64((uint32(x) + v) ^ v)
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"pure-integer-if-select": pure}}, x, c)
						if err != nil || got != want {
							t.Fatalf("enabled%v compact%v pure%v x%x c%x got%x want%x err%v", enabled, compact, pure, x, c, got, want, err)
						}
					}
				}
			}
		}
	}
}

func TestFallthroughIfDoesNotDelayPrefixTrap(t *testing.T) {
	old := nestedFallthroughIfEnabled
	t.Cleanup(func() { nestedFallthroughIfEnabled = old })
	// Evaluate x / d before entering a wrapper containing a selecting if.
	body := []byte{0, 0x20, 0, 0x20, 1, 0x6e, 0x02, 0x7f, 0x20, 2, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		nestedFallthroughIfEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, c := range []uint64{0, 1} {
				for _, d := range []uint64{0, 1, 3} {
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact}, 42, d, c)
					if d == 0 {
						if err == nil {
							t.Fatal("division by zero did not trap")
						}
						continue
					}
					arm := uint64(7)
					if c == 0 {
						arm = 9
					}
					if err != nil || got != 42/d+arm {
						t.Fatalf("enabled%v compact%v c%d d%d got%d err%v", enabled, compact, c, d, got, err)
					}
				}
			}
		}
	}
}

func TestFallthroughIfLoopTails(t *testing.T) {
	old := nestedFallthroughIfEnabled
	t.Cleanup(func() { nestedFallthroughIfEnabled = old })
	body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 2, 0x02, 0x7f, 0x20, 1, 0x41, 63, 0x71, 0x45, 0x04, 0x7f, 0x41, 19, 0x05, 0x41, 3, 0x0b, 0x0b, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	sizes := []uint64{63, 64, 65, 127, 128, 129, 255, 256, 257, 4095, 4096, 4097}
	for n := uint64(0); n <= 33; n++ {
		sizes = append(sizes, n)
	}
	for _, enabled := range []bool{false, true} {
		nestedFallthroughIfEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, pure := range []bool{false, true} {
				for _, n := range sizes {
					want := 3*n + 16*((n+63)/64)
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"pure-integer-if-select": pure}}, n|0x8877665500000000)
					if err != nil || got != want {
						t.Fatalf("enabled%v compact%v pure%v n%d got%d want%d err%v", enabled, compact, pure, n, got, want, err)
					}
				}
			}
		}
	}
}
