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
	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
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

func BenchmarkInstantiate_wago(b *testing.B) {
	m, _ := wasm.DecodeModule(fibWasm)
	wasm.ValidateModule(m)
	cm, _ := amd64.CompileModule(m)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		eng, _ := runtime.NewEngine()
		jm, _ := runtime.NewJobMemory(1 << 16)
		mem, _, _ := runtime.MapCode(cm.Code)
		runtime.Unmap(mem)
		jm.Close()
		eng.Close()
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

func globalBenchInstance(b *testing.B) (*wago.Instance, func()) {
	b.Helper()
	c, err := wago.Compile(globalBenchWasm)
	if err != nil {
		b.Fatal(err)
	}
	in, err := wago.Instantiate(c, nil)
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
