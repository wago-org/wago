//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCallResultRegisterOnlyAtImmediateReturn(t *testing.T) {
	savedInline := inlineEnabled
	inlineEnabled = false
	defer func() { inlineEnabled = savedInline }()

	i32 := []wasm.ValType{wasm.I32}
	callee := funcDef{params: i32, results: i32, body: []byte{0x00, 0x20, 0x00, 0x0b}}

	tail := modFuncs(t,
		funcDef{params: i32, results: i32, body: []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		callee,
	)
	if got := compileWithStats(t, tail, false).Funcs[0].Peephole["call-result-register"]; got != 1 {
		t.Fatalf("immediately returned call-result register = %d, want 1", got)
	}

	nonTail := modFuncs(t,
		funcDef{params: i32, results: i32, body: []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x41, 0x01, 0x6a, 0x0b}},
		callee,
	)
	if got := compileWithStats(t, nonTail, false).Funcs[0].Peephole["call-result-register"]; got != 0 {
		t.Fatalf("non-tail call-result register = %d, want 0", got)
	}
}
