package frontend

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func tailResultFuncType(params, results []wasm.ValType) wasm.RecType {
	return wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}}}}
}

func directTailRewriteModule(results []wasm.ValType) *wasm.Module {
	targetBody := []wasm.Instruction{}
	hasReferenceResult := false
	for _, result := range results {
		switch {
		case wasm.EqualValType(result, wasm.I32):
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrI32Const})
		case wasm.EqualValType(result, wasm.I64):
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrI64Const})
		case wasm.EqualValType(result, wasm.F32):
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrF32Const})
		case wasm.EqualValType(result, wasm.F64):
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrF64Const})
		case wasm.EqualValType(result, wasm.V128):
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrV128Const})
		default:
			hasReferenceResult = true
			targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrRefFunc, Index: 0})
		}
	}
	m := &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType(nil, results),
			tailResultFuncType(nil, results),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}},
		Code: []wasm.Func{
			{Body: wasm.Expr{Instrs: targetBody}},
			{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 0}}}},
		},
	}
	if hasReferenceResult {
		m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
	}
	return m
}

func indirectTailRewriteModule(results []wasm.ValType, mutateTable bool) *wasm.Module {
	m := directTailRewriteModule(results)
	m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1}}}}
	m.Elements = []wasm.Elem{{
		Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: 0, Offset: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}},
		Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
	}}
	m.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrReturnCallIndirect, Index: 0, Index2: 0}}
	if mutateTable {
		prefix := []wasm.Instruction{
			{Kind: wasm.InstrI32Const},
			{Kind: wasm.InstrRefFunc, Index: 0},
			{Kind: wasm.InstrTableSet, Index: 0},
		}
		m.Code[0].Body.Instrs = append(prefix, m.Code[0].Body.Instrs...)
	}
	return m
}

func twoTargetIndirectTailRewriteModule(first, second, caller []wasm.ValType) *wasm.Module {
	body := func(results []wasm.ValType) []wasm.Instruction {
		out := make([]wasm.Instruction, 0, len(results))
		for _, result := range results {
			if wasm.EqualValType(result, wasm.I64) {
				out = append(out, wasm.Instruction{Kind: wasm.InstrI64Const})
			} else {
				out = append(out, wasm.Instruction{Kind: wasm.InstrI32Const})
			}
		}
		return out
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType(nil, first),
			tailResultFuncType(nil, second),
			tailResultFuncType(nil, caller),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}, {Index: 2}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 2}}}},
		Elements: []wasm.Elem{{
			Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: 0, Offset: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}},
			Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0, 1}},
		}},
		Code: []wasm.Func{
			{Body: wasm.Expr{Instrs: body(first)}},
			{Body: wasm.Expr{Instrs: body(second)}},
			{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrReturnCallIndirect, Index: 0, Index2: 0}}}},
		},
	}
}

func importedIndirectTailRewriteModule(results []wasm.ValType) *wasm.Module {
	return &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType(nil, results),
			tailResultFuncType(nil, results),
		},
		Imports:   []wasm.Import{{Module: "env", Name: "target", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})}},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1}}}},
		Elements: []wasm.Elem{{
			Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: 0, Offset: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}},
			Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
		}},
		Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrReturnCallIndirect, Index: 0, Index2: 0}}}}},
	}
}

func refTailRewriteModule(results []wasm.ValType, dynamicGlobal bool) *wasm.Module {
	m := directTailRewriteModule(results)
	m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
	if dynamicGlobal {
		ref := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
		m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: ref}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 0}}}}}
		m.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrGlobalGet, Index: 0}, {Kind: wasm.InstrReturnCallRef, Index: 0}}
	} else {
		m.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 0}, {Kind: wasm.InstrReturnCallRef, Index: 0}}
	}
	return m
}

func recursiveReferenceTailModule(field wasm.ValType) *wasm.Module {
	result := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	functionRef := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	return &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(field), wasm.Const)}}}}},
			tailResultFuncType(nil, []wasm.ValType{result}),
			tailResultFuncType(nil, []wasm.ValType{result}),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}, {Index: 2}},
		Globals: []wasm.Global{{
			Type: wasm.GlobalType{Type: functionRef},
			Init: wasm.Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b}},
		}},
		Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}},
		Code: []wasm.Func{
			{BodyBytes: []byte{0xd0, 0x00, 0x0b}},
			{BodyBytes: []byte{0x23, 0x00, 0x15, 0x01, 0x0b}},
		},
	}
}

func importedRecursiveReferenceSignatureModule(field wasm.ValType) *wasm.Module {
	result := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	return &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(field), wasm.Const)}}}}},
			tailResultFuncType(nil, []wasm.ValType{result}),
		},
		Imports: []wasm.Import{{Module: "env", Name: "f", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})}},
	}
}

func recursiveParameterTailModule(field wasm.ValType, results []wasm.ValType) *wasm.Module {
	param := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	targetBody := []byte{0x0b}
	if len(results) != 0 {
		targetBody = []byte{0x41, 0x00, 0x0b}
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(field), wasm.Const)}}}}},
			tailResultFuncType([]wasm.ValType{param}, results),
			tailResultFuncType([]wasm.ValType{param}, results),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}, {Index: 2}},
		Code: []wasm.Func{
			{BodyBytes: targetBody},
			{BodyBytes: []byte{0x20, 0x00, 0x12, 0x00, 0x0b}},
		},
	}
}

func nestedFunctionParameterTailModule(callbackResults, tailResults []wasm.ValType) *wasm.Module {
	callback := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	targetBody := []wasm.Instruction{}
	if len(tailResults) != 0 {
		targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrI32Const})
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType(nil, callbackResults),
			tailResultFuncType([]wasm.ValType{callback}, tailResults),
			tailResultFuncType([]wasm.ValType{callback}, tailResults),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}, {Index: 2}},
		Code: []wasm.Func{
			{Body: wasm.Expr{Instrs: targetBody}},
			{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrLocalGet, Index: 0}, {Kind: wasm.InstrReturnCall, Index: 0}}}},
		},
	}
}

func importedCallbackSinkTailModule(results []wasm.ValType) *wasm.Module {
	targetBody := []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 1}, {Kind: wasm.InstrCall, Index: 0}}
	if len(results) != 0 {
		targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrI32Const})
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType([]wasm.ValType{wasm.FuncRef}, nil),
			tailResultFuncType(nil, results),
			tailResultFuncType(nil, results),
		},
		Imports:   []wasm.Import{{Module: "env", Name: "callback", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})}},
		FuncTypes: []wasm.TypeIdx{{Index: 1}, {Index: 2}},
		Elements:  []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{1}}}},
		Code: []wasm.Func{
			{Body: wasm.Expr{Instrs: targetBody}},
			{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 1}}}},
		},
	}
}

func importedReferenceCallbackTailModule(param wasm.ValType, results []wasm.ValType) *wasm.Module {
	targetBody := []wasm.Instruction{}
	if len(results) != 0 {
		targetBody = append(targetBody, wasm.Instruction{Kind: wasm.InstrI32Const})
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			tailResultFuncType([]wasm.ValType{param}, nil),
			tailResultFuncType(nil, results),
			tailResultFuncType(nil, results),
		},
		Imports:   []wasm.Import{{Module: "env", Name: "callback", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})}},
		FuncTypes: []wasm.TypeIdx{{Index: 1}, {Index: 2}},
		Code: []wasm.Func{
			{Body: wasm.Expr{Instrs: targetBody}},
			{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 1}}}},
		},
	}
}

func TestValidateTailResultRewriteAllowsCoordinatedKnownTargets(t *testing.T) {
	shapes := []struct {
		name    string
		results []wasm.ValType
	}{
		{name: "unused scalar", results: []wasm.ValType{wasm.I32}},
		{name: "multivalue", results: []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}},
		{name: "SIMD", results: []wasm.ValType{wasm.V128}},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				before func([]wasm.ValType) *wasm.Module
			}{
				{name: "direct", before: directTailRewriteModule},
				{name: "immutable indirect", before: func(results []wasm.ValType) *wasm.Module { return indirectTailRewriteModule(results, false) }},
				{name: "immediate ref", before: func(results []wasm.ValType) *wasm.Module { return refTailRewriteModule(results, false) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before := tc.before(shape.results)
					after := tc.before(nil)
					if err := ValidateTailResultRewrite(before, after); err != nil {
						t.Fatalf("coordinated result elimination: %v", err)
					}
				})
			}
		})
	}
}

func TestValidateTailResultRewriteFailsClosedForUnknownTargets(t *testing.T) {
	before := indirectTailRewriteModule([]wasm.ValType{wasm.I32}, true)
	after := indirectTailRewriteModule(nil, true)
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "unknown target set") {
		t.Fatalf("mutable table rewrite error = %v", err)
	}

	before = refTailRewriteModule([]wasm.ValType{wasm.I32}, true)
	after = refTailRewriteModule(nil, true)
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "dynamic or imported target") {
		t.Fatalf("typed-global return_call_ref rewrite error = %v", err)
	}

	before = importedIndirectTailRewriteModule([]wasm.ValType{wasm.I32})
	after = importedIndirectTailRewriteModule(nil)
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed imported function") {
		t.Fatalf("imported indirect target rewrite error = %v", err)
	}
}

func TestValidateTailResultRewriteRequiresEveryProvenIndirectTarget(t *testing.T) {
	before := twoTargetIndirectTailRewriteModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	after := twoTargetIndirectTailRewriteModule(nil, []wasm.ValType{wasm.I32}, nil)
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "did not rewrite local target 1") {
		t.Fatalf("partial immutable-table target rewrite error = %v", err)
	}

	after = twoTargetIndirectTailRewriteModule(nil, nil, nil)
	if err := ValidateTailResultRewrite(before, after); err != nil {
		t.Fatalf("complete immutable-table target rewrite: %v", err)
	}
}

func TestValidateTailResultRewriteRejectsExportedAndEmptyIndirectSets(t *testing.T) {
	before := indirectTailRewriteModule([]wasm.ValType{wasm.I32}, false)
	after := indirectTailRewriteModule(nil, false)
	before.Exports = []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
	after.Exports = []wasm.Export{{Name: "table", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through exported function references") {
		t.Fatalf("exported table rewrite error = %v", err)
	}

	before = indirectTailRewriteModule([]wasm.ValType{wasm.I32}, false)
	after = indirectTailRewriteModule(nil, false)
	before.Elements = nil
	after.Elements = nil
	if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "no proven non-null target") {
		t.Fatalf("empty table rewrite error = %v", err)
	}
}

func TestValidateTailResultRewriteRejectsPartialAndAdversarialTransforms(t *testing.T) {
	t.Run("missing retained source", func(t *testing.T) {
		module := directTailRewriteModule([]wasm.ValType{wasm.I32})
		if err := ValidateTailResultRewrite(nil, module); err == nil || !strings.Contains(err.Error(), "non-nil") {
			t.Fatalf("nil source error = %v", err)
		}
		if err := ValidateTailResultRewrite(module, module); err == nil || !strings.Contains(err.Error(), "distinct retained source") {
			t.Fatalf("aliased source error = %v", err)
		}
	})

	t.Run("callee only", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after.Types[0].SubTypes[0].Comp.Results = nil
		after.Code[0].Body.Instrs = nil
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "result is invalid") {
			t.Fatalf("partial direct rewrite error = %v", err)
		}
	})

	t.Run("site target changed", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		after.Code[1].Body.Instrs[0].Index = 1
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "site identity or target") {
			t.Fatalf("retargeted rewrite error = %v", err)
		}
	})

	t.Run("exported caller changed", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		before.Exports = []wasm.Export{{Name: "run", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}}}
		after.Exports = []wasm.Export{{Name: "run", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}}}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed exported function") {
			t.Fatalf("exported caller rewrite error = %v", err)
		}
	})

	t.Run("target escapes through exported table", func(t *testing.T) {
		before := indirectTailRewriteModule([]wasm.ValType{wasm.I32}, false)
		after := indirectTailRewriteModule(nil, false)
		before.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 0}}
		after.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrReturnCall, Index: 0}}
		before.Exports = []wasm.Export{{Name: "functions", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
		after.Exports = []wasm.Export{{Name: "functions", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}}}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through exported function references") {
			t.Fatalf("escaped direct target rewrite error = %v", err)
		}
	})

	t.Run("parameters changed", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		after.Types[0].SubTypes[0].Comp.Params = []wasm.ValType{wasm.I32}
		after.Code[1].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrReturnCall, Index: 0}}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed parameters") {
			t.Fatalf("parameter rewrite error = %v", err)
		}
	})

	t.Run("nested function parameter results changed", func(t *testing.T) {
		before := nestedFunctionParameterTailModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
		after := nestedFunctionParameterTailModule(nil, nil)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed parameters") {
			t.Fatalf("nested function parameter rewrite error = %v", err)
		}
	})
}

func TestValidateTailResultRewriteRejectsImportedReferenceSinks(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		for _, m := range []*wasm.Module{before, after} {
			m.Imports = []wasm.Import{{Module: "env", Name: "functions", Type: wasm.NewTableExternType(wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1}})}}
			m.Elements = []wasm.Elem{{
				Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: 0, Offset: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}},
				Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
			}}
		}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through imported function references") {
			t.Fatalf("imported table rewrite error = %v", err)
		}
	})

	t.Run("mutable global", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		for _, m := range []*wasm.Module{before, after} {
			m.Imports = []wasm.Import{{Module: "env", Name: "callback", Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef, Mutable: true})}}
			m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
			m.Code[0].Body.Instrs = append([]wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 0}, {Kind: wasm.InstrGlobalSet, Index: 0}}, m.Code[0].Body.Instrs...)
		}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through imported function references") {
			t.Fatalf("imported mutable global rewrite error = %v", err)
		}
	})

	t.Run("callback parameter", func(t *testing.T) {
		before := importedCallbackSinkTailModule([]wasm.ValType{wasm.I32})
		after := importedCallbackSinkTailModule(nil)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through imported function references") {
			t.Fatalf("imported callback rewrite error = %v", err)
		}
	})

	t.Run("immutable defined-reference global", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		for _, m := range []*wasm.Module{before, after} {
			m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{
				Kind: wasm.CompStruct,
				Fields: []wasm.FieldType{
					wasm.NewFieldType(wasm.StorageVal(wasm.FuncRef), wasm.Var),
				},
			}}}})
			object := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false))
			m.Imports = []wasm.Import{{Module: "env", Name: "object", Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: object})}}
		}
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through imported function references") {
			t.Fatalf("immutable imported object rewrite error = %v", err)
		}
	})

	t.Run("externref callback", func(t *testing.T) {
		before := importedReferenceCallbackTailModule(wasm.ExternRef, []wasm.ValType{wasm.I32})
		after := importedReferenceCallbackTailModule(wasm.ExternRef, nil)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "observable through imported function references") {
			t.Fatalf("externref callback rewrite error = %v", err)
		}
	})
}

func TestAnalyzeTailResultSitesMatchesASTAndByteBackedBodies(t *testing.T) {
	ast := refTailRewriteModule([]wasm.ValType{wasm.I32}, false)
	byteBacked := refTailRewriteModule([]wasm.ValType{wasm.I32}, false)
	byteBacked.Code[1].Body.Instrs = nil
	byteBacked.Code[1].BodyBytes = []byte{0xd2, 0x00, 0x15, 0x00, 0x0b}
	for _, m := range []*wasm.Module{ast, byteBacked} {
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("validate test module: %v", err)
		}
	}
	gotAST, err := AnalyzeTailResultSites(ast)
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := AnalyzeTailResultSites(byteBacked)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotAST) != 1 || len(gotBytes) != 1 || gotAST[0] != gotBytes[0] || !gotAST[0].Known || gotAST[0].KnownFunction != 0 {
		t.Fatalf("AST sites = %#v, byte-backed sites = %#v", gotAST, gotBytes)
	}
}

func TestValidateTailResultRewriteUsesExactValidationFeatures(t *testing.T) {
	t.Run("multi-memory", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		before.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1}}}
		after.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}, {Limits: wasm.Limits{Min: 1}}}
		if err := ValidateTailResultRewrite(before, after); err == nil {
			t.Fatal("compatibility validation unexpectedly accepted multi-memory rewrite")
		}
		if err := ValidateTailResultRewriteWithFeatures(before, after, wasm.ValidationFeatures{MultiMemory: true}); err != nil {
			t.Fatalf("multi-memory result rewrite: %v", err)
		}
	})

	t.Run("memory64 byte-backed memarg", func(t *testing.T) {
		before := directTailRewriteModule([]wasm.ValType{wasm.I32})
		after := directTailRewriteModule(nil)
		for _, m := range []*wasm.Module{before, after} {
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1, Addr64: true}}}
			m.Code[0].Body.Instrs = nil
			m.Code[1].Body.Instrs = nil
			m.Code[1].BodyBytes = []byte{
				0x42, 0x00, // i64.const 0
				0x28, 0x02, 0x80, 0x80, 0x80, 0x80, 0x10, // i32.load align=2 offset=2^32
				0x1a,       // drop
				0x12, 0x00, // return_call 0
				0x0b,
			}
		}
		before.Code[0].BodyBytes = []byte{0x41, 0x00, 0x0b}
		after.Code[0].BodyBytes = []byte{0x0b}
		if err := ValidateTailResultRewrite(before, after); err != nil {
			t.Fatalf("memory64 result rewrite: %v", err)
		}
	})
}

func TestValidateTailResultRewriteCallerOnlyCovarianceNeedsNoDynamicProof(t *testing.T) {
	nonNull := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false))
	nullable := wasm.FuncRef
	before := refTailRewriteModule([]wasm.ValType{nonNull}, true)
	after := refTailRewriteModule([]wasm.ValType{nonNull}, true)
	after.Types[1].SubTypes[0].Comp.Results = []wasm.ValType{nullable}
	if err := ValidateTailResultRewrite(before, after); err != nil {
		t.Fatalf("caller-only covariant widening: %v", err)
	}
}

func TestValidateTailResultRewriteComparesCompleteRecursiveSignatureGraphs(t *testing.T) {
	t.Run("imported signature", func(t *testing.T) {
		before := importedRecursiveReferenceSignatureModule(wasm.I32)
		after := importedRecursiveReferenceSignatureModule(wasm.I64)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed imported function") {
			t.Fatalf("recursive imported signature rewrite error = %v", err)
		}
	})

	t.Run("dynamic return-call-ref target contract", func(t *testing.T) {
		before := recursiveReferenceTailModule(wasm.I32)
		after := recursiveReferenceTailModule(wasm.I64)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "dynamic or imported target") {
			t.Fatalf("recursive dynamic target rewrite error = %v", err)
		}
	})

	t.Run("parameter graph", func(t *testing.T) {
		before := recursiveParameterTailModule(wasm.I32, []wasm.ValType{wasm.I32})
		after := recursiveParameterTailModule(wasm.I64, nil)
		if err := ValidateTailResultRewrite(before, after); err == nil || !strings.Contains(err.Error(), "changed parameters") {
			t.Fatalf("recursive parameter rewrite error = %v", err)
		}
	})
}

func BenchmarkAnalyzeTailResultSites(b *testing.B) {
	m := indirectTailRewriteModule([]wasm.ValType{wasm.I32, wasm.I64}, false)
	const funcs = 256
	m.FuncTypes = make([]wasm.TypeIdx, funcs)
	m.Code = make([]wasm.Func, funcs)
	for i := range m.Code {
		m.FuncTypes[i] = wasm.TypeIdx{Index: 0}
		m.Code[i].BodyBytes = []byte{0x41, 0x00, 0x13, 0x00, 0x00, 0x0b}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := AnalyzeTailResultSites(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateTailResultRewrite(b *testing.B) {
	for _, tc := range []struct {
		name   string
		before *wasm.Module
		after  *wasm.Module
	}{
		{name: "direct", before: directTailRewriteModule([]wasm.ValType{wasm.I32, wasm.I64}), after: directTailRewriteModule(nil)},
		{name: "immutable-indirect", before: indirectTailRewriteModule([]wasm.ValType{wasm.I32, wasm.I64}, false), after: indirectTailRewriteModule(nil, false)},
		{name: "immediate-ref", before: refTailRewriteModule([]wasm.ValType{wasm.I32}, false), after: refTailRewriteModule(nil, false)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := ValidateTailResultRewrite(tc.before, tc.after); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzValidateTailResultRewrite(f *testing.F) {
	f.Add(byte(0), true, false)
	f.Add(byte(1), false, false)
	f.Add(byte(2), true, true)
	f.Fuzz(func(t *testing.T, kind byte, eliminate, dynamic bool) {
		results := []wasm.ValType{wasm.I32}
		var build func([]wasm.ValType) *wasm.Module
		switch kind % 3 {
		case 0:
			build = directTailRewriteModule
		case 1:
			build = func(r []wasm.ValType) *wasm.Module { return indirectTailRewriteModule(r, dynamic) }
		default:
			build = func(r []wasm.ValType) *wasm.Module { return refTailRewriteModule(r, dynamic) }
		}
		before := build(results)
		afterResults := results
		if eliminate {
			afterResults = nil
		}
		after := build(afterResults)
		err := ValidateTailResultRewrite(before, after)
		wantReject := eliminate && dynamic && kind%3 != 0
		if wantReject && err == nil {
			t.Fatal("dynamic result rewrite unexpectedly accepted")
		}
		if !wantReject && err != nil {
			t.Fatalf("known/no-op result rewrite rejected: %v", err)
		}
	})
}
