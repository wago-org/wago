// Package wagobench compares wago against wazero.
package wagobench

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	return b
}

var (
	fibWasm         = mustRead("../../tests/fixtures/wasm/fib.wasm")           // iterative fib (loop)
	recurWasm       = mustRead("../../tests/fixtures/wasm/recur.wasm")         // recursive fibrec (calls)
	globalBenchWasm = mustRead("../../tests/fixtures/bench/global_bench.wasm") // globals/local/memory microbench fixture
	callWasm        = mustRead("../../tests/fixtures/bench/call.wasm")         // boundary-only identity export (i32)->i32
	hostcallWasm    = mustRead("../../tests/fixtures/bench/hostcall.wasm")     // returning host import env.host(i32)->i32
)

// BenchmarkCompile_wago includes decoding, validation, code generation, and release.
func BenchmarkCompile_wago(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := wasm.DecodeModule(fibWasm)
		if err != nil {
			b.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			b.Fatal(err)
		}
		cm, err := benchCompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		if err := cm.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile_wazero(b *testing.B) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm, err := r.CompileModule(ctx, fibWasm)
		if err != nil {
			b.Fatal(err)
		}
		cm.Close(ctx)
	}
}

// BenchmarkInstantiate_wago times a full wago.Instantiate + Close of a compiled
// module through the public API — the same lifecycle wazero's InstantiateModule +
// Close measures below — so the two are comparable and the instantiate-state
// reuse (engine stack, arena, linear memory) is exercised. The earlier version
// timed the raw mmap/munmap primitives directly, which bypassed those reuse
// caches. Built with -tags wago_guardpage this runs the signals-based
// (guard-page) path.
func BenchmarkInstantiate_wago(b *testing.B) {
	c, err := wago.Compile(nil, fibWasm)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = c.Close() })
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		in, err := wago.Instantiate(c, wago.InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		in.Close()
	}
}

func BenchmarkInstantiate_wazero(b *testing.B) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)
	cm, err := r.CompileModule(ctx, fibWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer cm.Close(ctx)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mod, err := r.InstantiateModule(ctx, cm, wazero.NewModuleConfig().WithName(""))
		if err != nil {
			b.Fatal(err)
		}
		mod.Close(ctx)
	}
}

func wagoSetup(b *testing.B, wasmBytes []byte, export string) (func(n int32) int32, func()) {
	b.Helper()
	m, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		b.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		b.Fatal(err)
	}
	cm, err := benchCompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = cm.Close() })
	localIdx := -1
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternFunc && m.Exports[i].Name == export {
			localIdx = int(m.Exports[i].Index.Index) - m.ImportedFuncCount()
		}
	}
	if localIdx < 0 {
		b.Fatalf("missing export %s", export)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	jm, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		eng.Close()
		b.Fatal(err)
	}
	ar, err := runtime.NewArena(4096)
	if err != nil {
		jm.Close()
		eng.Close()
		b.Fatal(err)
	}
	mem, base, err := runtime.MapCode(cm.Code)
	if err != nil {
		ar.Close()
		jm.Close()
		eng.Close()
		b.Fatal(err)
	}
	entry := base + uintptr(cm.Entry[localIdx])
	if err := cm.Close(); err != nil {
		runtime.Unmap(mem)
		ar.Close()
		jm.Close()
		eng.Close()
		b.Fatal(err)
	}
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(runtime.TrapBufferBytes)
	lin := jm.LinearMemory()
	call := func(n int32) int32 {
		binary.LittleEndian.PutUint32(serArgs, uint32(n))
		if err := eng.Call(entry, serArgs, lin, trap, results); err != nil {
			b.Fatal(err)
		}
		return int32(binary.LittleEndian.Uint32(results))
	}
	cleanup := func() { runtime.Unmap(mem); ar.Close(); jm.Close(); eng.Close() }
	return call, cleanup
}

func wazeroSetup(b *testing.B, wasmBytes []byte, export string) (api.Function, func()) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	mod, err := r.Instantiate(ctx, wasmBytes)
	if err != nil {
		r.Close(ctx)
		b.Fatal(err)
	}
	return mod.ExportedFunction(export), func() { r.Close(ctx) }
}

func memoryGrowLoopModule(maxPages byte) []byte {
	body := []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x40, 0x00, 0x1a, // local.get delta; memory.grow 0; drop
		0x20, 0x01, 0x41, 0x01, 0x6b, 0x22, 0x01, // --iterations
		0x0d, 0x00, // br_if loop
		0x0b, 0x0b, 0x0b, // end loop, block, function
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32}, nil,
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, []byte{0x01, 0x01, 0x01, maxPages}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func memoryGrowOnceModule(maxPages byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, []byte{0x01, 0x01, 0x01, maxPages}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x40, 0x00, 0x0b, // local.get delta; memory.grow 0; end
		}))),
	)
}

// BenchmarkMemoryGrowKernel compares the compiled guest-side memory.grow path.
// A batch amortizes the host-to-Wasm boundary so the result mostly measures the
// instruction itself. The max equals the initial size: delta zero succeeds and
// delta one exercises the specified -1 failure result without changing memory.
func BenchmarkMemoryGrowKernel(b *testing.B) {
	const batch = uint64(256)
	wasmBytes := memoryGrowLoopModule(1)
	for _, tc := range []struct {
		name  string
		delta uint64
	}{
		{name: "zero", delta: 0},
		{name: "failure", delta: 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Run("wago", func(b *testing.B) {
				c, err := wago.Compile(nil, wasmBytes)
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := wago.Instantiate(c, wago.InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ReportMetric(float64(batch), "grows/op")
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err = fn.Invoke(tc.delta, batch); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("wazero", func(b *testing.B) {
				fn, cleanup := wazeroSetup(b, wasmBytes, "run")
				defer cleanup()
				ctx := context.Background()
				b.ReportAllocs()
				b.ReportMetric(float64(batch), "grows/op")
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := fn.Call(ctx, tc.delta, batch); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkMemoryGrowSuccess isolates one 64-KiB growth from a fresh instance.
// Instance creation and export resolution are deliberately outside the timer.
// Run with a fixed iteration count (for example, -benchtime=100x) because every
// timed call needs a new instance to begin from the same one-page state.
func BenchmarkMemoryGrowSuccess(b *testing.B) {
	wasmBytes := memoryGrowOnceModule(2)
	b.Run("wago", func(b *testing.B) {
		c, err := wago.Compile(nil, wasmBytes)
		if err != nil {
			b.Fatal(err)
		}
		defer c.Close()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			in, err := wago.Instantiate(c, wago.InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			fn, err := in.WasmFunc("grow")
			if err != nil {
				in.Close()
				b.Fatal(err)
			}
			if got, err := fn.Invoke(0); err != nil || len(got) != 1 || got[0] != 1 {
				in.Close()
				b.Fatalf("warmup grow(0) = %v, %v; want [1]", got, err)
			}
			b.StartTimer()
			got, err := fn.Invoke(1)
			b.StopTimer()
			if err != nil || len(got) != 1 || got[0] != 1 {
				in.Close()
				b.Fatalf("grow(1) = %v, %v; want [1]", got, err)
			}
			in.Close()
		}
	})
	for _, tc := range []struct {
		name        string
		capacityMax bool
	}{
		{name: "wazero-default"},
		{name: "wazero-capacity-from-max", capacityMax: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ctx := context.Background()
			cfg := wazero.NewRuntimeConfigCompiler().WithMemoryCapacityFromMax(tc.capacityMax)
			r := wazero.NewRuntimeWithConfig(ctx, cfg)
			defer r.Close(ctx)
			cm, err := r.CompileModule(ctx, wasmBytes)
			if err != nil {
				b.Fatal(err)
			}
			defer cm.Close(ctx)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				mod, err := r.InstantiateModule(ctx, cm, wazero.NewModuleConfig().WithName(""))
				if err != nil {
					b.Fatal(err)
				}
				fn := mod.ExportedFunction("grow")
				if got, err := fn.Call(ctx, 0); err != nil || len(got) != 1 || got[0] != 1 {
					mod.Close(ctx)
					b.Fatalf("warmup grow(0) = %v, %v; want [1]", got, err)
				}
				b.StartTimer()
				got, err := fn.Call(ctx, 1)
				b.StopTimer()
				if err != nil || len(got) != 1 || got[0] != 1 {
					mod.Close(ctx)
					b.Fatalf("grow(1) = %v, %v; want [1]", got, err)
				}
				mod.Close(ctx)
			}
		})
	}
}

func BenchmarkExecFibLoop_wago(b *testing.B) {
	call, cleanup := wagoSetup(b, fibWasm, "fib")
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = call(30)
	}
}

func BenchmarkExecFibLoop_wazero(b *testing.B) {
	fn, cleanup := wazeroSetup(b, fibWasm, "fib")
	defer cleanup()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fn.Call(ctx, 30)
	}
}

func BenchmarkExecFibRec_wago(b *testing.B) {
	call, cleanup := wagoSetup(b, recurWasm, "fibrec")
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = call(25)
	}
}

func BenchmarkExecFibRec_wazero(b *testing.B) {
	fn, cleanup := wazeroSetup(b, recurWasm, "fibrec")
	defer cleanup()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fn.Call(ctx, 25)
	}
}

// BenchmarkExecCallOverhead measures a host -> Wasm boundary crossing through
// an identity export, with no guest computation mixed into the result.
func BenchmarkExecCallOverhead_wago(b *testing.B) {
	call, cleanup := wagoSetup(b, callWasm, "call")
	defer cleanup()
	if got := call(1); got != 1 {
		b.Fatalf("call(1) = %d, want 1", got)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var got int32
	for i := 0; i < b.N; i++ {
		got = call(1)
	}
	b.StopTimer()
	if got != 1 {
		b.Fatalf("call(1) = %d, want 1", got)
	}
}

func BenchmarkExecCallOverhead_wazero(b *testing.B) {
	fn, cleanup := wazeroSetup(b, callWasm, "call")
	defer cleanup()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fn.Call(ctx, 1)
	}
}

// BenchmarkExecTypedCall_wago measures the public resolved (i32) -> i32 entry
// path. Setup resolves the export once, matching wazero's exported-function
// lookup outside the timed loop. Every Invoke performs normal admission.
func BenchmarkExecTypedCall_wago(b *testing.B) {
	c, err := wago.Compile(nil, callWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("call")
	if err != nil {
		b.Fatal(err)
	}
	if got, err := fn.Invoke(1); err != nil || len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v, %v; want 1", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var got []uint64
	for i := 0; i < b.N; i++ {
		got, err = fn.Invoke(1)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v, want 1", got)
	}
}

// BenchmarkExecInstanceCall_wago measures name-based Instance.Invoke on the
// same identity export used by the resolved-function and session benchmarks.
func BenchmarkExecInstanceCall_wago(b *testing.B) {
	c, err := wago.Compile(nil, callWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("call", 1); err != nil || len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v, %v; want 1", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var got []uint64
	for i := 0; i < b.N; i++ {
		got, err = in.Invoke("call", 1)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v, want 1", got)
	}
}

// BenchmarkExecSessionCall_wago measures a caller-owned reservation held across
// repeated calls. The reservation is acquired before timing and released after.
func BenchmarkExecSessionCall_wago(b *testing.B) {
	c, err := wago.Compile(nil, callWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("call")
	if err != nil {
		b.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Invoke1(1); err != nil || len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v, %v", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var got []uint64
	for i := 0; i < b.N; i++ {
		got, err = s.Invoke1(1)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(got) != 1 || got[0] != 1 {
		b.Fatalf("call(1) = %v", got)
	}
}

// BenchmarkExecHostRoundtrip measures one full wasm -> host -> wasm roundtrip:
// the guest calls a value-returning host import (env.host) once and returns its
// result. wago routes this through its synchronous host-call trampoline (the
// P8.1 save/resume protocol); wazero through its host-function
// path. The host does trivial work (x+1) so the number is dominated by the
// boundary crossing. Compare against ExecCallOverhead (a plain guest-only call):
// the difference is the added cost of the host-boundary round trip.
func BenchmarkExecHostRoundtrip_wago(b *testing.B) {
	benchmarkExecHostRoundtripWago(b, func(call wago.HostCall) { call.SetI32(0, call.I32(0)+1) })
}

// BenchmarkExecHostCallback_wago measures the same callback transaction through
// Wago's allocation-free typed scalar portal and a prepared typed export. This
// matches wazero's timed path, which also resolves its exported function before
// the benchmark loop, while retaining BenchmarkExecHostRoundtrip_wago as the
// legacy arbitrary-entry API history series.
func BenchmarkExecHostCallback_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x int32) int32 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("roundtrip")
	if err != nil {
		b.Fatal(err)
	}
	if got, err := fn.Invoke(1); err != nil || len(got) != 1 || got[0] != 2 {
		b.Fatalf("roundtrip(1) = %v, %v; want 2", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.Invoke(1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExecHostCallbackInstance_wago measures the same typed host call
// through the ordinary name-based Instance.Invoke entry.
func BenchmarkExecHostCallbackInstance_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x int32) int32 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("roundtrip", 1); err != nil || len(got) != 1 || got[0] != 2 {
		b.Fatalf("roundtrip(1) = %v, %v; want 2", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("roundtrip", 1); err != nil {
			b.Fatal(err)
		}
	}
}

func hostcallF64Wasm() []byte {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	importEntry = append(importEntry, 0, 0)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
}

func hostcallAlternatingF64Wasm() []byte {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	importEntry = append(importEntry, 0, 0)
	body := wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("first", 0, 1), wasmtest.ExportEntry("second", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(body, body)),
	)
}

// Each operation invokes both exports to expose cache assumptions about one entry.
func BenchmarkExecHostCallbackInstanceAlternatingF64_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallAlternatingF64Wasm())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x float64) float64 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	for _, name := range []string{"first", "second"} {
		if got, err := in.Invoke(name, wago.F64(1.5)); err != nil || len(got) != 1 || got[0] != wago.F64(2.5) {
			b.Fatalf("%s(1.5) = %v, %v; want 2.5", name, got, err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("first", wago.F64(1.5)); err != nil {
			b.Fatal(err)
		}
		if _, err := in.Invoke("second", wago.F64(1.5)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecHostCallbackInstanceF64_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallF64Wasm())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x float64) float64 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("roundtrip", wago.F64(1.5)); err != nil || len(got) != 1 || got[0] != wago.F64(2.5) {
		b.Fatalf("roundtrip(1.5) = %v, %v; want 2.5", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("roundtrip", wago.F64(1.5)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecHostCallbackResolvedF64_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallF64Wasm())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x float64) float64 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("roundtrip")
	if err != nil {
		b.Fatal(err)
	}
	if got, err := fn.Invoke(wago.F64(1.5)); err != nil || len(got) != 1 || got[0] != wago.F64(2.5) {
		b.Fatalf("roundtrip(1.5) = %v, %v; want 2.5", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.Invoke(wago.F64(1.5)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExecSessionHostCallback_wago measures the same typed callback with
// one instance reservation and prebound host entry held across timed calls.
func BenchmarkExecSessionHostCallback_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", func(x int32) int32 { return x + 1 })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("roundtrip")
	if err != nil {
		b.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Invoke1(1); err != nil || len(got) != 1 || got[0] != 2 {
		b.Fatalf("roundtrip(1) = %v, %v", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var got []uint64
	for i := 0; i < b.N; i++ {
		got, err = s.Invoke1(1)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(got) != 1 || got[0] != 2 {
		b.Fatalf("roundtrip(1) = %v", got)
	}
}

func benchmarkExecHostRoundtripWago(b *testing.B, callback any) {
	c, err := wago.Compile(nil, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	imports := wago.NewImports()
	imports.HostFunc("env", "host", callback).Params(wago.ValI32).Results(wago.ValI32)
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("roundtrip", wago.I32(1)); err != nil { // warm up
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("roundtrip", wago.I32(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecHostRoundtrip_wazero(b *testing.B) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)
	if _, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(_ context.Context, x uint32) uint32 { return x + 1 }).Export("host").
		Instantiate(ctx); err != nil {
		b.Fatal(err)
	}
	mod, err := r.Instantiate(ctx, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	fn := mod.ExportedFunction("roundtrip")
	if _, err := fn.Call(ctx, 1); err != nil { // warm up
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn.Call(ctx, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func globalBenchInstance(b *testing.B) (*wago.Instance, func()) {
	b.Helper()
	c, err := wago.Compile(nil, globalBenchWasm)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = c.Close() })
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	return in, func() { in.Close(); c.Close() }
}

func BenchmarkExecGlobalGet_wago(b *testing.B) {
	in, cleanup := globalBenchInstance(b)
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("global_get"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecGlobalSet_wago(b *testing.B) {
	in, cleanup := globalBenchInstance(b)
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("global_set", wago.I32(int32(i))); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecLocalGet_wago(b *testing.B) {
	in, cleanup := globalBenchInstance(b)
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("local_get", wago.I32(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecMemoryLoad_wago(b *testing.B) {
	in, cleanup := globalBenchInstance(b)
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("memory_load"); err != nil {
			b.Fatal(err)
		}
	}
}
