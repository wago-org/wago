//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

// The existing WAGO_SHARED_SCALAR=0 mode selects real fallback lowering. Default
// shared admission is unchanged and has a separate independent-source outcome.
func TestSourceBranchNativeFallbackCoverage(t *testing.T) {
	for _, workers := range []int{1, 2} {
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x0b, 0x0b})
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, m.Code[0])
		var analysis wasm.ValidatedModuleAnalysis
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
			t.Fatal(err)
		}
		cg := codegen.SourceOptions(codegen.Options{}, m, &analysis, wasm.ValidationFeatures{})
		var reports []codegen.SourceReport
		codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
		cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1})
		if cm != nil && cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		want := regalloccheck.Verified
		if sharedScalarEnabled {
			want = regalloccheck.Inconclusive
		}
		if len(reports) != 2 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].Result.Verdict != want {
			t.Fatalf("workers%d shared=%v reports=%+v code=%x", workers, sharedScalarEnabled, reports, cm.Code)
		}
	}
}

func TestSourceBranchNativeObserverPanicCleanup(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x0b, 0x0b})
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
	f := &fn{a: sc.asm, sc: sc, m: m, ft: ft, nParams: 2, nLocals: 2, singleRegResult: true, skipFence: true}
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
		f.checkImmutable(R9, false, 4)
		f.a.MovRegReg32(R9, RAX)
	}()
	if failure == nil || !strings.Contains(failure.(string), "immutable GP reservation overwritten") {
		t.Fatalf("panic changed: %v", failure)
	}
	if f.sourceBranch != nil || f.sourceRestore != nil || leaf.Verify(nil).Verdict == regalloccheck.Verified {
		t.Fatal("panic retained proof")
	}
	f.checkEndLifetimes()
	beforeWrites, beforeEffects := writes, effects
	f.a.MovRegReg32(R9, RAX)
	if writes != beforeWrites+1 || effects != beforeEffects+1 {
		t.Fatal("previous observers not restored exactly once")
	}
	codegen.CloseSourceContext(cg, m)
	if len(reports) != 1 || reports[0].Result.Verdict != regalloccheck.Inconclusive {
		t.Fatalf("panic result=%+v", reports)
	}
}

func TestSourceBranchDispatchPreservesLeafResourceFailure(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x6a, 0x0b})
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
	exhausted := false
	for i := 0; i < 1024; i++ {
		leaf := shared.BeginSourceLeaf(&sc.scalar, m, 0, true)
		if leaf == nil {
			exhausted = true
			break
		}
		leaf.Close()
	}
	if !exhausted {
		t.Fatal("storage quota did not exhaust")
	}
	ft, _ := m.LocalFuncType(0)
	f := &fn{a: sc.asm, sc: sc, m: m, ft: ft, nParams: 2, nLocals: 2, singleRegResult: true, skipFence: true}
	checkSourceBegin(f, false)
	f.checkEndLifetimes()
	sc.scalar.FinishWorker()
	codegen.CloseSourceContext(cg, m)
	if len(reports) != 1 || reports[0].Result.Reason != regalloccheck.ResourceLimit || !strings.Contains(reports[0].Result.Message, "storage") {
		t.Fatalf("resource report overwritten: %+v", reports)
	}
}
