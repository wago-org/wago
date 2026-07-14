package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestCompiledAPIHelperCoverage(t *testing.T) {
	if got, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}); err != nil || got != nullFuncRefIndex {
		t.Fatalf("null funcref payload = %d, %v", got, err)
	}
	if got, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0xd2, 0x02, 0x0b}}); err != nil || got != 2 {
		t.Fatalf("ref.func payload = %d, %v", got, err)
	}
	if _, err := funcrefExprPayload(wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}); err == nil {
		t.Fatal("non-funcref expression accepted")
	}
	if funcTypeUsesV128(nil) || funcTypeUsesV128(&wasm.CompType{Params: []wasm.ValType{wasm.I32}}) ||
		!funcTypeUsesV128(&wasm.CompType{Params: []wasm.ValType{wasm.V128}}) ||
		!funcTypeUsesV128(&wasm.CompType{Results: []wasm.ValType{wasm.V128}}) {
		t.Fatal("v128 function signature detection changed")
	}
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}
	if !sigMatches(ft, &InstanceExport{params: []ValType{ValI32}, results: []ValType{ValI64}}) ||
		sigMatches(ft, &InstanceExport{params: []ValType{ValI64}, results: []ValType{ValI64}}) ||
		sigMatches(ft, &InstanceExport{params: []ValType{ValI32}}) {
		t.Fatal("cross-instance signature matching changed")
	}
	for _, tc := range []struct {
		sig  FuncSig
		want bool
	}{
		{FuncSig{}, true},
		{FuncSig{Params: []ValType{ValI32}}, true},
		{FuncSig{Params: []ValType{ValI64}}, false},
		{FuncSig{Params: []ValType{ValI32, ValI32}}, false},
		{FuncSig{Results: []ValType{ValI32}}, false},
	} {
		if got := asyncReplayable(tc.sig); got != tc.want {
			t.Errorf("asyncReplayable(%+v) = %v, want %v", tc.sig, got, tc.want)
		}
	}
	if bodyBytesUseMemoryGrow([]byte{0x0b}) || !bodyBytesUseMemoryGrow([]byte{0x40, 0x00, 0x0b}) || !bodyBytesUseMemoryGrow([]byte{0xff}) {
		t.Fatal("memory.grow byte scanner changed")
	}
	if instrsUseMemoryGrow([]wasm.Instruction{{Kind: wasm.InstrI32Add}}) || !instrsUseMemoryGrow([]wasm.Instruction{{Kind: wasm.InstrMemoryGrow}}) {
		t.Fatal("programmatic memory.grow scanner changed")
	}
	if !moduleUsesMemoryGrow(&wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0x40, 0x00, 0x0b}}}}) ||
		moduleUsesMemoryGrow(&wasm.Module{Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Add}}}}}}) {
		t.Fatal("module memory.grow detection changed")
	}

	elem, data := 0, 0
	for _, in := range []wasm.Instruction{
		{Kind: wasm.InstrTableInit, Index: 2},
		{Kind: wasm.InstrElemDrop, Index: 1},
		{Kind: wasm.InstrMemoryInit, Index: 4},
		{Kind: wasm.InstrDataDrop, Index: 3},
	} {
		segmentStateCount(in.Kind, in.Index, &elem, &data)
	}
	if elem != 3 || data != 5 {
		t.Fatalf("segment state counts = %d, %d", elem, data)
	}
	elem, data = 0, 0
	instrsSegmentStateCounts([]wasm.Instruction{
		{Kind: wasm.InstrTableInit, Index: 2},
		{Kind: wasm.InstrDataDrop, Index: 4},
	}, &elem, &data)
	if elem != 3 || data != 5 {
		t.Fatalf("instruction segment state counts = %d, %d", elem, data)
	}
	if ok := bodyBytesSegmentStateCounts([]byte{0xfc, 0x0c, 0x02, 0x00, 0xfc, 0x09, 0x04, 0x0b}, &elem, &data); !ok || elem != 3 || data != 5 {
		t.Fatalf("byte segment state counts = %d, %d, %v", elem, data, ok)
	}
	if bodyBytesSegmentStateCounts([]byte{0xff}, &elem, &data) {
		t.Fatal("malformed segment bytecode accepted")
	}

	var nilCompiled *Compiled
	if _, ok := nilCompiled.TableImport(); ok || nilCompiled.TableImports() != nil || nilCompiled.FuncDebugName(3) != "func3" {
		t.Fatal("nil compiled helpers changed")
	}
	c := &Compiled{
		tableImport: "env.a",
		extraTables: []tableDef{{ImportKey: "env.b"}},
		Exports:     map[string]int{"z": 1, "a": 1},
		NumImports:  1,
		Names:       &wasm.NameSec{FunctionNames: wasm.NameMap{{Index: 0, Name: "host"}}},
	}
	if _, ok := c.TableImport(); ok {
		t.Fatal("legacy single-table helper accepted multiple imports")
	}
	if got := c.TableImports(); len(got) != 2 || got[0] != "env.a" || got[1] != "env.b" {
		t.Fatalf("TableImports = %v", got)
	}
	if name, ok := c.FuncName(0); !ok || name != "host" {
		t.Fatalf("FuncName = %q, %v", name, ok)
	}
	if _, ok := c.LocalFuncName(-1); ok {
		t.Fatal("negative local function index accepted")
	}
	if got := c.FuncDebugName(1); got != "a" {
		t.Fatalf("FuncDebugName export fallback = %q", got)
	}
	imports := Imports{"env.g": NewGlobalI32(3, false)}
	defer imports["env.g"].(*Global).Close()
	in := &Instance{imports: imports}
	if got := in.Imports(); got["env.g"] != imports["env.g"] {
		t.Fatalf("Imports = %v, want supplied map", got)
	}
}

func TestDeferredHostLinkCachesReturningImport(t *testing.T) {
	funcImport := append(wasmtest.Name("env"), wasmtest.Name("answer")...)
	funcImport = append(funcImport, 0, 0) // function import, type 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(funcImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile deferred host module: %v", err)
	}
	defer c.Close()
	imports := Imports{"env.answer": HostFunc(func(_ HostModule, _, results []uint64) { results[0] = I32(42) })}
	first, err := c.linkModule(imports, nil)
	if err != nil {
		t.Fatalf("link returning host module: %v", err)
	}
	second, err := c.linkModule(imports, nil)
	if err != nil {
		t.Fatalf("repeat link returning host module: %v", err)
	}
	if c.needsLink {
		if c.hostLink == nil || first == c || second != first || !first.syncHostImports {
			t.Fatalf("deferred linked modules = %p, %p sync=%v", first, second, first.syncHostImports)
		}
	} else if first != c || second != c || !c.syncHostImports {
		t.Fatalf("eager synchronous module = %p, %p sync=%v", first, second, c.syncHostImports)
	}
	in, err := Instantiate(c, imports)
	if err != nil {
		t.Fatalf("Instantiate returning host module: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != I32(42) {
		t.Fatalf("run = %v, %v; want 42", got, err)
	}
}
