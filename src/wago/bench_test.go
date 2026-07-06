package wago

import (
	goruntime "runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

var benchResultSink []uint64
var benchBytesSink []byte
var benchCompiledSink *Compiled
var benchIntSink int32

func benchMustCompile(b *testing.B, mod []byte) *Compiled {
	b.Helper()
	c, err := Compile(mod)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	return c
}

func benchAddOneModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}))), // local.get 0; i32.const 1; i32.add; end
	)
}

func benchReturningImportModule() []byte {
	return returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}) // local.get 0; call 0; end
}

func benchTableReturningImportModule() []byte {
	return tableHostImportModule(returningI32Sig(), []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}) // local.get 0; i32.const 0; call_indirect type 0 table 0; end
}

func benchTableVoidImportModule() []byte {
	importSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	localSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	return tableHostImportModuleWithLocal(importSig, localSig, []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x20, 0x00, 0x0b}) // call_indirect; local.get 0; end
}

func benchTableV128ImportModule() []byte {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
	return tableHostImportModule(sig, []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}) // local.get 0; i32.const 0; call_indirect type 0 table 0; end
}

func BenchmarkSupportedFeatures(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if SupportedFeatures() == 0 {
			b.Fatal("no supported features")
		}
	}
}

func BenchmarkCompileSmallScalar(b *testing.B) {
	mod := benchAddOneModule()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := Compile(mod)
		if err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = c
	}
}

func BenchmarkInvokeAddOne(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	in, err := Instantiate(c, nil)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("f", I32(1)); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("f", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeLegacyHostFuncVoid(b *testing.B) {
	c := benchMustCompile(b, voidI32ImportCallerModule())
	var calls int32
	in, err := Instantiate(c, Imports{"env.log": HostFunc(func(_ HostModule, p, _ []uint64) { calls += AsI32(p[0]) & 1 })})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("g", I32(int32(i))); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	benchIntSink = calls
}

func BenchmarkInvokeHostFuncDirect(b *testing.B) {
	c := benchMustCompile(b, benchReturningImportModule())
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeReflectedHostFuncDirect(b *testing.B) {
	if goruntime.Compiler == "tinygo" {
		b.Skip("reflected host imports are unavailable under TinyGo")
	}
	c := benchMustCompile(b, benchReturningImportModule())
	in, err := Instantiate(c, Imports{"env.f": func(v int32) int32 { return v + 1 }})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeHostFuncTableIndirect(b *testing.B) {
	c := benchMustCompile(b, benchTableReturningImportModule())
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeLegacyHostFuncTableIndirect(b *testing.B) {
	c := benchMustCompile(b, benchTableVoidImportModule())
	var calls int32
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(_ HostModule, p, _ []uint64) { calls += AsI32(p[0]) & 1 })})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
	b.StopTimer()
	benchIntSink = calls
}

func BenchmarkInvokeReflectedHostFuncTableIndirect(b *testing.B) {
	if goruntime.Compiler == "tinygo" {
		b.Skip("reflected host imports are unavailable under TinyGo")
	}
	c := benchMustCompile(b, benchTableReturningImportModule())
	in, err := Instantiate(c, Imports{"env.f": func(v int32) int32 { return v + 1 }})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeHostFuncV128TableIndirect(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	c := benchMustCompile(b, benchTableV128ImportModule())
	inVec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	lo, hi := hostV128Slots(inVec)
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0], r[1] = p[0]+1, p[1]+1 })})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("g", lo, hi)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInstantiateImportedStartHostFunc(b *testing.B) {
	c := benchMustCompile(b, importedStartModule())
	imports := Imports{"env.start": HostFunc(func(HostModule, []uint64, []uint64) {})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := Instantiate(c, imports)
		if err != nil {
			b.Fatal(err)
		}
		in.Close()
	}
}

func BenchmarkInstantiateTableHostFuncThunk(b *testing.B) {
	c := benchMustCompile(b, benchTableReturningImportModule())
	imports := Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })}
	// Warm the host link cache so the benchmark isolates instance wiring and thunk mapping.
	warm, err := Instantiate(c, imports)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	warm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := Instantiate(c, imports)
		if err != nil {
			b.Fatal(err)
		}
		in.Close()
	}
}

func BenchmarkMarshalCompiledSmallScalar(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blob, err := c.MarshalBinary()
		if err != nil {
			b.Fatal(err)
		}
		benchBytesSink = blob
	}
}

func BenchmarkUnmarshalCompiledSmallScalar(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	blob, err := c.MarshalBinary()
	if err != nil {
		b.Fatalf("MarshalBinary: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out Compiled
		if err := out.UnmarshalBinary(blob); err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = &out
	}
}
