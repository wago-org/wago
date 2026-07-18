package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func mixedValueSlotCount(values []shared.MixedValue) uint16 {
	var slots uint16
	for _, value := range values {
		width, _ := shared.MixedValueSlots(value.Type)
		slots += uint16(width)
	}
	return slots
}

func CompileMixedModuleFunction(ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
	plan, err := shared.BuildMixedPlan(ft, locals, body)
	if err != nil {
		return nil, err
	}
	return emitMixedPlan(plan, nil)
}

func compileMixedModuleFunction(m *wasm.Module, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, []callReloc, error) {
	plan, err := shared.BuildMixedPlanWithResolvers(ft, locals, body, func(index uint32) (*wasm.CompType, bool) {
		return m.FuncSignature(index)
	}, func(index uint32) (wasm.ValType, bool, bool) {
		if int(index) >= len(m.Globals) {
			return wasm.ValType{}, false, false
		}
		global := m.Globals[index]
		return global.Type.Type, global.Type.Mutable, true
	})
	if err != nil {
		return nil, nil, err
	}
	for _, op := range plan.Ops {
		if op.Kind != shared.MixedCall {
			continue
		}
		if int(op.Target) >= len(m.Code) {
			return nil, nil, fmt.Errorf("riscv32: mixed call target %d is not local", op.Target)
		}
		targetType, ok := m.LocalFuncType(int(op.Target))
		if !ok || !usesMixedModuleCompiler(targetType, m.Code[op.Target].Locals.Runs) {
			return nil, nil, fmt.Errorf("riscv32: mixed call target %d does not use the mixed ABI", op.Target)
		}
	}
	var relocs []callReloc
	code, err := emitMixedPlan(plan, &relocs)
	return code, relocs, err
}

func emitMixedPlan(plan *shared.MixedPlan, relocSink *[]callReloc) ([]byte, error) {
	maxOutgoingSlots := uint16(0)
	for _, op := range plan.Ops {
		if op.Kind != shared.MixedCall {
			continue
		}
		params, results := mixedValueSlotCount(op.Args), mixedValueSlotCount(op.Results)
		if params > 8 {
			params -= 8
		} else {
			params = 0
		}
		if results > 8 {
			results -= 8
		} else {
			results = 0
		}
		if params > maxOutgoingSlots {
			maxOutgoingSlots = params
		}
		if results > maxOutgoingSlots {
			maxOutgoingSlots = results
		}
	}
	outgoingBytes := int32(maxOutgoingSlots) * 4
	valueBase := outgoingBytes
	dataBytes := int32(plan.LocalSlots+plan.MaxOperandSlots) * 4
	helperBytes := int32(0)
	for _, op := range plan.Ops {
		if op.Kind == shared.MixedF64Helper {
			helperBytes = embedded32.F64FrameBytes
			break
		}
	}
	helperBase := valueBase + dataBytes
	saveOffset := helperBase + helperBytes
	frame := (saveOffset + 4 + 15) &^ 15
	incomingSlots := plan.ParameterSlots
	if plan.ResultSlots > incomingSlots {
		incomingSlots = plan.ResultSlots
	}
	if incomingSlots > 8 && frame+int32(incomingSlots-8)*4 > 2047 {
		return nil, fmt.Errorf("riscv32: mixed stack ABI displacement exceeds 2047 bytes")
	}
	if frame > 2032 {
		return nil, fmt.Errorf("riscv32: mixed frame %d exceeds bounded stack displacement", frame)
	}
	var a rv.Asm
	off := func(slot uint16) int32 { return valueBase + int32(slot)*4 }
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
	must(a.Sw(rv.RA, rv.SP, saveOffset), "return address save")
	for i := uint16(0); i < plan.ParameterSlots; i++ {
		if i < 8 {
			must(a.Sw(rv.A0+rv.Reg(i), rv.SP, off(i)), "register parameter store")
		} else {
			must(a.Lw(rv.T0, rv.SP, frame+int32(i-8)*4), "stack parameter load")
			must(a.Sw(rv.T0, rv.SP, off(i)), "stack parameter store")
		}
	}
	a.MovImm32(rv.T0, 0)
	for i := plan.DeclaredLocalStart; i < plan.LocalSlots; i++ {
		must(a.Sw(rv.T0, rv.SP, off(i)), "local zero store")
	}
	must(a.Lw(rv.T0, rvContextReg, embedded32.ContextCancelCellOffset), "cancel cell")
	must(a.Lw(rv.T0, rv.T0, 0), "cancel value")
	clear := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
	must(a.Lw(rv.RA, rv.SP, saveOffset), "cancel return address restore")
	must(a.Addi(rv.SP, rv.SP, frame), "cancel frame release")
	must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "cancel trap cell")
	a.MovImm32(rv.T0, uint32(embedded32.TrapCanceled))
	must(a.Sw(rv.T0, rv.T1, 0), "cancel trap write")
	a.MovImm32(rv.A0, 0)
	a.Ret()
	if !a.PatchFarBranch(clear, a.Len()) {
		return nil, fmt.Errorf("riscv32: mixed cancellation branch out of range")
	}

	type mixedBranchPatch struct {
		at, label   int
		conditional bool
	}
	labels := make([]int, len(plan.Ops)+1)
	var branches []mixedBranchPatch
	for opIndex, op := range plan.Ops {
		labels[opIndex] = a.Len()
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
		case shared.MixedI64Mul:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i64 multiply left low")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i64 multiply right low")
			a.Mul(rv.A0, rv.T0, rv.T1)
			a.Mulhu(rv.A1, rv.T0, rv.T1)
			must(a.Lw(rv.T1, rv.SP, off(op.Right)+4), "i64 multiply right high")
			a.Mul(rv.T2, rv.T0, rv.T1)
			a.Add(rv.A1, rv.A1, rv.T2)
			must(a.Lw(rv.T0, rv.SP, off(op.Left)+4), "i64 multiply left high")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i64 multiply right low reload")
			a.Mul(rv.T2, rv.T0, rv.T1)
			a.Add(rv.A1, rv.A1, rv.T2)
			must(a.Sw(rv.A0, rv.SP, off(op.Dst)), "i64 multiply result low")
			must(a.Sw(rv.A1, rv.SP, off(op.Dst)+4), "i64 multiply result high")
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
		case shared.MixedSelect:
			must(a.Lw(rv.T0, rv.SP, off(op.Third)), "select condition")
			selectedLeft := a.FarBcond(rv.T0, rv.Zero, rv.CondNE, branchScratch)
			for i := uint8(0); i < op.Width; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Right)+int32(i)*4), "select right load")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "select right store")
			}
			if !a.PatchFarBranch(selectedLeft, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed select branch out of range")
			}
		case shared.MixedF64Helper:
			a.MovImm32(rv.T0, op.HelperOp)
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F64FrameOpOffset), "f64 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+int32(i)*4), "f64 helper left load")
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F64FrameALoOffset+int32(i)*4), "f64 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.Sw(rv.Zero, rv.SP, helperBase+embedded32.F64FrameAHiOffset), "f64 helper input high store")
			}
			if op.Arity == 2 {
				for i := int32(0); i < 2; i++ {
					must(a.Lw(rv.T0, rv.SP, off(op.Right)+i*4), "f64 helper right load")
					must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F64FrameBLoOffset+i*4), "f64 helper right store")
				}
			}
			must(a.Addi(rv.A0, rv.SP, helperBase), "f64 helper frame address")
			must(a.Lw(rv.A1, rvContextReg, embedded32.ContextHelperTableOffset), "f64 helper table")
			must(a.Lw(rv.T0, rv.A1, embedded32.HelperF64Offset), "f64 helper target")
			a.Blr(rv.T0)
			must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.F64FrameTrapOffset), "f64 helper trap")
			helperOK := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "f64 helper trap cell")
			must(a.Sw(rv.T0, rv.T1, 0), "f64 helper trap publish")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "f64 helper trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "f64 helper trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("riscv32: f64 helper trap branch out of range")
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.F64FrameOutLoOffset+int32(i)*4), "f64 helper result load")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "f64 helper result store")
			}
		case shared.MixedGlobalGet, shared.MixedGlobalSet:
			if op.Target > 511 {
				return nil, fmt.Errorf("riscv32: mixed global index %d exceeds direct displacement", op.Target)
			}
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextGlobalsBaseOffset), "global base")
			if op.Kind == shared.MixedGlobalGet {
				must(a.Lw(rv.T1, rv.T0, int32(op.Target*4)), "global.get")
				must(a.Sw(rv.T1, rv.SP, off(op.Dst)), "global.get result")
			} else {
				must(a.Lw(rv.T1, rv.SP, off(op.Left)), "global.set value")
				must(a.Sw(rv.T1, rv.T0, int32(op.Target*4)), "global.set")
			}
		case shared.MixedBranchZero, shared.MixedBranchNonzero:
			must(a.Lw(rv.T0, rv.SP, off(op.Third)), "branch condition")
			cond := rv.CondEQ
			if op.Kind == shared.MixedBranchNonzero {
				cond = rv.CondNE
			}
			branches = append(branches, mixedBranchPatch{at: a.FarBcond(rv.T0, rv.Zero, cond, branchScratch), label: op.Label, conditional: true})
		case shared.MixedJump:
			branches = append(branches, mixedBranchPatch{at: a.FarJump(rv.Zero, branchScratch), label: op.Label})
		case shared.MixedPollCancellation:
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextCancelCellOffset), "loop cancel cell")
			must(a.Lw(rv.T0, rv.T0, 0), "loop cancel value")
			loopClear := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "loop cancel trap cell")
			a.MovImm32(rv.T0, uint32(embedded32.TrapCanceled))
			must(a.Sw(rv.T0, rv.T1, 0), "loop cancel trap write")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "loop cancel return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "loop cancel frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(loopClear, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed loop cancellation branch out of range")
			}
		case shared.MixedCall:
			if relocSink == nil {
				return nil, fmt.Errorf("riscv32: mixed call has no relocation sink")
			}
			argReg := uint16(0)
			for _, arg := range op.Args {
				width, _ := shared.MixedValueSlots(arg.Type)
				for i := uint8(0); i < width; i++ {
					if argReg < 8 {
						must(a.Lw(rv.A0+rv.Reg(argReg), rv.SP, off(arg.Slot)+int32(i)*4), "register call argument")
					} else {
						must(a.Lw(rv.T0, rv.SP, off(arg.Slot)+int32(i)*4), "stack call argument load")
						must(a.Sw(rv.T0, rv.SP, int32(argReg-8)*4), "stack call argument store")
					}
					argReg++
				}
			}
			at := a.FarCall(branchScratch)
			*relocSink = append(*relocSink, callReloc{at: at, target: int(op.Target)})
			resultReg := uint16(0)
			for _, result := range op.Results {
				width, _ := shared.MixedValueSlots(result.Type)
				for i := uint8(0); i < width; i++ {
					if resultReg < 8 {
						must(a.Sw(rv.A0+rv.Reg(resultReg), rv.SP, off(result.Slot)+int32(i)*4), "register call result")
					} else {
						must(a.Lw(rv.T0, rv.SP, int32(resultReg-8)*4), "stack call result load")
						must(a.Sw(rv.T0, rv.SP, off(result.Slot)+int32(i)*4), "stack call result store")
					}
					resultReg++
				}
			}
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextTrapCellOffset), "call trap cell")
			must(a.Lw(rv.T0, rv.T0, 0), "call trap value")
			callOK := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.RA, rv.SP, saveOffset), "trapping call return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "trapping call frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(callOK, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed call trap branch out of range")
			}
		case shared.MixedUnreachable:
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "unreachable trap cell")
			a.MovImm32(rv.T0, uint32(embedded32.TrapUnreachable))
			must(a.Sw(rv.T0, rv.T1, 0), "unreachable trap write")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "unreachable return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "unreachable frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
		default:
			return nil, fmt.Errorf("riscv32: unsupported mixed operation %d", op.Kind)
		}
	}

	labels[len(plan.Ops)] = a.Len()
	for _, branch := range branches {
		if branch.label < 0 || branch.label >= len(labels) {
			return nil, fmt.Errorf("riscv32: invalid mixed branch label %d", branch.label)
		}
		var ok bool
		if branch.conditional {
			ok = a.PatchFarBranch(branch.at, labels[branch.label])
		} else {
			ok = a.PatchFarJump(branch.at, labels[branch.label])
		}
		if !ok {
			return nil, fmt.Errorf("riscv32: mixed structured branch out of range")
		}
	}

	resultReg := uint16(0)
	for _, result := range plan.Results {
		width, _ := shared.MixedValueSlots(result.Type)
		for i := uint8(0); i < width; i++ {
			if resultReg < 8 {
				must(a.Lw(rv.A0+rv.Reg(resultReg), rv.SP, off(result.Slot)+int32(i)*4), "register result load")
			} else {
				must(a.Lw(rv.T0, rv.SP, off(result.Slot)+int32(i)*4), "stack result load")
				must(a.Sw(rv.T0, rv.SP, frame+int32(resultReg-8)*4), "stack result store")
			}
			resultReg++
		}
	}
	must(a.Lw(rv.RA, rv.SP, saveOffset), "return address restore")
	must(a.Addi(rv.SP, rv.SP, frame), "frame release")
	a.Ret()
	return a.B, nil
}
