package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func CompileMixedModuleFunction(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
	plan, err := shared.BuildMixedPlan(ft, locals, body)
	if err != nil {
		return nil, err
	}
	if plan.ParameterSlots > 8 || plan.ResultSlots > 8 {
		return nil, fmt.Errorf("riscv32: mixed register ABI supports at most 8 parameter and result slots")
	}
	frame := int32(plan.LocalSlots+plan.MaxOperandSlots) * 4
	if frame < 16 {
		frame = 16
	}
	frame = (frame + 15) &^ 15
	if frame > 1024 {
		return nil, fmt.Errorf("riscv32: mixed frame %d exceeds bounded stack displacement", frame)
	}
	var a rv.Asm
	off := func(slot uint16) int32 { return int32(slot) * 4 }
	must := func(ok bool, what string) {
		if !ok {
			panic("riscv32: mixed " + what)
		}
	}

	must(a.Addi(rv.SP, rv.SP, -frame), "frame allocate")
	must(a.Lw(rv.T0, rvContextReg, embedded32.ContextStackLimitOffset), "stack limit")
	stackOK := a.FarBcond(rv.SP, rv.T0, rv.CondGEU, branchScratch)
	must(a.Addi(rv.SP, rv.SP, frame), "overflow frame release")
	must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "overflow trap cell")
	a.MovImm32(rv.T0, uint32(embedded32.TrapStackOverflow))
	must(a.Sw(rv.T0, rv.T1, 0), "overflow trap write")
	a.MovImm32(rv.A0, 0)
	a.Ret()
	if !a.PatchFarBranch(stackOK, a.Len()) {
		return nil, fmt.Errorf("riscv32: mixed stack branch out of range")
	}
	for i := uint16(0); i < plan.ParameterSlots; i++ {
		must(a.Sw(rv.A0+rv.Reg(i), rv.SP, off(i)), "parameter store")
	}
	a.MovImm32(rv.T0, 0)
	for i := plan.DeclaredLocalStart; i < plan.LocalSlots; i++ {
		must(a.Sw(rv.T0, rv.SP, off(i)), "local zero store")
	}
	must(a.Lw(rv.T0, rvContextReg, embedded32.ContextCancelCellOffset), "cancel cell")
	must(a.Lw(rv.T0, rv.T0, 0), "cancel value")
	clear := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
	must(a.Addi(rv.SP, rv.SP, frame), "cancel frame release")
	must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "cancel trap cell")
	a.MovImm32(rv.T0, uint32(embedded32.TrapCanceled))
	must(a.Sw(rv.T0, rv.T1, 0), "cancel trap write")
	a.MovImm32(rv.A0, 0)
	a.Ret()
	if !a.PatchFarBranch(clear, a.Len()) {
		return nil, fmt.Errorf("riscv32: mixed cancellation branch out of range")
	}

	for _, op := range plan.Ops {
		switch op.Kind {
		case shared.MixedConst:
			for i := uint8(0); i < op.Width; i++ {
				a.MovImm32(rv.T0, op.Words[i])
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "constant store")
			}
		case shared.MixedCopy:
			for i := uint8(0); i < op.Width; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+int32(i)*4), "copy load")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "copy store")
			}
		case shared.MixedI32Add, shared.MixedI32Sub, shared.MixedI32Mul, shared.MixedI32And, shared.MixedI32Or, shared.MixedI32Xor:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 left")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i32 right")
			switch op.Kind {
			case shared.MixedI32Add:
				a.Add(rv.T0, rv.T0, rv.T1)
			case shared.MixedI32Sub:
				a.Sub(rv.T0, rv.T0, rv.T1)
			case shared.MixedI32Mul:
				a.Mul(rv.T0, rv.T0, rv.T1)
			case shared.MixedI32And:
				a.And(rv.T0, rv.T0, rv.T1)
			case shared.MixedI32Or:
				a.Or(rv.T0, rv.T0, rv.T1)
			case shared.MixedI32Xor:
				a.Xor(rv.T0, rv.T0, rv.T1)
			}
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "i32 result")
		case shared.MixedI64Add, shared.MixedI64Sub:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i64 left low")
			must(a.Lw(rv.T1, rv.SP, off(op.Left)+4), "i64 left high")
			must(a.Lw(rv.T2, rv.SP, off(op.Right)), "i64 right low")
			must(a.Lw(rv.T3, rv.SP, off(op.Right)+4), "i64 right high")
			if op.Kind == shared.MixedI64Add {
				a.Add64(rv.A0, rv.A1, rv.T0, rv.T1, rv.T2, rv.T3, rv.T4)
			} else {
				a.Sub64(rv.A0, rv.A1, rv.T0, rv.T1, rv.T2, rv.T3, rv.T4)
			}
			must(a.Sw(rv.A0, rv.SP, off(op.Dst)), "i64 result low")
			must(a.Sw(rv.A1, rv.SP, off(op.Dst)+4), "i64 result high")
		case shared.MixedI64And, shared.MixedI64Or, shared.MixedI64Xor:
			for i := int32(0); i < 2; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+i*4), "i64 logic left")
				must(a.Lw(rv.T1, rv.SP, off(op.Right)+i*4), "i64 logic right")
				switch op.Kind {
				case shared.MixedI64And:
					a.And(rv.T0, rv.T0, rv.T1)
				case shared.MixedI64Or:
					a.Or(rv.T0, rv.T0, rv.T1)
				case shared.MixedI64Xor:
					a.Xor(rv.T0, rv.T0, rv.T1)
				}
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+i*4), "i64 logic result")
			}
		case shared.MixedF32Abs, shared.MixedF32Neg:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "f32 unary")
			mask := uint32(0x7fffffff)
			if op.Kind == shared.MixedF32Neg {
				mask = 0x80000000
			}
			a.MovImm32(rv.T1, mask)
			if op.Kind == shared.MixedF32Abs {
				a.And(rv.T0, rv.T0, rv.T1)
			} else {
				a.Xor(rv.T0, rv.T0, rv.T1)
			}
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "f32 unary result")
		case shared.MixedF32Copysign:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "f32 magnitude")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "f32 sign")
			a.MovImm32(rv.T2, 0x7fffffff)
			a.And(rv.T0, rv.T0, rv.T2)
			a.MovImm32(rv.T2, 0x80000000)
			a.And(rv.T1, rv.T1, rv.T2)
			a.Or(rv.T0, rv.T0, rv.T1)
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "f32 copysign result")
		case shared.MixedF64Abs, shared.MixedF64Neg:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)+4), "f64 high")
			mask := uint32(0x7fffffff)
			if op.Kind == shared.MixedF64Neg {
				mask = 0x80000000
			}
			a.MovImm32(rv.T1, mask)
			if op.Kind == shared.MixedF64Abs {
				a.And(rv.T0, rv.T0, rv.T1)
			} else {
				a.Xor(rv.T0, rv.T0, rv.T1)
			}
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)+4), "f64 high result")
		case shared.MixedF64Copysign:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "f64 low")
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "f64 low result")
			must(a.Lw(rv.T0, rv.SP, off(op.Left)+4), "f64 magnitude high")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)+4), "f64 sign high")
			a.MovImm32(rv.T2, 0x7fffffff)
			a.And(rv.T0, rv.T0, rv.T2)
			a.MovImm32(rv.T2, 0x80000000)
			a.And(rv.T1, rv.T1, rv.T2)
			a.Or(rv.T0, rv.T0, rv.T1)
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)+4), "f64 high result")
		case shared.MixedV128Not:
			for i := int32(0); i < 4; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+i*4), "v128 not load")
				a.Not(rv.T0, rv.T0)
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+i*4), "v128 not store")
			}
		case shared.MixedV128And, shared.MixedV128AndNot, shared.MixedV128Or, shared.MixedV128Xor, shared.MixedI32x4Add, shared.MixedI32x4Sub:
			for i := int32(0); i < 4; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+i*4), "v128 left")
				must(a.Lw(rv.T1, rv.SP, off(op.Right)+i*4), "v128 right")
				switch op.Kind {
				case shared.MixedV128And:
					a.And(rv.T0, rv.T0, rv.T1)
				case shared.MixedV128AndNot:
					a.Not(rv.T1, rv.T1)
					a.And(rv.T0, rv.T0, rv.T1)
				case shared.MixedV128Or:
					a.Or(rv.T0, rv.T0, rv.T1)
				case shared.MixedV128Xor:
					a.Xor(rv.T0, rv.T0, rv.T1)
				case shared.MixedI32x4Add:
					a.Add(rv.T0, rv.T0, rv.T1)
				case shared.MixedI32x4Sub:
					a.Sub(rv.T0, rv.T0, rv.T1)
				}
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+i*4), "v128 result")
			}
		case shared.MixedV128Bitselect:
			for i := int32(0); i < 4; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+i*4), "bitselect left")
				must(a.Lw(rv.T1, rv.SP, off(op.Right)+i*4), "bitselect right")
				must(a.Lw(rv.T2, rv.SP, off(op.Third)+i*4), "bitselect mask")
				a.And(rv.T0, rv.T0, rv.T2)
				a.Not(rv.T2, rv.T2)
				a.And(rv.T1, rv.T1, rv.T2)
				a.Or(rv.T0, rv.T0, rv.T1)
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+i*4), "bitselect result")
			}
		default:
			return nil, fmt.Errorf("riscv32: unsupported mixed operation %d", op.Kind)
		}
	}

	resultReg := uint16(0)
	for _, result := range plan.Results {
		width, _ := shared.MixedValueSlots(result.Type)
		for i := uint8(0); i < width; i++ {
			must(a.Lw(rv.A0+rv.Reg(resultReg), rv.SP, off(result.Slot)+int32(i)*4), "result load")
			resultReg++
		}
	}
	must(a.Addi(rv.SP, rv.SP, frame), "frame release")
	a.Ret()
	return a.B, nil
}
