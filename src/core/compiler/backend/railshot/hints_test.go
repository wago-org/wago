//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBodyMemoryCallHints(t *testing.T) {
	callOnly := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrCall},
	}}
	if !bodyHasCall(callOnly) {
		t.Fatal("call-only body should report a call")
	}
	if bodyTouchesMemory(callOnly) {
		t.Fatal("call-only body should not report memory")
	}
	if !bodyUseStackReg(callOnly, true) {
		t.Fatal("call-only body should use STACK_REG")
	}

	callMemory := wasm.Expr{Instrs: []wasm.Instruction{
		{Kind: wasm.InstrLocalGet},
		{Kind: wasm.InstrI32Load},
		{Kind: wasm.InstrCall},
	}}
	if !bodyHasCall(callMemory) {
		t.Fatal("call+memory body should report a call")
	}
	if !bodyTouchesMemory(callMemory) {
		t.Fatal("call+memory body should report memory")
	}
	// Memory-touching call functions use the eager spill/reload model in BOTH
	// modes: STACK_REG's lazy state desyncs against the explicit-bounds memAddr
	// scratch allocation (see compile.go).
	if bodyUseStackReg(callMemory, true) {
		t.Fatal("guard-mode call+memory body should use eager spill/reload")
	}
	if bodyUseStackReg(callMemory, false) {
		t.Fatal("explicit-bounds call+memory body should use eager spill/reload")
	}
	callMemory.Instrs[2].Index = 7
	if !bodyCalls(callMemory, 7) {
		t.Fatal("call+memory body should report the matching call target")
	}
	if bodyCalls(callMemory, 8) {
		t.Fatal("call+memory body should not report a different call target")
	}
}
