//go:build arm64 && wago_regalloccheck && !tinygo && !wago_profile

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

// Compile and inspect only; never invoke the guest loop or its native bytes.
func TestSourceLoopFallbackCoverage(t *testing.T) {
	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprintf("workers%d", workers), func(t *testing.T) {
			body := []byte{3, 0x40, 0x20, 1, 0x20, 0, 0x21, 1, 0x21, 0, 0x20, 2, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}}, Code: []wasm.Func{{BodyBytes: body}, {BodyBytes: body}}}
			var v wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &v); err != nil {
				t.Fatal(err)
			}
			opts := codegen.SourceOptions(codegen.Options{}, m, &v, wasm.ValidationFeatures{})
			var reports []codegen.SourceReport
			codegen.SetSourceReporter(opts, m, func(r codegen.SourceReport) { reports = append(reports, r) })
			cm, err := CompileModuleWith(m, CompileOptions{Codegen: opts, Workers: workers, DeferCodeMapping: workers != 1})
			if err != nil {
				t.Fatal(err)
			}
			if cm != nil && cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if len(reports) != 2 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].Result.Verdict != regalloccheck.Verified {
				t.Fatalf("reports=%+v code=%x", reports, cm.Code)
			}
		})
	}
}

func TestSourceLoopNativeObserverPanicCleanup(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{{BodyBytes: []byte{3, 0x40, 0x20, 1, 0x20, 0, 0x21, 1, 0x21, 0, 0x20, 2, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}}}}
	var analysis wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
		t.Fatal(err)
	}
	cg := codegen.SourceOptions(codegen.Options{}, m, &analysis, wasm.ValidationFeatures{})
	var reports []codegen.SourceReport
	codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
	sc := newCompileScratch(0)
	shared.SetSourceContext(&sc.scalar, codegen.SourceContextFor(cg, m))
	defer sc.scalar.FinishWorker()
	ft, _ := m.LocalFuncType(0)
	f := &fn{a: sc.asm, sc: sc, m: m, ft: ft, nParams: 3, nLocals: 3, singleRegResult: true, skipFence: true}
	writes, effects := 0, 0
	f.a.ObserveGPWrites(func(uint32) { writes++ })
	f.a.ObserveRegalloc(func(regalloccheck.Effect) { effects++ })
	checkSourceBegin(f, false)
	if f.sourceBranch == nil {
		t.Fatal("source observer unavailable")
	}
	leaf := f.sourceBranch
	var failure any
	func() {
		defer func() { failure = recover() }()
		defer f.checkEndLifetimes()
		f.checkImmutable(X9, false, 4)
		f.a.MovReg32(X9, X0)
	}()
	if failure == nil || !strings.Contains(failure.(string), "immutable GP reservation overwritten") {
		t.Fatalf("panic changed: %v", failure)
	}
	if f.sourceBranch != nil || f.sourceRestore != nil || leaf.Verify(nil, true).Verdict == regalloccheck.Verified {
		t.Fatal("panic retained proof")
	}
	f.checkEndLifetimes()
	beforeWrites, beforeEffects := writes, effects
	f.a.MovReg32(X9, X0)
	if writes != beforeWrites+1 || effects != beforeEffects+1 {
		t.Fatal("previous observers not restored exactly once")
	}
	codegen.CloseSourceContext(cg, m)
	if len(reports) != 1 || reports[0].Result.Verdict != regalloccheck.Inconclusive {
		t.Fatalf("panic result=%+v", reports)
	}
}
