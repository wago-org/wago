//go:build linux && amd64

package amd64

import (
	"bytes"
	"embed"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"math"
	"os"
	"strconv"
	"testing"
)

//go:embed testdata/loop_experiment/*.wasm
var loopExperimentFixtures embed.FS

func loopExperimentModule(t testing.TB, name string) *wasm.Module {
	raw, err := loopExperimentFixtures.ReadFile("testdata/loop_experiment/" + name + ".wasm")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

type loopExperimentRun struct {
	eng                   *coreruntime.Engine
	jm                    *coreruntime.JobMemory
	args, out, trap, code []byte
	entry                 uintptr
}

func newLoopExperimentRun(t testing.TB, m *wasm.Module, opts CompileOptions, size uint64) *loopExperimentRun {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cm.CodeImage.Close() })
	e, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	jm, err := coreruntime.NewJobMemory(int(size))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jm.Close() })
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ar.Close() })
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { coreruntime.Unmap(code) })
	return &loopExperimentRun{eng: e, jm: jm, args: ar.Alloc(256), out: ar.Alloc(256), trap: ar.Alloc(coreruntime.TrapBufferBytes), code: code, entry: entry + uintptr(cm.Entry[0])}
}
func (r *loopExperimentRun) call(args ...uint64) error {
	clear(r.args)
	clear(r.out)
	clear(r.trap)
	for i, a := range args {
		binary.LittleEndian.PutUint64(r.args[i*8:], a)
	}
	return r.eng.Call(r.entry, r.args, r.jm.LinearMemory(), r.trap, r.out)
}
func loopExperimentSetup(mem []byte, name string) {
	for i := range mem {
		mem[i] = byte(i*19 + 1)
	}
	if name == "pointer" {
		for i := 0; i < len(mem)/16; i++ {
			binary.LittleEndian.PutUint32(mem[i*16:], uint32(((i+1)*16)%len(mem)))
		}
	}
	if name == "map-f32" {
		for i := 0; i+4 <= len(mem); i += 4 {
			binary.LittleEndian.PutUint32(mem[i:], math.Float32bits(float32((i/4)%2048)*0.125))
		}
	}
	if name == "dependent-f64" {
		values := []float64{1e16, 1, -1e16, 1, math.Copysign(0, -1), math.SmallestNonzeroFloat64}
		for i := 0; i+8 <= len(mem); i += 8 {
			binary.LittleEndian.PutUint64(mem[i:], math.Float64bits(values[(i/8)%len(values)]))
		}
	}
}
func TestExperimentalReplication(t *testing.T) {
	for _, name := range []string{"map-i32", "dependent-i32", "dependent-f64", "pointer", "simd-i32"} {
		modes := []string{"count2", "count4", "guard2"}
		if name == "simd-i32" {
			modes = []string{"simd2", "simd4"}
		}
		for _, mode := range modes {
			t.Run(name+"/"+mode, func(t *testing.T) {
				m := loopExperimentModule(t, name)
				original := append([]byte(nil), m.Code[0].BodyBytes...)
				changed, reason, err := shared.RewriteReplication(m, mode)
				if err != nil || reason != "accepted" || changed == m {
					t.Fatal(reason, err)
				}
				if !bytes.Equal(m.Code[0].BodyBytes, original) {
					t.Fatal("input changed")
				}
				opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: experimentBenchmarkFeatures()}
				ref := newLoopExperimentRun(t, m, opts, 65536)
				opts.ExperimentalLoopMode = mode
				got := newLoopExperimentRun(t, m, opts, 65536)
				for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 31, 32, 33, 512} {
					for _, pair := range [][2]uint64{{128, 256}, {128, 128}, {132, 128}, {128, 132}, {1, 1025}, {65520, 65504}, {128, 65520}} {
						loopExperimentSetup(ref.jm.CurrentBytes(), name)
						copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
						args := []uint64{pair[0], pair[1], n, 3, 7}
						if name == "pointer" {
							args = []uint64{0, n, 0}
						}
						a, b := ref.call(args...), got.call(args...)
						if (a != nil) != (b != nil) || a == nil && !bytes.Equal(ref.out, got.out) || !bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes()) {
							t.Fatalf("n=%d pair=%v errors=%v/%v outputs=%x/%x", n, pair, a, b, ref.out[:40], got.out[:40])
						}
					}
				}
				if diagnosticsEnabled {
					var s ModuleStats
					opts.Stats = &s
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					x := s.Funcs[0]
					t.Logf("module=%d function=%d frame=%d spills=%d reloads=%d", len(cm.Code), x.CodeBytes, x.FrameBytes, x.Spills, x.Reloads)
					cm.CodeImage.Close()
				}
			})
		}
	}
}

func TestExperimentalReplicationRejects(t *testing.T) {
	m := loopExperimentModule(t, "map-i32")
	for _, x := range []struct {
		name string
		body []byte
	}{
		{"call", []byte{0x10, 0}}, {"nested", []byte{2, 0x40, 0x0b}}, {"extra-exit", []byte{0x0c, 0}}, {"growth", bytes.Repeat([]byte{0x41, 0, 0x21, 5}, 70)}, {"memory-grow", []byte{0x41, 0, 0x40, 0, 0x21, 5}}, {"atomic", []byte{0xfe, 0, 0, 0}}, {"reference", []byte{0xd0, 0x70}},
	} {
		t.Run(x.name, func(t *testing.T) {
			body := append(append([]byte(nil), m.Code[0].BodyBytes[:9]...), x.body...)
			body = append(body, m.Code[0].BodyBytes[9:]...)
			var p shared.ReplicationPlan
			if why := shared.InspectReplication(body, m, 4, false, false, &p); why == "" {
				t.Fatal("admitted unsupported body")
			}
		})
	}
	for _, kind := range []string{"shared", "memory64", "interruptible"} {
		t.Run(kind, func(t *testing.T) {
			copyM := *m
			copyM.Memories = append([]wasm.MemType(nil), m.Memories...)
			opts := CompileOptions{ExperimentalLoopMode: "count4"}
			switch kind {
			case "shared":
				copyM.Memories[0].Shared = true
			case "memory64":
				copyM.Memories[0].Limits.Addr64 = true
			case "interruptible":
				opts.Interruptible = true
			}
			if kind != "interruptible" {
				if out, _, err := shared.RewriteReplication(&copyM, "count4"); err != nil || out != &copyM {
					t.Fatal("memory exclusion", err)
				}
			} else {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
	}
}

func experimentBenchmarkFeatures() shared.AMD64Features {
	if os.Getenv("WAGO_LOOP_FEATURES") == "modern" {
		return shared.AMD64ModernBaseline
	}
	return 0
}

func BenchmarkExperimentalReplication(b *testing.B) {
	mode := os.Getenv("WAGO_LOOP_REPLICATION")
	for _, name := range []string{"map-i32", "dependent-i32", "dependent-f64", "pointer", "simd-i32", "map-f32"} {
		if name == "simd-i32" && mode != "" && mode != "simd2" && mode != "simd4" {
			continue
		}
		if name != "simd-i32" && (mode == "simd2" || mode == "simd4") {
			continue
		}
		if mode == "vector-f32" && name != "map-f32" || mode == "vector-i32" && name != "map-i32" {
			continue
		}
		m := loopExperimentModule(b, name)
		opts := CompileOptions{ExperimentalLoopMode: mode, AMD64FeaturesSet: true, AMD64Features: experimentBenchmarkFeatures()}
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
		streamN := uint64(4194304)
		if name == "dependent-f64" {
			streamN = 2097152
		}
		if name == "simd-i32" {
			streamN = 1048576
		}
		if name == "pointer" {
			streamN = 2097152
		}
		for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513, 8192, 262144, streamN} {
			b.Run(name+"/execute/"+fmtExperimentCount(n), func(b *testing.B) {
				size, destination := uint64(16<<20), uint64(8<<20)
				if n == streamN {
					size, destination = 64<<20, 32<<20
				}
				r := newLoopExperimentRun(b, m, opts, size)
				loopExperimentSetup(r.jm.CurrentBytes(), name)
				args := []uint64{destination, 128, n, 3, 7}
				if name == "map-f32" {
					args[3], args[4] = uint64(math.Float32bits(3)), uint64(math.Float32bits(7))
				}
				if name == "dependent-f64" {
					args[3] = math.Float64bits(3)
				}
				if name == "pointer" {
					args = []uint64{0, n, 0}
				}
				ref := newLoopExperimentRun(b, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: opts.AMD64Features}, size)
				copy(ref.jm.CurrentBytes(), r.jm.CurrentBytes())
				if err := ref.call(args...); err != nil {
					b.Fatal(err)
				}
				if err := r.call(args...); err != nil {
					b.Fatal(err)
				}
				if !bytes.Equal(ref.out, r.out) || !bytes.Equal(ref.jm.CurrentBytes(), r.jm.CurrentBytes()) {
					b.Fatal("incorrect benchmark result")
				}
				clear(r.args)
				for i, a := range args {
					binary.LittleEndian.PutUint64(r.args[i*8:], a)
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
func fmtExperimentCount(n uint64) string { return strconv.FormatUint(n, 10) }

func TestExperimentalWritingMemory32Wrap(t *testing.T) {
	for _, name := range []string{"map-i32", "map-f32", "dependent-f64", "simd-i32"} {
		modes := []string{"count2", "count4", "guard2"}
		if name == "simd-i32" {
			modes = []string{"simd2", "simd4"}
		}
		if name == "map-f32" {
			modes = append(modes, "vector-f32")
		}
		if name == "map-i32" {
			modes = append(modes, "vector-i32")
		}
		for _, mode := range modes {
			t.Run(name+"/"+mode, func(t *testing.T) {
				m := loopExperimentModule(t, name)
				m.Memories[0].Limits.Max = 65536
				ref := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: experimentBenchmarkFeatures()}, 1<<32)
				got := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: experimentBenchmarkFeatures(), ExperimentalLoopMode: mode}, 1<<32)
				for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0x40000000, 0xffffffff} {
					for _, pair := range [][2]uint64{{256, 0xfffffff0}, {0xfffffff0, 256}, {0xfffffff0, 0xfffffff0}, {256, 0xfffffff9}, {0xfffffff9, 256}} {
						// Large counts use a boundary access that traps within two
						// iterations. They check widened range multiplication without
						// executing billions of otherwise valid wrapped accesses.
						if n > 9 && pair[0] != 0xfffffff9 && pair[1] != 0xfffffff9 {
							continue
						}
						for _, r := range []*loopExperimentRun{ref, got} {
							mem := r.jm.CurrentBytes()
							for _, start := range []uint64{0, 256, (1 << 32) - 128} {
								for i := uint64(0); i < 128; i++ {
									mem[start+i] = byte(i*19 + 7)
								}
							}
						}
						args := []uint64{pair[0], pair[1], n, math.Float64bits(1), math.Float64bits(2)}
						if name == "map-f32" {
							args[3], args[4] = uint64(math.Float32bits(1)), uint64(math.Float32bits(2))
						}
						a, b := ref.call(args...), got.call(args...)
						if (a != nil) != (b != nil) || a == nil && !bytes.Equal(ref.out, got.out) {
							t.Fatalf("n=%d pair=%v err=%v/%v outputs=%x/%x", n, pair, a, b, ref.out[:40], got.out[:40])
						}
						for _, start := range []uint64{0, 256, (1 << 32) - 128} {
							if !bytes.Equal(ref.jm.CurrentBytes()[start:start+128], got.jm.CurrentBytes()[start:start+128]) {
								t.Fatal("effects before trap differ", n, pair)
							}
						}
					}
				}
			})
		}
	}
}

func BenchmarkExperimentalRejectedCompile(b *testing.B) {
	mode := os.Getenv("WAGO_LOOP_REPLICATION")
	for _, name := range []string{"small", "large", "many-functions"} {
		var m *wasm.Module
		switch name {
		case "small":
			m = benchSmallScalarModule(b)
		case "large":
			m = benchALUHeavyModule(b)
		case "many-functions":
			base := benchSmallScalarModule(b)
			copyM := *base
			m = &copyM
			m.Code = make([]wasm.Func, 512)
			m.FuncTypes = make([]wasm.TypeIdx, 512)
			for i := range m.Code {
				m.Code[i] = base.Code[0]
				m.FuncTypes[i] = base.FuncTypes[0]
			}
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{ExperimentalLoopMode: mode, Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
	}
}

func BenchmarkExperimentalReduction(b *testing.B) {
	for _, kind := range []string{"i32", "xor", "add-minus-one", "tee-drop", "header-eq"} {
		m := experimentalReductionModule(b, kind)
		b.Run(kind+"/compile", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
		for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513, 8192} {
			b.Run(kind+"/execute/"+fmtExperimentCount(n), func(b *testing.B) {
				r := newLoopExperimentRun(b, m, CompileOptions{}, 131072)
				for i := range r.jm.CurrentBytes() {
					r.jm.CurrentBytes()[i] = byte(i*19 + 7)
				}
				saved := shared.ReductionForms
				shared.ReductionForms = false
				ref := newLoopExperimentRun(b, m, CompileOptions{}, 131072)
				shared.ReductionForms = saved
				copy(ref.jm.CurrentBytes(), r.jm.CurrentBytes())
				args := []uint64{1, n}
				if err := ref.call(args...); err != nil {
					b.Fatal(err)
				}
				if err := r.call(args...); err != nil {
					b.Fatal(err)
				}
				if !bytes.Equal(ref.out, r.out) {
					b.Fatal("incorrect reduction")
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
					b.Fatal("final result differs")
				}
			})
		}
	}
}
