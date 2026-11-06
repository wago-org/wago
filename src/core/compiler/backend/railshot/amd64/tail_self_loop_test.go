//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSelfTailLoopLocalResetAndPermutations(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, block, add, mul := wasm.I32, byte(0x41), byte(0x7f), byte(0x6a), byte(0x6c)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, constant, block, add, mul = wasm.I64, 0x42, 0x7e, 0x7c, 0x7e
			fold = func(v uint64) uint64 { return v }
		}
		body := []byte{1, 1, block,
			0x20, 0, 0x45, 0x04, block,
			0x20, 1, constant, 3, mul, 0x20, 2, add, 0x20, 3, add,
			0x05, constant, 13, 0x21, 3,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x20, 1, constant, 7, add, 0x12, 0, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}, body})
		for _, n := range []uint64{0, 1, 2, 31, 100000} {
			a, b := fold(0xfffffffffffffff0), fold(0x80000000cafebabe)
			aa, bb := a, b
			if !wide {
				aa |= 0x1234567800000000
				bb |= 0xdeadbeef00000000
			}
			for range n {
				a, b = b, fold(a+7)
			}
			want := fold(3*a + b)
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			got := runCompiledAmd64u(t, compiled, n|0xdeadbeef00000000, aa, bb)
			if compiled.CodeImage != nil {
				compiled.CodeImage.Close()
			}
			if got != want {
				t.Fatalf("wide=%t n=%d got=%x want %x", wide, n, got, want)
			}
			if diagnosticsEnabled && !profileEnabled && stats.Funcs[0].Peephole["tail-self-loop"] == 0 {
				t.Fatal("self tail still re-enters its native frame")
			}
		}
	}
}

func TestSelfTailLoopSpilledParametersAndOption(t *testing.T) {
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I64
	}
	params[0] = wasm.I32
	body := []byte{0, 0x20, 0, 0x45, 0x04, 0x7e, 0x20, 1, 0x42, 2, 0x7e}
	for i := byte(2); i < 8; i++ {
		body = append(body, 0x20, i, 0x42, i+1, 0x7e, 0x7c)
	}
	body = append(body, 0x05, 0x20, 0, 0x41, 1, 0x6b)
	for i := byte(2); i < 8; i++ {
		body = append(body, 0x20, i)
	}
	body = append(body, 0x20, 1, 0x42, 7, 0x7c, 0x12, 0, 0x0b, 0x0b)
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, body})
	for _, enabled := range []bool{false, true} {
		for _, n := range []uint64{0, 1, 2, 31, 100000} {
			args := []uint64{n | 0xdeadbeef00000000, ^uint64(0), 17, 0x8000000000000000, 31, 43, 0xcafebabef00d1234, 71}
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
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"tail-self-loop": enabled}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			got := runCompiledAmd64u(t, compiled, args...)
			if compiled.CodeImage != nil {
				compiled.CodeImage.Close()
			}
			if got != want {
				t.Fatalf("enabled=%t n=%d got=%x want %x", enabled, n, got, want)
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["tail-self-loop"] != 0) != (enabled && !profileEnabled) {
				t.Fatal("self-tail option did not select its implementation")
			}
		}
	}
}

func TestSelfTailLoopEntryConditionAndFlags(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, nonzero := range []bool{false, true} {
			typ, constant, block, eqz, add, sub := wasm.I32, byte(0x41), byte(0x7f), byte(0x45), byte(0x6a), byte(0x6b)
			fold := func(v uint64) uint64 { return uint64(uint32(v)) }
			if wide {
				typ, constant, block, eqz, add, sub = wasm.I64, 0x42, 0x7e, 0x50, 0x7c, 0x7d
				fold = func(v uint64) uint64 { return v }
			}
			body := []byte{0, 0x20, 0, eqz}
			if nonzero {
				body = append(body, 0x45)
			}
			body = append(body, 0x04, block, 0x20, 1, 0x05, 0x20, 0, constant, 1)
			if nonzero {
				body = append(body, add, 0x20, 1, constant, 7, add)
			} else {
				body = append(body, sub, 0x20, 1, 0x20, 0, constant, 1, sub, add)
			}
			body = append(body, 0x12, 0, 0x0b, 0x0b)
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body})
			for _, n := range []uint64{0, 1, 2, 31, 100000} {
				a := fold(0xfffffffffffffff0)
				want := a
				if nonzero {
					if n == 0 {
						want = fold(a + 7)
					}
				} else {
					want = fold(a + n*(n-1)/2)
				}
				nn, aa := n, a
				if !wide {
					nn |= 0xdeadbeef00000000
					aa |= 0x1234567800000000
				}
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				compiled, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				got := runCompiledAmd64u(t, compiled, nn, aa)
				if compiled.CodeImage != nil {
					compiled.CodeImage.Close()
				}
				if got != want {
					t.Fatalf("wide=%t nonzero=%t n=%d got=%x want %x", wide, nonzero, n, got, want)
				}
				if diagnosticsEnabled && !profileEnabled {
					if stats.Funcs[0].Peephole["tail-self-entry-test"] == 0 {
						t.Fatal("self tail still branches through the entry test")
					}
					if !nonzero && stats.Funcs[0].Peephole["tail-self-test-flags"] == 0 {
						t.Fatal("self tail did not retain counter flags")
					}
				}
			}
		}
	}
}
