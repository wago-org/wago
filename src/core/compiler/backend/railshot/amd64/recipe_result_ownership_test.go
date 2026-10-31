//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestRecipeResultSurvivesFixedRegisterShift(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), localSlot: []uint32{0}, nLocalSlots: 1, globalCellReg: regNone, memSizeReg: regNone, policy: currentCodegenPolicy()}
	f.a.Push(RCX)
	f.a.SubRsp(256)
	f.a.MovImm32(RDI, 2)
	f.a.Store32(RSP, f.localAddr(0), RDI)
	// Reuse input zero, requiring the non-stack-compatible recipe path.
	recipe := CustomInstruction{Nodes: []CustomInstructionNode{
		{Op: CustomInstructionInput, Input: 0, Width: 32},
		{Op: CustomInstructionAdd, A: 0, B: 0, Width: 32},
	}, Output: 1, ResultWidth: 32}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 6})
	// Force the recipe result into the subsequent shift's fixed count register.
	for _, r := range gpAlloc {
		if r != RCX {
			f.reserved = f.reserved.union(maskOf(r))
		}
	}
	if err := f.emitCustomInstruction(recipe, &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}); err != nil {
		t.Fatal(err)
	}
	f.reserved = 0
	if result := f.s.back(); result.st.kind != stReg || result.st.reg != RCX {
		t.Fatalf("recipe result storage = %+v, want RCX", result.st)
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 10})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.pushBinOp(opShl, mtI32)
	f.materialize(f.s.back())
	f.pushBinOp(opAdd, mtI32)
	r := f.materialize(f.s.back())
	f.a.MovReg64(RAX, r)
	if f.maxSpill*8+f.nLocalSlots*8 > 256 {
		t.Fatal("test frame too small")
	}
	f.a.AddRsp(256)
	f.a.Pop(RCX)
	f.a.Store64(RCX, 0, RAX)
	f.a.Ret()
	if got := runCompiledAmd64u(t, &encoder.CompiledModule{Code: f.a.B, Entry: []int{0}}); got != 52 {
		t.Fatalf("got %d, want 52", got)
	}
}
