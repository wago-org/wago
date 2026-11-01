//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestLocalIntegerTailTransfersArePhysicalLeaves(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, block, eqz, sub := wasm.I32, byte(0x41), byte(0x7f), byte(0x45), byte(0x6b)
		if wide {
			typ, constant, block, eqz, sub = wasm.I64, 0x42, 0x7e, 0x50, 0x7d
		}
		body := func(value, target byte) []byte {
			return []byte{0, 0x20, 0, eqz, 0x04, block, constant, value, 0x05, 0x20, 0, constant, 1, sub, 0x12, target, 0x0b, 0x0b}
		}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ}, body(5, 1)}, funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ}, body(9, 0)})
		for _, workers := range []int{1, 4} {
			for _, enabled := range []bool{false, true} {
				var stats ModuleStats
				c, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-self-loop": enabled}})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range []uint64{0, 1, 2, 31, 100000} {
					input := n
					if !wide {
						input |= 0xdeadbeef00000000
					}
					want := uint64(5)
					if n&1 != 0 {
						want = 9
					}
					if got := runCompiledAmd64u(t, c, input); got != want {
						t.Fatalf("wide=%t workers=%d enabled=%t n=%d got=%d want=%d", wide, workers, enabled, n, got, want)
					}
				}
				if c.CodeImage != nil {
					c.CodeImage.Close()
				}
				if diagnosticsEnabled {
					for _, st := range stats.Funcs {
						if (st.Peephole["tail-local-leaf"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("wide=%t workers=%d enabled=%t local tail transfer still has native call planning", wide, workers, enabled)
						}
						if (st.Peephole["tail-local-entry-thread"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("wide=%t workers=%d enabled=%t local tail entry still repeats its callee header", wide, workers, enabled)
						}
						if (st.Peephole["tail-local-zero-frame-flags"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("wide=%t workers=%d enabled=%t empty tail frame still destroys argument flags", wide, workers, enabled)
						}

					}
				}
			}
		}
	}
}

func TestLocalIntegerTailTransfersPreserveSpilledArguments(t *testing.T) {
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I64
	}
	params[0] = wasm.I32
	makeBody := func(target byte) []byte {
		body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7e, 0x20, 1, 0x42, 2, 0x7e}
		for i := byte(2); i < 8; i++ {
			body = append(body, 0x20, i, 0x42, i+1, 0x7e, 0x7c)
		}
		body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
		for i := byte(2); i < 8; i++ {
			body = append(body, 0x20, i)
		}
		return append(body, 0x20, 1, 0x42, 7, 0x7c, 0x12, target, 0x0b, 0x0b)
	}
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, makeBody(1)}, funcDef{params, []wasm.ValType{wasm.I64}, makeBody(0)})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-self-loop": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 100000} {
			args := []uint64{n | 0xdeadbeef00000000, ^uint64(0), 17, 1 << 63, 31, 43, 0xcafebabef00d1234, 71}
			var values [7]uint64
			copy(values[:], args[1:])
			for range n {
				first := values[0]
				copy(values[:], values[1:])
				values[6] = first + 7
			}
			want := uint64(0)
			for i, v := range values {
				want += uint64(i+2) * v
			}
			if got := runCompiledAmd64u(t, c, args...); got != want {
				t.Fatalf("enabled=%t n=%d got=%x want=%x", enabled, n, got, want)
			}
		}
		if c.CodeImage != nil {
			c.CodeImage.Close()
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["tail-local-leaf"] != 0) != (enabled && !profileEnabled) {
			t.Fatalf("enabled=%t spilled tail leaf selection mismatch", enabled)
		}
	}
}
