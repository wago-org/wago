//go:build (linux || darwin) && arm64

package arm64

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// Numeric call_indirect uses the wrapper ABI. Exercise the mixed emitter
// directly so a pre-flush in descriptor dispatch cannot remove the pressure.
func TestMixedCallIndirectPressureARM64(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			t.Run(fmt.Sprintf("f64=%t/stack-reg=%t", f64, stackReg), func(t *testing.T) {
				typ, vt := mtF32, wasm.F32
				if f64 {
					typ, vt = mtF64, wasm.F64
				}
				var stats CodegenStats
				f := fn{a: &a64.Asm{}, s: newStack(), m: &wasm.Module{}, policy: currentCodegenPolicy(), stats: &stats, usesCalls: stackReg, nLocals: 27, nLocalSlots: 27, memSizeReg: regNone}
				f.locals = make([]localDef, 27)
				f.localSlot = make([]uint32, 27)
				f.localType = make([]machineType, 27)
				a := f.a
				a.StpPre(FP, LR, SP, -16)
				a.SubImm64(SP, SP, 512)
				for i, r := range pinnedFLocalRegs[:27] {
					f.locals[i] = localDef{reg: r, isFloat: true, state: lsReg}
					f.localSlot[i] = uint32(i)
					f.localType[i] = typ
					f.fpinnedLocalMask = f.fpinnedLocalMask.add(r)
					a.MovImm64(X16, floatBits(float64(i+1), f64))
					a.FmovFromGpr(r, X16, f64)
				}
				f.pushValue(storage{kind: stConst, typ: mtI64, cval: 99})
				// The vector source overlaps the scalar's destination and is not canonical.
				lanes := [2]uint64{0x0123456789abcdef, 0xfedcba9876543210}
				for i, v := range lanes {
					a.MovImm64(X16, v)
					f.st64(SP, f.spillOff(i), X16)
				}
				f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 0})
				params := make([]wasm.ValType, 0, 10)
				// The first three arguments form an FP swap chain; the fifth consumes the
				// last free register and must use the mixed-call pressure spill fallback.
				for i, r := range []Reg{1, 2, 0, 3, 15, 15, 15, 15} {
					if i < 5 {
						a.Fadd(r, pinnedFLocalRegs[i], pinnedFLocalRegs[i], f64)
						f.pushFReg(r, typ)
					} else {
						a.MovImm64(X16, floatBits(2*float64(i+1), f64))
						f.st64(SP, f.spillOff(i-3), X16)
						f.pushValue(storage{kind: stSlot, typ: typ, slot: uint32(i - 3)})
					}
					params = append(params, vt)
				}
				// Two owned GP arguments force a swap across the ABI argument bank.
				a.MovImm64(X1, 21)
				f.pushReg(X1, mtI64)
				a.MovImm64(X0, 34)
				f.pushReg(X0, mtI64)
				params = append(params, wasm.I64, wasm.I64)
				target := a.Adr(X2)
				f.pinned = f.pinned.add(X2)
				before := stats.GCCodeBytes.SpillReload
				f.emitMixedRegisterCallVia(-1, X2, &wasm.CompType{Params: params, Results: []wasm.ValType{wasm.I64}})
				if stats.GCCodeBytes.SpillReload <= before {
					t.Fatal("mixed call did not use pressure spill fallback")
				}
				if stats.Peephole["machine-swap-chain"] != 1 {
					t.Fatalf("expected an FP swap chain: %v", stats.Peephole)
				}
				if f.spillFloor != 0 || f.fpinned != 0 || f.pinned != 0 {
					t.Fatal("temporary floor or register pins leaked")
				}
				if f.frameSize() > 512 {
					t.Fatalf("test frame too small: %d", f.frameSize())
				}
				result := f.materialize(f.popValue())
				var failures []int
				check := func(r Reg, want uint64) {
					a.MovImm64(X16, want)
					a.CmpReg64(r, X16)
					failures = append(failures, a.Bcond(condNE))
				}
				check(result, 1234)
				for i, v := range []uint64{99, lanes[0], lanes[1]} {
					f.ld64(X9, SP, f.spillOff(i))
					check(X9, v)
				}
				a.MovImm64(X0, 1)
				done := a.Branch()
				fail := a.Len()
				a.MovImm64(X0, 0)
				for _, at := range failures {
					if !a.PatchBranch19(at, fail) {
						t.Fatal("check branch out of range")
					}
				}
				if !a.PatchBranch26(done, a.Len()) {
					t.Fatal("return branch out of range")
				}
				a.AddImm64(SP, SP, 512)
				a.LdpPost(FP, LR, SP, 16)
				a.Ret()
				// A separate wrong target returns a distinct value.
				a.MovImm64(X0, 999)
				a.Ret()
				if !a.PatchAdr(target, a.Len()) {
					t.Fatal("indirect target out of range")
				}
				failures = nil
				for i := 0; i < 8; i++ {
					a.FmovToGpr(X9, Reg(i), f64)
					check(X9, floatBits(2*float64(i+1), f64))
				}
				check(X0, 21)
				check(X1, 34)
				a.MovImm64(X0, 1234)
				a.Ret()
				fail = a.Len()
				a.MovImm64(X0, 0)
				a.Ret()
				for _, at := range failures {
					if !a.PatchBranch19(at, fail) {
						t.Fatal("callee check branch out of range")
					}
				}
				code, err := arm64spike.MapExec(a.B)
				if err != nil {
					t.Fatal(err)
				}
				defer coreruntime.Unmap(code)
				if got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), 0, 0); got != 1 {
					t.Fatalf("indirect call or lower stack check failed: %d", got)
				}
			})
		}
	}
}
