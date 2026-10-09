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

// These compile real fallback entries without changing shared admission. No
// bytes from fault-injection checks are ever executed.
func TestSourceLeafNativeFallbackCoverage(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			for _, op := range []byte{0x6a, 0x71, 0x72, 0x73} {
				if typ == wasm.I64 {
					op += 0x12
				}
				m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x0b})
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
				m.Code = append(m.Code, m.Code[0])
				var analysis wasm.ValidatedModuleAnalysis
				if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
					t.Fatal(err)
				}
				cg := codegen.SourceOptions(codegen.Options{}, m, &analysis, wasm.ValidationFeatures{})
				var reports []codegen.SourceReport
				if !codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) }) {
					t.Fatal("reporter unavailable")
				}
				cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1})
				if cm != nil && cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(reports) != 2 || reports[0].LocalFunction != 0 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].LocalFunction != 1 || reports[1].Result.Verdict != regalloccheck.Verified {
					t.Fatalf("workers=%d type=%v op=%x reports=%+v code=%x", workers, typ, op, reports, cm.Code)
				}
			}
		}
	}
}

func TestSourceLeafNativeObserverPanicCleanup(t *testing.T) {
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
	ft, _ := m.LocalFuncType(0)
	f := &fn{a: sc.asm, sc: sc, m: m, ft: ft, nParams: 2, nLocals: 2, singleRegResult: true, skipFence: true}
	writes, effects := 0, 0
	f.a.ObserveGPWrites(func(uint32) { writes++ })
	f.a.ObserveRegalloc(func(regalloccheck.Effect) { effects++ })
	checkSourceBegin(f, false)
	if f.sourceLeaf == nil {
		t.Fatal("source observer unavailable")
	}
	leaf := f.sourceLeaf
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
	if f.sourceLeaf != nil || f.sourceRestore != nil || leaf.Verify(nil, false).Verdict == regalloccheck.Verified {
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

func TestSourceLeafUnsupportedNativeCoverage(t *testing.T) {
	for _, mode := range []string{"compaction", "interruptible", "explicit return", "source absent"} {
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x6a, 0x0b})
		if mode == "explicit return" {
			m = mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 1, 0x6a, 0x0f, 0x0b})
		}
		m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		m.Code = append(m.Code, m.Code[0])
		var analysis wasm.ValidatedModuleAnalysis
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
			t.Fatal(err)
		}
		cg := codegen.SourceOptions(codegen.Options{}, m, &analysis, wasm.ValidationFeatures{})
		var reports []codegen.SourceReport
		codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
		opts := CompileOptions{Codegen: cg, CompactNative: mode == "compaction", Interruptible: mode == "interruptible", DeferCodeMapping: true}
		if mode == "source absent" {
			opts.Codegen = codegen.Options{}
		}
		cm, err := CompileModuleWith(m, opts)
		if cm != nil && cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if err != nil {
			t.Fatalf("mode=%s: %v", mode, err)
		}
		if mode == "source absent" {
			if len(reports) != 0 {
				t.Fatal("source-free backend produced report")
			}
			codegen.CloseSourceContext(cg, m)
		}
		if len(reports) != 2 || reports[1].Result.Verdict != regalloccheck.Inconclusive {
			t.Fatalf("mode=%s reports=%+v", mode, reports)
		}
	}
}
