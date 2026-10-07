//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSourceCallFinalStructuredFailure(t *testing.T) {
	if sharedScalarEnabled {
		t.Skip("exact final identity-leaf recipe requires the fallback compiler")
	}
	cg, m, _ := sourceCallNativeModule(t)
	defer codegen.CloseSourceContext(cg, m)
	cm, err := compileModuleWith(m, CompileOptions{Codegen: cg, Optimizations: map[string]bool{"inline": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	cm.InternalEntry[2]++ // Inspect the bad map only; no native byte executes.
	err = checkSourceCallsFinal(m, CompileOptions{Codegen: cg}, cm)
	var failure *sourceCallVerificationError
	if !errors.As(err, &failure) || failure.Result.Verdict != regalloccheck.Rejected {
		t.Fatalf("error=%v", err)
	}
}

func sourceCallNativeModule(t *testing.T) (codegen.Options, *wasm.Module, *[]codegen.SourceReport) {
	t.Helper()
	caller := []byte{0x20, 0, 0x20, 1, 0x10, 2, 0x0b}
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}, {}}, Code: []wasm.Func{{BodyBytes: caller}, {BodyBytes: caller}, {BodyBytes: []byte{0x20, 0, 0x0b}}}}
	for i := range m.Code {
		m.Code[i].LocalDeclBytes = 1
	}
	var v wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &v); err != nil {
		t.Fatal(err)
	}
	cg := codegen.SourceOptions(codegen.Options{}, m, &v, wasm.ValidationFeatures{})
	var reports []codegen.SourceReport
	codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
	return cg, m, &reports
}

// Real final module compilation only. Generated native code is never invoked.
func TestSourceCallFinalFallbackCoverage(t *testing.T) {
	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			cg, m, reports := sourceCallNativeModule(t)
			cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1, Optimizations: map[string]bool{"inline": false}})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			want := regalloccheck.Verified
			if sharedScalarEnabled && len(*reports) == 3 && (*reports)[1].Result.Verdict == regalloccheck.Inconclusive {
				// Final-byte admission is independent of the producing path.
				// Other shared shapes remain inconclusive; a fully decoded
				// identical recipe may verify. Forced fallback must verify.
				want = regalloccheck.Inconclusive
			}
			if len(*reports) != 3 || (*reports)[0].Result.Verdict != regalloccheck.Inconclusive || (*reports)[1].Result.Verdict != want || (*reports)[2].Result.Verdict != want {
				t.Fatalf("reports=%+v code=%x", *reports, cm.Code)
			}
			if codegen.SourceContextFor(cg, m) != nil {
				t.Fatal("context retained")
			}
		})
	}
}
