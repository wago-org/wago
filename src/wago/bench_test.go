package wago

import (
	"context"
	"fmt"
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
var benchUintSink uint64

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

// benchBranchHintExecModule counts down its argument in native wasm code. The
// br_if exits the loop only once, so its condition is overwhelmingly unlikely
// to be true. The optional metadata section describes exactly that branch.
func benchBranchHintExecModule(withHint bool) []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x20, 0x00, 0x21, 0x01, // local 1 = param 0
		0x02, 0x7f, // block (result i32)
		0x03, 0x40, // loop
		0x41, 0x00, 0x20, 0x01, 0x45, 0x0d, 0x01, // branch value; local.get 1; i32.eqz; br_if 1
		0x1a,                                     // drop the branch value on the loop's fall-through path
		0x20, 0x01, 0x41, 0x01, 0x6b, 0x21, 0x01, // local 1--
		0x0c, 0x00, // br 0
		0x0b, 0x41, 0x00, 0x0b, // end loop; fallback result; end block
		0x0b, // end function
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
	}
	if withHint {
		name := wasmtest.Name("metadata.code.branch_hint")
		payload := append(name, 0x01, 0x00, 0x01, 0x10, 0x01, 0x00) // func 0, br_if at body offset 16, unlikely
		sections = append(sections, wasmtest.Section(0, payload))
	}
	// body already includes the local declarations, unlike wasmtest.Code.
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code)))
	return wasmtest.Module(sections...)
}

// benchBranchHintTreeModule keeps a three-level, mostly-cold branch tree in a
// counting loop. Cold arms update seven scratch locals, making the generated
// code large enough for branch layout and allocation choices to matter beyond a
// single trivially predicted exit branch.
func benchBranchHintTreeModule(withHint bool) []byte {
	body := []byte{0x01, 0x08, 0x7f} // eight i32 locals, including the parameter
	// local 1 is the hot accumulator; locals 2..8 are touched only by cold arms.
	body = append(body, 0x41, 0x00, 0x21, 0x01)                   // local 1 = 0
	body = append(body, 0x02, 0x7f, 0x03, 0x40)                   // block (result i32); loop
	body = append(body, 0x20, 0x01, 0x20, 0x00, 0x45, 0x0d, 0x01) // branch accumulator if n == 0
	exitOffset := uint32(len(body) - 2)                           // br_if opcode, not its label immediate
	body = append(body, 0x1a)                                     // discard the branch value on the hot loop path
	var ifOffsets []uint32
	for _, mask := range []byte{0x0f, 0x3f, 0xff} {
		body = append(body, 0x20, 0x00, 0x41, mask, 0x71, 0x45) // (n & mask) == 0
		ifOffsets = append(ifOffsets, uint32(len(body)))
		body = append(body, 0x04, 0x40) // if: rare then arm
		for local := byte(2); local <= 8; local++ {
			body = append(body, 0x20, local, 0x41, local, 0x6a, 0x21, local)
		}
		body = append(body, 0x05) // else: hot arm
		body = append(body, 0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01)
		body = append(body, 0x0b)
	}
	body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, 0x0c, 0x00) // n--; br loop
	body = append(body, 0x0b, 0x20, 0x01, 0x0b, 0x0b)                         // end loop; fallback accumulator; end block/function
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
	}
	if withHint {
		name := wasmtest.Name("metadata.code.branch_hint")
		payload := append(name, wasmtest.ULEB(1)...)
		payload = append(payload, wasmtest.ULEB(0)...)
		payload = append(payload, wasmtest.ULEB(uint32(len(ifOffsets)+1))...)
		payload = append(payload, wasmtest.ULEB(exitOffset)...)
		payload = append(payload, 0x01, 0x00) // loop exit is unlikely
		for _, off := range ifOffsets {
			payload = append(payload, wasmtest.ULEB(off)...)
			payload = append(payload, 0x01, 0x00) // `then` is unlikely; the else arm is hot
		}
		sections = append(sections, wasmtest.Section(0, payload))
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code)))
	return wasmtest.Module(sections...)
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

func benchImportedMemoryReexportModule() []byte {
	entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	entry = append(entry, 0x02, 0x00, 0x01) // memory import, min=1
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(entry)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
	)
}

func benchImportedTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
	)
}

func benchImportedExternrefTableModule() []byte {
	entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	entry = append(entry, 0x01, 0x6f, 0x01, 0x01, 0x01) // table externref, min=1, max=1
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entry)))
}

func benchExportedExternrefTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
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

func BenchmarkInvokeBranchHintLoop(b *testing.B) {
	withoutHint := benchMustCompile(b, benchBranchHintExecModule(false))
	withHint := benchMustCompile(b, benchBranchHintExecModule(true))
	if len(withoutHint.Code) == 0 || (goruntime.GOARCH == "arm64" && string(withoutHint.Code) == string(withHint.Code)) {
		b.Fatal("unlikely br_if hint did not select deferred cold-edge layout")
	}
	for _, tc := range []struct {
		name string
		c    *Compiled
	}{
		{"none", withoutHint},
		{"unlikely_exit", withHint},
	} {
		b.Run(tc.name, func(b *testing.B) {
			in, err := Instantiate(tc.c, InstantiateOptions{})
			if err != nil {
				b.Fatalf("Instantiate: %v", err)
			}
			defer in.Close()
			if got, err := in.Invoke("f", I32(10_000)); err != nil || len(got) != 1 || got[0] != 0 {
				b.Fatalf("warm Invoke = %v, %v", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := in.Invoke("f", I32(10_000))
				if err != nil {
					b.Fatal(err)
				}
				benchResultSink = result
			}
		})
	}
}

func BenchmarkInvokeBranchHintTree(b *testing.B) {
	withoutHint := benchMustCompile(b, benchBranchHintTreeModule(false))
	withHint := benchMustCompile(b, benchBranchHintTreeModule(true))
	if string(withoutHint.Code) == string(withHint.Code) {
		b.Fatal("branch hints did not affect the branch-tree native layout or allocation")
	}
	for _, tc := range []struct {
		name string
		c    *Compiled
	}{
		{"none", withoutHint},
		{"hinted_tree", withHint},
	} {
		b.Run(tc.name, func(b *testing.B) {
			in, err := Instantiate(tc.c, InstantiateOptions{})
			if err != nil {
				b.Fatalf("Instantiate: %v", err)
			}
			defer in.Close()
			if _, err := in.Invoke("f", I32(10_000)); err != nil {
				b.Fatalf("warm Invoke: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := in.Invoke("f", I32(10_000))
				if err != nil {
					b.Fatal(err)
				}
				benchResultSink = result
			}
		})
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
	in, err := rt.Instantiate(context.Background(), mod)
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

func BenchmarkInvokeNumericGlobalRoundTrip(b *testing.B) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set_and_get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x23, 0x00, 0x0b}))),
	)
	c := benchMustCompile(b, mod)
	in, err := Instantiate(c)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("set_and_get", 1); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchResultSink, err = in.Invoke("set_and_get", uint64(i))
		if err != nil {
			b.Fatal(err)
		}
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

func benchExternrefTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set_and_get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(
			tableTestI32Const(0), tableTestLocalGet(0), []byte{0x26, 0x00},
			tableTestI32Const(0), []byte{0x25, 0x00},
		)))),
	)
}

func BenchmarkInvokeNullExternrefTableRoundTrip(b *testing.B) {
	c := benchMustCompile(b, benchExternrefTableModule())
	in, err := Instantiate(c)
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
		benchResultSink, err = in.Invoke("set_and_get", 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvokeNonNullExternrefTableRoundTrip(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(benchExternrefTableModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
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
		benchResultSink, err = in.Invoke("set_and_get", token)
		if err != nil {
			b.Fatal(err)
		}
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
	in, err := rt.Instantiate(context.Background(), mod)
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
	producer, err := rt.Instantiate(context.Background(), producerMod)
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
	producer, err := rt.Instantiate(context.Background(), producerMod)
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
	importer, err := rt.Instantiate(context.Background(), importerMod, WithImports(Imports{"env.target": target}))
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

func benchOwnedHostFuncrefModule(b testing.TB) []byte {
	return watToWasm(b, `(module
		(type $target-type (func (result i32)))
		(import "env" "target" (func $target (type $target-type)))
		(table 1 funcref)
		(elem declare func $target)
		(func (export "get") (result funcref) (ref.func $target))
	)`)
}

func BenchmarkInvokeOwnedHostFuncrefEgress(b *testing.B) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		b.Fatalf("NewHostFuncRef: %v", err)
	}
	mod, err := rt.Compile(benchOwnedHostFuncrefModule(b))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer func() {
		_ = in.Close()
		_ = rt.Close()
		_ = owner.Close()
	}()
	if _, err := in.Invoke("get"); err != nil {
		b.Fatalf("warm Invoke: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchResultSink, err = in.Invoke("get")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvokeOwnedHostFuncrefIndirect(b *testing.B) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		b.Fatalf("NewHostFuncRef: %v", err)
	}
	producerMod, err := rt.Compile(benchOwnedHostFuncrefModule(b))
	if err != nil {
		b.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	consumerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		b.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		b.Fatalf("Instantiate consumer: %v", err)
	}
	out, err := producer.Invoke("get")
	if err != nil || len(out) != 1 || out[0] == 0 {
		b.Fatalf("get = %v, %v", out, err)
	}
	token := out[0]
	defer func() {
		_ = consumer.Close()
		_ = producer.Close()
		_ = rt.Close()
		_ = owner.Close()
	}()
	if got, err := consumer.Invoke("call", token); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		b.Fatalf("warm call = %v, %v", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchResultSink, err = consumer.Invoke("call", token)
		if err != nil {
			b.Fatal(err)
		}
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

func BenchmarkExportedExternrefTableCached(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchExportedExternrefTableModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer func() {
		_ = in.Close()
		_ = rt.Close()
	}()
	if _, err := in.ExportedTable("table"); err != nil {
		b.Fatalf("warm ExportedTable: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table, err := in.ExportedTable("table")
		if err != nil {
			b.Fatal(err)
		}
		benchTableSink = table
	}
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

func benchmarkInvokeTableBulk(b *testing.B, wat, export string) {
	b.Helper()
	compiled := benchMustCompile(b, watToWasm(b, wat))
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke(export); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInvokeTable0CopyFuncref(b *testing.B) {
	benchmarkInvokeTableBulk(b, `(module
		(table 8 8 funcref)
		(elem (i32.const 0) func $f $f $f $f)
		(func $f)
		(func (export "copy")
			(table.copy 0 0 (i32.const 2) (i32.const 0) (i32.const 4))))`, "copy")
}

func BenchmarkInvokeTable0InitFuncref(b *testing.B) {
	benchmarkInvokeTableBulk(b, `(module
		(table 8 8 funcref)
		(elem $e funcref (ref.func $f) (ref.func $f) (ref.func $f) (ref.func $f))
		(func $f)
		(func (export "init")
			(table.init 0 $e (i32.const 0) (i32.const 0) (i32.const 4))))`, "init")
}

func BenchmarkInvokeTable0CopyExternref(b *testing.B) {
	benchmarkInvokeTableBulk(b, `(module
		(table 8 8 externref)
		(func (export "copy")
			(table.copy 0 0 (i32.const 2) (i32.const 0) (i32.const 4))))`, "copy")
}

func BenchmarkInvokeTable0InitExternref(b *testing.B) {
	benchmarkInvokeTableBulk(b, `(module
		(table 8 8 externref)
		(elem $e externref (ref.null extern) (ref.null extern) (ref.null extern) (ref.null extern))
		(func (export "init")
			(table.init 0 $e (i32.const 0) (i32.const 0) (i32.const 4))))`, "init")
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
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	relay, err := rt.Instantiate(context.Background(), relayMod)
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
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.echo": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })}))
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateFuncrefIngressCaller(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateOwnedHostFuncref(b *testing.B) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		b.Fatalf("NewHostFuncRef: %v", err)
	}
	mod, err := rt.Compile(benchOwnedHostFuncrefModule(b))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer func() {
		_ = rt.Close()
		_ = owner.Close()
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": owner}))
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func benchmarkOwnedHostFuncRefGlobal(b testing.TB) (*Runtime, *Global) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		b.Fatalf("NewHostFuncRef: %v", err)
	}
	producerMod, err := rt.Compile(benchOwnedHostFuncrefModule(b))
	if err != nil {
		b.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.target": owner}))
	if err != nil {
		b.Fatalf("Instantiate producer: %v", err)
	}
	out, err := producer.Invoke("get")
	if err != nil || len(out) != 1 || out[0] == 0 {
		b.Fatalf("get token = %v, %v", out, err)
	}
	global, err := rt.NewFuncRefGlobal(ValueOf(ValFuncRef, out[0]).FuncRef(), true)
	if err != nil {
		b.Fatalf("NewFuncRefGlobal: %v", err)
	}
	if err := producer.Close(); err != nil {
		b.Fatalf("Close producer: %v", err)
	}
	b.Cleanup(func() {
		_ = global.Close()
		_ = rt.Close()
		_ = owner.Close()
	})
	return rt, global
}

func BenchmarkRuntimeInstantiateImportedFuncRefGlobal(b *testing.B) {
	rt, global := benchmarkOwnedHostFuncRefGlobal(b)
	mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, true)))))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	imports := Imports{"env.ref": global}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkStoreBoundFuncRefGlobalGetValue(b *testing.B) {
	_, global := benchmarkOwnedHostFuncRefGlobal(b)
	if _, err := global.GetValue(); err != nil {
		b.Fatalf("warm GetValue: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, err := global.GetValue()
		if err != nil {
			b.Fatal(err)
		}
		benchUintSink = value.Bits()
	}
}

func BenchmarkRuntimeInstantiateImportedExternrefGlobal(b *testing.B) {
	rt := NewRuntime()
	ref, err := rt.NewExternRef("shared-global")
	if err != nil {
		b.Fatalf("NewExternRef: %v", err)
	}
	global, err := rt.NewExternRefGlobal(ref, true)
	if err != nil {
		b.Fatalf("NewExternRefGlobal: %v", err)
	}
	mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.ExternRef, true)))))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	imports := Imports{"env.ref": global}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	b.Cleanup(func() {
		_ = global.Close()
		_ = rt.Close()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateImportedNumericGlobal(b *testing.B) {
	rt := NewRuntime()
	global := NewGlobalI64(1, true)
	mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "value", wasm.I64, true)))))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	imports := Imports{"env.value": global}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	b.Cleanup(func() {
		_ = global.Close()
		_ = rt.Close()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkStoreBoundExternrefGlobalGetValue(b *testing.B) {
	rt := NewRuntime()
	ref, err := rt.NewExternRef("global")
	if err != nil {
		b.Fatalf("NewExternRef: %v", err)
	}
	global, err := rt.NewExternRefGlobal(ref, true)
	if err != nil {
		b.Fatalf("NewExternRefGlobal: %v", err)
	}
	b.Cleanup(func() {
		_ = global.Close()
		_ = rt.Close()
	})
	if _, err := global.GetValue(); err != nil {
		b.Fatalf("warm GetValue: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, err := global.GetValue()
		if err != nil {
			b.Fatal(err)
		}
		benchUintSink = value.Bits()
	}
}

func BenchmarkRuntimeInstantiateExternrefTable(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchExternrefTableModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			b.Fatal(err)
		}
		if err := in.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRuntimeInstantiatePassiveExternrefElements(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(watToWasm(b, `(module
		(table 8 8 externref)
		(elem $e externref (ref.null extern) (ref.null extern) (ref.null extern) (ref.null extern))
		(func (export "init")
			(table.init 0 $e (i32.const 0) (i32.const 0) (i32.const 4))))`))
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
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
	warm, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateSharedMemoryImport(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(importMemModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	memory, err := NewSharedMemory(1, 2)
	if err != nil {
		b.Fatalf("NewSharedMemory: %v", err)
	}
	imports := Imports{"env.mem": memory}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer func() {
		_ = memory.Close()
		_ = rt.Close()
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateImportedMemoryReexport(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchImportedMemoryReexportModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	memory, err := NewSharedMemory(1, 2)
	if err != nil {
		b.Fatalf("NewSharedMemory: %v", err)
	}
	imports := Imports{"env.memory": memory}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	if _, err := warm.ExportedMemory("memory"); err != nil {
		b.Fatalf("warm ExportedMemory: %v", err)
	}
	_ = warm.Close()
	defer func() {
		_ = memory.Close()
		_ = rt.Close()
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		exported, err := in.ExportedMemory("memory")
		if err != nil {
			b.Fatal(err)
		}
		if exported != memory {
			b.Fatal("memory re-export identity changed")
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
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			b.Fatal(err)
		}
		_ = in.Close()
	}
}

func BenchmarkRuntimeInstantiateImportedExternrefTable(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchImportedExternrefTableModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	table, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		b.Fatalf("NewExternRefTable: %v", err)
	}
	imports := Imports{"env.table": table}
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer func() {
		_ = table.Close()
		_ = rt.Close()
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
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
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
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
	warm, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		b.Fatalf("warm Instantiate: %v", err)
	}
	_ = warm.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
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

func BenchmarkMarshalCompiledStructuralReferences(b *testing.B) {
	c := structuralReferenceCodecFixture()
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

func BenchmarkUnmarshalCompiledStructuralReferences(b *testing.B) {
	c := structuralReferenceCodecFixture()
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

func benchBulkMemoryModule(op byte) []byte {
	body := []byte{0x20, 0x00, 0x20, 0x01}
	if op == 0x0b { // memory.fill: dst, byte, n has the same three i32 inputs.
		body = append(body, 0x20, 0x02, 0xfc, op, 0x00, 0x0b)
	} else {
		body = append(body, 0x20, 0x02, 0xfc, op, 0x00, 0x00, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func BenchmarkBulkMemoryARM64(b *testing.B) {
	for _, tc := range []struct {
		name string
		op   byte
	}{
		{"copy", 0x0a},
		{"fill", 0x0b},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := benchMustCompile(b, benchBulkMemoryModule(tc.op))
			in, err := Instantiate(c, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			for i := range in.Memory().Bytes() {
				in.Memory().Bytes()[i] = byte(i)
			}
			for _, n := range []uint64{64, 256, 4096} {
				b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(n))
					for i := 0; i < b.N; i++ {
						arg1 := uint64(0)
						if tc.op == 0x0b {
							arg1 = 0xa5
						}
						if _, err := in.Invoke("run", I32(32768), arg1, n); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
