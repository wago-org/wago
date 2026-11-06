//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestUnsignedRemainderRuntimeUnitDivisor(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { sharedScalarEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, op := wasm.I32, byte(0x70)
		if wide {
			typ, op = wasm.I64, 0x82
		}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x0b}})
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"branch-fold": enabled, "canonical-i32": false}})
			if err != nil {
				t.Fatal(err)
			}
			values := []uint64{0, 1, 2, 3, 17, 0x7fffffff, 0x80000000, 0xffffffff, 0x100000000, 0x8000000000000000, ^uint64(0)}
			n := uint64(0x123456789abcdef)
			for range 128 {
				n ^= n << 13
				n ^= n >> 7
				n ^= n << 17
				values = append(values, n)
			}
			for _, a := range values {
				for _, b := range []uint64{1, 2, 3, 17, 0x80000000, 0xffffffff, 0x100000001, ^uint64(0)} {
					aa, bb := a, b
					if !wide {
						aa, bb = uint64(uint32(a)), uint64(uint32(b))
					}
					want := aa % bb
					if !wide {
						aa |= 0xdeadbeef00000000
						bb |= 0xcafebabe00000000
					}
					if got := runCompiledAmd64u(t, c, aa, bb); got != want {
						t.Fatalf("wide=%t enabled=%t a=%x b=%x got=%x want=%x", wide, enabled, a, b, got, want)
					}
				}
			}
			c.CodeImage.Close()
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["rem-unit-divisor"] != 0) != enabled {
				t.Fatal("runtime unit divisor path selection mismatch")
			}
			_, _, err = runMemAmd64WithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"branch-fold": enabled}}, nil, 17, 0)
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
				t.Fatalf("wide=%t enabled=%t got %v want divide by zero", wide, enabled, err)
			}
		}
	}
}
