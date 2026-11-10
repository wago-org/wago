//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"
)

func TestUnsignedMagic32FullProductShift(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { sharedScalarEnabled = old }()
	for _, d := range []uint32{3, 5, 10, 17, 100, 400, 1000, 0xffffffff} {
		for _, rem := range []bool{false, true} {
			op := divOp{"i32.div_u", 0x6e, false, false}
			if rem {
				op = divOp{"i32.rem_u", 0x70, false, true}
			}
			m := divConstMod(t, false, op, uint64(d))
			for _, enabled := range []bool{false, true} {
				var stats ModuleStats
				c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"magic-div": enabled, "canonical-i32": false, "value-facts": false}})
				if err != nil {
					t.Fatal(err)
				}
				values := []uint32{0, 1, d - 1, d, d + 1, 0x7fffffff, 0x80000000, 0xffffffff}
				n := uint32(0x12345678)
				for range 64 {
					n ^= n << 13
					n ^= n >> 17
					n ^= n << 5
					values = append(values, n)
				}
				for _, n := range values {
					want := n / d
					if rem {
						want = n % d
					}
					if got := runCompiledAmd64u(t, c, uint64(n)|0xdeadbeef00000000); got != uint64(want) {
						t.Fatalf("d=%d n=%x rem=%t enabled=%t got=%x want=%x", d, n, rem, enabled, got, want)
					}
				}
				if c.CodeImage != nil {
					c.CodeImage.Close()
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-product-shift"] != 0) != enabled {
					t.Fatalf("d=%d rem=%t enabled=%t full-product shift selection mismatch", d, rem, enabled)
				}
			}
		}
	}
}
