//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSourceIntegerFinalRealPrivateEntry(t *testing.T) {
	old := sharedScalarEnabled
	sharedScalarEnabled = false
	t.Cleanup(func() { sharedScalarEnabled = old })
	for _, workers := range []int{1, 2} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			for _, op := range []byte{0x71, 0x72, 0x73} {
				second := byte(0x73)
				if op == 0x73 {
					second = 0x6a
				}
				if typ == wasm.I64 {
					op += 0x12
					second += 0x12
				}
				m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x20, 2, second, 0x0b})
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
				m.Code = append(m.Code, m.Code[0])
				m.Code[0].BodyBytes = []byte{0x20, 2, 0x0b}
				m.Exports = nil
				var a wasm.ValidatedModuleAnalysis
				if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
					t.Fatal(err)
				}
				cg := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
				var reports []codegen.SourceReport
				codegen.SetSourceReporter(cg, m, func(r codegen.SourceReport) { reports = append(reports, r) })
				cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1})
				if cm != nil && cm.CodeImage != nil {
					t.Cleanup(func() { cm.CodeImage.Close() })
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(reports) != 2 || reports[0].Result.Verdict != regalloccheck.Inconclusive || reports[1].Result.Verdict != regalloccheck.Verified {
					t.Fatalf("workers=%d type=%v op=%x reports=%+v entries=%v internal=%v", workers, typ, op, reports, cm.Entry, cm.InternalEntry)
				}
			}
		}
	}
}
