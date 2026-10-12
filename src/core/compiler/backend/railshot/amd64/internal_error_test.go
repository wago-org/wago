//go:build amd64

package amd64

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// An inconsistent local-count hint is an internal invariant violation, not a
// Wasm rejection. Use a bounds panic at the real recovery boundary so this
// fixture does not need an operating-system access violation.
func TestCompilePanicHasInternalClassification(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "")
	m := mod1(t, []wasm.ValType{wasm.I32}, nil, []byte{0, 0x0b})
	hints := &funcHintView{} // zero local count conflicts with the one parameter
	for _, index := range []int{0, 1} {
		_, _, _, err := compileFuncAttempt(m, nil, index,
			false, false, false, false, false,
			nil, hints, nil, nil, false, 0,
			false, false, false, false,
			nil, nil, nil, false, inlineTargetTable{}, newCompileScratch(minStackArenaCap))
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
		if !errors.As(err, &ice) || ice.Backend != "amd64" || ice.FunctionIndex != 0 || ice.WasmOffset != -1 || !errors.As(err, &cause) {
			t.Fatalf("typed context or original runtime panic lost: %#v / %v", ice, err)
		}
		if !strings.Contains(cause.Error(), "index out of range") {
			t.Fatalf("fixture must use a bounds panic: %v", cause)
		}
	}
}

func TestCompileDebugPanicRethrowsOriginal(t *testing.T) {
	t.Setenv("WAGO_DEBUG_PANIC", "1")
	m := mod1(t, []wasm.ValType{wasm.I32}, nil, []byte{0, 0x0b})
	hints := &funcHintView{} // zero local count conflicts with the one parameter
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("debug mode swallowed the compiler panic")
		} else if cause, ok := recovered.(runtime.Error); !ok {
			t.Errorf("debug mode changed panic value: %T", recovered)
		} else if !strings.Contains(cause.Error(), "index out of range") {
			t.Errorf("fixture must use a bounds panic: %v", cause)
		}
	}()
	_, _, _, _ = compileFuncAttempt(m, nil, 0,
		false, false, false, false, false,
		nil, hints, nil, nil, false, 0,
		false, false, false, false,
		nil, nil, nil, false, inlineTargetTable{}, newCompileScratch(minStackArenaCap))
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
