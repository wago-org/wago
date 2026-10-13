//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func TestFloatSelfComparePressureAndBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, compare, reinterpret := wasm.F32, byte(0x5b), byte(0xbc)
		vals := []uint64{0, 0x80000000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345, 0xffc54321, 0x3f800000}
		if wide {
			ft, compare, reinterpret = wasm.F64, 0x61, 0xbd
			vals = []uint64{0, 0x8000000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0xfff8000000004321, 0x3ff0000000000000}
		}
		for k := byte(0); k < 6; k++ {
			op := compare + k
			body := []byte{1, 24, 0x7f, 0x3f, 0, 0x1a}
			for i := 0; i < 24; i++ {
				body = append(body, 0x20, 1, 0x41, byte(i+1), 0x6a, 0x22, byte(i+2))
			}
			body = append(body, 0x20, 0, 0x20, 0, op)
			for i := 0; i < 24; i++ {
				body = append(body, 0x73)
			}
			body = append(body, 0xad, 0x20, 0, reinterpret)
			if !wide {
				body = append(body, 0xad)
			}
			body = append(body, 0x85, 0x0b)
			m := mod1(t, []wasm.ValType{ft, wasm.I32}, []wasm.ValType{wasm.I64}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			var stats ModuleStats
			opts := CompileOptions{AMD64FeaturesSet: true}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["float-self-compare-full-width"] != 1 {
				t.Fatalf("self-compare not selected: %v", stats.Funcs[0].Peephole)
			}
			branch := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 0, op, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}
			bm := mod1(t, []wasm.ValType{ft}, []wasm.ValType{wasm.I32}, branch)
			bm.Memories = m.Memories
			bc, e := CompileModuleWith(bm, CompileOptions{AMD64FeaturesSet: true})
			if e != nil {
				t.Fatal(e)
			}
			if bc.CodeImage != nil {
				defer bc.CodeImage.Close()
			}
			for _, bits := range vals {
				x := math.Float64frombits(bits)
				if !wide {
					x = float64(math.Float32frombits(uint32(bits)))
				}
				truth := !math.IsNaN(x)
				switch k {
				case 1:
					truth = !truth
				case 2, 3:
					truth = false
				}
				boolean := uint64(0)
				branchWant := uint64(2)
				if truth {
					boolean = 1
					branchWant = 1
				}
				want := boolean
				for i := 0; i < 24; i++ {
					want ^= 7 + uint64(i+1)
				}
				want ^= bits
				if got := runCompiledAmd64u(t, cm, bits, 7); got != want {
					t.Fatalf("wide=%v op=%d bits=%x got=%x want=%x", wide, k, bits, got, want)
				}
				if got := runCompiledAmd64u(t, bc, bits); got != branchWant {
					t.Fatalf("wide=%v op=%d bits=%x branch=%x want=%x", wide, k, bits, got, branchWant)
				}
			}
		}
	}
}

// A strict self-relation is constant only after evaluating its operand.
func TestFloatSelfCompareLoadTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, load, align, compare := byte(0x7d), byte(0x2a), byte(2), byte(0x5b)
		if wide {
			ft, load, align, compare = 0x7c, 0x2b, 3, 0x61
		}
		for k := byte(0); k < 6; k++ {
			body := []byte{1, 1, ft, 0x20, 0, load, align, 0, 0x22, 1, 0x20, 1, compare + k, 0x0b}
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			got, _, err := runMemAmd64(t, m, nil, 0)
			want := uint64(1)
			if k == 1 || k == 2 || k == 3 {
				want = 0
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%d got=%d err=%v want=%d", wide, k, got, err, want)
			}
			if _, _, err := runMemAmd64(t, m, nil, 65536); err == nil {
				t.Fatalf("wide=%v op=%d: self-comparison eliminated the load trap", wide, k)
			}
		}
	}
}
