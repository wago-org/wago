//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"math"
	"os"
	"testing"
)

func experimentF64Controls(mode string, assert bool) func() {
	a, b, c, d, e, z, force := regionLoopEnabled, regionAdjacentEnabled, regionWideIndependentEnabled, regionWideAdjacentEnabled, scalarMemoryRecurrenceEnabled, regionZeroCounterEnabled, regionLoopTestFast
	regionLoopEnabled, regionAdjacentEnabled, regionWideIndependentEnabled, regionWideAdjacentEnabled, scalarMemoryRecurrenceEnabled, regionZeroCounterEnabled, regionLoopTestFast = false, false, false, false, false, false, assert
	switch mode {
	case "pair128":
		regionLoopEnabled = true
	case "wide256":
		regionWideIndependentEnabled = true
		regionAdjacentEnabled = true // Also admits the planner in the driver.
	case "adjacent128":
		regionAdjacentEnabled = true
	case "adjacent256":
		regionAdjacentEnabled = true
		regionWideAdjacentEnabled = true
	}
	return func() {
		regionLoopEnabled, regionAdjacentEnabled, regionWideIndependentEnabled, regionWideAdjacentEnabled, scalarMemoryRecurrenceEnabled, regionZeroCounterEnabled, regionLoopTestFast = a, b, c, d, e, z, force
	}
}
func experimentF64Init(mem []byte) {
	for i := 0; i+8 <= len(mem); i += 8 {
		binary.LittleEndian.PutUint64(mem[i:], math.Float64bits(float64((i/8)%2048)*0.125))
	}
}
func TestExperimentalExistingF64Paths(t *testing.T) {
	for _, mode := range []string{"pair128", "wide256", "adjacent128", "adjacent256"} {
		t.Run(mode, func(t *testing.T) {
			adjacent := mode == "adjacent128" || mode == "adjacent256"
			m := wideIndependentFixture(t, 0xa2, false)
			if adjacent {
				m = adjacentLoopFixture(t, 0xa0)
			}
			opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline}
			restore := experimentF64Controls("scalar", false)
			ref := newLoopExperimentRun(t, m, opts, 65536)
			if diagnosticsEnabled {
				var stats ModuleStats
				opts.Stats = &stats
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if stats.Funcs[0].Peephole["region-loop-fast"] != 0 {
					t.Fatal("scalar reference is optimized", stats.Funcs[0].Peephole)
				}
				t.Logf("scalar module=%d labels=%v", len(cm.Code), stats.Funcs[0].Peephole)
				cm.CodeImage.Close()
				opts.Stats = nil
			}
			restore()
			restore = experimentF64Controls(mode, false)
			got := newLoopExperimentRun(t, m, opts, 65536)
			if diagnosticsEnabled {
				var stats ModuleStats
				opts.Stats = &stats
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				x := stats.Funcs[0]
				t.Logf("mode=%s module=%d function=%d spills=%d reloads=%d frame=%d labels=%v", mode, len(cm.Code), x.CodeBytes, x.Spills, x.Reloads, x.FrameBytes, x.Peephole)
				if x.Peephole["region-loop-fast"] != 1 {
					t.Fatal("path absent")
				}
				cm.CodeImage.Close()
				opts.Stats = nil
			}
			restore()
			restore = experimentF64Controls(mode, true)
			fast := newLoopExperimentRun(t, m, opts, 65536)
			restore()
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 512} {
				experimentF64Init(ref.jm.CurrentBytes())
				copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
				copy(fast.jm.CurrentBytes(), ref.jm.CurrentBytes())
				args := []uint64{128, 32768, 0, n, math.Float64bits(3), math.Float64bits(7)}
				if adjacent {
					args = []uint64{128, 16384, 32768, 0, n}
				}
				a, b := ref.call(args...), got.call(args...)
				// These existing fixtures are bottom-tested. A zero limit wraps
				// through 2^32 iterations and traps; compare the scalar prefix.
				if n == 0 {
					if a == nil || b == nil || !bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes()) {
						t.Fatal("zero-limit trap prefix differs", a, b)
					}
					continue
				}
				if a != nil || b != nil || !bytes.Equal(ref.out, got.out) || !bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes()) {
					t.Fatal(n, a, b)
				}
				if n >= 4 && (mode != "pair128" || n%2 == 0) {
					if err := fast.call(args...); err != nil {
						t.Fatal("emitted path did not execute", n, err)
					}
				}
			}
		})
	}
}
func BenchmarkExperimentalExistingF64(b *testing.B) {
	mode := os.Getenv("WAGO_LOOP_F64_MODE")
	if mode == "" {
		mode = "scalar"
	}
	restore := experimentF64Controls(mode, false)
	defer restore()
	adjacent := mode == "adjacent128" || mode == "adjacent256"
	names := []string{"independent", "adjacent"}
	if mode == "pair128" || mode == "wide256" {
		names = []string{"independent"}
	}
	if adjacent {
		names = []string{"adjacent"}
	}
	for _, name := range names {
		m := wideIndependentFixture(b, 0xa2, false)
		if name == "adjacent" {
			m = adjacentLoopFixture(b, 0xa0)
		}
		opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline}
		b.Run(name+"/compile", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
		for _, n := range []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 512, 8192} {
			b.Run(name+"/execute/"+fmtExperimentCount(n), func(b *testing.B) {
				r := newLoopExperimentRun(b, m, opts, 1<<20)
				experimentF64Init(r.jm.CurrentBytes())
				args := []uint64{128, 1 << 19, 0, n, math.Float64bits(3), math.Float64bits(7)}
				if name == "adjacent" {
					args = []uint64{128, 1 << 18, 1 << 19, 0, n}
				}
				setScalar := experimentF64Controls("scalar", false)
				ref := newLoopExperimentRun(b, m, opts, 1<<20)
				setScalar()
				copy(ref.jm.CurrentBytes(), r.jm.CurrentBytes())
				if err := ref.call(args...); err != nil {
					b.Fatal(err)
				}
				if err := r.call(args...); err != nil {
					b.Fatal(err)
				}
				if !bytes.Equal(ref.out, r.out) || !bytes.Equal(ref.jm.CurrentBytes(), r.jm.CurrentBytes()) {
					b.Fatal("incorrect output")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := r.eng.Call(r.entry, r.args, r.jm.LinearMemory(), r.trap, r.out); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if !bytes.Equal(ref.out, r.out) {
					b.Fatal("final output differs")
				}
			})
		}
	}
}
