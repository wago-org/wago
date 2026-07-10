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
var benchTableSink *Table
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

func benchTableOwnerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(7))))),
	)
}

func benchImportedAndLocalTableShapeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
	)
}

func benchImportedAndLocalTablesModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		tableTestFuncSection(0, 0, 0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call0", 0, 1),
			wasmtest.ExportEntry("call1", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemAt(1, 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(9))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 1))),
		)),
	)
}

func benchTwoImportedAndLocalTableShapeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			tableTestImportTable("env", "first", 1, 1),
			tableTestImportTable("env", "second", 1, 1),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
	)
}

func benchTwoImportedAndLocalTablesModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			tableTestImportTable("env", "first", 1, 1),
			tableTestImportTable("env", "second", 1, 1),
		)),
		tableTestFuncSection(0, 0, 0, 0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call0", 0, 1),
			wasmtest.ExportEntry("call1", 0, 2),
			wasmtest.ExportEntry("call2", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemAt(2, 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(9))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 1))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 2))),
		)),
	)
}

func benchTable0IndirectModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 0))),
		)),
	)
}

func benchTwoLocalTablesModule() []byte { return benchTwoLocalTablesModuleWithExports(false) }

func benchTwoLocalTablesModuleWithExports(exportTables bool) []byte {
	table1Elem := []byte{0x02, 0x01} // active with explicit table index 1
	table1Elem = append(table1Elem, tableTestI32Const(0)...)
	table1Elem = append(table1Elem, 0x0b, 0x00) // end offset, elemkind funcref
	table1Elem = append(table1Elem, tableTestFuncIdxVec(0)...)
	exports := [][]byte{
		wasmtest.ExportEntry("call0", 0, 1),
		wasmtest.ExportEntry("call1", 0, 2),
	}
	if exportTables {
		exports = append(exports,
			wasmtest.ExportEntry("table0", 1, 0),
			wasmtest.ExportEntry("table1", 1, 1),
		)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0, 0, 0),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 0x01, 0x01},
			[]byte{0x70, 0x01, 0x01, 0x01},
		)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0), table1Elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 1))),
		)),
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

func BenchmarkInvokeNullExternref(b *testing.B) {
	c := benchMustCompile(b, externrefControlModule())
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

func BenchmarkInvokeNonNullExternrefRoundTrip(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(externrefControlModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	ref, err := rt.NewExternRef(struct{}{})
	if err != nil {
		b.Fatalf("NewExternRef: %v", err)
	}
	token := ValueExternRef(ref).Bits()
	if _, err := in.Invoke("id", token); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("id", token)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeRefFuncGlobalEgress(b *testing.B) {
	c := benchMustCompile(b, noTableRefFuncGlobalModule())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("get_global"); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("get_global")
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

func BenchmarkInvokeNullExternrefGlobalRoundTrip(b *testing.B) {
	c := benchMustCompile(b, nullableLocalExternrefGlobalsModule())
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

func BenchmarkInvokeNonNullExternrefGlobalRoundTrip(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(nullableLocalExternrefGlobalsModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	ref, err := rt.NewExternRef(struct{}{})
	if err != nil {
		b.Fatalf("NewExternRef: %v", err)
	}
	token := ValueExternRef(ref).Bits()
	if _, err := in.Invoke("set_and_get", token); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("set_and_get", token)
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

func BenchmarkInvokeTable0IndirectFixed(b *testing.B) {
	c := benchMustCompile(b, benchTable0IndirectModule())
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("call")
	if err != nil || len(res) != 1 || AsI32(res[0]) != 7 {
		b.Fatalf("warm call = %v, %v; want 7", res, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("call")
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeTable0IndirectTwoTableModule(b *testing.B) {
	c := benchMustCompile(b, benchTwoLocalTablesModule())
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("call0")
	if err != nil || len(res) != 1 || AsI32(res[0]) != 7 {
		b.Fatalf("warm call0 = %v, %v; want 7", res, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("call0")
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeTable1Indirect(b *testing.B) {
	c := benchMustCompile(b, benchTwoLocalTablesModule())
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("call1")
	if err != nil || len(res) != 1 || AsI32(res[0]) != 7 {
		b.Fatalf("warm call1 = %v, %v; want 7", res, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("call1")
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func benchmarkInvokeImportedAndLocalTable(b *testing.B, export string, want int32) {
	ownerCompiled := benchMustCompile(b, benchTableOwnerModule())
	owner, err := Instantiate(ownerCompiled)
	if err != nil {
		b.Fatalf("Instantiate owner: %v", err)
	}
	defer owner.Close()
	table, err := owner.ExportedTable("table")
	if err != nil {
		b.Fatalf("ExportedTable: %v", err)
	}
	consumerCompiled := benchMustCompile(b, benchImportedAndLocalTablesModule())
	consumer, err := Instantiate(consumerCompiled, Imports{"env.table": table})
	if err != nil {
		b.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()
	res, err := consumer.Invoke(export)
	if err != nil || len(res) != 1 || AsI32(res[0]) != want {
		b.Fatalf("warm %s = %v, %v; want %d", export, res, err, want)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := consumer.Invoke(export)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeImportedTable0IndirectWithLocalTable(b *testing.B) {
	benchmarkInvokeImportedAndLocalTable(b, "call0", 7)
}

func BenchmarkInvokeLocalTable1AfterImportedTable(b *testing.B) {
	benchmarkInvokeImportedAndLocalTable(b, "call1", 9)
}

func benchmarkInvokeTwoImportedAndLocalTable(b *testing.B, export string, want int32) {
	ownerCompiled := benchMustCompile(b, benchTableOwnerModule())
	firstOwner, err := Instantiate(ownerCompiled)
	if err != nil {
		b.Fatalf("Instantiate first owner: %v", err)
	}
	defer firstOwner.Close()
	first, err := firstOwner.ExportedTable("table")
	if err != nil {
		b.Fatalf("Export first table: %v", err)
	}
	secondOwner, err := Instantiate(ownerCompiled)
	if err != nil {
		b.Fatalf("Instantiate second owner: %v", err)
	}
	defer secondOwner.Close()
	second, err := secondOwner.ExportedTable("table")
	if err != nil {
		b.Fatalf("Export second table: %v", err)
	}
	consumerCompiled := benchMustCompile(b, benchTwoImportedAndLocalTablesModule())
	consumer, err := Instantiate(consumerCompiled, Imports{"env.first": first, "env.second": second})
	if err != nil {
		b.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()
	res, err := consumer.Invoke(export)
	if err != nil || len(res) != 1 || AsI32(res[0]) != want {
		b.Fatalf("warm %s = %v, %v; want %d", export, res, err, want)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := consumer.Invoke(export)
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = res
	}
}

func BenchmarkInvokeFirstImportedTable0WithTwoImports(b *testing.B) {
	benchmarkInvokeTwoImportedAndLocalTable(b, "call0", 7)
}

func BenchmarkInvokeSecondImportedTable1(b *testing.B) {
	benchmarkInvokeTwoImportedAndLocalTable(b, "call1", 7)
}

func BenchmarkInvokeLocalTable2AfterTwoImports(b *testing.B) {
	benchmarkInvokeTwoImportedAndLocalTable(b, "call2", 9)
}

func BenchmarkExportedTable0Cached(b *testing.B) {
	c := benchMustCompile(b, benchTwoLocalTablesModuleWithExports(true))
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.ExportedTable("table0"); err != nil {
		b.Fatalf("warm ExportedTable table0: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table, err := in.ExportedTable("table0")
		if err != nil {
			b.Fatal(err)
		}
		benchTableSink = table
	}
}

func BenchmarkExportedTable1Cached(b *testing.B) {
	c := benchMustCompile(b, benchTwoLocalTablesModuleWithExports(true))
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.ExportedTable("table1"); err != nil {
		b.Fatalf("warm ExportedTable table1: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table, err := in.ExportedTable("table1")
		if err != nil {
			b.Fatal(err)
		}
		benchTableSink = table
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

func BenchmarkInvokeHostFuncExternrefRoundTrip(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(externrefHostRoundTripModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	ref, err := rt.NewExternRef(struct{}{})
	if err != nil {
		b.Fatalf("NewExternRef: %v", err)
	}
	token := ValueExternRef(ref).Bits()
	in, err := rt.Instantiate(nil, mod, WithImports(Imports{"env.echo": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })}))
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("roundtrip", token); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := in.Invoke("roundtrip", token)
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

func BenchmarkRuntimeInstantiateExternrefControl(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(externrefControlModule())
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

func BenchmarkRuntimeInstantiateNoTableRefFuncGlobal(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(noTableRefFuncGlobalModule())
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

func BenchmarkRuntimeInstantiateNullableExternrefGlobals(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(nullableLocalExternrefGlobalsModule())
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

func BenchmarkRuntimeInstantiateTwoLocalTables(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchTwoLocalTablesModule())
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

func BenchmarkRuntimeInstantiateTwoLocalTableExports(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchTwoLocalTablesModuleWithExports(true))
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

func BenchmarkRuntimeInstantiateImportedAndLocalTables(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchImportedAndLocalTableShapeModule())
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

func BenchmarkRuntimeInstantiateTwoImportedAndLocalTables(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchTwoImportedAndLocalTableShapeModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	first, err := NewTable(1, 1)
	if err != nil {
		b.Fatalf("NewTable first: %v", err)
	}
	defer first.Close()
	second, err := NewTable(1, 1)
	if err != nil {
		b.Fatalf("NewTable second: %v", err)
	}
	defer second.Close()
	imports := Imports{"env.first": first, "env.second": second}
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
