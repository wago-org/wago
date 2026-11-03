//go:build !wago_standalone && ((linux && amd64) || ((linux || darwin) && arm64))

package wago

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func unwindPreparationFixture(t *testing.T, table, chain bool) (*wasm.Module, *wasm.ValidatedModuleAnalysis) {
	t.Helper()
	imported := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), byte(wasm.ExternFunc), 0x00)
	funcs := [][]byte{wasmtest.ULEB(0)}
	codes := [][]byte{wasmtest.Code([]byte{0x10, 0x00, 0x41, 0x01, 0x6a, 0x0b})}
	if chain {
		funcs = append(funcs, wasmtest.ULEB(0))
		codes = append(codes, wasmtest.Code([]byte{0x10, 0x01, 0x0b}))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imported)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
	}
	if table {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(codes...)))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	analysis := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, analysis); err != nil {
		t.Fatal(err)
	}
	return m, analysis
}

func conservativeUnwindDeclarationFixture(t *testing.T, ref byte, shape string) (*wasm.Module, *wasm.ValidatedModuleAnalysis) {
	t.Helper()
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), byte(wasm.ExternFunc), 0)
	imports := [][]byte{functionImport}
	var declaration []byte
	switch shape {
	case "defined-table":
		declaration = wasmtest.Section(4, wasmtest.Vec([]byte{ref, 0, 0}))
	case "imported-table":
		imports = append(imports, append(append(append(wasmtest.Name("env"), wasmtest.Name("t")...), byte(wasm.ExternTable)), ref, 0, 0))
	case "passive-expr":
		declaration = wasmtest.Section(9, wasmtest.Vec([]byte{5, ref, 0}))
	case "declarative-expr":
		declaration = wasmtest.Section(9, wasmtest.Vec([]byte{7, ref, 0}))
	case "passive-funcs":
		declaration = wasmtest.Section(9, wasmtest.Vec([]byte{1, 0, 0}))
	case "declarative-funcs":
		declaration = wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 0}))
	default:
		t.Fatalf("unknown declaration shape %q", shape)
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		declaration,
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	analysis := new(wasm.ValidatedModuleAnalysis)
	if err = wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, analysis); err != nil {
		t.Fatal(err)
	}
	return m, analysis
}

func TestConservativeUnwindDeclarationMatrix(t *testing.T) {
	shapes := []string{"defined-table", "imported-table", "passive-expr", "declarative-expr", "passive-funcs", "declarative-funcs"}
	testCases := 0
	for _, ref := range []byte{0x70, 0x6c, 0x6f} { // funcref, i31ref, externref
		for _, shape := range shapes {
			if (shape == "passive-funcs" || shape == "declarative-funcs") && ref != 0x70 {
				continue
			}
			testCases++
			t.Run(fmt.Sprintf("%s/ref=%x", shape, ref), func(t *testing.T) {
				m, analysis := conservativeUnwindDeclarationFixture(t, ref, shape)
				eligible := gcFrameCanUseConservativeUnwind(m, analysis)
				if want := ref == 0x70; eligible != want {
					t.Fatalf("eligible=%t, want %t", eligible, want)
				}
				var oldDiagnostic, newDiagnostic string
				old := newGCFrameRootPlanMode(m, true, false, false, &oldDiagnostic, analysis)
				got := newGCFrameRootPlanMode(m, true, false, true, &newDiagnostic, analysis)
				if oldDiagnostic != newDiagnostic {
					t.Fatalf("diagnostic changed: old=%q new=%q", oldDiagnostic, newDiagnostic)
				}
				if ref != 0x70 {
					if !reflect.DeepEqual(old, got) {
						t.Fatalf("fallback plan changed: old=%#v new=%#v", old, got)
					}
					return
				}
				if old == nil || got == nil {
					t.Fatalf("ordinary funcref declaration lost plan: old=%#v new=%#v", old, got)
				}
			})
		}
	}
	if testCases != 14 {
		t.Fatalf("declaration matrix has %d cases, want 14", testCases)
	}
}

func TestConservativeUnwindI31TableRegression(t *testing.T) {
	m, analysis := conservativeUnwindDeclarationFixture(t, 0x6c, "defined-table")
	if gcFrameCanUseConservativeUnwind(m, analysis) {
		t.Fatal("ordinary i31ref table admitted by funcref-only exception")
	}
	var oldDiagnostic, newDiagnostic string
	old := newGCFrameRootPlanMode(m, true, false, false, &oldDiagnostic, analysis)
	got := newGCFrameRootPlanMode(m, true, false, true, &newDiagnostic, analysis)
	if oldDiagnostic != newDiagnostic || !reflect.DeepEqual(old, got) {
		t.Fatalf("i31ref fallback changed: old=%#v (%q), new=%#v (%q)", old, oldDiagnostic, got, newDiagnostic)
	}
}

func TestConservativeUnwindPreparationOldPlanOracle(t *testing.T) {
	for _, tc := range []struct {
		name         string
		table, chain bool
	}{
		{name: "import-plus-one"},
		{name: "unused-funcref-table", table: true},
		{name: "local-call-chain", chain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, analysis := unwindPreparationFixture(t, tc.table, tc.chain)
			if !gcFrameCanUseConservativeUnwind(m, analysis) {
				t.Fatal("fixture was not positively admitted")
			}
			var oldDiagnostic, newDiagnostic string
			old := newGCFrameRootPlan(m, true, false, &oldDiagnostic, analysis)
			candidate := newGCFrameRootPlanMode(m, true, false, true, &newDiagnostic, analysis)
			if old == nil || candidate == nil || oldDiagnostic != "" || newDiagnostic != "" {
				t.Fatalf("old=%v (%q), candidate=%v (%q)", old, oldDiagnostic, candidate, newDiagnostic)
			}
			for i := 0; i < old.FunctionCount(); i++ {
				if p := old.Function(i); p != nil {
					p.UnwindOnly = true
					if len(p.Locals) == 0 {
						p.Locals = nil
					}
				}
				if p := candidate.Function(i); p != nil && len(p.Locals) == 0 {
					p.Locals = nil
				}
			}
			if !reflect.DeepEqual(old, candidate) {
				t.Fatalf("planner oracle differs:\nold=%#v\nnew=%#v", old, candidate)
			}
			for i := 0; i < candidate.FunctionCount(); i++ {
				p := candidate.Function(i)
				if p == nil {
					continue
				}
				if !p.Candidate || !p.Exact || !p.Conservative || !p.UnwindOnly || p.AllocationMaskCount() != 0 || !p.ValidLiveMasks() {
					t.Fatalf("function %d plan = %#v", i, p)
				}
			}
		})
	}
}

func TestConservativeUnwindPreparationFallbackMatrix(t *testing.T) {
	base, _ := unwindPreparationFixture(t, false, false)
	assertFallback := func(t *testing.T, m *wasm.Module, analysis *wasm.ValidatedModuleAnalysis) {
		t.Helper()
		if gcFrameCanUseConservativeUnwind(m, analysis) {
			t.Fatal("unexpected conservative admission")
		}
		var a, b string
		old := newGCFrameRootPlan(m, true, false, &a, analysis)
		got := newGCFrameRootPlanMode(m, true, false, true, &b, analysis)
		if a != b || !reflect.DeepEqual(old, got) {
			t.Fatalf("fallback differs: old=%#v (%q), got=%#v (%q)", old, a, got, b)
		}
	}
	t.Run("absent-analysis", func(t *testing.T) { assertFallback(t, base, nil) })
	t.Run("mismatched-analysis", func(t *testing.T) {
		other, _ := unwindPreparationFixture(t, false, false)
		assertFallback(t, base, func() *wasm.ValidatedModuleAnalysis {
			a := new(wasm.ValidatedModuleAnalysis)
			_ = wasm.ValidateModuleWithAnalysis(other, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a)
			return a
		}())
	})
	t.Run("unreachable-call", func(t *testing.T) {
		m, err := wasm.DecodeModule(wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("f")...), byte(wasm.ExternFunc), 0x00))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x10, 0x00, 0x0b}))),
		))
		if err != nil {
			t.Fatal(err)
		}
		a := new(wasm.ValidatedModuleAnalysis)
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
			t.Fatal(err)
		}
		assertFallback(t, m, a)
	})
}

func TestConservativeUnwindSummaryPositiveAllowlist(t *testing.T) {
	m, analysis := unwindPreparationFixture(t, true, true)
	if got := analysis.Flags() &^ gcFrameUnwindValidatedFlags; got != 0 {
		t.Fatalf("unexpected flags %#x", got)
	}
	if !gcFrameCanUseConservativeUnwind(m, analysis) {
		t.Fatal("numeric declarations and direct calls rejected")
	}
	for _, flag := range []wasm.ValidatedFuncFlags{
		wasm.ValidatedFuncHasControl, wasm.ValidatedFuncHasIndirectCall, wasm.ValidatedFuncHasTailCall,
		wasm.ValidatedFuncUsesSIMD, wasm.ValidatedFuncUsesExceptionHandling, wasm.ValidatedFuncUsesReferenceTypes,
		wasm.ValidatedFuncTouchesGlobal, wasm.ValidatedFuncTouchesMemory, wasm.ValidatedFuncTouchesTable,
		wasm.ValidatedFuncNeedsDetailedAdmission,
	} {
		if flag&gcFrameUnwindValidatedFlags != 0 {
			t.Fatalf("excluded flag %#x is allowlisted", flag)
		}
	}
}

func TestConservativeUnwindValidatedDeclarationFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []byte
	}{
		{"global", wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0, 0x41, 0, 0x0b}))},
		{"memory", wasmtest.Section(5, wasmtest.Vec([]byte{0, 0}))},
		{"imported-start", wasmtest.Section(8, []byte{0})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), byte(wasm.ExternFunc), 0)
			m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), tc.extra, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b})))))
			if err != nil {
				t.Fatal(err)
			}
			a := new(wasm.ValidatedModuleAnalysis)
			if err = wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
				t.Fatal(err)
			}
			if !a.ValidFor(m) {
				t.Fatal("fresh analysis identity invalid")
			}
			var od, nd string
			old := newGCFrameRootPlan(m, true, false, &od, a)
			got := newGCFrameRootPlanMode(m, true, false, true, &nd, a)
			if od != nd || !reflect.DeepEqual(old, got) {
				t.Fatalf("old=%q new=%q", od, nd)
			}
			if tc.name != "imported-start" && gcFrameCanUseConservativeUnwind(m, a) {
				t.Fatal("declaration admitted")
			}
			t.Logf("diagnostic=%q plan-present=%t", nd, got != nil)
		})
	}
}
