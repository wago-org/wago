//go:build arm64 && wago_regalloccheck

package arm64

import (
	"errors"
	plugincodegen "github.com/wago-org/wago/codegen/arm64"
	"github.com/wago-org/wago/src/core/plugins"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSourceContextSurvivesEarlyFunctionStateFailure(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "")
	m := mod1(t, nil, nil, []byte{0, 0x0b})
	var a wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
		t.Fatal(err)
	}
	var opts codegen.Options
	if !codegen.AttachSourceContext(&opts, m, &a, wasm.ValidationFeatures{}) {
		t.Fatal("context unavailable")
	}
	sc := newCompileScratch(0)
	shared.SetSourceContext(&sc.scalar, codegen.SourceContextFor(opts, m))
	defer sc.scalar.FinishWorker()
	for _, index := range []int{0, 1} {
		_, _, _, err := compileFuncAttempt(m, nil, index,
			false, false, false, false,
			nil, nil, immutableTableHint{}, nil, false, 0,
			false, false, false, nil, nil, nil,
			false, inlineTargetTable{}, nil, CodegenPolicy{}, sc)
		if err == nil || (index == 0 && !strings.Contains(err.Error(), "internal compiler error")) {
			t.Fatalf("expected early failure, got %v", err)
		}
		token := shared.BeginSourceAttempt(&sc.scalar, m, 0)
		if token == nil {
			t.Fatal("early unwind lost context")
		}
		shared.EndSourceAttempt(token)
	}
}

func TestSourceContextCompileClosesBorrowedFacts(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, failure := range []bool{false, true} {
			m := mod1(t, nil, nil, []byte{0, 0x0b})
			for i := 1; i < 8; i++ {
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
				m.Code = append(m.Code, m.Code[0])
			}
			var a wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
				t.Fatal(err)
			}
			cg := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
			retained := codegen.SourceContextFor(cg, m)
			opts := CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1}
			if failure {
				opts.SyncHostSlots = -1
			}
			cm, err := CompileModuleWith(m, opts)
			if (err != nil) != failure {
				t.Fatalf("workers=%d failure=%v: %v", workers, failure, err)
			}
			if cm != nil && cm.CodeImage != nil {
				if err := cm.CodeImage.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, ok := codegen.ValidatedSourceContext(retained, m); ok {
				t.Fatal("completed compilation retained validation context")
			}
			if !a.ValidFor(m) {
				t.Fatal("cleanup invalidated borrowed analysis")
			}
		}
	}
}

// Compile-only: the callbacks emit no result, and faulty output is never run.
func TestSourceContextRetainedPluginCannotRetainFacts(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, failure := range []bool{false, true} {
			m := managedPluginBoundaryModuleARM64(t)
			for i := 1; i < 8; i++ {
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
				m.Code = append(m.Code, m.Code[0])
			}
			var a wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &a); err != nil {
				t.Fatal(err)
			}
			cg := codegen.SourceOptions(codegen.Options{}, m, &a, wasm.ValidationFeatures{})
			retained := make(chan plugincodegen.ManagedContext, 8)
			lowering := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityManaged,
				Managed: func(ctx plugincodegen.ManagedContext) error {
					native := (*pluginARM64Context)(ctx.(*managedPluginARM64Context))
					token := shared.BeginSourceAttempt(&native.f.sc.scalar, m, int(native.f.traceFuncIdx)-m.ImportedFuncCount())
					if token == nil {
						return errors.New("worker source context missing")
					}
					shared.EndSourceAttempt(token)
					retained <- ctx
					if failure {
						return errors.New("test lowering stopped")
					}
					return nil
				},
			}
			cm, err := CompileModuleWith(m, CompileOptions{Codegen: cg, Workers: workers, DeferCodeMapping: workers != 1, CustomInstructions: map[uint32]plugins.Instruction{0: {Codegen: lowering}}})
			if (err != nil) != failure {
				t.Fatalf("workers=%d failure=%v: %v", workers, failure, err)
			}
			if cm != nil && cm.CodeImage != nil {
				if err := cm.CodeImage.Close(); err != nil {
					t.Fatal(err)
				}
			}
			close(retained)
			if len(retained) == 0 {
				t.Fatalf("lowering context was not retained: %v", err)
			}
			for ctx := range retained {
				native := (*pluginARM64Context)(ctx.(*managedPluginARM64Context))
				token := shared.BeginSourceAttempt(&native.f.sc.scalar, m, 0)
				if token != nil {
					shared.EndSourceAttempt(token)
					t.Fatal("retained scratch retained validation facts")
				}
			}
			if !a.ValidFor(m) {
				t.Fatal("cleanup changed borrowed analysis")
			}
		}
	}
}
