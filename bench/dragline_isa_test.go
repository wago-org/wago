package wagobench

import (
	"bytes"
	"os"
	"runtime"
	"slices"
	"strconv"
	"testing"

	wago "github.com/wago-org/wago"
	"wagobench/internal/hwcounters"
)

func TestDraglineParallelWasm3ArtifactDeterminism(t *testing.T) {
	source, err := os.ReadFile("corpus/wasm3.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compile := func(workers int) []byte {
		t.Helper()
		compiled, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).WithBoundsChecks(wago.BoundsChecksExplicit).WithFunctionWorkers(workers).Compile(source)
		if err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
		defer compiled.Close()
		artifact, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	serial, parallel := compile(1), compile(8)
	if !bytes.Equal(parallel, serial) {
		t.Fatal("parallel large-function candidate search changed the artifact")
	}
}

var draglineAdmittedISAModules = map[string]bool{
	"isa_i32":        true,
	"isa_i64":        true,
	"isa_cmp_i32":    true,
	"isa_cmp_i64":    true,
	"isa_f32":        true,
	"isa_f64":        true,
	"isa_cmp_f32":    true,
	"isa_cmp_f64":    true,
	"isa_ctl":        true,
	"isa_var":        true,
	"isa_cvt":        true,
	"isa_mem":        true,
	"isa_mem_narrow": true,
	"isa_call":       true,
	"isa_cvt_mvp":    true,
	"isa_bulk_mem":   true,
	"isa_signext":    true,
}

// TestDraglineMVPISAInventory makes the performance claim auditable. Numeric,
// comparison, conversion, and scalar-memory instructions have one timed row per
// opcode. Control/stack marker instructions are intentionally measured in the
// representative compositions below (they do not all produce standalone native
// work); the pinned spec corpus remains their exhaustive semantic gate. Bulk
// SIMD and reference-type instructions remain outside this scalar inventory;
// the standardized bulk-memory primitives have a separately calibrated row.
func TestDraglineMVPISAInventory(t *testing.T) {
	expectedCounts := map[string]int{
		"isa_i32": 18, "isa_i64": 18,
		"isa_cmp_i32": 11, "isa_cmp_i64": 11,
		"isa_f32": 14, "isa_f64": 14,
		"isa_cmp_f32": 6, "isa_cmp_f64": 6,
		"isa_mem": 7, "isa_mem_narrow": 19,
		"isa_ctl": 5, "isa_call": 2, "isa_var": 2,
		"isa_cvt": 8, "isa_cvt_mvp": 25,
		"isa_bulk_mem": 9,
		"isa_signext":  5,
	}
	requiredCompositions := map[string][]string{
		"isa_ctl":  {"loop_baseline", "br_if", "if_else", "br_table", "select"},
		"isa_call": {"call_direct", "call_indirect"},
		"isa_var":  {"local_getset", "global_getset"},
	}
	seen, total := make(map[string]bool, len(expectedCounts)), 0
	for _, module := range readManifest(t, "isa-manifest.json") {
		name := module.name()
		want, admitted := expectedCounts[name]
		if !admitted {
			if draglineAdmittedISAModules[name] {
				t.Fatalf("post-MVP module %s is admitted without an inventory row", name)
			}
			continue
		}
		seen[name] = true
		if len(module.Exec) != want {
			t.Fatalf("%s exports = %d, want %d", name, len(module.Exec), want)
		}
		total += len(module.Exec)
		exports := make(map[string]bool, len(module.Exec))
		for _, entry := range module.Exec {
			exports[entry.Export] = true
		}
		for _, required := range requiredCompositions[name] {
			if !exports[required] {
				t.Fatalf("%s is missing structural composition %s", name, required)
			}
		}
	}
	if len(seen) != len(expectedCounts) || len(draglineAdmittedISAModules) != len(expectedCounts) {
		t.Fatalf("MVP inventory modules = %d/%d; admitted=%d", len(seen), len(expectedCounts), len(draglineAdmittedISAModules))
	}
	if total != 180 {
		t.Fatalf("admitted ISA rows = %d, want 180", total)
	}
}

// TestDraglineISACorpus is the compatibility gate for ISA families admitted by
// Dragline. Every admitted module compiles without falling back to Railshot, and
// every declared benchmark entry produces the same bits as Railshot from an
// independently instantiated module using the manifest's full trip counts.
// Adding a family to this map is the admission step; unsupported families
// remain strict compile errors.
func TestDraglineISACorpus(t *testing.T) {
	modules := readManifest(t, "isa-manifest.json")
	for _, m := range modules {
		m := m
		if !draglineAdmittedISAModules[m.name()] {
			continue
		}
		t.Run(m.name(), func(t *testing.T) {
			railshot, err := wago.NewRuntimeConfig().
				WithBoundsChecks(wago.BoundsChecksExplicit).
				WithCompiler(wago.CompilerRailshot).
				Compile(m.bytes)
			if err != nil {
				t.Fatalf("Railshot compile: %v", err)
			}
			defer railshot.Close()

			dragline, err := wago.NewRuntimeConfig().
				WithBoundsChecks(wago.BoundsChecksExplicit).
				WithCompiler(wago.CompilerDragline).
				Compile(m.bytes)
			if err != nil {
				t.Fatalf("Dragline compile: %v", err)
			}
			defer dragline.Close()
			if dragline.Compiler() != wago.CompilerDragline {
				t.Fatalf("compiler = %s, want dragline", dragline.Compiler())
			}

			wantInstance, err := wago.Instantiate(railshot, wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Railshot instantiate: %v", err)
			}
			defer wantInstance.Close()
			gotInstance, err := wago.Instantiate(dragline, wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Dragline instantiate: %v", err)
			}
			defer gotInstance.Close()

			for _, entry := range m.Exec {
				entry := entry
				t.Run(entry.Export, func(t *testing.T) {
					args := make([]uint64, len(entry.Args))
					for i, arg := range entry.Args {
						args[i] = wago.I32(arg)
					}
					want, err := wantInstance.Invoke(entry.Export, args...)
					if err != nil {
						t.Fatalf("Railshot invoke: %v", err)
					}
					got, err := gotInstance.Invoke(entry.Export, args...)
					if err != nil {
						t.Fatalf("Dragline invoke: %v", err)
					}
					if !slices.Equal(got, want) {
						t.Fatalf("result = %#x, want %#x", got, want)
					}
				})
			}
		})
	}
}

// TestDraglineTieredISACorpus proves that the stable Railshot-to-Dragline
// installation boundary executes every admitted MVP ISA row, not merely a
// synthetic scalar function. Each result is compared before and after the
// one-shot tier publication on the same instance.
func TestDraglineTieredISACorpus(t *testing.T) {
	for _, m := range readManifest(t, "isa-manifest.json") {
		m := m
		if !draglineAdmittedISAModules[m.name()] {
			continue
		}
		t.Run(m.name(), func(t *testing.T) {
			base := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
			railshot, err := base.WithTiering(true).Compile(m.bytes)
			if err != nil {
				t.Fatalf("tierable Railshot compile: %v", err)
			}
			defer railshot.Close()
			dragline, err := base.WithCompiler(wago.CompilerDragline).Compile(m.bytes)
			if err != nil {
				t.Fatalf("Dragline compile: %v", err)
			}
			defer dragline.Close()
			in, err := wago.Instantiate(railshot, wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("tierable Railshot instantiate: %v", err)
			}
			defer in.Close()
			for _, entry := range m.Exec {
				args := make([]uint64, len(entry.Args))
				for i, arg := range entry.Args {
					args[i] = wago.I32(arg)
				}
				_, err := in.Invoke(entry.Export, args...)
				if err != nil {
					t.Fatalf("Railshot %s: %v", entry.Export, err)
				}
			}
			// Re-run the manifest after installation because module state (notably
			// memory/global rows) must advance identically from the same point.
			if err := in.InstallDragline(dragline); err != nil {
				t.Fatalf("install Dragline: %v", err)
			}
			if in.ActiveCompiler() != wago.CompilerDragline {
				t.Fatalf("active compiler = %s", in.ActiveCompiler())
			}
			// Independently establish the expected post-call state with Railshot.
			wantInstance, err := wago.Instantiate(railshot, wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("expected Railshot instantiate: %v", err)
			}
			defer wantInstance.Close()
			for _, entry := range m.Exec {
				args := make([]uint64, len(entry.Args))
				for i, arg := range entry.Args {
					args[i] = wago.I32(arg)
				}
				if _, err := wantInstance.Invoke(entry.Export, args...); err != nil {
					t.Fatalf("prime expected Railshot %s: %v", entry.Export, err)
				}
			}
			for _, entry := range m.Exec {
				args := make([]uint64, len(entry.Args))
				for i, arg := range entry.Args {
					args[i] = wago.I32(arg)
				}
				want, err := wantInstance.Invoke(entry.Export, args...)
				if err != nil {
					t.Fatalf("expected Railshot %s: %v", entry.Export, err)
				}
				got, err := in.Invoke(entry.Export, args...)
				if err != nil {
					t.Fatalf("tiered Dragline %s: %v", entry.Export, err)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("tiered %s = %#x, want %#x", entry.Export, got, want)
				}
			}
		})
	}
}

func TestDraglineF32RoundTripRecurrence(t *testing.T) {
	module := draglineISAModule(t, "isa_cvt")

	compile := func(compiler wago.CompilerEngine) *wago.Compiled {
		t.Helper()
		compiled, err := wago.NewRuntimeConfig().
			WithBoundsChecks(wago.BoundsChecksExplicit).
			WithCompiler(compiler).
			Compile(module.bytes)
		if err != nil {
			t.Fatalf("%s compile: %v", compiler, err)
		}
		t.Cleanup(func() { compiled.Close() })
		return compiled
	}

	wantInstance, err := wago.Instantiate(compile(wago.CompilerRailshot), wago.InstantiateOptions{})
	if err != nil {
		t.Fatalf("Railshot instantiate: %v", err)
	}
	t.Cleanup(func() { wantInstance.Close() })
	gotInstance, err := wago.Instantiate(compile(wago.CompilerDragline), wago.InstantiateOptions{})
	if err != nil {
		t.Fatalf("Dragline instantiate: %v", err)
	}
	t.Cleanup(func() { gotInstance.Close() })

	for _, export := range []string{"f32_convert_s", "promote_demote"} {
		for _, n := range []uint64{0, 1, 2, 3, 17, 255, 1024, 2047, 2048, 4095, 4096, 4097} {
			want, err := wantInstance.Invoke(export, wago.I32(int32(n)))
			if err != nil {
				t.Fatalf("Railshot %s(%d): %v", export, n, err)
			}
			got, err := gotInstance.Invoke(export, wago.I32(int32(n)))
			if err != nil {
				t.Fatalf("Dragline %s(%d): %v", export, n, err)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%s(%d) = %#x, want %#x", export, n, got, want)
			}
		}
	}
}

func TestDraglineCoupledIntegerGroups(t *testing.T) {
	for _, moduleName := range []string{"isa_i32", "isa_i64"} {
		module := draglineISAModule(t, moduleName)
		t.Run(moduleName, func(t *testing.T) {
			compile := func(compiler wago.CompilerEngine) *wago.Compiled {
				t.Helper()
				compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).Compile(module.bytes)
				if err != nil {
					t.Fatalf("%s compile: %v", compiler, err)
				}
				t.Cleanup(func() { compiled.Close() })
				return compiled
			}
			wantInstance, err := wago.Instantiate(compile(wago.CompilerRailshot), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Railshot instantiate: %v", err)
			}
			t.Cleanup(func() { wantInstance.Close() })
			gotInstance, err := wago.Instantiate(compile(wago.CompilerDragline), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Dragline instantiate: %v", err)
			}
			t.Cleanup(func() { gotInstance.Close() })

			for _, export := range []string{"add", "sub", "mul", "and", "or", "xor", "shl", "shr_s", "shr_u", "rotl", "rotr", "div_s", "div_u", "rem_s", "rem_u", "clz", "ctz", "popcnt"} {
				for _, n := range []int32{0, 1, 2, 3, 17, 255} {
					want, err := wantInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Railshot %s(%d): %v", export, n, err)
					}
					got, err := gotInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Dragline %s(%d): %v", export, n, err)
					}
					if !slices.Equal(got, want) {
						t.Fatalf("%s(%d) = %#x, want %#x", export, n, got, want)
					}
				}
			}
		})
	}
}

func TestDraglinePowerRotationGroups(t *testing.T) {
	for _, moduleName := range []string{"isa_i32", "isa_i64"} {
		module := draglineISAModule(t, moduleName)
		t.Run(moduleName, func(t *testing.T) {
			compile := func(compiler wago.CompilerEngine) *wago.Compiled {
				t.Helper()
				compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).Compile(module.bytes)
				if err != nil {
					t.Fatalf("%s compile: %v", compiler, err)
				}
				t.Cleanup(func() { compiled.Close() })
				return compiled
			}
			wantInstance, err := wago.Instantiate(compile(wago.CompilerRailshot), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Railshot instantiate: %v", err)
			}
			t.Cleanup(func() { wantInstance.Close() })
			gotInstance, err := wago.Instantiate(compile(wago.CompilerDragline), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Dragline instantiate: %v", err)
			}
			t.Cleanup(func() { gotInstance.Close() })

			for _, export := range []string{"rotl", "rotr"} {
				for _, n := range []int32{0, 1, 2, 3, 17, 255, 256, 1024, 2047, 2048, 4095, 4096} {
					want, err := wantInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Railshot %s(%d): %v", export, n, err)
					}
					got, err := gotInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Dragline %s(%d): %v", export, n, err)
					}
					if !slices.Equal(got, want) {
						t.Fatalf("%s(%d) = %#x, want %#x", export, n, got, want)
					}
				}
			}
		})
	}
}

func TestDraglineCoupledFloatGroups(t *testing.T) {
	for _, moduleName := range []string{"isa_f32", "isa_f64"} {
		module := draglineISAModule(t, moduleName)
		t.Run(moduleName, func(t *testing.T) {
			compile := func(compiler wago.CompilerEngine) *wago.Compiled {
				t.Helper()
				compiled, err := wago.NewRuntimeConfig().WithCompiler(compiler).Compile(module.bytes)
				if err != nil {
					t.Fatalf("%s compile: %v", compiler, err)
				}
				t.Cleanup(func() { compiled.Close() })
				return compiled
			}
			wantInstance, err := wago.Instantiate(compile(wago.CompilerRailshot), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Railshot instantiate: %v", err)
			}
			t.Cleanup(func() { wantInstance.Close() })
			gotInstance, err := wago.Instantiate(compile(wago.CompilerDragline), wago.InstantiateOptions{})
			if err != nil {
				t.Fatalf("Dragline instantiate: %v", err)
			}
			t.Cleanup(func() { gotInstance.Close() })

			for _, export := range []string{"add", "sub", "mul", "div", "min", "max", "sqrt", "abs", "neg", "ceil", "floor", "trunc", "nearest"} {
				for _, n := range []int32{
					0, 1, 2, 3, 4, 8, 16, 17, 32, 64, 128, 255,
					256, 512, 1024, 2048, 4096,
				} {
					want, err := wantInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Railshot %s(%d): %v", export, n, err)
					}
					got, err := gotInstance.Invoke(export, wago.I32(n))
					if err != nil {
						t.Fatalf("Dragline %s(%d): %v", export, n, err)
					}
					if !slices.Equal(got, want) {
						t.Fatalf("%s(%d) = %#x, want %#x", export, n, got, want)
					}
				}
			}
		})
	}
}

func draglineISAModule(t *testing.T, name string) corpusModule {
	t.Helper()
	for _, candidate := range readManifest(t, "isa-manifest.json") {
		if candidate.name() == name {
			return candidate
		}
	}
	t.Fatalf("%s module is missing", name)
	return corpusModule{}
}

// BenchmarkRailshotDraglineISAExec is the paired execution gate for ISA
// families admitted by Dragline. Setup, compilation, instantiation, export
// lookup, and one warmup invocation happen outside the timed region. Keeping
// both backends under the same benchmark node keeps the raw output easy to
// compare. Go groups repeated -count samples by sub-benchmark, so experiments
// that require alternating order need an external round driver.
func BenchmarkRailshotDraglineISAExec(b *testing.B) {
	modules := readManifest(b, "isa-manifest.json")
	compilers := []wago.CompilerEngine{wago.CompilerRailshot, wago.CompilerDragline}
	if os.Getenv("WAGO_DRAGLINE_BENCH_ORDER") == "dragline-first" {
		slices.Reverse(compilers)
	}
	for _, m := range modules {
		if !draglineAdmittedISAModules[m.name()] {
			continue
		}
		for _, entry := range m.Exec {
			for _, compiler := range compilers {
				compiler := compiler
				b.Run(m.name()+"."+entry.Export+"/"+compiler.String(), func(b *testing.B) {
					benchmarkISAExport(b, compiler, m, entry, false)
				})
			}
		}
	}
}

// BenchmarkRailshotTieredDraglineISAExec applies the same release gate after a
// live Railshot instance publishes the Dragline tier behind stable thunks.
func BenchmarkRailshotTieredDraglineISAExec(b *testing.B) {
	modules := readManifest(b, "isa-manifest.json")
	compilers := []wago.CompilerEngine{wago.CompilerRailshot, wago.CompilerDragline}
	if os.Getenv("WAGO_DRAGLINE_BENCH_ORDER") == "dragline-first" {
		slices.Reverse(compilers)
	}
	for _, m := range modules {
		if !draglineAdmittedISAModules[m.name()] {
			continue
		}
		for _, entry := range m.Exec {
			for _, compiler := range compilers {
				compiler := compiler
				b.Run(m.name()+"."+entry.Export+"/"+compiler.String(), func(b *testing.B) {
					benchmarkISAExport(b, compiler, m, entry, true)
				})
			}
		}
	}
}

func benchmarkISAExport(b *testing.B, compiler wago.CompilerEngine, m corpusModule, entry execEntry, tiered bool) {
	cfg := wago.NewRuntimeConfig().
		WithBoundsChecks(wago.BoundsChecksExplicit).
		WithCompiler(compiler)
	if compiler == wago.CompilerDragline && os.Getenv("WAGO_DRAGLINE_BENCH_TARGET") == "native" {
		cfg = cfg.WithTarget(wago.TargetNative)
	}
	compileConfig := cfg
	if tiered && compiler == wago.CompilerDragline {
		compileConfig = wago.NewRuntimeConfig().
			WithBoundsChecks(wago.BoundsChecksExplicit).
			WithTiering(true)
	}
	compiled, err := compileConfig.Compile(m.bytes)
	if err != nil {
		b.Fatalf("%s compile with %s: %v", m.name(), compiler, err)
	}
	b.Cleanup(func() { compiled.Close() })
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		b.Fatalf("%s instantiate with %s: %v", m.name(), compiler, err)
	}
	b.Cleanup(func() { instance.Close() })
	if tiered && compiler == wago.CompilerDragline {
		dragline, err := cfg.Compile(m.bytes)
		if err != nil {
			b.Fatalf("%s compile installed Dragline tier: %v", m.name(), err)
		}
		b.Cleanup(func() { dragline.Close() })
		if err := instance.InstallDragline(dragline); err != nil {
			b.Fatalf("%s install Dragline tier: %v", m.name(), err)
		}
	}
	fn, err := instance.PrepareFunction(entry.Export)
	if err != nil {
		b.Fatalf("%s prepare %s with %s: %v", m.name(), entry.Export, compiler, err)
	}
	args := make([]uint64, len(entry.Args))
	for i, arg := range entry.Args {
		args[i] = wago.I32(arg)
	}
	if override := os.Getenv("WAGO_DRAGLINE_ISA_ARG"); override != "" && len(args) == 1 {
		arg, err := strconv.ParseInt(override, 0, 32)
		if err != nil {
			b.Fatalf("WAGO_DRAGLINE_ISA_ARG: %v", err)
		}
		args[0] = wago.I32(int32(arg))
	}
	if _, err := fn.Invoke(args...); err != nil {
		b.Fatalf("warmup invoke: %v", err)
	}
	var counters *hwcounters.Group
	if os.Getenv("WAGO_HARDWARE_COUNTERS") == "1" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		counters, err = hwcounters.Open()
		if err != nil {
			b.Fatalf("open hardware counters: %v", err)
		}
		defer func() {
			if err := counters.Close(); err != nil {
				b.Errorf("close hardware counters: %v", err)
			}
		}()
	}
	b.ReportAllocs()
	b.ResetTimer()
	if counters != nil {
		if err := counters.Start(); err != nil {
			b.Fatalf("start hardware counters: %v", err)
		}
	}
	for i := 0; i < b.N; i++ {
		if _, err := fn.Invoke(args...); err != nil {
			b.Fatal(err)
		}
	}
	if counters != nil {
		counts, err := counters.Stop()
		if err != nil {
			b.Fatalf("stop hardware counters: %v", err)
		}
		for _, count := range counts {
			if count.TimeRunning == 0 {
				b.Fatalf("hardware counter %s was never scheduled", count.Name)
			}
			b.ReportMetric(count.Scaled()/float64(b.N), count.Name+"/op")
		}
	}
}

// BenchmarkRailshotDraglineISACompile measures the public decode, validate, and
// compile path for each admitted module. It records heap work and native code
// bytes alongside latency; execution benchmarks intentionally keep this setup
// outside their timed regions.
func BenchmarkRailshotDraglineISACompile(b *testing.B) {
	modules := readManifest(b, "isa-manifest.json")
	for _, m := range modules {
		if !draglineAdmittedISAModules[m.name()] {
			continue
		}
		for _, compiler := range []wago.CompilerEngine{wago.CompilerRailshot, wago.CompilerDragline} {
			compiler := compiler
			b.Run(m.name()+"/"+compiler.String(), func(b *testing.B) {
				cfg := wago.NewRuntimeConfig().WithCompiler(compiler)
				if compiler == wago.CompilerDragline && os.Getenv("WAGO_DRAGLINE_BENCH_TARGET") == "native" {
					cfg = cfg.WithTarget(wago.TargetNative)
				}
				var codeBytes uint64
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					compiled, err := cfg.Compile(m.bytes)
					if err != nil {
						b.Fatal(err)
					}
					codeBytes += uint64(compiled.CodeSize())
					if err := compiled.Close(); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(codeBytes)/float64(b.N), "code-B/op")
			})
		}
	}
}

// BenchmarkDraglineNeutralCompile measures the public cold compile path for
// the non-SIMD application modules used by the external-compiler gate. Keeping
// this in-process view separate prevents child-process startup and artifact
// serialization from being mistaken for compiler work.
func BenchmarkDraglineNeutralCompile(b *testing.B) {
	wanted := map[string]bool{
		"blake-as": true, "json-as": true, "utf-as": true,
		"regexmatch": true, "wasm3": true,
	}
	for _, m := range loadCorpus(b) {
		if !wanted[m.name()] {
			continue
		}
		m := m
		b.Run(m.name(), func(b *testing.B) {
			cfg := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).WithBoundsChecks(wago.BoundsChecksExplicit)
			b.ReportAllocs()
			var codeBytes uint64
			for i := 0; i < b.N; i++ {
				compiled, err := cfg.Compile(m.bytes)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes += uint64(compiled.CodeSize())
				if err := compiled.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(codeBytes)/float64(b.N), "code-B/op")
		})
	}
}
