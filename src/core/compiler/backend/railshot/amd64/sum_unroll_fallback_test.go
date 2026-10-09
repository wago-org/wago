//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestSumUnrollBoundedEmission(t *testing.T) {
	for _, c := range [][2]int{{2, 2}, {8, 4}, {8, 8}, {16, 4}, {16, 8}} {
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			f := &fn{a: &x86.Asm{}, m: &wasm.Module{Memories: []wasm.MemType{{}}},
				locals:          []localDef{{reg: R12}, {reg: R13}, {reg: R14}},
				pinnedLocalMask: regMask(0).add(R12).add(R13).add(R14), linearSumLoop: 1 | 3<<16}
			pins := f.pinned
			if !f.tryExperimentalLinearSumLatch(nil, 1, c[0], c[1], 512) {
				t.Fatal("candidate rejected")
			}
			if f.a.Len() > 256+8*c[0]+16*c[1] {
				t.Fatalf("encoding exceeded bound: %d", f.a.Len())
			}
			if f.pinned != pins {
				t.Fatal("transient pins leaked")
			}
			for _, r := range gpAlloc {
				if f.regUser[r] != nil {
					t.Fatal("register owner leaked")
				}
			}
		})
	}
}

func TestSumUnrollEmissionRejectIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		factor, chains, budget int
		pressure               bool
	}{
		{"budget", 16, 8, 511, false}, {"pressure", 8, 8, 512, true},
		{"factor", 32, 8, 512, false}, {"chains", 8, 9, 512, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{a: &x86.Asm{B: []byte{0x90}}, m: &wasm.Module{Memories: []wasm.MemType{{}}},
				locals:          []localDef{{reg: R12}, {reg: R13}, {reg: R14}},
				pinnedLocalMask: regMask(0).add(R12).add(R13).add(R14), linearSumLoop: 1 | 3<<16}
			if tc.pressure {
				for _, r := range gpAlloc {
					f.reserved = f.reserved.add(r)
				}
			}
			saved := *f
			if f.tryExperimentalLinearSumLatch(nil, 1, tc.factor, tc.chains, tc.budget) {
				t.Fatal("unsafe candidate accepted")
			}
			if !bytes.Equal(f.a.B, []byte{0x90}) || f.pinned != saved.pinned || f.reserved != saved.reserved || f.regUser != saved.regUser {
				t.Fatal("rejection changed compiler state")
			}
		})
	}
}

func TestSumUnrollNativeFallback(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := sumUnrollExperiment
	defer func() { sumUnrollExperiment = saved }()
	for _, tc := range []struct {
		name                             string
		pressure, factor, chains, budget int
	}{
		{"budget", 0, 16, 8, 511}, {"pressure", 12, 8, 8, 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sumUnrollModule(t, tc.pressure)
			original := append([]byte(nil), m.Code[0].BodyBytes...)
			sumUnrollExperiment.factor = tc.factor
			sumUnrollExperiment.chains = tc.chains
			sumUnrollExperiment.budget = tc.budget
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if s := stats.Funcs[0]; s.Peephole["experimental-linear-sum"] != 0 || s.Peephole["linear-sum-unroll4"] != 1 {
				t.Fatalf("fallback not selected: %v", s.Peephole)
			}
			mem, err := rt.NewJobMemory(65536)
			if err != nil {
				t.Fatal(err)
			}
			defer mem.Close()
			for i := range mem.CurrentBytes() {
				mem.CurrentBytes()[i] = byte(i * 17)
			}
			n := sumUnrollNative(t, m, CompileOptions{})
			want, trap := sumOracle(mem.CurrentBytes(), 1, 33, 9, tc.pressure)
			got, err := n.call(mem, 1, 33, 9, tc.pressure)
			if trap || err != nil || got != want {
				t.Fatalf("fallback execution %x want %x: %v", got, want, err)
			}
			if !bytes.Equal(original, m.Code[0].BodyBytes) {
				t.Fatal("compiler changed Wasm instructions")
			}
		})
	}
}
