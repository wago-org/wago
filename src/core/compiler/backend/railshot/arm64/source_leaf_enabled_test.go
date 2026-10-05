//go:build arm64 && wago_regalloccheck && !tinygo && !wago_profile

package arm64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
