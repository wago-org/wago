//go:build linux && amd64

package wago

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func tableTestBody(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, 0x0b)
}

func tableTestI32Const(v int32) []byte {
	out := []byte{0x41}
	out = append(out, wasmtest.SLEB32(v)...)
	return out
}

func tableTestLocalGet(i uint32) []byte {
	return append([]byte{0x20}, wasmtest.ULEB(i)...)
}

func tableTestRefFunc(i uint32) []byte {
	return append([]byte{0xd2}, wasmtest.ULEB(i)...)
}

func tableTestRefNullFunc() []byte { return []byte{0xd0, 0x70} }

func tableTestCallIndirect(typeIdx, tableIdx uint32) []byte {
	out := append([]byte{0x11}, wasmtest.ULEB(typeIdx)...)
	return append(out, wasmtest.ULEB(tableIdx)...)
}

func tableTestBulk(sub uint32, immediates ...uint32) []byte {
	out := append([]byte{0xfc}, wasmtest.ULEB(sub)...)
	for _, imm := range immediates {
		out = append(out, wasmtest.ULEB(imm)...)
	}
	return out
}

func tableTestFuncSection(typeIdxs ...uint32) []byte {
	items := make([][]byte, len(typeIdxs))
	for i, idx := range typeIdxs {
		items[i] = wasmtest.ULEB(idx)
	}
	return wasmtest.Section(3, wasmtest.Vec(items...))
}

func tableTestImportTable(module, name string, min, max uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x01, 0x70) // import kind table, elemtype funcref
	if max != 0 {
		out = append(out, 0x01) // limits: min + max
		out = append(out, wasmtest.ULEB(min)...)
		out = append(out, wasmtest.ULEB(max)...)
	} else {
		out = append(out, 0x00) // limits: min only
		out = append(out, wasmtest.ULEB(min)...)
	}
	return out
}

func tableTestTableWithInit(min, max uint32, expr []byte) []byte {
	out := []byte{0x40, 0x00, 0x70} // table with initializer, funcref table type
	if max != 0 {
		out = append(out, 0x01) // limits: min + max
		out = append(out, wasmtest.ULEB(min)...)
		out = append(out, wasmtest.ULEB(max)...)
	} else {
		out = append(out, 0x00) // limits: min only
		out = append(out, wasmtest.ULEB(min)...)
	}
	out = append(out, expr...)
	return append(out, 0x0b)
}

func tableTestFuncIdxVec(funcs ...uint32) []byte {
	out := wasmtest.ULEB(uint32(len(funcs)))
	for _, f := range funcs {
		out = append(out, wasmtest.ULEB(f)...)
	}
	return out
}

func tableTestActiveElem(offset int32, funcs ...uint32) []byte {
	out := []byte{0x00}
	out = append(out, tableTestI32Const(offset)...)
	out = append(out, 0x0b)
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestActiveElemAt(tableIdx uint32, offset int32, funcs ...uint32) []byte {
	out := append([]byte{0x02}, wasmtest.ULEB(tableIdx)...)
	out = append(out, tableTestI32Const(offset)...)
	out = append(out, 0x0b, 0x00) // end offset, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestPassiveElem(funcs ...uint32) []byte {
	out := []byte{0x01, 0x00} // passive, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestActiveElemExpr(offset int32, exprs ...[]byte) []byte {
	out := []byte{0x04} // active table 0, funcref expression payloads
	out = append(out, tableTestI32Const(offset)...)
	out = append(out, 0x0b) // end offset expr
	return append(out, tableTestExprVec(exprs...)...)
}

func tableTestPassiveElemExpr(exprs ...[]byte) []byte {
	out := []byte{0x05, 0x70} // passive, elemtype funcref, expression payloads
	return append(out, tableTestExprVec(exprs...)...)
}

func tableTestExprVec(exprs ...[]byte) []byte {
	out := wasmtest.ULEB(uint32(len(exprs)))
	for _, expr := range exprs {
		out = append(out, expr...)
		out = append(out, 0x0b)
	}
	return out
}

func tableTestRefFuncExpr(i uint32) []byte { return tableTestRefFunc(i) }

func tableTestRefNullFuncExpr() []byte { return tableTestRefNullFunc() }

func tableTestDeclarativeElem(funcs ...uint32) []byte {
	out := []byte{0x03, 0x00} // declarative, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestForceExplicitBounds(t *testing.T) {
	t.Helper()
	t.Setenv("WAGO_BOUNDS", "explicit")
}

func tableTestInstantiate(t *testing.T, mod []byte) *Instance {
	t.Helper()
	return tableTestInstantiateWithImports(t, mod, nil)
}

func tableTestInstantiateWithImports(t *testing.T, mod []byte, imports Imports) *Instance {
	t.Helper()
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	inst, err := Instantiate(c, imports)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	return inst
}

func tableTestCallI32(t *testing.T, inst *Instance, name string, args ...uint64) int32 {
	t.Helper()
	res, err := inst.Invoke(name, args...)
	if err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}
	return AsI32(res[0])
}

func tableTestExpectTrap(t *testing.T, err error, code TrapCode) {
	t.Helper()
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("error = %v, want trap %v", err, code)
	}
}

func tableInitializerModule(initExpr []byte, activeElems ...[]byte) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
		)),
		tableTestFuncSection(0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(3, 3, initExpr))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 2))),
	}
	if len(activeElems) != 0 {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(activeElems...)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code(tableTestBody(tableTestI32Const(7))),
		wasmtest.Code(tableTestBody(tableTestI32Const(42))),
		wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
	)))
	return wasmtest.Module(sections...)
}

func tableInitializerImportModule() []byte {
	importFunc := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importFunc = append(importFunc, 0x00) // import kind func
	importFunc = append(importFunc, wasmtest.ULEB(0)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32 imported initializer target
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32 callAt
		)),
		wasmtest.Section(2, wasmtest.Vec(importFunc)),
		tableTestFuncSection(1),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(2, 2, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)
}

func tableInitializerZeroLengthModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
		)),
		tableTestFuncSection(0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(0, 2, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("callAt", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(11))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
}

func tableInitializerGrowModule(growValue []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
		)),
		tableTestFuncSection(0, 0, 0, 1, 0),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(1, 2, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
			wasmtest.ExportEntry("size", 0, 4),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(11))),
			wasmtest.Code(tableTestBody(tableTestI32Const(22))),
			wasmtest.Code(tableTestBody(growValue, tableTestI32Const(1), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
		)),
	)
}

func TestFuncrefTableInitializerExpressionPrefillsTable(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerModule(tableTestRefFuncExpr(1)))
	defer inst.Close()
	for _, idx := range []int32{0, 1, 2} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 42 {
			t.Fatalf("callAt(%d) = %d, want table initializer target 42", idx, got)
		}
	}
}

func TestFuncrefTableInitializerExpressionActiveSegmentOverrides(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerModule(tableTestRefFuncExpr(1), tableTestActiveElem(1, 0)))
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 42 {
		t.Fatalf("callAt(0) = %d, want initializer target 42", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 7 {
		t.Fatalf("callAt(1) = %d, want active element target 7", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 42 {
		t.Fatalf("callAt(2) = %d, want initializer target 42", got)
	}
}

func TestFuncrefTableInitializerExpressionActiveRefNullOverrides(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerModule(tableTestRefFuncExpr(1), tableTestActiveElemExpr(1, tableTestRefNullFuncExpr())))
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 42 {
		t.Fatalf("callAt(0) = %d, want initializer target 42", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 42 {
		t.Fatalf("callAt(2) = %d, want initializer target 42", got)
	}
}

func TestFuncrefTableInitializerExpressionPassiveTableInitRefNullOverrides(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(3, 3, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 1),
			wasmtest.ExportEntry("initNull", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElemExpr(tableTestRefNullFuncExpr()))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(42))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(12, 0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	for _, idx := range []int32{0, 1, 2} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 42 {
			t.Fatalf("callAt(%d) before initNull = %d, want initializer target 42", idx, got)
		}
	}
	if _, err := inst.Invoke("initNull"); err != nil {
		t.Fatalf("initNull: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 42 {
		t.Fatalf("callAt(0) after initNull = %d, want initializer target 42", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 42 {
		t.Fatalf("callAt(2) after initNull = %d, want initializer target 42", got)
	}
}

func TestFuncrefTableInitializerExpressionNullLeavesEntriesUninitialized(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerModule(tableTestRefNullFuncExpr()))
	defer inst.Close()
	_, err := inst.Invoke("callAt", I32(0))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestFuncrefTableInitializerExpressionSurvivesCompiledCodec(t *testing.T) {
	tableTestForceExplicitBounds(t)
	c, err := Compile(nil, tableInitializerModule(tableTestRefFuncExpr(1)))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.HasTableInitFunc || loaded.TableInitFunc != 1 {
		t.Fatalf("loaded table initializer = enabled %v func %d, want enabled func 1", loaded.HasTableInitFunc, loaded.TableInitFunc)
	}
	inst, err := Instantiate(loaded)
	if err != nil {
		t.Fatalf("Instantiate loaded: %v", err)
	}
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 42 {
		t.Fatalf("callAt(0) after codec = %d, want table initializer target 42", got)
	}
}

func TestFuncrefTableInitializerExpressionRejectsWhenReferenceTypesDisabled(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	_, err := Compile(cfg, tableInitializerModule(tableTestRefFuncExpr(1)))
	if err == nil || !strings.Contains(err.Error(), "initializer expression") || !strings.Contains(err.Error(), "reference-types disabled") {
		t.Fatalf("Compile with reference-types disabled error = %v, want table initializer rejection", err)
	}
}

func TestFuncrefTableInitializerExpressionCanTargetHostImport(t *testing.T) {
	inst := tableTestInstantiateWithImports(t, tableInitializerImportModule(), Imports{
		"env.f": HostFunc(func(_ HostModule, _ []uint64, r []uint64) { r[0] = I32(55) }),
	})
	defer inst.Close()
	for _, idx := range []int32{0, 1} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 55 {
			t.Fatalf("callAt(%d) = %d, want host import result 55", idx, got)
		}
	}
}

func TestFuncrefTableInitializerExpressionCanTargetCrossInstanceImport(t *testing.T) {
	producer := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(123))))),
	)
	prodInst := tableTestInstantiate(t, producer)
	defer prodInst.Close()
	export, err := prodInst.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export f: %v", err)
	}
	consumer, err := Compile(nil, tableInitializerImportModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consInst, err := Instantiate(consumer, Imports{"env.f": export})
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consInst.Close()
	if got := tableTestCallI32(t, consInst, "callAt", I32(0)); got != 123 {
		t.Fatalf("cross-instance table initializer call = %d, want 123", got)
	}
}

func TestFuncrefTableInitializerExpressionZeroLengthTableDoesNotWrite(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerZeroLengthModule())
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "size"); got != 0 {
		t.Fatalf("initial table.size = %d, want 0", got)
	}
	_, err := inst.Invoke("callAt", I32(0))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestFuncrefTableInitializerExpressionGrowWithRefNullUsesOperand(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerGrowModule(tableTestRefNullFuncExpr()))
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) before grow = %d, want initializer target 11", got)
	}
	if got := tableTestCallI32(t, inst, "grow"); got != 1 {
		t.Fatalf("table.grow = %d, want old size 1", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("table.size after grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) after grow = %d, want initializer target 11", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestFuncrefTableInitializerExpressionGrowWithRefFuncUsesOperand(t *testing.T) {
	inst := tableTestInstantiate(t, tableInitializerGrowModule(tableTestRefFuncExpr(1)))
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) before grow = %d, want initializer target 11", got)
	}
	if got := tableTestCallI32(t, inst, "grow"); got != 1 {
		t.Fatalf("table.grow = %d, want old size 1", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("table.size after grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) after grow = %d, want initializer target 11", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 22 {
		t.Fatalf("callAt(1) after grow = %d, want grow operand target 22", got)
	}
}

func TestCompiledValidationRejectsInvalidTableInitializerFunction(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  uint32
	}{
		{name: "at function count", idx: 1},
		{name: "large uint32", idx: ^uint32(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{Code: []byte{0}, Entry: []int{0}, Funcs: []FuncSig{{}}, HasTable: true, TableSize: 1, TableMax: 1, HasTableInitFunc: true, TableInitFunc: tc.idx, FuncTypeID: []uint32{0}}
			want := fmt.Sprintf("table initializer function index %d out of range", tc.idx)
			if err := c.validate(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("validate invalid table initializer = %v, want %q", err, want)
			}
		})
	}
}

func TestTableSizeGrowGetSetAndIndirectCall(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), // 0: () -> i32
			wasmtest.FuncType(nil, nil),                      // 1: () -> ()
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x01}, []byte{0x00})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x03})), // table funcref min=1 max=3
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("set0", 0, 1),
			wasmtest.ExportEntry("call0", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),                         // target: i32.const 42
			wasmtest.Code([]byte{0x41, 0x00, 0xd2, 0x00, 0x26, 0x00, 0x0b}), // set0: table.set 0 (ref.func 0)
			wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),       // call0: call_indirect type 0 table 0
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	if _, err := inst.Invoke("set0"); err != nil {
		t.Fatalf("set0: %v", err)
	}
	res, err := inst.Invoke("call0")
	if err != nil {
		t.Fatalf("call0: %v", err)
	}
	if got := AsI32(res[0]); got != 42 {
		t.Fatalf("call0 = %d, want 42", got)
	}
}

func TestTableInitUsesOriginalElementIndexAndElemDrop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
			wasmtest.FuncType(nil, nil),                                           // 2: () -> ()
		)),
		tableTestFuncSection(0, 1, 2, 2, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // table funcref min=1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 1),
			wasmtest.ExportEntry("init", 0, 2),
			wasmtest.ExportEntry("drop", 0, 3),
			wasmtest.ExportEntry("zeroInit", 0, 4),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestDeclarativeElem(0), // element index 0 is not passive
			tableTestPassiveElem(0),     // table.init/elem.drop must still address this as index 1
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(12, 1, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(13, 1))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(0), tableTestBulk(12, 1, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	_, err := inst.Invoke("callAt", I32(0))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if _, err := inst.Invoke("init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 7 {
		t.Fatalf("callAt(0) after table.init = %d, want 7", got)
	}
	if _, err := inst.Invoke("drop"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := inst.Invoke("zeroInit"); err != nil {
		t.Fatalf("zero-length init after elem.drop: %v", err)
	}
	_, err = inst.Invoke("init")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
}

func TestTableInitCopiesPassiveSegmentAndChecksBounds(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),  // 2: (i32,i32,i32) -> ()
		)),
		tableTestFuncSection(0, 0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})), // table funcref min=3
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 2),
			wasmtest.ExportEntry("init", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(5))),
			wasmtest.Code(tableTestBody(tableTestI32Const(9))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), tableTestLocalGet(2), tableTestBulk(12, 0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("init", I32(1), I32(0), I32(2)); err != nil {
		t.Fatalf("init in-bounds: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 5 {
		t.Fatalf("callAt(1) after init = %d, want 5", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 9 {
		t.Fatalf("callAt(2) after init = %d, want 9", got)
	}
	if _, err := inst.Invoke("init", I32(3), I32(2), I32(0)); err != nil {
		t.Fatalf("zero-length init at table/segment boundary: %v", err)
	}
	_, err := inst.Invoke("init", I32(2), I32(0), I32(2))
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	_, err = inst.Invoke("init", I32(0), I32(1), I32(2))
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 5 {
		t.Fatalf("callAt(1) after trapped init = %d, want 5", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 9 {
		t.Fatalf("callAt(2) after trapped init = %d, want 9", got)
	}
}

func TestTableCopyHandlesOverlapAndBounds(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),  // 2: (i32,i32,i32) -> ()
		)),
		tableTestFuncSection(0, 0, 0, 0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x04})), // table funcref min=4
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 4),
			wasmtest.ExportEntry("copy", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(40))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), tableTestLocalGet(2), tableTestBulk(14, 0, 0))),
		)),
	)

	check := func(t *testing.T, inst *Instance, want ...int32) {
		t.Helper()
		for i, w := range want {
			if got := tableTestCallI32(t, inst, "callAt", I32(int32(i))); got != w {
				t.Fatalf("callAt(%d) = %d, want %d", i, got, w)
			}
		}
	}

	t.Run("dst below src copies forward", func(t *testing.T) {
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
		if _, err := inst.Invoke("copy", I32(0), I32(1), I32(3)); err != nil {
			t.Fatalf("copy forward: %v", err)
		}
		check(t, inst, 20, 30, 40, 40)
	})
	t.Run("dst inside source copies backward", func(t *testing.T) {
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
		if _, err := inst.Invoke("copy", I32(1), I32(0), I32(3)); err != nil {
			t.Fatalf("copy backward: %v", err)
		}
		check(t, inst, 10, 10, 20, 30)
	})
	t.Run("zero length may use one-past-end", func(t *testing.T) {
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
		if _, err := inst.Invoke("copy", I32(4), I32(0), I32(0)); err != nil {
			t.Fatalf("copy zero-length dst boundary: %v", err)
		}
		if _, err := inst.Invoke("copy", I32(0), I32(4), I32(0)); err != nil {
			t.Fatalf("copy zero-length src boundary: %v", err)
		}
		check(t, inst, 10, 20, 30, 40)
	})
	t.Run("out of bounds traps before changing table", func(t *testing.T) {
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
		_, err := inst.Invoke("copy", I32(2), I32(0), I32(3))
		tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
		check(t, inst, 10, 20, 30, 40)
	})
}

func TestTableGrowSuccessDoesNotCrash(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
		)),
		tableTestFuncSection(0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})), // table funcref min=2 max=4
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow2", 0, 1),
			wasmtest.ExportEntry("isNull", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("initial table.size = %d, want 2", got)
	}
	if got := tableTestCallI32(t, inst, "grow2"); got != 2 {
		t.Fatalf("table.grow = %d, want old size 2", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 4 {
		t.Fatalf("table.size after grow = %d, want 4", got)
	}
	for _, idx := range []int32{2, 3} {
		if got := tableTestCallI32(t, inst, "isNull", I32(idx)); got != 1 {
			t.Fatalf("isNull(%d) after table.grow null = %d, want 1", idx, got)
		}
	}
}

func TestTableGrowMinOnlyFuncrefTableToTwentyWithNull(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x0a})), // table funcref min=10, no maximum
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow10", 0, 1),
			wasmtest.ExportEntry("isNull", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(10), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "size"); got != 10 {
		t.Fatalf("initial table.size = %d, want 10", got)
	}
	if got := tableTestCallI32(t, inst, "grow10"); got != 10 {
		t.Fatalf("table.grow = %d, want old size 10", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 20 {
		t.Fatalf("table.size after grow = %d, want 20", got)
	}
	for idx := int32(0); idx < 20; idx++ {
		if got := tableTestCallI32(t, inst, "isNull", I32(idx)); got != 1 {
			t.Fatalf("isNull(%d) after table.grow null = %d, want 1", idx, got)
		}
	}
}

func TestMinOnlyFuncrefTableWithoutGrowthKeepsMinimumCapacity(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x0a})), // table funcref min=10, no maximum
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.TableMax != 10 {
		t.Fatalf("fixed-use min-only table capacity = %d, want minimum 10", compiled.TableMax)
	}
}

func TestTableGrowWithNonNullRefFuncPopulatesNewSlots(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
		)),
		tableTestFuncSection(0, 0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x04})), // table funcref min=1 max=4
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("grow2", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(77))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(2), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "size"); got != 1 {
		t.Fatalf("initial table.size = %d, want 1", got)
	}
	if got := tableTestCallI32(t, inst, "grow2"); got != 1 {
		t.Fatalf("table.grow = %d, want old size 1", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 3 {
		t.Fatalf("table.size after grow = %d, want 3", got)
	}
	for _, idx := range []int32{1, 2} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 77 {
			t.Fatalf("callAt(%d) after grow = %d, want 77", idx, got)
		}
	}
}

func TestTableGrowFailureLeavesTableUnchanged(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x03, 0x03})), // table funcref min=max=3
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow1", 0, 3),
			wasmtest.ExportEntry("callAt", 0, 4),
			wasmtest.ExportEntry("size", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(11))),
			wasmtest.Code(tableTestBody(tableTestI32Const(22))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(1), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "size"); got != 3 {
		t.Fatalf("initial table.size = %d, want 3", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) before grow = %d, want 11", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 22 {
		t.Fatalf("callAt(1) before grow = %d, want 22", got)
	}
	_, err := inst.Invoke("callAt", I32(2))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)

	if got := tableTestCallI32(t, inst, "grow1"); got != -1 {
		t.Fatalf("over-max table.grow = %d, want -1", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 3 {
		t.Fatalf("table.size after failed grow = %d, want 3", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 11 {
		t.Fatalf("callAt(0) after failed grow = %d, want 11", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 22 {
		t.Fatalf("callAt(1) after failed grow = %d, want 22", got)
	}
	_, err = inst.Invoke("callAt", I32(2))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestTableGrowOverflowDeltaLeavesTableUnchanged(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x05})), // table funcref min=2 max=5
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("growHuge", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
			wasmtest.ExportEntry("size", 0, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(33))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(-1), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "growHuge"); got != -1 {
		t.Fatalf("overflow table.grow = %d, want -1", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("table.size after overflow grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 33 {
		t.Fatalf("callAt(0) after overflow grow = %d, want 33", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestTableGrowZeroWithNonNullRefFuncDoesNotMutate(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})), // table funcref min=2 max=4
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow0", 0, 3),
			wasmtest.ExportEntry("callAt", 0, 4),
			wasmtest.ExportEntry("size", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(0), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "grow0"); got != 2 {
		t.Fatalf("zero table.grow = %d, want old size 2", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("table.size after zero grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 10 {
		t.Fatalf("callAt(0) after zero grow = %d, want 10", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 20 {
		t.Fatalf("callAt(1) after zero grow = %d, want 20", got)
	}
}

func TestTableFillZeroLengthAtBoundaryDoesNotMutate(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})), // table funcref min=2
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fillBoundary", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestRefFunc(0), tableTestI32Const(0), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("fillBoundary"); err != nil {
		t.Fatalf("zero-length table.fill at boundary: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 10 {
		t.Fatalf("callAt(0) after zero fill = %d, want 10", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 20 {
		t.Fatalf("callAt(1) after zero fill = %d, want 20", got)
	}
}

func TestTableFillSubrangeAndNullBehavior(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 0, 0, 1, 1, 2, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x05})), // table funcref min=5
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fillFunc", 0, 5),
			wasmtest.ExportEntry("fillNull", 0, 6),
			wasmtest.ExportEntry("callAt", 0, 7),
			wasmtest.ExportEntry("isNull", 0, 8),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 3, 4))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(40))),
			wasmtest.Code(tableTestBody(tableTestI32Const(50))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefFunc(4), tableTestI32Const(3), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("fillFunc"); err != nil {
		t.Fatalf("fillFunc: %v", err)
	}
	for _, idx := range []int32{1, 2, 3} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 50 {
			t.Fatalf("callAt(%d) after fillFunc = %d, want 50", idx, got)
		}
	}
	if _, err := inst.Invoke("fillNull"); err != nil {
		t.Fatalf("fillNull: %v", err)
	}
	for _, idx := range []int32{2, 3} {
		if got := tableTestCallI32(t, inst, "isNull", I32(idx)); got != 1 {
			t.Fatalf("isNull(%d) after fillNull = %d, want 1", idx, got)
		}
		_, err := inst.Invoke("callAt", I32(idx))
		tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 10 {
		t.Fatalf("callAt(0) after fillNull = %d, want 10", got)
	}
	for _, idx := range []int32{1, 4} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 50 {
			t.Fatalf("callAt(%d) after fillNull = %d, want 50", idx, got)
		}
	}
}

func TestTableInitCopiesNullElementExpressions(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})), // table funcref min=3
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 1),
			wasmtest.ExportEntry("initNulls", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0, 0, 0),
			tableTestPassiveElemExpr(tableTestRefNullFuncExpr(), tableTestRefNullFuncExpr()),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(99))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestI32Const(0), tableTestI32Const(2), tableTestBulk(12, 1, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	for _, idx := range []int32{0, 1, 2} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 99 {
			t.Fatalf("callAt(%d) before initNulls = %d, want 99", idx, got)
		}
	}
	if _, err := inst.Invoke("initNulls"); err != nil {
		t.Fatalf("initNulls: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 99 {
		t.Fatalf("callAt(0) after initNulls = %d, want 99", got)
	}
	for _, idx := range []int32{1, 2} {
		_, err := inst.Invoke("callAt", I32(idx))
		tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	}
}

func TestTableCopyCopiesNullEntries(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 0, 0, 0, 1, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x06})), // table funcref min=6
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("setNull1", 0, 6),
			wasmtest.ExportEntry("copy", 0, 7),
			wasmtest.ExportEntry("callAt", 0, 8),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 3, 4, 5))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(40))),
			wasmtest.Code(tableTestBody(tableTestI32Const(50))),
			wasmtest.Code(tableTestBody(tableTestI32Const(60))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(3), tableTestI32Const(0), tableTestI32Const(3), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("setNull1"); err != nil {
		t.Fatalf("setNull1: %v", err)
	}
	if _, err := inst.Invoke("copy"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	checks := map[int32]int32{0: 10, 2: 30, 3: 10, 5: 30}
	for idx, want := range checks {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != want {
			t.Fatalf("callAt(%d) after copy = %d, want %d", idx, got, want)
		}
	}
	for _, idx := range []int32{1, 4} {
		_, err := inst.Invoke("callAt", I32(idx))
		tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	}
}

func TestTableGrowCapacitySurvivesCompiledCodec(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})), // table funcref min=2 max=4
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow2", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.TableMax != 4 {
		t.Fatalf("loaded TableMax = %d, want 4", loaded.TableMax)
	}
	inst, err := Instantiate(loaded)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "grow2"); got != 2 {
		t.Fatalf("table.grow after compiled codec = %d, want old size 2", got)
	}
}

func TestMultipleLocalTableExportsResolveByName(t *testing.T) {
	producerModule := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 0x01, 0x01},
			[]byte{0x70, 0x01, 0x01, 0x01},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("first", 1, 0),
			wasmtest.ExportEntry("second", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0),
			tableTestActiveElemAt(1, 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(11))),
			wasmtest.Code(tableTestBody(tableTestI32Const(22))),
		)),
	)
	producerCompiled, err := Compile(nil, producerModule)
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := Instantiate(producerCompiled)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}

	first, err := producer.ExportedTable("first")
	if err != nil {
		t.Fatalf("export first: %v", err)
	}
	second, err := producer.ExportedTable("second")
	if err != nil {
		t.Fatalf("export second: %v", err)
	}
	if first == second {
		t.Fatal("distinct table exports returned the same handle")
	}
	secondAgain, err := producer.ExportedTable("second")
	if err != nil {
		t.Fatalf("export second again: %v", err)
	}
	if secondAgain != second {
		t.Fatal("repeated table export did not reuse its ownership handle")
	}
	if _, err := producer.ExportedTable("missing"); err == nil || !strings.Contains(err.Error(), "no exported table") {
		t.Fatalf("missing table export error = %v, want exact-name rejection", err)
	}

	consumerModule := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestCallIndirect(0, 0))),
		)),
	)
	consumerCompiled, err := Compile(nil, consumerModule)
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	for _, tc := range []struct {
		name  string
		table *Table
		want  int32
	}{
		{name: "first", table: first, want: 11},
		{name: "second", table: second, want: 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumer, err := Instantiate(consumerCompiled, Imports{"env.t": tc.table})
			if err != nil {
				t.Fatalf("Instantiate consumer: %v", err)
			}
			if got := tableTestCallI32(t, consumer, "call"); got != tc.want {
				t.Fatalf("call through %s table = %d, want %d", tc.name, got, tc.want)
			}
			if err := consumer.Close(); err != nil {
				t.Fatalf("close consumer: %v", err)
			}
		})
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close producer after consumers: %v", err)
	}
}

func TestImportedThenLocalFuncrefTablesExecuteAndExportExactly(t *testing.T) {
	ownerCompiled := MustCompile(watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(table $first (export "first") 1 1 funcref)
		(table $shared (export "shared") 2 2 funcref)
		(func $owner (type $ret) (i32.const 17))
		(elem (table $shared) (i32.const 0) func $owner))`))
	owner, err := Instantiate(ownerCompiled)
	if err != nil {
		t.Fatalf("Instantiate owner: %v", err)
	}
	first, err := owner.ExportedTable("first")
	if err != nil {
		t.Fatalf("export owner table 0: %v", err)
	}
	shared, err := owner.ExportedTable("shared")
	if err != nil {
		t.Fatalf("export owner table 1: %v", err)
	}

	consumerCompiled := MustCompile(watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(import "owner" "shared" (table $imported 2 2 funcref))
		(table $local 2 4 funcref)
		(export "shared" (table $imported))
		(export "local" (table $local))
		(func $imported-value (type $ret) (i32.const 31))
		(func $local-value (type $ret) (i32.const 42))
		(elem (table $imported) (i32.const 1) func $imported-value)
		(elem (table $local) (i32.const 0) func $local-value)
		(func (export "call-imported") (param i32) (result i32)
			(local.get 0) (call_indirect $imported (type $ret)))
		(func (export "call-local") (param i32) (result i32)
			(local.get 0) (call_indirect $local (type $ret)))
		(func (export "copy-owner-to-local")
			(i32.const 1) (i32.const 0) (i32.const 1)
			(table.copy $local $imported))
		(func (export "local-size") (result i32) (table.size $local))
		(func (export "grow-local") (result i32)
			(ref.null func) (i32.const 1) (table.grow $local)))`))
	if _, err := consumerCompiled.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "not serializable") {
		t.Fatalf("MarshalBinary imported+local metadata error = %v, want codec-v19 rejection", err)
	}
	tooSmall, err := NewTable(1, 2)
	if err != nil {
		t.Fatalf("NewTable tooSmall: %v", err)
	}
	if _, err := Instantiate(consumerCompiled, Imports{"owner.shared": tooSmall}); err == nil || !strings.Contains(err.Error(), "required minimum") {
		t.Fatalf("Instantiate with too-small imported table = %v, want limit mismatch", err)
	}
	if err := tooSmall.Close(); err != nil {
		t.Fatalf("close too-small table: %v", err)
	}

	consumer, err := Instantiate(consumerCompiled, Imports{"owner.shared": shared})
	if err != nil {
		t.Fatalf("Instantiate imported+local consumer: %v", err)
	}
	if got := tableTestCallI32(t, consumer, "call-imported", I32(0)); got != 17 {
		t.Fatalf("call-imported(0) = %d, want owner value 17", got)
	}
	if got := tableTestCallI32(t, consumer, "call-imported", I32(1)); got != 31 {
		t.Fatalf("call-imported(1) = %d, want consumer value 31", got)
	}
	if got := tableTestCallI32(t, consumer, "call-local", I32(0)); got != 42 {
		t.Fatalf("call-local(0) = %d, want local value 42", got)
	}
	if _, err := consumer.Invoke("copy-owner-to-local"); err != nil {
		t.Fatalf("cross-table copy: %v", err)
	}
	if got := tableTestCallI32(t, consumer, "call-local", I32(1)); got != 17 {
		t.Fatalf("call-local(1) after copy = %d, want owner value 17", got)
	}
	if got := tableTestCallI32(t, consumer, "local-size"); got != 2 {
		t.Fatalf("local table size = %d, want 2", got)
	}
	if got := tableTestCallI32(t, consumer, "grow-local"); got != 2 {
		t.Fatalf("local table grow = %d, want old size 2", got)
	}
	if got := tableTestCallI32(t, consumer, "local-size"); got != 3 {
		t.Fatalf("local table size after grow = %d, want 3", got)
	}

	reexported, err := consumer.ExportedTable("shared")
	if err != nil {
		t.Fatalf("re-export imported table: %v", err)
	}
	if reexported != shared {
		t.Fatal("imported table re-export did not preserve the original handle")
	}
	local, err := consumer.ExportedTable("local")
	if err != nil {
		t.Fatalf("export local table 1: %v", err)
	}
	if local == shared {
		t.Fatal("local table export aliases imported table 0")
	}
	if again, err := consumer.ExportedTable("local"); err != nil || again != local {
		t.Fatalf("repeated local table export = %p, %v; want stable %p", again, err, local)
	}
	if _, err := consumer.ExportedTable("missing"); err == nil || !strings.Contains(err.Error(), "no exported table") {
		t.Fatalf("missing table export error = %v, want exact-name rejection", err)
	}

	downstreamCompiled := MustCompile(watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(import "consumer" "local" (table $table 2 4 funcref))
		(func (export "call") (param i32) (result i32)
			(local.get 0) (call_indirect $table (type $ret))))`))
	downstream, err := Instantiate(downstreamCompiled, Imports{"consumer.local": local})
	if err != nil {
		t.Fatalf("Instantiate downstream: %v", err)
	}
	if got := tableTestCallI32(t, downstream, "call", I32(0)); got != 42 {
		t.Fatalf("downstream call(0) = %d, want 42", got)
	}
	if got := tableTestCallI32(t, downstream, "call", I32(1)); got != 17 {
		t.Fatalf("downstream call(1) = %d, want 17", got)
	}
	if err := downstream.Close(); err != nil {
		t.Fatalf("close downstream: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close consumer before owner: %v", err)
	}
	first.mu.Lock()
	firstClosed := first.closed
	first.mu.Unlock()
	shared.mu.Lock()
	sharedClosed := shared.closed
	shared.mu.Unlock()
	if firstClosed || sharedClosed {
		t.Fatalf("consumer close released owner table chain: first closed=%v shared closed=%v", firstClosed, sharedClosed)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("close owner after consumers: %v", err)
	}
}

func TestMultipleImportedFuncrefTablesExecuteAndExportExactly(t *testing.T) {
	newOwner := func(name string, value int32) (*Instance, *Table) {
		t.Helper()
		compiled, err := Compile(nil, watToWasmCA(t, fmt.Sprintf(`(module
			(type $ret (func (result i32)))
			(table (export %q) 2 2 funcref)
			(func $value (type $ret) (i32.const %d))
			(elem (i32.const 0) func $value))`, name, value)))
		if err != nil {
			t.Fatalf("Compile owner %s: %v", name, err)
		}
		owner, err := Instantiate(compiled)
		if err != nil {
			t.Fatalf("Instantiate owner %s: %v", name, err)
		}
		table, err := owner.ExportedTable(name)
		if err != nil {
			t.Fatalf("export owner table %s: %v", name, err)
		}
		return owner, table
	}

	ownerA, tableA := newOwner("table", 11)
	ownerB, tableB := newOwner("table", 22)
	consumerWasm := watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(import "a" "table" (table $t0 2 2 funcref))
		(import "b" "table" (table $t1 2 2 funcref))
		(table $t2 2 4 funcref)
		(table $t3 2 2 funcref)
		(export "t0" (table $t0))
		(export "t1" (table $t1))
		(export "t2" (table $t2))
		(export "t3" (table $t3))
		(func $f2 (type $ret) (i32.const 33))
		(func $f3 (type $ret) (i32.const 44))
		(elem (table $t0) (i32.const 1) func $f2)
		(elem (table $t1) (i32.const 1) func $f3)
		(elem (table $t2) (i32.const 0) func $f3)
		(elem (table $t3) (i32.const 0) func $f2)
		(func (export "call0") (param i32) (result i32)
			(local.get 0) (call_indirect $t0 (type $ret)))
		(func (export "call1") (param i32) (result i32)
			(local.get 0) (call_indirect $t1 (type $ret)))
		(func (export "call2") (param i32) (result i32)
			(local.get 0) (call_indirect $t2 (type $ret)))
		(func (export "call3") (param i32) (result i32)
			(local.get 0) (call_indirect $t3 (type $ret)))
		(func (export "copy0to2")
			(i32.const 1) (i32.const 0) (i32.const 1)
			(table.copy $t2 $t0))
		(func (export "grow2") (result i32)
			(ref.null func) (i32.const 1) (table.grow $t2)))`)
	consumerCompiled, err := Compile(nil, consumerWasm)
	if err != nil {
		t.Fatalf("Compile multiple imported tables: %v", err)
	}
	if key, ok := consumerCompiled.TableImport(); ok || key != "" {
		t.Fatalf("legacy TableImport = %q, %v; want false for multiple imports", key, ok)
	}
	keys := consumerCompiled.TableImports()
	if len(keys) != 2 || keys[0] != "a.table" || keys[1] != "b.table" {
		t.Fatalf("TableImports = %v, want [a.table b.table]", keys)
	}
	if consumerCompiled.tableImportCount() != 2 || len(consumerCompiled.extraTables) != 3 {
		t.Fatalf("indexed metadata imports/extra = %d/%d, want 2/3", consumerCompiled.tableImportCount(), len(consumerCompiled.extraTables))
	}
	rt := NewRuntime()
	module, err := rt.Compile(consumerWasm)
	if err != nil {
		t.Fatalf("Runtime.Compile multiple imported tables: %v", err)
	}
	moduleImports := module.Imports()
	if len(moduleImports) != 2 || moduleImports[0].Kind != ImportTable || moduleImports[0].Key() != "a.table" || moduleImports[1].Kind != ImportTable || moduleImports[1].Key() != "b.table" {
		t.Fatalf("Module.Imports = %#v, want ordered table imports a.table/b.table", moduleImports)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("close metadata runtime: %v", err)
	}
	if _, err := consumerCompiled.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "indexed table import metadata") {
		t.Fatalf("MarshalBinary multiple imported tables = %v, want codec-v19 rejection", err)
	}
	consumer, err := Instantiate(consumerCompiled, Imports{"a.table": tableA, "b.table": tableB})
	if err != nil {
		t.Fatalf("Instantiate multiple imported tables: %v", err)
	}
	for _, tc := range []struct {
		export string
		index  uint32
		want   int32
	}{
		{export: "call0", index: 0, want: 11},
		{export: "call0", index: 1, want: 33},
		{export: "call1", index: 0, want: 22},
		{export: "call1", index: 1, want: 44},
		{export: "call2", index: 0, want: 44},
		{export: "call3", index: 0, want: 33},
	} {
		if got := tableTestCallI32(t, consumer, tc.export, I32(int32(tc.index))); got != tc.want {
			t.Fatalf("%s(%d) = %d, want %d", tc.export, tc.index, got, tc.want)
		}
	}
	if _, err := consumer.Invoke("copy0to2"); err != nil {
		t.Fatalf("copy imported table 0 to local table 2: %v", err)
	}
	if got := tableTestCallI32(t, consumer, "call2", I32(1)); got != 11 {
		t.Fatalf("call2(1) after copy = %d, want 11", got)
	}
	if got := tableTestCallI32(t, consumer, "grow2"); got != 2 {
		t.Fatalf("grow2 = %d, want old size 2", got)
	}

	reexportA, err := consumer.ExportedTable("t0")
	if err != nil || reexportA != tableA {
		t.Fatalf("re-export table 0 = %p, %v; want %p", reexportA, err, tableA)
	}
	reexportB, err := consumer.ExportedTable("t1")
	if err != nil || reexportB != tableB {
		t.Fatalf("re-export table 1 = %p, %v; want %p", reexportB, err, tableB)
	}
	local2, err := consumer.ExportedTable("t2")
	if err != nil || local2 == tableA || local2 == tableB {
		t.Fatalf("export local table 2 = %p, %v; want distinct local handle", local2, err)
	}
	local3, err := consumer.ExportedTable("t3")
	if err != nil || local3 == tableA || local3 == tableB || local3 == local2 {
		t.Fatalf("export local table 3 = %p, %v; want distinct local handle", local3, err)
	}

	downstreamCompiled, err := Compile(nil, watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(import "consumer" "t1" (table $table 2 2 funcref))
		(func (export "call") (param i32) (result i32)
			(local.get 0) (call_indirect $table (type $ret))))`))
	if err != nil {
		t.Fatalf("Compile downstream: %v", err)
	}
	downstream, err := Instantiate(downstreamCompiled, Imports{"consumer.t1": reexportB})
	if err != nil {
		t.Fatalf("Instantiate downstream: %v", err)
	}
	if got := tableTestCallI32(t, downstream, "call", I32(0)); got != 22 {
		t.Fatalf("downstream call(0) = %d, want 22", got)
	}
	if err := downstream.Close(); err != nil {
		t.Fatalf("close downstream: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close consumer: %v", err)
	}
	for name, table := range map[string]*Table{"a": tableA, "b": tableB} {
		table.mu.Lock()
		closed := table.closed
		table.mu.Unlock()
		if closed {
			t.Fatalf("consumer close released owner %s table", name)
		}
	}
	if err := ownerA.Close(); err != nil {
		t.Fatalf("close owner A: %v", err)
	}
	if err := ownerB.Close(); err != nil {
		t.Fatalf("close owner B: %v", err)
	}
}

func TestMultipleImportedFuncrefTablesMayAliasOneHandle(t *testing.T) {
	shared, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer shared.Close()
	compiled, err := Compile(nil, watToWasmCA(t, `(module
		(type $ret (func (result i32)))
		(import "env" "shared" (table $zero 1 1 funcref))
		(import "env" "shared" (table $one 1 1 funcref))
		(export "zero" (table $zero))
		(export "one" (table $one))
		(func $f (type $ret) (i32.const 77))
		(elem (table $one) (i32.const 0) func $f)
		(func (export "call-zero") (result i32)
			(i32.const 0) (call_indirect $zero (type $ret))))`))
	if err != nil {
		t.Fatalf("Compile duplicate table imports: %v", err)
	}
	in, err := Instantiate(compiled, Imports{"env.shared": shared})
	if err != nil {
		t.Fatalf("Instantiate duplicate table imports: %v", err)
	}
	defer in.Close()
	if got := tableTestCallI32(t, in, "call-zero"); got != 77 {
		t.Fatalf("call-zero = %d, want 77 from aliased table 1 write", got)
	}
	zero, err := in.ExportedTable("zero")
	if err != nil {
		t.Fatalf("export zero: %v", err)
	}
	one, err := in.ExportedTable("one")
	if err != nil {
		t.Fatalf("export one: %v", err)
	}
	if zero != shared || one != shared {
		t.Fatalf("aliased re-exports = %p/%p, want shared %p", zero, one, shared)
	}
}

func TestMultipleImportedTablesCheckEveryLimit(t *testing.T) {
	compiled, err := Compile(nil, watToWasmCA(t, `(module
		(import "a" "table" (table 1 2 funcref))
		(import "b" "table" (table 2 3 funcref)))`))
	if err != nil {
		t.Fatalf("Compile multiple imported table limits: %v", err)
	}
	a, err := NewTable(1, 2)
	if err != nil {
		t.Fatalf("NewTable a: %v", err)
	}
	defer a.Close()
	for _, tc := range []struct {
		name string
		min  uint32
		max  uint32
		want string
	}{
		{name: "minimum", min: 1, max: 3, want: "required minimum"},
		{name: "maximum", min: 2, max: 4, want: "required maximum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewTable(tc.min, tc.max)
			if err != nil {
				t.Fatalf("NewTable b: %v", err)
			}
			defer b.Close()
			if _, err := Instantiate(compiled, Imports{"a.table": a, "b.table": b}); err == nil || !strings.Contains(err.Error(), "b.table") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Instantiate limit mismatch = %v, want b.table %q", err, tc.want)
			}
		})
	}
}

func TestImportedThenLocalTablesRejectSharedMemoryBasedataAlias(t *testing.T) {
	tableTestForceExplicitBounds(t)
	memoryOwner := MustCompile(watToWasmCA(t, `(module
		(memory (export "memory") 1))`))
	owner, err := Instantiate(memoryOwner)
	if err != nil {
		t.Fatalf("Instantiate memory owner: %v", err)
	}
	defer owner.Close()
	memory, err := owner.ExportedMemory("memory")
	if err != nil {
		t.Fatalf("export memory: %v", err)
	}
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer table.Close()

	compiled := MustCompile(watToWasmCA(t, `(module
		(import "owner" "memory" (memory 1))
		(import "owner" "table" (table 1 1 funcref))
		(table 1 1 funcref))`))
	if _, err := Instantiate(compiled, Imports{"owner.memory": memory, "owner.table": table}); err == nil || !strings.Contains(err.Error(), "may not declare its own globals, table, or data-segment state") {
		t.Fatalf("Instantiate shared-memory imported+local tables = %v, want basedata ownership rejection", err)
	}

	second, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable second: %v", err)
	}
	defer second.Close()
	multipleImports, err := Compile(nil, watToWasmCA(t, `(module
		(import "owner" "memory" (memory 1))
		(import "owner" "a" (table 1 1 funcref))
		(import "owner" "b" (table 1 1 funcref)))`))
	if err != nil {
		t.Fatalf("Compile shared-memory multiple imported tables: %v", err)
	}
	if _, err := Instantiate(multipleImports, Imports{"owner.memory": memory, "owner.a": table, "owner.b": second}); err == nil || !strings.Contains(err.Error(), "may not declare its own globals, table, or data-segment state") {
		t.Fatalf("Instantiate shared-memory multiple imported tables = %v, want basedata ownership rejection", err)
	}
}

func TestImportedThenLocalFailedInstantiationRetainsSharedTableWrites(t *testing.T) {
	ownerCompiled := MustCompile(watToWasmCA(t, `(module
		(table $shared (export "shared") 1 1 funcref))`))
	owner, err := Instantiate(ownerCompiled)
	if err != nil {
		t.Fatalf("Instantiate owner: %v", err)
	}
	shared, err := owner.ExportedTable("shared")
	if err != nil {
		t.Fatalf("export shared table: %v", err)
	}

	failedCompiled := MustCompile(watToWasmCA(t, `(module
		(import "owner" "shared" (table $imported 1 1 funcref))
		(table $local 1 1 funcref)
		(func $f (result i32) (i32.const 909))
		(elem (table $imported) (i32.const 0) func $f)
		(elem (table $local) (i32.const 1) func $f))`))
	if in, err := Instantiate(failedCompiled, Imports{"owner.shared": shared}); err == nil || in != nil || !strings.Contains(err.Error(), "table 1") {
		t.Fatalf("failed imported+local instantiate = %v, %v; want local table-1 bounds failure", in, err)
	}
	shared.mu.Lock()
	if got := len(shared.retained); got != 1 {
		shared.mu.Unlock()
		t.Fatalf("retained failed-instance roots = %d, want 1", got)
	}
	var root *Instance
	for retained := range shared.retained {
		root = retained
	}
	shared.mu.Unlock()
	if root == nil {
		t.Fatal("shared table retained no failed instance")
	}
	root.lifeMu.Lock()
	closedBeforeOwner := root.resourcesClosed
	root.lifeMu.Unlock()
	if closedBeforeOwner {
		t.Fatal("failed instance resources closed while its funcref remained shared")
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("close owner: %v", err)
	}
	root.lifeMu.Lock()
	closedAfterOwner := root.resourcesClosed
	root.lifeMu.Unlock()
	if !closedAfterOwner {
		t.Fatal("closing the shared-table owner did not release the failed instance root")
	}
}

func TestMultipleImportedTablesRetainFailedInstancesAcrossEveryHandle(t *testing.T) {
	first, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable first: %v", err)
	}
	second, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable second: %v", err)
	}
	failed, err := Compile(nil, watToWasmCA(t, `(module
		(import "env" "first" (table $first 1 1 funcref))
		(import "env" "second" (table $second 1 1 funcref))
		(table $local 1 1 funcref)
		(func $f (result i32) (i32.const 909))
		(elem (table $first) (i32.const 0) func $f)
		(elem (table $second) (i32.const 0) func $f)
		(elem (table $local) (i32.const 1) func $f))`))
	if err != nil {
		t.Fatalf("Compile failed-instantiation fixture: %v", err)
	}
	if in, err := Instantiate(failed, Imports{"env.first": first, "env.second": second}); err == nil || in != nil || !strings.Contains(err.Error(), "table 2") {
		t.Fatalf("Instantiate failed fixture = %v, %v; want local table-2 bounds failure", in, err)
	}
	first.mu.Lock()
	var root *Instance
	for retained := range first.retained {
		root = retained
	}
	firstCount := len(first.retained)
	first.mu.Unlock()
	second.mu.Lock()
	_, secondHasRoot := second.retained[root]
	secondCount := len(second.retained)
	second.mu.Unlock()
	if root == nil || firstCount != 1 || secondCount != 1 || !secondHasRoot {
		t.Fatalf("retained roots first/second = %d/%d same=%v, want one shared failed root", firstCount, secondCount, secondHasRoot)
	}
	root.lifeMu.Lock()
	refs := root.resourceRefs
	root.lifeMu.Unlock()
	if refs != 2 {
		t.Fatalf("failed root resource refs = %d, want 2 distinct imported-table owners", refs)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	root.lifeMu.Lock()
	closedAfterFirst := root.resourcesClosed
	root.lifeMu.Unlock()
	if closedAfterFirst {
		t.Fatal("first imported table close released root still retained by second")
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}
	root.lifeMu.Lock()
	closedAfterSecond := root.resourcesClosed
	root.lifeMu.Unlock()
	if !closedAfterSecond {
		t.Fatal("last imported table close did not release failed root")
	}

	alias, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable alias: %v", err)
	}
	aliased, err := Compile(nil, watToWasmCA(t, `(module
		(import "env" "same" (table $zero 1 1 funcref))
		(import "env" "same" (table $one 1 1 funcref))
		(table $local 1 1 funcref)
		(func $f)
		(elem (table $zero) (i32.const 0) func $f)
		(elem (table $one) (i32.const 0) func $f)
		(elem (table $local) (i32.const 1) func $f))`))
	if err != nil {
		t.Fatalf("Compile aliased failed fixture: %v", err)
	}
	if in, err := Instantiate(aliased, Imports{"env.same": alias}); err == nil || in != nil {
		t.Fatalf("Instantiate aliased failed fixture = %v, %v; want failure", in, err)
	}
	alias.mu.Lock()
	var aliasRoot *Instance
	for retained := range alias.retained {
		aliasRoot = retained
	}
	aliasCount := len(alias.retained)
	alias.mu.Unlock()
	if aliasRoot == nil || aliasCount != 1 {
		t.Fatalf("aliased retained roots = %d, want 1", aliasCount)
	}
	aliasRoot.lifeMu.Lock()
	aliasRefs := aliasRoot.resourceRefs
	aliasRoot.lifeMu.Unlock()
	if aliasRefs != 1 {
		t.Fatalf("aliased failed root resource refs = %d, want 1", aliasRefs)
	}
	if err := alias.Close(); err != nil {
		t.Fatalf("close alias: %v", err)
	}
}

func TestMinOnlyTableExportCapacityIsPerTable(t *testing.T) {
	compile := func(t *testing.T, exported uint32) *Compiled {
		t.Helper()
		mod := wasmtest.Module(
			wasmtest.Section(4, wasmtest.Vec(
				[]byte{0x70, 0x00, 0x00},
				[]byte{0x70, 0x00, 0x00},
			)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, exported))),
		)
		compiled, err := Compile(nil, mod)
		if err != nil {
			t.Fatalf("Compile table %d export: %v", exported, err)
		}
		return compiled
	}

	for _, tc := range []struct {
		exported uint32
		want     [2]int
	}{
		{exported: 0, want: [2]int{64, 0}},
		{exported: 1, want: [2]int{0, 64}},
	} {
		compiled := compile(t, tc.exported)
		got := [2]int{compiled.tableDef(0).Max, compiled.tableDef(1).Max}
		if got != tc.want {
			t.Fatalf("export table %d capacities = %v, want %v", tc.exported, got, tc.want)
		}
	}
}

func TestCompiledCodecRejectsUnencodedTableExportNames(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := compiled.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "table export metadata") {
		t.Fatalf("MarshalBinary error = %v, want table export metadata rejection", err)
	}
}

func TestCompiledCodecPreservesTableImport(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 4))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if key, ok := loaded.TableImport(); !ok || key != "env.t" {
		t.Fatalf("loaded TableImport = %q, %v; want env.t, true", key, ok)
	}
	if _, err := Instantiate(loaded); err == nil {
		t.Fatal("Instantiate without imported table succeeded")
	}
	tbl, err := NewTable(2, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()
	inst, err := Instantiate(loaded, Imports{"env.t": tbl})
	if err != nil {
		t.Fatalf("Instantiate with imported table: %v", err)
	}
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "size"); got != 2 {
		t.Fatalf("table.size with imported table after codec = %d, want 2", got)
	}
}

func TestImportedTableLimitsCheckedAtInstantiate(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 4))),
		tableTestFuncSection(0),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody()))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	tooSmall, err := NewTable(1, 4)
	if err != nil {
		t.Fatalf("NewTable tooSmall: %v", err)
	}
	defer tooSmall.Close()
	if _, err := Instantiate(c, Imports{"env.t": tooSmall}); err == nil {
		t.Fatal("Instantiate accepted imported table below declared minimum")
	}
	tooLargeMax, err := NewTable(2, 5)
	if err != nil {
		t.Fatalf("NewTable tooLargeMax: %v", err)
	}
	defer tooLargeMax.Close()
	if _, err := Instantiate(c, Imports{"env.t": tooLargeMax}); err == nil {
		t.Fatal("Instantiate accepted imported table above declared maximum")
	}
}

func TestImportedTableInstantiateUsesGrownDescriptorLength(t *testing.T) {
	tbl, err := NewTable(1, 3)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()
	growMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 3))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("t", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))),
		)),
	)
	growInst := tableTestInstantiateWithImports(t, growMod, Imports{"env.t": tbl})
	defer growInst.Close()
	if got := tableTestCallI32(t, growInst, "grow"); got != 1 {
		t.Fatalf("imported table.grow = %d, want old size 1", got)
	}
	reexported, err := growInst.ExportedTable("t")
	if err != nil {
		t.Fatalf("re-export imported table: %v", err)
	}

	initMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 3))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(2, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(77))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	initInst := tableTestInstantiateWithImports(t, initMod, Imports{"env.t": reexported})
	defer initInst.Close()
	if got := tableTestCallI32(t, initInst, "callAt", I32(2)); got != 77 {
		t.Fatalf("callAt(2) after active elem into grown imported table = %d, want 77", got)
	}
}

func TestTableGrowImportedTableVisibleToAnotherInstance(t *testing.T) {
	tbl, err := NewTable(1, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	growMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 4))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow2", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))))),
	)
	sizeMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 4))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	growInst := tableTestInstantiateWithImports(t, growMod, Imports{"env.t": tbl})
	defer growInst.Close()
	sizeInst := tableTestInstantiateWithImports(t, sizeMod, Imports{"env.t": tbl})
	defer sizeInst.Close()

	if got := tableTestCallI32(t, sizeInst, "size"); got != 1 {
		t.Fatalf("observer initial table.size = %d, want 1", got)
	}
	if got := tableTestCallI32(t, growInst, "grow2"); got != 1 {
		t.Fatalf("imported table.grow = %d, want old size 1", got)
	}
	if got := tableTestCallI32(t, sizeInst, "size"); got != 3 {
		t.Fatalf("observer table.size after grow = %d, want 3", got)
	}
	if got := tbl.Size(); got != 3 {
		t.Fatalf("host Table.Size after grow = %d, want 3", got)
	}
}

func TestImportedTableGetSetPreservesProducerFuncref(t *testing.T) {
	tbl, err := NewTable(2, 2)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	writerA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
		tableTestFuncSection(0),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(123))))),
	)
	instA := tableTestInstantiateWithImports(t, writerA, Imports{"env.t": tbl})
	defer instA.Close()

	copierB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy0to1", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(456))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestI32Const(0), []byte{0x25, 0x00}, []byte{0x26, 0x00})),
		)),
	)
	instB := tableTestInstantiateWithImports(t, copierB, Imports{"env.t": tbl})
	defer instB.Close()
	if _, err := instB.Invoke("copy0to1"); err != nil {
		t.Fatalf("copy0to1: %v", err)
	}

	observer := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)
	obs := tableTestInstantiateWithImports(t, observer, Imports{"env.t": tbl})
	defer obs.Close()
	if got := tableTestCallI32(t, obs, "callAt", I32(1)); got != 123 {
		t.Fatalf("callAt(1) after table.get/table.set copy = %d, want producer value 123", got)
	}
}

func TestTableSetImportedTableVisibleToAnotherInstance(t *testing.T) {
	tbl, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	setterMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set0", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(123))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(0), []byte{0x26, 0x00})),
		)),
	)
	callerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)
	setter := tableTestInstantiateWithImports(t, setterMod, Imports{"env.t": tbl})
	defer setter.Close()
	caller := tableTestInstantiateWithImports(t, callerMod, Imports{"env.t": tbl})
	defer caller.Close()

	_, err = caller.Invoke("callAt", I32(0))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if _, err := setter.Invoke("set0"); err != nil {
		t.Fatalf("set0: %v", err)
	}
	if got := tableTestCallI32(t, caller, "callAt", I32(0)); got != 123 {
		t.Fatalf("callAt(0) after cross-instance table.set = %d, want 123", got)
	}
}

func TestTableFillAndCopyImportedTableVisibleToAnotherInstance(t *testing.T) {
	tbl, err := NewTable(4, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 4, 4))),
		tableTestFuncSection(0, 0, 0, 1, 1, 1, 0, 2, 2),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fill20", 0, 3),
			wasmtest.ExportEntry("setNull1", 0, 4),
			wasmtest.ExportEntry("copy0To2", 0, 5),
			wasmtest.ExportEntry("size", 0, 6),
			wasmtest.ExportEntry("isNull", 0, 7),
			wasmtest.ExportEntry("callAt", 0, 8),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(1), tableTestI32Const(2), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestI32Const(0), tableTestI32Const(2), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	mutator := tableTestInstantiateWithImports(t, mod, Imports{"env.t": tbl})
	defer mutator.Close()
	observer := tableTestInstantiateWithImports(t, mod, Imports{"env.t": tbl})
	defer observer.Close()

	if got := tableTestCallI32(t, observer, "size"); got != 4 {
		t.Fatalf("observer table.size = %d, want 4", got)
	}
	if _, err := mutator.Invoke("fill20"); err != nil {
		t.Fatalf("fill20: %v", err)
	}
	for _, idx := range []int32{0, 1} {
		if got := tableTestCallI32(t, observer, "callAt", I32(idx)); got != 20 {
			t.Fatalf("observer callAt(%d) after fill = %d, want 20", idx, got)
		}
	}
	if _, err := mutator.Invoke("setNull1"); err != nil {
		t.Fatalf("setNull1: %v", err)
	}
	if _, err := mutator.Invoke("copy0To2"); err != nil {
		t.Fatalf("copy0To2: %v", err)
	}
	for _, idx := range []int32{0, 2} {
		if got := tableTestCallI32(t, observer, "callAt", I32(idx)); got != 20 {
			t.Fatalf("observer callAt(%d) after copy = %d, want 20", idx, got)
		}
	}
	for _, idx := range []int32{1, 3} {
		if got := tableTestCallI32(t, observer, "isNull", I32(idx)); got != 1 {
			t.Fatalf("observer isNull(%d) after copy = %d, want 1", idx, got)
		}
		_, err := observer.Invoke("callAt", I32(idx))
		tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	}
}

func TestCompiledCodecPreservesMinOnlyTableImportAndAcceptsLargerHostTable(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 0))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if key, ok := loaded.TableImport(); !ok || key != "env.t" {
		t.Fatalf("loaded TableImport = %q, %v; want env.t, true", key, ok)
	}
	if loaded.tableImportMin != 2 || loaded.tableImportHasMax || loaded.tableImportMax != 0 {
		t.Fatalf("loaded table import limits = min %d max %d hasMax %v, want min 2 no max", loaded.tableImportMin, loaded.tableImportMax, loaded.tableImportHasMax)
	}
	tbl, err := NewTable(3, 5)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()
	inst, err := Instantiate(loaded, Imports{"env.t": tbl})
	if err != nil {
		t.Fatalf("Instantiate with larger host table and min-only import: %v", err)
	}
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "size"); got != 3 {
		t.Fatalf("table.size with larger host table = %d, want 3", got)
	}
}

func TestTableFillTrapWithMixedContentsIsAtomic(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 1, 1, 2, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("setNull1", 0, 3),
			wasmtest.ExportEntry("fillOOB", 0, 4),
			wasmtest.ExportEntry("callAt", 0, 5),
			wasmtest.ExportEntry("isNull", 0, 6),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestRefFunc(0), tableTestI32Const(3), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()
	if _, err := inst.Invoke("setNull1"); err != nil {
		t.Fatalf("setNull1: %v", err)
	}

	_, err := inst.Invoke("fillOOB")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	wantCalls := map[int32]int32{0: 10, 2: 30, 3: 20}
	for idx, want := range wantCalls {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != want {
			t.Fatalf("callAt(%d) after trapped fill = %d, want %d", idx, got, want)
		}
	}
	if got := tableTestCallI32(t, inst, "isNull", I32(1)); got != 1 {
		t.Fatalf("isNull(1) after trapped fill = %d, want 1", got)
	}
}

func TestTableCopyTrapWithMixedContentsIsAtomic(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 0, 1, 1, 2, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("setNull1", 0, 4),
			wasmtest.ExportEntry("copyOOB", 0, 5),
			wasmtest.ExportEntry("callAt", 0, 6),
			wasmtest.ExportEntry("isNull", 0, 7),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(40))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestI32Const(0), tableTestI32Const(3), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()
	if _, err := inst.Invoke("setNull1"); err != nil {
		t.Fatalf("setNull1: %v", err)
	}

	_, err := inst.Invoke("copyOOB")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	for idx, want := range map[int32]int32{0: 10, 2: 30, 3: 40} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != want {
			t.Fatalf("callAt(%d) after trapped copy = %d, want %d", idx, got, want)
		}
	}
	if got := tableTestCallI32(t, inst, "isNull", I32(1)); got != 1 {
		t.Fatalf("isNull(1) after trapped copy = %d, want 1", got)
	}
}

func TestTableInitTrapWithNullExpressionsIsAtomic(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 0, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 2),
			wasmtest.ExportEntry("initOOB", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0, 1, 0),
			tableTestPassiveElemExpr(tableTestRefFuncExpr(1), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestI32Const(0), tableTestI32Const(3), tableTestBulk(12, 1, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	_, err := inst.Invoke("initOOB")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	for idx, want := range map[int32]int32{0: 10, 1: 20, 2: 10} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != want {
			t.Fatalf("callAt(%d) after trapped init = %d, want %d", idx, got, want)
		}
	}
}

func TestElemDropAfterPassiveNullExpressionSegment(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 1, 2, 2, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 1),
			wasmtest.ExportEntry("init", 0, 2),
			wasmtest.ExportEntry("drop", 0, 3),
			wasmtest.ExportEntry("zeroInit", 0, 4),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElemExpr(tableTestRefNullFuncExpr()))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(12, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(13, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestI32Const(0), tableTestI32Const(0), tableTestBulk(12, 0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("drop"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	_, err := inst.Invoke("init")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	if _, err := inst.Invoke("zeroInit"); err != nil {
		t.Fatalf("zero-length init after drop: %v", err)
	}
}

func TestActiveElementExpressionSegmentCanInitializeNulls(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemExpr(0, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(1)))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(44))),
			wasmtest.Code(tableTestBody(tableTestI32Const(55))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 44 {
		t.Fatalf("callAt(0) = %d, want 44", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 55 {
		t.Fatalf("callAt(2) = %d, want 55", got)
	}
}

func TestImportedTableActiveNullElementVisibleToAnotherInstance(t *testing.T) {
	tbl, err := NewTable(3, 3)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	initMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(0),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemExpr(0, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(66))))),
	)
	observerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)
	initializer := tableTestInstantiateWithImports(t, initMod, Imports{"env.t": tbl})
	defer initializer.Close()
	observer := tableTestInstantiateWithImports(t, observerMod, Imports{"env.t": tbl})
	defer observer.Close()

	for _, idx := range []int32{0, 2} {
		if got := tableTestCallI32(t, observer, "callAt", I32(idx)); got != 66 {
			t.Fatalf("observer callAt(%d) = %d, want 66", idx, got)
		}
	}
	_, err = observer.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestTableGetTrapDoesNotPerturbTable(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 0, 1, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("trapGet", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
			wasmtest.ExportEntry("isNull", 0, 4),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(71))),
			wasmtest.Code(tableTestBody(tableTestI32Const(72))),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), []byte{0x25, 0x00}, []byte{0xd1})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 71 {
		t.Fatalf("callAt(0) before trapGet = %d, want 71", got)
	}
	for i := 0; i < 3; i++ {
		_, err := inst.Invoke("trapGet")
		tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 71 {
		t.Fatalf("callAt(0) after trapGet = %d, want 71", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 72 {
		t.Fatalf("callAt(1) after trapGet = %d, want 72", got)
	}
}

func TestTableZeroLengthBoundaryAndHugeIndexCases(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 1, 1, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copyAtEnd", 0, 2),
			wasmtest.ExportEntry("initAtEnd", 0, 3),
			wasmtest.ExportEntry("fillHugeZero", 0, 4),
			wasmtest.ExportEntry("callAt", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0, 1),
			tableTestPassiveElem(0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(81))),
			wasmtest.Code(tableTestBody(tableTestI32Const(82))),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestI32Const(2), tableTestI32Const(0), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestI32Const(2), tableTestI32Const(0), tableTestBulk(12, 1, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), tableTestRefFunc(0), tableTestI32Const(0), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if _, err := inst.Invoke("copyAtEnd"); err != nil {
		t.Fatalf("zero-length table.copy at boundary: %v", err)
	}
	if _, err := inst.Invoke("initAtEnd"); err != nil {
		t.Fatalf("zero-length table.init at table/segment boundary: %v", err)
	}
	_, err := inst.Invoke("fillHugeZero")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 81 {
		t.Fatalf("callAt(0) after boundary cases = %d, want 81", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 82 {
		t.Fatalf("callAt(1) after boundary cases = %d, want 82", got)
	}
}

func TestTableNegativeRuntimeIndexesTrapWithoutMutation(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		tableTestFuncSection(0, 0, 1, 1, 1, 1, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("setNeg", 0, 2),
			wasmtest.ExportEntry("initNeg", 0, 3),
			wasmtest.ExportEntry("copyNeg", 0, 4),
			wasmtest.ExportEntry("fillNeg", 0, 5),
			wasmtest.ExportEntry("callAt", 0, 6),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1), tableTestPassiveElem(1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(31))),
			wasmtest.Code(tableTestBody(tableTestI32Const(32))),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(12, 1, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), tableTestI32Const(0), tableTestI32Const(1), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(-1), tableTestRefFunc(1), tableTestI32Const(1), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()
	assertTable := func(context string) {
		t.Helper()
		if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 31 {
			t.Fatalf("callAt(0) %s = %d, want 31", context, got)
		}
		if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 32 {
			t.Fatalf("callAt(1) %s = %d, want 32", context, got)
		}
	}
	assertTable("before traps")
	for _, name := range []string{"setNeg", "initNeg", "copyNeg", "fillNeg"} {
		if _, err := inst.Invoke(name); err == nil {
			t.Fatalf("%s with i32.const -1 succeeded, want trap", name)
		}
		assertTable("after " + name)
	}
}

func TestImportedTableGrowFailureVisibleToAnotherInstanceAsNoChange(t *testing.T) {
	tbl, err := NewTable(2, 2)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	initGrowMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow1", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(91))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(1), tableTestBulk(15, 0))),
		)),
	)
	observerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("callAt", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	grower := tableTestInstantiateWithImports(t, initGrowMod, Imports{"env.t": tbl})
	defer grower.Close()
	observer := tableTestInstantiateWithImports(t, observerMod, Imports{"env.t": tbl})
	defer observer.Close()

	if got := tableTestCallI32(t, grower, "grow1"); got != -1 {
		t.Fatalf("over-max imported table.grow = %d, want -1", got)
	}
	if got := tableTestCallI32(t, observer, "size"); got != 2 {
		t.Fatalf("observer table.size after failed grow = %d, want 2", got)
	}
	if got := tableTestCallI32(t, observer, "callAt", I32(0)); got != 91 {
		t.Fatalf("observer callAt(0) after failed grow = %d, want 91", got)
	}
	_, err = observer.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
}

func TestImportedTableGrowWithNonNullInitializerVisibleToAnotherInstance(t *testing.T) {
	tbl, err := NewTable(1, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	growMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 4))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow2", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(111))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(2), tableTestBulk(15, 0))),
		)),
	)
	observerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 4))),
		tableTestFuncSection(0, 1),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("callAt", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	grower := tableTestInstantiateWithImports(t, growMod, Imports{"env.t": tbl})
	defer grower.Close()
	observer := tableTestInstantiateWithImports(t, observerMod, Imports{"env.t": tbl})
	defer observer.Close()

	if got := tableTestCallI32(t, observer, "size"); got != 1 {
		t.Fatalf("observer initial table.size = %d, want 1", got)
	}
	_, err = observer.Invoke("callAt", I32(0))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, grower, "grow2"); got != 1 {
		t.Fatalf("imported table.grow = %d, want old size 1", got)
	}
	if got := tableTestCallI32(t, observer, "size"); got != 3 {
		t.Fatalf("observer table.size after grow = %d, want 3", got)
	}
	for _, idx := range []int32{1, 2} {
		if got := tableTestCallI32(t, observer, "callAt", I32(idx)); got != 111 {
			t.Fatalf("observer callAt(%d) after non-null grow = %d, want 111", idx, got)
		}
	}
}

func TestEmptyActiveElementBoundsCheckedAtInstantiation(t *testing.T) {
	t.Run("local exact boundary accepted", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(2))),
		)
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
	})
	t.Run("local one past boundary rejected", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(3))),
		)
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if _, err := Instantiate(c, nil); err == nil || !strings.Contains(err.Error(), "active element segment 0 out of bounds") {
			t.Fatalf("Instantiate empty OOB = %v, want active element bounds error", err)
		}
	})
	t.Run("imported exact boundary accepted", func(t *testing.T) {
		tbl, err := NewTable(2, 2)
		if err != nil {
			t.Fatalf("NewTable: %v", err)
		}
		defer tbl.Close()
		mod := wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(2))),
		)
		inst := tableTestInstantiateWithImports(t, mod, Imports{"env.t": tbl})
		defer inst.Close()
	})
	t.Run("imported one past boundary rejected without mutating shared table", func(t *testing.T) {
		tbl, err := NewTable(2, 2)
		if err != nil {
			t.Fatalf("NewTable: %v", err)
		}
		defer tbl.Close()
		seedMod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
			tableTestFuncSection(0),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(77))))),
		)
		seed := tableTestInstantiateWithImports(t, seedMod, Imports{"env.t": tbl})
		defer seed.Close()

		oobMod := wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(3))),
		)
		c, err := Compile(nil, oobMod)
		if err != nil {
			t.Fatalf("Compile OOB: %v", err)
		}
		if _, err := Instantiate(c, Imports{"env.t": tbl}); err == nil || !strings.Contains(err.Error(), "active element segment 0 out of bounds") {
			t.Fatalf("Instantiate imported empty OOB = %v, want active element bounds error", err)
		}

		callMod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 2))),
			tableTestFuncSection(0),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call0", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}))),
		)
		caller := tableTestInstantiateWithImports(t, callMod, Imports{"env.t": tbl})
		defer caller.Close()
		if got := tableTestCallI32(t, caller, "call0"); got != 77 {
			t.Fatalf("call0 after failed empty segment = %d, want 77", got)
		}
	})
}

func TestTableActiveElementBoundsAtInstantiation(t *testing.T) {
	t.Run("valid segment applies", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			)),
			tableTestFuncSection(0, 1),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 1))),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code(tableTestBody(tableTestI32Const(77))),
				wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			)),
		)
		inst := tableTestInstantiate(t, mod)
		defer inst.Close()
		if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 77 {
			t.Fatalf("callAt(0) after active elem = %d, want 77", got)
		}
	})

	t.Run("OOB segment fails instantiation", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			tableTestFuncSection(0),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(1, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(1))))),
		)
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if _, err := Instantiate(c, nil); err == nil || !strings.Contains(err.Error(), "active element segment") {
			t.Fatalf("Instantiate local active OOB = %v, want active element bounds error", err)
		}
	})
}

func TestImportedTableActiveElementEarlierSegmentPersistsBeforeLaterOOB(t *testing.T) {
	tbl, err := NewTable(3, 3)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	seedMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(0),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(77))))),
	)
	seed := tableTestInstantiateWithImports(t, seedMod, Imports{"env.t": tbl})
	defer seed.Close()

	badMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(0),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0),
			tableTestActiveElem(3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(909))))),
	)
	c, err := Compile(nil, badMod)
	if err != nil {
		t.Fatalf("Compile bad module: %v", err)
	}
	bad, err := Instantiate(c, Imports{"env.t": tbl})
	if bad != nil {
		defer bad.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "active element segment") {
		t.Fatalf("Instantiate bad module error = %v, want active element bounds error", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close failed module: %v", err)
	}

	observerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(1),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))))),
	)
	observer := tableTestInstantiateWithImports(t, observerMod, Imports{"env.t": tbl})
	defer observer.Close()
	if got := tableTestCallI32(t, observer, "callAt", I32(0)); got != 909 {
		t.Fatalf("callAt(0) after failed multi-segment instantiate = %d, want 909 from the earlier segment", got)
	}
}

func TestSharedTableFailedInstanceRootsStayCapacityBounded(t *testing.T) {
	tbl, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}

	var previous *Instance
	for value := 1; value <= 4; value++ {
		mod := MustCompile(watToWasmCA(t, fmt.Sprintf(`(module
			(import "env" "table" (table 1 funcref))
			(elem (i32.const 0) $f)
			(func $f (result i32) (i32.const %d))
			(func $start unreachable)
			(start $start))`, value)))
		if _, err := Instantiate(mod, Imports{"env.table": tbl}); err == nil {
			t.Fatalf("iteration %d: trapping start instantiated successfully", value)
		}
		if err := mod.Close(); err != nil {
			t.Fatalf("iteration %d: Close module: %v", value, err)
		}

		tbl.mu.Lock()
		if got := len(tbl.retained); got != 1 {
			tbl.mu.Unlock()
			t.Fatalf("iteration %d: retained roots = %d, want table-capacity bound 1", value, got)
		}
		var current *Instance
		for root := range tbl.retained {
			current = root
		}
		tbl.mu.Unlock()
		if previous != nil {
			previous.lifeMu.Lock()
			closed := previous.resourcesClosed
			previous.lifeMu.Unlock()
			if !closed {
				t.Fatalf("iteration %d: overwritten failed-instance root was not released", value)
			}
		}
		previous = current
	}

	if err := tbl.Close(); err != nil {
		t.Fatalf("Close table: %v", err)
	}
	previous.lifeMu.Lock()
	closed := previous.resourcesClosed
	previous.lifeMu.Unlock()
	if !closed {
		t.Fatal("closing the table did not release its last failed-instance root")
	}
}

func TestImportedTableActiveElementOOBDoesNotMutateHostTable(t *testing.T) {
	tbl, err := NewTable(3, 3)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	seedMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(0, 0),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemExpr(0, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(1)))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(301))),
			wasmtest.Code(tableTestBody(tableTestI32Const(303))),
		)),
	)
	observerMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(1, 1),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 0),
			wasmtest.ExportEntry("isNull", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
		)),
	)
	seed := tableTestInstantiateWithImports(t, seedMod, Imports{"env.t": tbl})
	defer seed.Close()
	observer := tableTestInstantiateWithImports(t, observerMod, Imports{"env.t": tbl})
	defer observer.Close()

	assertOriginal := func(phase string) {
		t.Helper()
		if got := tableTestCallI32(t, observer, "callAt", I32(0)); got != 301 {
			t.Fatalf("callAt(0) %s = %d, want 301", phase, got)
		}
		if got := tableTestCallI32(t, observer, "isNull", I32(1)); got != 1 {
			t.Fatalf("isNull(1) %s = %d, want 1", phase, got)
		}
		if got := tableTestCallI32(t, observer, "callAt", I32(2)); got != 303 {
			t.Fatalf("callAt(2) %s = %d, want 303", phase, got)
		}
	}
	assertOriginal("before OOB instantiate")

	oobMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 3, 3))),
		tableTestFuncSection(0),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemExpr(2, tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestI32Const(909))))),
	)
	c, err := Compile(nil, oobMod)
	if err != nil {
		t.Fatalf("Compile OOB module: %v", err)
	}
	bad, err := Instantiate(c, Imports{"env.t": tbl})
	if bad != nil {
		defer bad.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "active element segment") {
		t.Fatalf("Instantiate OOB module error = %v, want active element segment bounds error", err)
	}
	assertOriginal("after OOB instantiate")
}

func TestImportedMinOnlyTableImportObservesAlreadyGrownHostTable(t *testing.T) {
	tbl, err := NewTable(1, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()

	growMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 4))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow2", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))))),
	)
	grower := tableTestInstantiateWithImports(t, growMod, Imports{"env.t": tbl})
	defer grower.Close()
	if got := tableTestCallI32(t, grower, "grow2"); got != 1 {
		t.Fatalf("imported table.grow = %d, want old size 1", got)
	}

	minOnlyMod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 0))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	observer := tableTestInstantiateWithImports(t, minOnlyMod, Imports{"env.t": tbl})
	defer observer.Close()
	if got := tableTestCallI32(t, observer, "size"); got != 3 {
		t.Fatalf("min-only importer table.size = %d, want grown size 3", got)
	}
}

func TestMultipleLocalTableArenaFootprintIsBounded(t *testing.T) {
	one, err := Compile(nil, wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01}))))
	if err != nil {
		t.Fatalf("Compile one table: %v", err)
	}
	two, err := Compile(nil, wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(
		[]byte{0x70, 0x01, 0x01, 0x01},
		[]byte{0x70, 0x01, 0x01, 0x01},
	))))
	if err != nil {
		t.Fatalf("Compile two tables: %v", err)
	}
	if err := one.validateArenaFootprint(); err != nil {
		t.Fatalf("one-table footprint: %v", err)
	}
	if err := two.validateArenaFootprint(); err != nil {
		t.Fatalf("two-table footprint: %v", err)
	}
	// The second capacity-one funcref descriptor is 8+32 bytes and the two-entry
	// pointer directory is 16 bytes. Table 0 keeps its existing direct descriptor.
	if got, want := two.instantiateArenaNeed-one.instantiateArenaNeed, 56; got != want {
		t.Fatalf("two-table arena delta = %d bytes, want %d", got, want)
	}
}

func TestImportedThenLocalTableArenaFootprintIsBounded(t *testing.T) {
	imported, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
	))
	if err != nil {
		t.Fatalf("Compile imported table: %v", err)
	}
	combined, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
	))
	if err != nil {
		t.Fatalf("Compile imported+local tables: %v", err)
	}
	if err := imported.validateArenaFootprint(); err != nil {
		t.Fatalf("imported-table footprint: %v", err)
	}
	if err := combined.validateArenaFootprint(); err != nil {
		t.Fatalf("imported+local footprint: %v", err)
	}
	// Adding one capacity-one local funcref table allocates its 40-byte
	// descriptor plus the 16-byte two-table directory. Imported table 0 remains
	// owned by its producer and adds no importer-local descriptor.
	if got, want := combined.instantiateArenaNeed-imported.instantiateArenaNeed, 56; got != want {
		t.Fatalf("imported+local arena delta = %d bytes, want %d", got, want)
	}
}

func TestMultipleImportedTableArenaFootprintIsBounded(t *testing.T) {
	one, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "first", 0, 0))),
	))
	if err != nil {
		t.Fatalf("Compile one imported table: %v", err)
	}
	two, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			tableTestImportTable("env", "first", 0, 0),
			tableTestImportTable("env", "second", 0, 0),
		)),
	))
	if err != nil {
		t.Fatalf("Compile two imported tables: %v", err)
	}
	withLocal, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(
			tableTestImportTable("env", "first", 0, 0),
			tableTestImportTable("env", "second", 0, 0),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
	))
	if err != nil {
		t.Fatalf("Compile two imported plus local table: %v", err)
	}
	for name, compiled := range map[string]*Compiled{"one": one, "two": two, "with-local": withLocal} {
		if err := compiled.validateArenaFootprint(); err != nil {
			t.Fatalf("%s footprint: %v", name, err)
		}
	}
	if got, want := two.instantiateArenaNeed-one.instantiateArenaNeed, 16; got != want {
		t.Fatalf("second imported table arena delta = %d, want 16-byte directory", got)
	}
	if got, want := withLocal.instantiateArenaNeed-two.instantiateArenaNeed, 48; got != want {
		t.Fatalf("local table after two imports arena delta = %d, want 40-byte descriptor plus 8-byte directory growth", got)
	}
}

func TestCompileRejectsUnsupportedTableIndexes(t *testing.T) {
	compileErrContains := func(t *testing.T, mod []byte, want string) {
		t.Helper()
		_, err := Compile(nil, mod)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile error = %v, want substring %q", err, want)
		}
	}
	moduleWithBody := func(results []wasm.ValType, body []byte, extra ...[]byte) []byte {
		sections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results))),
			tableTestFuncSection(0),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		}
		sections = append(sections, extra...)
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		return wasmtest.Module(sections...)
	}

	t.Run("multiple local table metadata", func(t *testing.T) {
		mod := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01}, []byte{0x70, 0x01, 0x02, 0x04})))
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if !c.HasTable || c.TableSize != 1 || c.TableMax != 1 || len(c.extraTables) != 1 {
			t.Fatalf("compiled table metadata = HasTable %v size/max %d/%d extra %#v", c.HasTable, c.TableSize, c.TableMax, c.extraTables)
		}
		if got := c.extraTables[0]; got.Size != 2 || got.Max != 4 {
			t.Fatalf("table 1 metadata = %#v, want size/max 2/4", got)
		}
		if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "multiple-table metadata is not serializable") {
			t.Fatalf("MarshalBinary error = %v, want multiple-table rejection", err)
		}
		inst, err := Instantiate(c)
		if err != nil {
			t.Fatalf("Instantiate: %v", err)
		}
		defer inst.Close()
	})
	t.Run("table import plus local table metadata", func(t *testing.T) {
		mod := wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 1, 1))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})),
		)
		c, err := Compile(nil, mod)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if key, ok := c.TableImport(); !ok || key != "env.t" {
			t.Fatalf("TableImport = %q, %v; want env.t, true", key, ok)
		}
		if !c.HasTable || c.TableSize != 0 || c.TableMax != 0 || len(c.extraTables) != 1 {
			t.Fatalf("compiled imported+local metadata = HasTable %v size/max %d/%d extra %#v", c.HasTable, c.TableSize, c.TableMax, c.extraTables)
		}
		if got := c.extraTables[0]; got.Size != 2 || got.Max != 4 {
			t.Fatalf("local table 1 metadata = %#v, want size/max 2/4", got)
		}
		if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "multiple-table metadata") {
			t.Fatalf("MarshalBinary error = %v, want imported+local metadata rejection", err)
		}
	})
	cases := []struct {
		name    string
		results []wasm.ValType
		body    []byte
		extra   []byte
		want    string
	}{
		{name: "table.get index 1", results: []wasm.ValType{wasm.I32}, body: tableTestBody(tableTestI32Const(0), []byte{0x25, 0x01}, []byte{0xd1}), want: "table"},
		{name: "table.set index 1", body: tableTestBody(tableTestI32Const(0), tableTestRefNullFunc(), []byte{0x26, 0x01}), want: "table"},
		{name: "table.init index 1", body: tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(0), tableTestBulk(12, 0, 1)), extra: wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))), want: "table"},
		{name: "table.copy destination index 1", body: tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(0), tableTestBulk(14, 1, 0)), want: "table"},
		{name: "table.grow index 1", results: []wasm.ValType{wasm.I32}, body: tableTestBody(tableTestRefNullFunc(), tableTestI32Const(0), tableTestBulk(15, 1)), want: "table"},
		{name: "table.size index 1", results: []wasm.ValType{wasm.I32}, body: tableTestBody(tableTestBulk(16, 1)), want: "table"},
		{name: "table.fill index 1", body: tableTestBody(tableTestI32Const(0), tableTestRefNullFunc(), tableTestI32Const(0), tableTestBulk(17, 1)), want: "table"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.extra != nil {
				compileErrContains(t, moduleWithBody(tc.results, tc.body, tc.extra), tc.want)
				return
			}
			compileErrContains(t, moduleWithBody(tc.results, tc.body), tc.want)
		})
	}
}

func TestCompiledCodecPreservesPassiveNullElementPayloads(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 0, 2, 1),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 2),
			wasmtest.ExportEntry("callAt", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0, 0, 0),
			tableTestPassiveElemExpr(tableTestRefFuncExpr(1), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(101))),
			wasmtest.Code(tableTestBody(tableTestI32Const(202))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestI32Const(3), tableTestBulk(12, 1, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
		)),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(c.passiveElems) != 2 || len(c.passiveElems[1].Values) != 3 || !c.passiveElems[1].Values[1].Null {
		t.Fatalf("compiled passive elems = %#v, want elem 1 middle null", c.passiveElems)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.passiveElems) != 2 || len(loaded.passiveElems[1].Values) != 3 || !loaded.passiveElems[1].Values[1].Null {
		t.Fatalf("loaded passive elems = %#v, want elem 1 middle null", loaded.passiveElems)
	}
	inst, err := Instantiate(loaded)
	if err != nil {
		t.Fatalf("Instantiate loaded: %v", err)
	}
	defer inst.Close()
	if _, err := inst.Invoke("init"); err != nil {
		t.Fatalf("init after codec round trip: %v", err)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 202 {
		t.Fatalf("callAt(0) after init = %d, want 202", got)
	}
	_, err = inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 101 {
		t.Fatalf("callAt(2) after init = %d, want 101", got)
	}
}

func TestCompileRejectsMalformedElementExpressions(t *testing.T) {
	compileErrContains := func(t *testing.T, mod []byte, want string) {
		t.Helper()
		_, err := Compile(nil, mod)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile error = %v, want substring %q", err, want)
		}
	}
	t.Run("unsupported expression opcode", func(t *testing.T) {
		seg := tableTestPassiveElemExpr(tableTestI32Const(0))
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(9, wasmtest.Vec(seg)),
		)
		compileErrContains(t, mod, "type mismatch")
	})
	t.Run("ref.null extern for funcref table", func(t *testing.T) {
		seg := []byte{0x05, 0x70}
		seg = append(seg, tableTestExprVec([]byte{0xd0, 0x6f})...)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(9, wasmtest.Vec(seg)),
		)
		compileErrContains(t, mod, "type mismatch")
	})
	t.Run("missing expression end", func(t *testing.T) {
		seg := []byte{0x05, 0x70, 0x01, 0xd0, 0x70}
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(9, wasmtest.Vec(seg)),
		)
		compileErrContains(t, mod, "decode")
	})
}

func TestCompiledCodecMinOnlyTableImportRejectsBelowMinAfterLoad(t *testing.T) {
	tableTestForceExplicitBounds(t)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "t", 2, 0))),
		tableTestFuncSection(0),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tbl, err := NewTable(1, 4)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer tbl.Close()
	if _, err := Instantiate(loaded, Imports{"env.t": tbl}); err == nil || !strings.Contains(err.Error(), "required minimum 2") {
		t.Fatalf("Instantiate below min after Load error = %v, want required minimum", err)
	}
}

func TestTableGrowFillGetSetAndSize(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // 0: () -> i32
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // 1: (i32) -> i32
			wasmtest.FuncType(nil, nil),                                           // 2: () -> ()
		)),
		tableTestFuncSection(0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 1, 1, 2, 2, 2, 0, 0, 0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x04, 0x04})), // table funcref min=4 max=4
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 4),
			wasmtest.ExportEntry("grow2", 0, 5),
			wasmtest.ExportEntry("grow1", 0, 6),
			wasmtest.ExportEntry("fill", 0, 7),
			wasmtest.ExportEntry("setNull1", 0, 8),
			wasmtest.ExportEntry("setFunc1", 0, 9),
			wasmtest.ExportEntry("isNull", 0, 10),
			wasmtest.ExportEntry("callAt", 0, 11),
			wasmtest.ExportEntry("fillNull2", 0, 12),
			wasmtest.ExportEntry("fillOOB", 0, 13),
			wasmtest.ExportEntry("setOOB", 0, 14),
			wasmtest.ExportEntry("getOOB", 0, 15),
			wasmtest.ExportEntry("grow0", 0, 16),
			wasmtest.ExportEntry("growHuge", 0, 17),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(10))),
			wasmtest.Code(tableTestBody(tableTestI32Const(20))),
			wasmtest.Code(tableTestBody(tableTestI32Const(30))),
			wasmtest.Code(tableTestBody(tableTestI32Const(40))),
			wasmtest.Code(tableTestBody(tableTestBulk(16, 0))),
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(2), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(1), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefFunc(3), tableTestI32Const(2), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefNullFunc(), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefFunc(0), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0xd1})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(2), tableTestRefNullFunc(), tableTestI32Const(1), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(3), tableTestRefFunc(0), tableTestI32Const(2), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(4), tableTestRefFunc(0), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(4), []byte{0x25, 0x00}, []byte{0xd1})),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestI32Const(0), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestRefNullFunc(), tableTestI32Const(-1), tableTestBulk(15, 0))),
		)),
	)
	inst := tableTestInstantiate(t, mod)
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "size"); got != 4 {
		t.Fatalf("initial table.size = %d, want 4", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 10 {
		t.Fatalf("callAt(0) = %d, want 10", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 20 {
		t.Fatalf("callAt(1) = %d, want 20", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(2)); got != 30 {
		t.Fatalf("callAt(2) = %d, want 30", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(3)); got != 40 {
		t.Fatalf("callAt(3) = %d, want 40", got)
	}
	if _, err := inst.Invoke("fill"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	for _, idx := range []int32{1, 2} {
		if got := tableTestCallI32(t, inst, "callAt", I32(idx)); got != 40 {
			t.Fatalf("callAt(%d) after fill = %d, want 40", idx, got)
		}
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(3)); got != 40 {
		t.Fatalf("callAt(3) after fill = %d, want 40", got)
	}
	if _, err := inst.Invoke("setNull1"); err != nil {
		t.Fatalf("setNull1: %v", err)
	}
	if got := tableTestCallI32(t, inst, "isNull", I32(1)); got != 1 {
		t.Fatalf("isNull(1) after table.set null = %d, want 1", got)
	}
	_, err := inst.Invoke("callAt", I32(1))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if _, err := inst.Invoke("setFunc1"); err != nil {
		t.Fatalf("setFunc1: %v", err)
	}
	if got := tableTestCallI32(t, inst, "isNull", I32(1)); got != 0 {
		t.Fatalf("isNull(1) after table.set func = %d, want 0", got)
	}
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 10 {
		t.Fatalf("callAt(1) after table.set func = %d, want 10", got)
	}
	_, err = inst.Invoke("getOOB")
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	_, err = inst.Invoke("setOOB")
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(1)); got != 10 {
		t.Fatalf("callAt(1) after trapped table.set = %d, want 10", got)
	}
	_, err = inst.Invoke("fillOOB")
	tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(3)); got != 40 {
		t.Fatalf("callAt(3) after trapped table.fill = %d, want 40", got)
	}
	if _, err := inst.Invoke("fillNull2"); err != nil {
		t.Fatalf("fillNull2: %v", err)
	}
	if got := tableTestCallI32(t, inst, "isNull", I32(2)); got != 1 {
		t.Fatalf("isNull(2) after table.fill null = %d, want 1", got)
	}
	_, err = inst.Invoke("callAt", I32(2))
	tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
	if got := tableTestCallI32(t, inst, "callAt", I32(3)); got != 40 {
		t.Fatalf("callAt(3) after table.fill null = %d, want 40", got)
	}
	if got := tableTestCallI32(t, inst, "grow0"); got != 4 {
		t.Fatalf("zero table.grow = %d, want old size 4", got)
	}
	if got := tableTestCallI32(t, inst, "size"); got != 4 {
		t.Fatalf("table.size after zero grow = %d, want 4", got)
	}
	for _, name := range []string{"grow1", "grow2", "growHuge"} {
		if got := tableTestCallI32(t, inst, name); got != -1 {
			t.Fatalf("%s over maximum = %d, want -1", name, got)
		}
		if got := tableTestCallI32(t, inst, "size"); got != 4 {
			t.Fatalf("table.size after %s = %d, want 4", name, got)
		}
	}
}
