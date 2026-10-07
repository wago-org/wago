package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestLegacyDeclarativeDescriptorFactsPreserveFeatures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		edit         func(*wasm.Module)
		wantRef      bool
		wantFeatures CoreFeatures
	}{
		{name: "declaration", wantFeatures: CoreFeatureBulkMemoryOperations},
		{name: "active", edit: func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive }, wantRef: true},
		{name: "passive", edit: func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive }, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations},
		{name: "body bytes", edit: func(m *wasm.Module) { m.Code = []wasm.Func{{BodyBytes: []byte{0xd2, 0, 0x1a, 0x0b}}} }, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes},
		// The programmatic-body fallback records descriptor/segment facts but
		// historically only infers SIMD feature bits. Preserve that contract;
		// byte-backed bodies above exercise exact executable feature accounting.
		{name: "body instructions", edit: func(m *wasm.Module) {
			m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc}}}}}
		}, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations},
		{name: "global bytes", edit: func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}}
		}, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes},
		{name: "global instructions", edit: func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc}}}}}
		}, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes},
		{name: "expression declaration", edit: func(m *wasm.Module) {
			m.Elements[0].Kind = wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 0, 0x0b}}}}
		}, wantRef: true, wantFeatures: CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}}
			if tc.edit != nil {
				tc.edit(m)
			}
			wantFacts, err := frontend.AnalyzeModuleFacts(m)
			if err != nil {
				t.Fatal(err)
			}
			requirements := analyzeModuleRequirements(m)
			if !reflect.DeepEqual(requirements.moduleFacts, wantFacts) {
				t.Fatalf("fused facts = %+v, frontend = %+v", requirements.moduleFacts, wantFacts)
			}
			if requirements.moduleFacts.UsesRefFunc != tc.wantRef || frontend.RequiresFuncRefDescriptorsFromFacts(m, requirements.moduleFacts) != tc.wantRef {
				t.Fatalf("descriptor demand = %+v, want %v", requirements.moduleFacts, tc.wantRef)
			}
			if requirements.features != tc.wantFeatures {
				t.Fatalf("features = %#x, want %#x", requirements.features, tc.wantFeatures)
			}
		})
	}
}

func TestLegacyDeclarativeValidatedDescriptorFacts(t *testing.T) {
	for _, refBody := range []bool{false, true} {
		body := []byte{0x0b}
		if refBody {
			body = []byte{0xd2, 0, 0x1a, 0x0b}
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		var analysis wasm.ValidatedModuleAnalysis
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
			t.Fatal(err)
		}
		fused := analyzeModuleRequirementsWithValidation(m, &analysis)
		ordinary := analyzeModuleRequirements(m)
		if !reflect.DeepEqual(fused, ordinary) {
			t.Fatalf("validated requirements = %+v, ordinary = %+v", fused, ordinary)
		}
		if frontend.RequiresFuncRefDescriptorsFromFacts(m, fused.moduleFacts) != refBody {
			t.Fatalf("body ref.func=%v, facts=%+v", refBody, fused.moduleFacts)
		}
		want := CoreFeatureBulkMemoryOperations
		if refBody {
			want |= CoreFeatureReferenceTypes
		}
		if fused.features != want {
			t.Fatalf("features=%#x, want %#x", fused.features, want)
		}
	}
}
