//go:build linux && amd64 && !tinygo

package amd64

import (
	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestExplicitPluginCPUFeatures(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, tc := range []struct {
			name     string
			selected shared.AMD64Features
			required plugincodegen.Features
			accept   bool
		}{
			{"missing-avx2", shared.AMD64ModernBaseline, plugincodegen.FeatureAVX2, false},
			{"avx2", shared.AMD64ModernBaseline | shared.AMD64AVX2, plugincodegen.FeatureAVX2, true},
			{"missing-avx-state", shared.AMD64AVX2, plugincodegen.FeatureAVX2, false},
			{"missing-avx512", shared.AMD64ModernBaseline, plugincodegen.FeatureAVX512, false},
			{"avx512", shared.AMD64KnownFeatures, plugincodegen.FeatureAVX512, true},
			{"unknown", shared.AMD64KnownFeatures, 1 << 20, false},
			{"ordinary", shared.AMD64ModernBaseline, 0, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := hostSyncModule(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0, 0x10, 0, 0x0b})
				called := false
				lowering := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Features: tc.required, Emit: func(ctx plugincodegen.Context) error {
					called = true
					r := ctx.AllocGP()
					ctx.Encoder().MovImm32(r, 1)
					return ctx.OutputI32(r)
				}}
				cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, AMD64FeaturesSet: true, AMD64Features: tc.selected, CustomInstructions: map[uint32]CustomInstruction{0: {Codegen: lowering, ResultWidth: 32}}})
				if cm != nil && cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if (err == nil) != tc.accept || called != tc.accept {
					t.Fatalf("accepted=%v emitter=%v want=%v: %v", err == nil, called, tc.accept, err)
				}
				if tc.accept && tc.required&plugincodegen.FeatureAVX2 != 0 && !cm.RequiresAVX2 {
					t.Fatal("lost AVX2 artifact requirement")
				}
			})
		}
	}
}
