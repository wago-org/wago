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
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	return b
}

var (
	fibWasm         = mustRead("../tests/testdata/fib.wasm")   // iterative fib (loop)
	recurWasm       = mustRead("../tests/testdata/recur.wasm") // recursive fibrec (calls)
	globalBenchWasm = mustRead("testdata/global_bench.wasm")   // globals/local/memory microbench fixture
	hostcallWasm    = mustRead("testdata/hostcall.wasm")       // returning host import env.host(i32)->i32
)

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
		if _, err := amd64.CompileModule(m); err != nil {
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
	cm, _ := r.CompileModule(ctx, fibWasm)
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
	m, _ := wasm.DecodeModule(wasmBytes)
	wasm.ValidateModule(m)
	cm, err := amd64.CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	var localIdx int
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternFunc && m.Exports[i].Name == export {
			localIdx = int(m.Exports[i].Index.Index) - m.ImportedFuncCount()
		}
	}
	eng, _ := runtime.NewEngine()
	jm, _ := runtime.NewJobMemory(1 << 16)
	ar, _ := runtime.NewArena(4096)
	mem, base, _ := runtime.MapCode(cm.Code)
	entry := base + uintptr(cm.Entry[localIdx])
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	lin := jm.LinearMemory()
	call := func(n int32) int32 {
		binary.LittleEndian.PutUint32(serArgs, uint32(n))
		eng.Call(entry, serArgs, lin, trap, results)
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
		b.Fatal(err)
	}
	return mod.ExportedFunction(export), func() { r.Close(ctx) }
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

// BenchmarkExecCallOverhead measures the cross-boundary call cost (fib(1)).
func BenchmarkExecCallOverhead_wago(b *testing.B) {
	call, cleanup := wagoSetup(b, fibWasm, "fib")
	defer cleanup()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = call(1)
	}
}

func BenchmarkExecCallOverhead_wazero(b *testing.B) {
	fn, cleanup := wazeroSetup(b, fibWasm, "fib")
	defer cleanup()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fn.Call(ctx, 1)
	}
}

// BenchmarkExecHostRoundtrip measures one full wasm -> host -> wasm roundtrip:
// the guest calls a value-returning host import (env.host) once and returns its
// result. wago routes this through its synchronous host-call trampoline (the
// P8.1 save/resume protocol that unlocks WASI); wazero through its host-function
// path. The host does trivial work (x+1) so the number is dominated by the
// boundary crossing. Compare against ExecCallOverhead (a plain guest-only call):
// the difference is the added cost of the host-boundary round trip.
func BenchmarkExecHostRoundtrip_wago(b *testing.B) {
	c, err := wago.Compile(nil, hostcallWasm)
	if err != nil {
		b.Fatal(err)
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wago.Imports{
		"env.host": wago.HostFunc(func(_ wago.HostModule, p, r []uint64) { r[0] = p[0] + 1 }),
	}})
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
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	return in, func() { in.Close() }
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
