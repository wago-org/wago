//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Force an earlier argument into the fixed register of a later calculation.
// This is independent of the local-pinning heuristic that exposed the failure.
func TestRegisterCallPreservesArgumentsAcrossFixedScratch(t *testing.T) {
	for _, mode := range []string{"integer", "mixed", "tail"} {
		for _, prefix := range []bool{false, true} {
			if mode == "tail" && prefix {
				continue
			}
			for _, tc := range []struct {
				name  string
				reg   Reg
				op    wOp
				value int64
			}{
				{"shift", RCX, opShl, 32}, {"divide-rax", RAX, opDivU, 4}, {"divide-rdx", RDX, opDivU, 4},
			} {
				t.Run(fmt.Sprintf("%s/%s/prefix=%v", mode, tc.name, prefix), func(t *testing.T) {
					f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), m: &wasm.Module{}, localSlot: []uint32{0}, nLocalSlots: 1, globalCellReg: regNone, memSizeReg: regNone, policy: currentCodegenPolicy()}
					// The wrapper keeps its result buffer outside the compiler-owned frame.
					f.a.Push(RCX)
					f.a.SubRsp(256)
					f.a.MovImm32(RDI, 2)
					f.a.Store32(RSP, f.localAddr(0), RDI)
					expression := func() {
						f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
						f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
						f.pushBinOp(tc.op, mtI32)
					}
					if prefix {
						expression()
					}
					f.a.MovImm32(tc.reg, 123)
					f.pushReg(tc.reg, mtI32)
					if prefix {
						f.pushValue(storage{kind: stConst, typ: mtI32, cval: 41})
					} else {
						expression()
					}
					ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}
					if mode == "mixed" {
						ft.Params = append(ft.Params, wasm.F64)
						f.pushValue(storage{kind: stConst, typ: mtF64, cval: 0x3ff0000000000000})
					}
					if mode == "tail" {
						f.emitTailRegisterJump(ft, func() { f.a.Add32(RAX, RCX) })
					} else {
						if mode == "mixed" {
							f.emitMixedRegisterCall(0, ft)
						} else {
							f.emitRegisterCall(0, ft, -1)
						}
						f.a.MovReg64(RAX, f.materialize(f.s.back()))
					}
					if f.maxSpill*8+f.nLocalSlots*8 > 256 {
						t.Fatal("test frame too small")
					}
					f.a.AddRsp(256)
					f.a.Pop(RCX)
					f.a.Store64(RCX, 0, RAX)
					f.a.Ret()
					if mode != "tail" {
						target := f.a.Len()
						f.a.Add32(RAX, RCX)
						f.a.Ret()
						if len(f.relocs) != 1 {
							t.Fatalf("relocations: %v", f.relocs)
						}
						at := f.relocs[0].at
						binary.LittleEndian.PutUint32(f.a.B[at:], uint32(int32(target-int(at)-4)))
					}
					want := uint64(123 + tc.value)
					if prefix {
						want = 164
					}
					if got := runCompiledAmd64u(t, &encoder.CompiledModule{Code: f.a.B, Entry: []int{0}}); got != want {
						t.Fatalf("got %d, want %d", got, want)
					}
				})
			}
		}
	}
}

func TestCallExpressionsKeepDeferredLoadOrder(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), localSlot: []uint32{0}}
	load := f.pushValue(storage{kind: stMemRef, typ: mtI32, reg: R12, idx: 4})
	f.regUser[R12] = load
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 8})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32})
	f.pushBinOp(opDivU, mtI32)
	var first encoder.Asm
	first.LoadIdx(R12, RBX, R12, 0, 4, false, false)
	f.materializeCallExpressions(f.rootsBottomToTop())
	if len(f.a.B) < len(first.B) || string(f.a.B[:len(first.B)]) != string(first.B) {
		t.Fatal("later division overtook the deferred memory load")
	}
}
