//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
