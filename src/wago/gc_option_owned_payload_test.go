//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
)

func TestGCOptionOwnedPayloadValidation(t *testing.T) {
	source, _ := iteratorCarrierFixture(16, 0, 0)
	m, decodeErr := wasm.DecodeModule(source)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	f := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).frontendFeatures()
	policy := wasm.ValidationFeatures{CompactImports: f.CompactImports || f.MultiMemory, MultiMemory: f.MultiMemory, ExtendedConstGlobals: f.ExtendedConstGlobals, GCConstExpr: f.GCStructProducts || f.GCArrayProducts || f.GCI31Products}
	owner, err := wasm.NewOwnedBodySource(m, policy, 1, wasm.ValidationLimits{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, original, err := owner.Snapshot()
	if err != nil || !original.ValidFor(snapshot) {
		t.Fatal(err)
	}
	view, err := buildGCOptionBodyView(snapshot)
	if err != nil || view == nil {
		t.Fatal(err)
	}
	var edits []wasm.BodyEdit
	for global := range view.Origins {
		local := int(global) - snapshot.ImportedFuncCount()
		c := view.Module.Code[local]
		edits = append(edits, wasm.BodyEdit{LocalFunction: uint32(local), BodyBytes: c.BodyBytes, Locals: c.Locals})
	}
	private, a, err := owner.Apply(edits, policy, 1, wasm.ValidationLimits{})
	if err != nil || !a.ValidFor(private) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(private.Code, view.Module.Code) {
		t.Fatal("owned module changed private bytecode or local declarations")
	}
	var full wasm.ValidatedModuleAnalysis
	if err = wasm.ValidateModuleWithAnalysis(private, policy, 1, wasm.ValidationLimits{}, &full); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < a.FuncCount(); n++ {
		if a.Func(n) != full.Func(n) {
			t.Fatalf("function%d facts differ", n)
		}
	}
	if a.Flags() != full.Flags() || a.DataStateCount() != full.DataStateCount() || a.ElemStateCount() != full.ElemStateCount() {
		t.Fatal("aggregate facts differ")
	}
	t.Logf("fresh complete facts match full validation: %d edits, %d functions", len(edits), a.FuncCount())
}
