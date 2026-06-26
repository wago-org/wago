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
	globalBenchWasm = benchWasmModule(
		benchSection(1, benchVec(
			benchFuncType(nil, []wasm.ValType{wasm.I32}),
			benchFuncType([]wasm.ValType{wasm.I32}, nil),
			benchFuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		benchSection(3, benchVec([]byte{0x00}, []byte{0x01}, []byte{0x02}, []byte{0x00})),
		benchSection(5, benchVec([]byte{0x00, 0x01})),
		benchSection(6, benchVec(benchGlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		benchSection(7, benchVec(
			benchExportEntry("global_get", 0, 0),
			benchExportEntry("global_set", 0, 1),
			benchExportEntry("local_get", 0, 2),
			benchExportEntry("memory_load", 0, 3),
		)),
		benchSection(10, benchVec(
			benchCode([]byte{0x23, 0x00, 0x0b}),                   // global.get 0
			benchCode([]byte{0x20, 0x00, 0x24, 0x00, 0x0b}),       // local.get 0; global.set 0
			benchCode([]byte{0x20, 0x00, 0x0b}),                   // local.get 0
			benchCode([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b}), // i32.const 0; i32.load align=4 offset=0
		)),
	)
)

func benchULEB(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func benchSection(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, benchULEB(uint32(len(payload)))...)
	return append(out, payload...)
}

func benchWasmModule(sections ...[]byte) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

func benchVec(items ...[]byte) []byte {
	out := benchULEB(uint32(len(items)))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func benchName(s string) []byte { return append(benchULEB(uint32(len(s))), []byte(s)...) }

func benchFuncType(params, results []wasm.ValType) []byte {
	out := []byte{0x60}
	out = append(out, benchULEB(uint32(len(params)))...)
	for _, p := range params {
		out = append(out, byte(p))
	}
	out = append(out, benchULEB(uint32(len(results)))...)
	for _, r := range results {
		out = append(out, byte(r))
	}
	return out
}

func benchGlobalEntry(t wasm.ValType, mutable bool, init []byte) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	out := []byte{byte(t), mut}
	return append(out, init...)
}

func benchExportEntry(name string, kind byte, idx uint32) []byte {
	out := benchName(name)
	out = append(out, kind)
	return append(out, benchULEB(idx)...)
}

func benchCode(body []byte) []byte {
	fn := append([]byte{0x00}, body...)
	return append(benchULEB(uint32(len(fn))), fn...)
}

func BenchmarkCompile_wago(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m, err := wasm.Decode(fibWasm)
		if err != nil {
			b.Fatal(err)
		}
		if err := wasm.Validate(m); err != nil {
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
	m, _ := wasm.Decode(fibWasm)
	wasm.Validate(m)
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
	m, _ := wasm.Decode(wasmBytes)
	wasm.Validate(m)
	cm, err := amd64.CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	var localIdx int
	for i := range m.Exports {
		if m.Exports[i].Kind == wasm.ExternFunc && m.Exports[i].Name == export {
			localIdx = int(m.Exports[i].Index) - m.ImportedFuncCount()
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
