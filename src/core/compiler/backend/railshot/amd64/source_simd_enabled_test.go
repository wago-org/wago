//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A complete original-source contract alone cannot certify emitted SIMD.
// This compiles valid guest code but never executes it or modifies its bytes.
func TestSourceSIMDNativeRequiresPhysicalRecipe(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, body := range [][]byte{
			{0, 0x20, 0, 0x0b},
			{0, 0x20, 0, 0xfd, 0x4d, 0x0b}, // v128.not
		} {
			m := mod1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, body)
			m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
			m.Code = append(m.Code, m.Code[0])
			var analysis wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			l, result, err := wasm.BuildSourceLedger(m, &analysis, 1, wasm.ValidationFeatures{}, wasm.SourceLedgerLimits{})
			if err != nil || result.Coverage != wasm.SourceContractsComplete {
				t.Fatal("vector source contract incomplete", result, err)
			}
			l.Close()
			cg := codegen.SourceOptions(codegen.Options{}, m, &analysis, wasm.ValidationFeatures{})
			var reports []codegen.SourceReport
			codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
			cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1})
			if cm != nil && cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(reports) != 2 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].Result.Verdict != regalloccheck.Inconclusive {
				t.Fatalf("source contracts became machine proof: %+v", reports)
			}
		}
	}
}
