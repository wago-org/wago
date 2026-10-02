//go:build arm64

package arm64

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
)

// A missing compiler hint is an internal invariant violation, not a Wasm
// rejection. Inject it at the real recovery boundary without relying on a
// particular unfixed code-generation bug or adding a production test hook.
func TestCompilePanicHasInternalClassification(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "")
	m := mod1(t, nil, nil, []byte{0, 0x0b})
	for _, index := range []int{0, 1} {
		_, _, _, err := compileFuncAttempt(m, nil, index,
			false, false, false, false,
			nil, nil, immutableTableHint{}, nil, false, 0,
			false, false, false, nil, nil, nil,
			false, inlineTargetTable{}, nil, CodegenPolicy{}, nil)
		if index == 1 {
			if err == nil || strings.Contains(err.Error(), "internal compiler error") {
				t.Fatalf("ordinary unknown-function rejection = %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "internal compiler error") || !strings.Contains(err.Error(), "function 0") {
			t.Fatalf("recovered invariant failure = %v; want distinct ICE with function context", err)
		}
		var ice *railcore.InternalCompilerError
		var cause runtime.Error
		if !errors.As(err, &ice) || ice.Backend != "arm64" || ice.FunctionIndex != 0 || ice.WasmOffset != -1 || !errors.As(err, &cause) {
			t.Fatalf("typed context or original runtime panic lost: %#v / %v", ice, err)
		}
	}
}

func TestCompileDebugPanicRethrowsOriginal(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "1")
	m := mod1(t, nil, nil, []byte{0, 0x0b})
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("debug mode swallowed the compiler panic")
		} else if _, ok := recovered.(runtime.Error); !ok {
			t.Errorf("debug mode changed panic value: %T", recovered)
		}
	}()
	_, _, _, _ = compileFuncAttempt(m, nil, 0,
		false, false, false, false,
		nil, nil, immutableTableHint{}, nil, false, 0,
		false, false, false, nil, nil, nil,
		false, inlineTargetTable{}, nil, CodegenPolicy{}, nil)
}

func TestRegisterExhaustionDiagnosticKeepsCause(t *testing.T) {
	original := regExhausted{class: "GP"}
	f := fn{traceFuncIdx: 4, wasmPC: 23}
	err := f.compilerPanicError(nil, 0, original)
	var recovered regExhausted
	if !errors.As(err, &recovered) || recovered != original || err.FunctionIndex != 4 || err.WasmOffset != 23 || !strings.Contains(err.Error(), "no GP register available") {
		t.Fatalf("register exhaustion lost classification or cause: %v", err)
	}
}
