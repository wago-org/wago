package arm32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
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
	plan, err := shared.BuildMixedPlanWithCalls(ft, locals, body, func(index uint32) (*wasm.CompType, bool) {
		return m.FuncSignature(index)
	})
	if err != nil {
		return nil, nil, err
	}
	for _, op := range plan.Ops {
		if op.Kind != shared.MixedCall {
			continue
		}
		if int(op.Target) >= len(m.Code) {
			return nil, nil, fmt.Errorf("arm32: mixed call target %d is not local", op.Target)
		}
		targetType, ok := m.LocalFuncType(int(op.Target))
		if !ok || !usesMixedModuleCompiler(targetType, m.Code[op.Target].Locals.Runs) {
			return nil, nil, fmt.Errorf("arm32: mixed call target %d does not use the mixed ABI", op.Target)
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
		if params > 4 {
			params -= 4
		} else {
			params = 0
		}
		if results > 4 {
			results -= 4
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
	outgoingBytes := uint32(maxOutgoingSlots) * 4
	valueBase := outgoingBytes
	dataBytes := uint32(plan.LocalSlots+plan.MaxOperandSlots) * 4
	saveOffset := uint16(valueBase + dataBytes)
	frame := (valueBase + dataBytes + 4 + 15) &^ 15
	incomingSlots := plan.ParameterSlots
	if plan.ResultSlots > incomingSlots {
		incomingSlots = plan.ResultSlots
	}
	if incomingSlots > 4 && frame+uint32(incomingSlots-4)*4 > 4096 {
		return nil, fmt.Errorf("arm32: mixed stack ABI displacement exceeds 4095 bytes")
	}
	if frame > 4096 {
		return nil, fmt.Errorf("arm32: mixed frame %d exceeds bounded stack displacement", frame)
	}
	var a a32.Asm
	must := func(ok bool, what string) {
		if !ok {
			panic("arm32: mixed " + what)
		}
	}
	off := func(slot uint16) uint16 { return uint16(valueBase) + slot*4 }

	must(a.MovImm32(a32.R12, frame), "frame size")
	must(a.Sub(a32.SP, a32.SP, a32.R12), "frame allocate")
	must(a.Ldr(a32.R12, armContextReg, embedded32.ContextStackLimitOffset), "stack limit")
	must(a.Cmp(a32.SP, a32.R12), "stack compare")
	stackOK := a.FarBcond(a32.CondCS)
	must(a.MovImm32(a32.R12, frame), "overflow frame size")
	must(a.Add(a32.SP, a32.SP, a32.R12), "overflow frame release")
	must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "overflow trap cell")
	must(a.MovImm32(a32.R0, uint32(embedded32.TrapStackOverflow)), "overflow trap")
	must(a.Str(a32.R0, a32.R1, 0), "overflow trap write")
	must(a.MovImm32(a32.R0, 0), "overflow result")
	a.Ret()
	a.Align4()
	if !a.PatchFarBranch(stackOK, a.Len()) {
		return nil, fmt.Errorf("arm32: mixed stack branch out of range")
	}
	must(a.Str(a32.LR, a32.SP, saveOffset), "return address save")
	for i := uint16(0); i < plan.ParameterSlots; i++ {
		if i < 4 {
			must(a.Str(a32.R0+a32.Reg(i), a32.SP, off(i)), "register parameter store")
		} else {
			must(a.Ldr(a32.R0, a32.SP, uint16(frame)+uint16(i-4)*4), "stack parameter load")
			must(a.Str(a32.R0, a32.SP, off(i)), "stack parameter store")
		}
	}
	must(a.MovImm32(a32.R0, 0), "local zero")
	for i := plan.DeclaredLocalStart; i < plan.LocalSlots; i++ {
		must(a.Str(a32.R0, a32.SP, off(i)), "local zero store")
	}
	must(a.Ldr(a32.R0, armContextReg, embedded32.ContextCancelCellOffset), "cancel cell")
	must(a.Ldr(a32.R0, a32.R0, 0), "cancel value")
	must(a.MovImm32(a32.R1, 0), "cancel zero")
	must(a.Cmp(a32.R0, a32.R1), "cancel compare")
	clear := a.FarBcond(a32.CondEQ)
	must(a.Ldr(a32.LR, a32.SP, saveOffset), "cancel return address restore")
	must(a.MovImm32(a32.R12, frame), "cancel frame size")
	must(a.Add(a32.SP, a32.SP, a32.R12), "cancel frame release")
	must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "cancel trap cell")
	must(a.MovImm32(a32.R0, uint32(embedded32.TrapCanceled)), "cancel trap")
	must(a.Str(a32.R0, a32.R1, 0), "cancel trap write")
	must(a.MovImm32(a32.R0, 0), "cancel result")
	a.Ret()
	a.Align4()
	if !a.PatchFarBranch(clear, a.Len()) {
		return nil, fmt.Errorf("arm32: mixed cancellation branch out of range")
	}

	for _, op := range plan.Ops {
		switch op.Kind {
		case shared.MixedConst:
			for i := uint8(0); i < op.Width; i++ {
				must(a.MovImm32(a32.R0, op.Words[i]), "constant")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "constant store")
			}
		case shared.MixedCopy:
			for i := uint8(0); i < op.Width; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+uint16(i)*4), "copy load")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "copy store")
			}
		case shared.MixedI32Add, shared.MixedI32Sub, shared.MixedI32Mul, shared.MixedI32And, shared.MixedI32Or, shared.MixedI32Xor:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "i32 left")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "i32 right")
			switch op.Kind {
			case shared.MixedI32Add:
				must(a.Add(a32.R0, a32.R0, a32.R1), "i32 add")
			case shared.MixedI32Sub:
				must(a.Sub(a32.R0, a32.R0, a32.R1), "i32 sub")
			case shared.MixedI32Mul:
				must(a.Mul(a32.R0, a32.R0, a32.R1), "i32 mul")
			case shared.MixedI32And:
				must(a.And(a32.R0, a32.R0, a32.R1), "i32 and")
			case shared.MixedI32Or:
				must(a.Orr(a32.R0, a32.R0, a32.R1), "i32 or")
			case shared.MixedI32Xor:
				must(a.Eor(a32.R0, a32.R0, a32.R1), "i32 xor")
			}
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "i32 result")
		case shared.MixedI64Add, shared.MixedI64Sub:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "i64 left low")
			must(a.Ldr(a32.R1, a32.SP, off(op.Left)+4), "i64 left high")
			must(a.Ldr(a32.R2, a32.SP, off(op.Right)), "i64 right low")
			must(a.Ldr(a32.R3, a32.SP, off(op.Right)+4), "i64 right high")
			if op.Kind == shared.MixedI64Add {
				must(a.Add64(a32.R0, a32.R1, a32.R0, a32.R1, a32.R2, a32.R3), "i64 add")
			} else {
				must(a.Sub64(a32.R0, a32.R1, a32.R0, a32.R1, a32.R2, a32.R3), "i64 sub")
			}
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "i64 result low")
			must(a.Str(a32.R1, a32.SP, off(op.Dst)+4), "i64 result high")
		case shared.MixedI64And, shared.MixedI64Or, shared.MixedI64Xor:
			for i := uint16(0); i < 2; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+i*4), "i64 logic left")
				must(a.Ldr(a32.R1, a32.SP, off(op.Right)+i*4), "i64 logic right")
				switch op.Kind {
				case shared.MixedI64And:
					must(a.And(a32.R0, a32.R0, a32.R1), "i64 and")
				case shared.MixedI64Or:
					must(a.Orr(a32.R0, a32.R0, a32.R1), "i64 or")
				case shared.MixedI64Xor:
					must(a.Eor(a32.R0, a32.R0, a32.R1), "i64 xor")
				}
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+i*4), "i64 logic result")
			}
		case shared.MixedF32Abs, shared.MixedF32Neg:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "f32 unary")
			mask := uint32(0x7fffffff)
			if op.Kind == shared.MixedF32Neg {
				mask = 0x80000000
			}
			must(a.MovImm32(a32.R1, mask), "f32 mask")
			if op.Kind == shared.MixedF32Abs {
				must(a.And(a32.R0, a32.R0, a32.R1), "f32 abs")
			} else {
				must(a.Eor(a32.R0, a32.R0, a32.R1), "f32 neg")
			}
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "f32 unary result")
		case shared.MixedF32Copysign:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "f32 magnitude")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "f32 sign")
			must(a.MovImm32(a32.R2, 0x7fffffff), "f32 magnitude mask")
			must(a.And(a32.R0, a32.R0, a32.R2), "f32 clear sign")
			must(a.MovImm32(a32.R2, 0x80000000), "f32 sign mask")
			must(a.And(a32.R1, a32.R1, a32.R2), "f32 select sign")
			must(a.Orr(a32.R0, a32.R0, a32.R1), "f32 copysign")
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "f32 copysign result")
		case shared.MixedF64Abs, shared.MixedF64Neg:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)+4), "f64 high")
			mask := uint32(0x7fffffff)
			if op.Kind == shared.MixedF64Neg {
				mask = 0x80000000
			}
			must(a.MovImm32(a32.R1, mask), "f64 mask")
			if op.Kind == shared.MixedF64Abs {
				must(a.And(a32.R0, a32.R0, a32.R1), "f64 abs")
			} else {
				must(a.Eor(a32.R0, a32.R0, a32.R1), "f64 neg")
			}
			must(a.Str(a32.R0, a32.SP, off(op.Dst)+4), "f64 high result")
		case shared.MixedF64Copysign:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "f64 low")
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "f64 low result")
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)+4), "f64 magnitude high")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)+4), "f64 sign high")
			must(a.MovImm32(a32.R2, 0x7fffffff), "f64 magnitude mask")
			must(a.And(a32.R0, a32.R0, a32.R2), "f64 clear sign")
			must(a.MovImm32(a32.R2, 0x80000000), "f64 sign mask")
			must(a.And(a32.R1, a32.R1, a32.R2), "f64 select sign")
			must(a.Orr(a32.R0, a32.R0, a32.R1), "f64 copysign")
			must(a.Str(a32.R0, a32.SP, off(op.Dst)+4), "f64 high result")
		case shared.MixedV128Not:
			for i := uint16(0); i < 4; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+i*4), "v128 not load")
				must(a.Mvn(a32.R0, a32.R0), "v128 not")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+i*4), "v128 not store")
			}
		case shared.MixedV128And, shared.MixedV128AndNot, shared.MixedV128Or, shared.MixedV128Xor, shared.MixedI32x4Add, shared.MixedI32x4Sub:
			for i := uint16(0); i < 4; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+i*4), "v128 left")
				must(a.Ldr(a32.R1, a32.SP, off(op.Right)+i*4), "v128 right")
				switch op.Kind {
				case shared.MixedV128And:
					must(a.And(a32.R0, a32.R0, a32.R1), "v128 and")
				case shared.MixedV128AndNot:
					must(a.Bic(a32.R0, a32.R0, a32.R1), "v128 andnot")
				case shared.MixedV128Or:
					must(a.Orr(a32.R0, a32.R0, a32.R1), "v128 or")
				case shared.MixedV128Xor:
					must(a.Eor(a32.R0, a32.R0, a32.R1), "v128 xor")
				case shared.MixedI32x4Add:
					must(a.Add(a32.R0, a32.R0, a32.R1), "i32x4 add")
				case shared.MixedI32x4Sub:
					must(a.Sub(a32.R0, a32.R0, a32.R1), "i32x4 sub")
				}
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+i*4), "v128 result")
			}
		case shared.MixedV128Bitselect:
			for i := uint16(0); i < 4; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+i*4), "bitselect left")
				must(a.Ldr(a32.R1, a32.SP, off(op.Right)+i*4), "bitselect right")
				must(a.Ldr(a32.R2, a32.SP, off(op.Third)+i*4), "bitselect mask")
				must(a.And(a32.R0, a32.R0, a32.R2), "bitselect selected")
				must(a.Bic(a32.R1, a32.R1, a32.R2), "bitselect rejected")
				must(a.Orr(a32.R0, a32.R0, a32.R1), "bitselect merge")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+i*4), "bitselect result")
			}
		case shared.MixedSelect:
			must(a.Ldr(a32.R0, a32.SP, off(op.Third)), "select condition")
			must(a.MovImm32(a32.R1, 0), "select zero")
			must(a.Cmp(a32.R0, a32.R1), "select compare")
			selectedLeft := a.FarBcond(a32.CondNE)
			for i := uint8(0); i < op.Width; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Right)+uint16(i)*4), "select right load")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "select right store")
			}
			if !a.PatchFarBranch(selectedLeft, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed select branch out of range")
			}
		case shared.MixedCall:
			if relocSink == nil {
				return nil, fmt.Errorf("arm32: mixed call has no relocation sink")
			}
			argReg := uint16(0)
			for _, arg := range op.Args {
				width, _ := shared.MixedValueSlots(arg.Type)
				for i := uint8(0); i < width; i++ {
					if argReg < 4 {
						must(a.Ldr(a32.R0+a32.Reg(argReg), a32.SP, off(arg.Slot)+uint16(i)*4), "register call argument")
					} else {
						must(a.Ldr(a32.R12, a32.SP, off(arg.Slot)+uint16(i)*4), "stack call argument load")
						must(a.Str(a32.R12, a32.SP, uint16(argReg-4)*4), "stack call argument store")
					}
					argReg++
				}
			}
			at := a.Call()
			*relocSink = append(*relocSink, callReloc{at: at, target: int(op.Target)})
			resultReg := uint16(0)
			for _, result := range op.Results {
				width, _ := shared.MixedValueSlots(result.Type)
				for i := uint8(0); i < width; i++ {
					if resultReg < 4 {
						must(a.Str(a32.R0+a32.Reg(resultReg), a32.SP, off(result.Slot)+uint16(i)*4), "register call result")
					} else {
						must(a.Ldr(a32.R0, a32.SP, uint16(resultReg-4)*4), "stack call result load")
						must(a.Str(a32.R0, a32.SP, off(result.Slot)+uint16(i)*4), "stack call result store")
					}
					resultReg++
				}
			}
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextTrapCellOffset), "call trap cell")
			must(a.Ldr(a32.R0, a32.R0, 0), "call trap value")
			must(a.MovImm32(a32.R1, 0), "call trap zero")
			must(a.Cmp(a32.R0, a32.R1), "call trap compare")
			callOK := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "trapping call return address restore")
			must(a.MovImm32(a32.R12, frame), "trapping call frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "trapping call frame release")
			must(a.MovImm32(a32.R0, 0), "trapping call result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(callOK, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed call trap branch out of range")
			}
		case shared.MixedUnreachable:
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "unreachable trap cell")
			must(a.MovImm32(a32.R0, uint32(embedded32.TrapUnreachable)), "unreachable trap")
			must(a.Str(a32.R0, a32.R1, 0), "unreachable trap write")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "unreachable return address restore")
			must(a.MovImm32(a32.R12, frame), "unreachable frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "unreachable frame release")
			must(a.MovImm32(a32.R0, 0), "unreachable result")
			a.Ret()
			a.Align4()
		default:
			return nil, fmt.Errorf("arm32: unsupported mixed operation %d", op.Kind)
		}
	}

	resultReg := uint16(0)
	for _, result := range plan.Results {
		width, _ := shared.MixedValueSlots(result.Type)
		for i := uint8(0); i < width; i++ {
			if resultReg < 4 {
				must(a.Ldr(a32.R0+a32.Reg(resultReg), a32.SP, off(result.Slot)+uint16(i)*4), "register result load")
			} else {
				must(a.Ldr(a32.R12, a32.SP, off(result.Slot)+uint16(i)*4), "stack result load")
				must(a.Str(a32.R12, a32.SP, uint16(frame)+uint16(resultReg-4)*4), "stack result store")
			}
			resultReg++
		}
	}
	must(a.Ldr(a32.LR, a32.SP, saveOffset), "return address restore")
	must(a.MovImm32(a32.R12, frame), "frame release size")
	must(a.Add(a32.SP, a32.SP, a32.R12), "frame release")
	a.Ret()
	a.Align4()
	return a.B, nil
}
