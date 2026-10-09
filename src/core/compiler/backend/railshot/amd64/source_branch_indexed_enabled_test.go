//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// This test compiles private bodies; it never executes their native image.
func TestSourceBranchIndexedFallbackCoverage(t *testing.T) {
	for c := byte(0); c < 2; c++ {
		for then := byte(0); then < 2; then++ {
			for otherwise := byte(0); otherwise < 2; otherwise++ {
				for _, workers := range []int{1, 2} {
					t.Run(fmt.Sprintf("%d%d%d/workers%d", c, then, otherwise, workers), func(t *testing.T) {
						body := []byte{0x20, c, 0x04, 0x7f, 0x20, then, 0x05, 0x20, otherwise, 0x0b, 0x0b}
						m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}}, FuncTypes: []wasm.TypeIdx{{}, {}}, Code: []wasm.Func{{BodyBytes: body}, {BodyBytes: body}}}
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
						want := regalloccheck.Verified
						if sharedScalarEnabled {
							want = regalloccheck.Inconclusive
						}
						if len(reports) != 2 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].Result.Verdict != want {
							t.Fatalf("reports=%+v code=%x", reports, cm.Code)
						}
					})
				}
			}
		}
	}
}
