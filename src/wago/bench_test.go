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
	c, err := Compile(nil, mod)
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

func benchMinOnlyTableGrowModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})), // table funcref min=0, no maximum
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(
			tableTestRefNullFunc(),
			tableTestI32Const(1),
			tableTestBulk(15, 0),
		)))),
	)
}

func benchMinOnlyTableFixedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})), // table funcref min=0, no maximum
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
}

func benchImportedTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
	)
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
		c, err := Compile(nil, mod)
		if err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = c
	}
}

func BenchmarkInvokeAddOne(b *testing.B) {
	c := benchMustCompile(b, benchAddOneModule())
	in, err := Instantiate(c, InstantiateOptions{})
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

func BenchmarkInvokeReexportedInstanceFunc(b *testing.B) {
	rt, producer, relay := instantiateImportedFunctionReexport(b)
	defer closeImportedFunctionReexport(b, rt, producer, relay)
	if _, err := relay.Invoke("forward", I32(1)); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := relay.Invoke("forward", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeNullFuncref(b *testing.B) {
	c := benchMustCompile(b, nullableFuncrefModule())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("id", 0); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("id", 0)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeNullFuncrefGlobalRoundTrip(b *testing.B) {
	c := benchMustCompile(b, nullableLocalFuncrefGlobalsModule())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("set_and_get", 0); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("set_and_get", 0)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeLocalFuncrefEgress(b *testing.B) {
	rt := NewRuntime()
	producerMod, err := rt.Compile(funcrefImportedProducerModule())
	if err != nil {
		b.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(nil, producerMod)
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	defer func() {
		_ = producer.Close()
		_ = rt.Close()
	}()
	if _, err := producer.Invoke("get"); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := producer.Invoke("get")
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeImportedFuncrefEgress(b *testing.B) {
	rt := NewRuntime()
	producerMod, err := rt.Compile(funcrefImportedProducerModule())
	if err != nil {
		b.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(nil, producerMod)
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	target, err := producer.ExportedFunc("target")
	if err != nil {
		b.Fatalf("Export target: %v", err)
	}
	importerMod, err := rt.Compile(funcrefImportedRefFuncModule())
	if err != nil {
		b.Fatalf("Compile importer: %v", err)
	}
	importer, err := rt.Instantiate(nil, importerMod, WithImports(Imports{"env.target": target}))
	if err != nil {
		b.Fatalf("Instantiate importer: %v", err)
	}
	defer func() {
		_ = importer.Close()
		_ = producer.Close()
		_ = rt.Close()
	}()
	if _, err := importer.Invoke("get"); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := importer.Invoke("get")
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeTableGrowNull(b *testing.B) {
	c := benchMustCompile(b, benchMinOnlyTableGrowModule())
	if c.TableMax <= 0 {
		b.Fatalf("table growth capacity = %d, want positive", c.TableMax)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	for done := 0; done < b.N; {
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			b.Fatalf("Instantiate: %v", err)
		}
		batch := c.TableMax
		if remaining := b.N - done; batch > remaining {
			batch = remaining
		}
		b.StartTimer()
		for i := 0; i < batch; i++ {
			res, err := in.Invoke("grow")
			if err != nil {
				b.Fatal(err)
			}
			if got := AsI32(res[0]); got != int32(i) {
				b.Fatalf("table.grow = %d, want old size %d", got, i)
			}
			benchResultSink = res
		}
		b.StopTimer()
		_ = in.Close()
		done += batch
	}
}

func BenchmarkInvokeNonNullFuncrefRoundTrip(b *testing.B) {
	rt := NewRuntime()
	producerMod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		b.Fatalf("Compile producer: %v", err)
	}
	relayMod, err := rt.Compile(nullableFuncrefModule())
	if err != nil {
		b.Fatalf("Compile relay: %v", err)
	}
	producer, err := rt.Instantiate(nil, producerMod)
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	relay, err := rt.Instantiate(nil, relayMod)
	if err != nil {
		b.Fatalf("Instantiate relay: %v", err)
	}
	defer func() {
		_ = producer.Close()
		_ = relay.Close()
		_ = rt.Close()
	}()
	ref, err := producer.Invoke("get")
	if err != nil || len(ref) != 1 || ref[0] == 0 {
		b.Fatalf("producer get = %v, %v", ref, err)
	}
	if _, err := relay.Invoke("id", ref[0]); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := relay.Invoke("id", ref[0])
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeLegacyHostFuncVoid(b *testing.B) {
	c := benchMustCompile(b, voidI32ImportCallerModule())
	var calls int32
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.log": HostFunc(func(_ HostModule, p, _ []uint64) { calls += AsI32(p[0]) & 1 })}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": func(v int32) int32 { return v + 1 }}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, _ []uint64) { calls += AsI32(p[0]) & 1 })}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": func(v int32) int32 { return v + 1 }}})
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
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0], r[1] = p[0]+1, p[1]+1 })}})
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

func BenchmarkRuntimeInstantiateSmallScalar(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchAddOneModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(nil, mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateNullableFuncrefGlobals(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(nullableLocalFuncrefGlobalsModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(nil, mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateMinOnlyTableFixed(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchMinOnlyTableFixedModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(nil, mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateMinOnlyTableGrow(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchMinOnlyTableGrowModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(nil, mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateImportedTable(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchImportedTableModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	table, err := NewTable(1, 1)
	if err != nil {
		b.Fatalf("NewTable: %v", err)
	}
	defer table.Close()
	imports := Imports{"env.table": table}
	warm, err := rt.Instantiate(nil, mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(nil, mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkInstantiateImportedStartHostFunc(b *testing.B) {
	c := benchMustCompile(b, importedStartModule())
	imports := Imports{"env.start": HostFunc(func(HostModule, []uint64, []uint64) {})}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := Instantiate(c, InstantiateOptions{Imports: imports})
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
	warm, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	warm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := Instantiate(c, InstantiateOptions{Imports: imports})
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
