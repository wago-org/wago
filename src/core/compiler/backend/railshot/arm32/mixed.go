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
	plan, err := shared.BuildMixedPlanWithModuleResolvers(ft, locals, body, func(index uint32) (*wasm.CompType, bool) {
		return m.FuncSignature(index)
	}, func(index uint32) (wasm.ValType, bool, uint32, bool) {
		slot, ok := shared.EmbeddedGlobalSlot(m, index)
		if !ok {
			return wasm.ValType{}, false, 0, false
		}
		global := m.Globals[index]
		return global.Type.Type, global.Type.Mutable, slot, true
	}, func(index uint32) (*wasm.CompType, bool) {
		return m.TypeFunc(index)
	}, func(index uint32) (wasm.ValType, bool) {
		if index != 0 || len(m.Tables) != 1 {
			return wasm.ValType{}, false
		}
		return wasm.RefVal(m.Tables[0].Type.Ref), true
	})
	if err != nil {
		return nil, nil, err
	}
	for i := range plan.Ops {
		op := &plan.Ops[i]
		if (op.Kind == shared.MixedMemoryInit || op.Kind == shared.MixedDataDrop) && uint64(op.Target) >= uint64(len(m.Data)) {
			return nil, nil, fmt.Errorf("arm32: invalid mixed data segment %d", op.Target)
		}
		if (op.Kind == shared.MixedTableInit || op.Kind == shared.MixedElemDrop) && uint64(op.Target) >= uint64(len(m.Elements)) {
			return nil, nil, fmt.Errorf("arm32: invalid mixed element segment %d", op.Target)
		}
		if op.Kind == shared.MixedTableInit && op.Lane != 0 {
			return nil, nil, fmt.Errorf("arm32: invalid mixed table.init table %d", op.Lane)
		}
		if op.Kind == shared.MixedCallIndirect {
			if op.Lane != 0 || len(m.Tables) != 1 {
				return nil, nil, fmt.Errorf("arm32: invalid mixed indirect table %d", op.Lane)
			}
			typeID, ok := shared.EmbeddedFunctionTypeID(m, op.Target)
			if !ok {
				return nil, nil, fmt.Errorf("arm32: invalid mixed indirect type %d", op.Target)
			}
			op.Target = typeID
			continue
		}
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
		if op.Kind != shared.MixedCall && op.Kind != shared.MixedCallIndirect {
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
	helperBytes := uint32(0)
	for _, op := range plan.Ops {
		switch op.Kind {
		case shared.MixedF64Helper:
			if helperBytes < embedded32.F64FrameBytes {
				helperBytes = embedded32.F64FrameBytes
			}
		case shared.MixedF32Helper:
			if helperBytes < embedded32.F32FrameBytes {
				helperBytes = embedded32.F32FrameBytes
			}
		case shared.MixedI64Helper:
			if helperBytes < embedded32.I64FrameBytes {
				helperBytes = embedded32.I64FrameBytes
			}
		case shared.MixedSIMDHelper:
			if helperBytes < embedded32.SIMDFrameBytes {
				helperBytes = embedded32.SIMDFrameBytes
			}
		}
	}
	helperBase := uint16(valueBase + dataBytes)
	indirectBytes := uint16(0)
	for _, op := range plan.Ops {
		if op.Kind == shared.MixedCallIndirect {
			indirectBytes = 4
			break
		}
	}
	indirectTargetOffset := helperBase + uint16(helperBytes)
	saveOffset := indirectTargetOffset + indirectBytes
	frame := (uint32(saveOffset) + 4 + 15) &^ 15
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

	type mixedBranchPatch struct {
		at, label   int
		conditional bool
	}
	emitTrapReturn := func(kind embedded32.Trap, name string) int {
		at := a.Len()
		must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), name+" trap cell")
		must(a.MovImm32(a32.R0, uint32(kind)), name+" trap")
		must(a.Str(a32.R0, a32.R1, 0), name+" trap write")
		must(a.Ldr(a32.LR, a32.SP, saveOffset), name+" return address restore")
		must(a.MovImm32(a32.R12, frame), name+" frame size")
		must(a.Add(a32.SP, a32.SP, a32.R12), name+" frame release")
		must(a.MovImm32(a32.R0, 0), name+" result")
		a.Ret()
		a.Align4()
		return at
	}
	emitMemoryTrap := func(done int, traps []int, name string) error {
		trap := a.Len()
		must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), name+" trap cell")
		must(a.MovImm32(a32.R0, uint32(embedded32.TrapMemoryOutOfBounds)), name+" trap")
		must(a.Str(a32.R0, a32.R1, 0), name+" trap write")
		must(a.Ldr(a32.LR, a32.SP, saveOffset), name+" trap return address restore")
		must(a.MovImm32(a32.R12, frame), name+" trap frame size")
		must(a.Add(a32.SP, a32.SP, a32.R12), name+" trap frame release")
		must(a.MovImm32(a32.R0, 0), name+" trap result")
		a.Ret()
		a.Align4()
		finish := a.Len()
		if !a.PatchBranch(done, finish) {
			return fmt.Errorf("arm32: mixed %s success branch out of range", name)
		}
		for _, branch := range traps {
			if !a.PatchFarBranch(branch, trap) {
				return fmt.Errorf("arm32: mixed %s trap branch out of range", name)
			}
		}
		return nil
	}
	labels := make([]int, len(plan.Ops)+1)
	var branches []mixedBranchPatch
	for opIndex, op := range plan.Ops {
		labels[opIndex] = a.Len()
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
		case shared.MixedI32Eqz, shared.MixedI32Eq, shared.MixedI32Ne,
			shared.MixedI32LtS, shared.MixedI32LtU, shared.MixedI32GtS, shared.MixedI32GtU,
			shared.MixedI32LeS, shared.MixedI32LeU, shared.MixedI32GeS, shared.MixedI32GeU:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "i32 compare left")
			cond := a32.CondEQ
			if op.Kind == shared.MixedI32Eqz {
				must(a.MovImm32(a32.R1, 0), "i32 eqz zero")
			} else {
				must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "i32 compare right")
				switch op.Kind {
				case shared.MixedI32Eq:
					cond = a32.CondEQ
				case shared.MixedI32Ne:
					cond = a32.CondNE
				case shared.MixedI32LtS:
					cond = a32.CondLT
				case shared.MixedI32LtU:
					cond = a32.CondCC
				case shared.MixedI32GtS:
					cond = a32.CondGT
				case shared.MixedI32GtU:
					cond = a32.CondHI
				case shared.MixedI32LeS:
					cond = a32.CondLE
				case shared.MixedI32LeU:
					cond = a32.CondLS
				case shared.MixedI32GeS:
					cond = a32.CondGE
				case shared.MixedI32GeU:
					cond = a32.CondCS
				}
			}
			must(a.Cmp(a32.R0, a32.R1), "i32 compare")
			must(a.MovImm32(a32.R2, 0), "i32 compare false")
			skip := a.FarBcond(cond.Invert())
			must(a.MovImm32(a32.R2, 1), "i32 compare true")
			if !a.PatchFarBranch(skip, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed i32 comparison branch out of range")
			}
			must(a.Str(a32.R2, a32.SP, off(op.Dst)), "i32 compare result")
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
		case shared.MixedI64Mul:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "i64 multiply left low")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "i64 multiply right low")
			must(a.Umull(a32.R2, a32.R3, a32.R0, a32.R1), "i64 multiply low product")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)+4), "i64 multiply right high")
			must(a.Mul(a32.R0, a32.R0, a32.R1), "i64 multiply first cross product")
			must(a.Add(a32.R3, a32.R3, a32.R0), "i64 multiply first cross add")
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)+4), "i64 multiply left high")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "i64 multiply right low reload")
			must(a.Mul(a32.R0, a32.R0, a32.R1), "i64 multiply second cross product")
			must(a.Add(a32.R3, a32.R3, a32.R0), "i64 multiply second cross add")
			must(a.Str(a32.R2, a32.SP, off(op.Dst)), "i64 multiply result low")
			must(a.Str(a32.R3, a32.SP, off(op.Dst)+4), "i64 multiply result high")
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
		case shared.MixedSIMDHelper:
			must(a.MovImm32(a32.R0, op.HelperOp), "simd helper op")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameOpOffset), "simd helper op store")
			inputStart := 0
			if op.HasMemory {
				if len(op.Args) == 0 || op.Args[0].Type != wasm.I32 {
					return nil, fmt.Errorf("arm32: simd memory helper has no i32 address")
				}
				must(a.Ldr(a32.R0, a32.SP, off(op.Args[0].Slot)), "simd memory address")
				must(a.MovImm32(a32.R1, op.MemoryOffset), "simd memory static offset")
				must(a.Adds(a32.R0, a32.R0, a32.R1), "simd memory effective address")
				addressOK := a.FarBcond(a32.CondCC)
				must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "simd memory overflow trap cell")
				must(a.MovImm32(a32.R0, uint32(embedded32.TrapMemoryOutOfBounds)), "simd memory overflow trap")
				must(a.Str(a32.R0, a32.R1, 0), "simd memory overflow trap write")
				must(a.Ldr(a32.LR, a32.SP, saveOffset), "simd memory overflow return address restore")
				must(a.MovImm32(a32.R12, frame), "simd memory overflow frame size")
				must(a.Add(a32.SP, a32.SP, a32.R12), "simd memory overflow frame release")
				must(a.MovImm32(a32.R0, 0), "simd memory overflow result")
				a.Ret()
				a.Align4()
				if !a.PatchFarBranch(addressOK, a.Len()) {
					return nil, fmt.Errorf("arm32: simd memory overflow branch out of range")
				}
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameAddressOffset), "simd memory address store")
				inputStart = 1
			}
			vectorInput := 0
			vectorBases := []uint16{embedded32.SIMDFrameAOffset, embedded32.SIMDFrameBOffset, embedded32.SIMDFrameCOffset}
			for _, input := range op.Args[inputStart:] {
				width, _ := shared.MixedValueSlots(input.Type)
				if input.Type == wasm.V128 {
					for i := uint16(0); i < 4; i++ {
						must(a.Ldr(a32.R0, a32.SP, off(input.Slot)+i*4), "simd helper vector input load")
						must(a.Str(a32.R0, a32.SP, helperBase+vectorBases[vectorInput]+i*4), "simd helper vector input store")
					}
					vectorInput++
				} else {
					for i := uint8(0); i < width; i++ {
						must(a.Ldr(a32.R0, a32.SP, off(input.Slot)+uint16(i)*4), "simd helper scalar input load")
						must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameScalarLoOffset+uint16(i)*4), "simd helper scalar input store")
					}
					if width == 1 {
						must(a.MovImm32(a32.R0, 0), "simd helper scalar high zero")
						must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameScalarHiOffset), "simd helper scalar high store")
					}
				}
			}
			for i := uint16(0); i < 4; i++ {
				must(a.MovImm32(a32.R0, op.Words[i]), "simd helper immediate")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameImmediateOffset+i*4), "simd helper immediate store")
			}
			must(a.MovImm32(a32.R0, op.Lane), "simd helper lane")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameLaneOffset), "simd helper lane store")
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "simd helper memory base")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameMemoryBaseOffset), "simd helper memory base store")
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "simd helper memory length")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameMemoryLenOffset), "simd helper memory length store")
			must(a.MovReg(a32.R0, a32.SP), "simd helper frame base")
			must(a.MovImm32(a32.R1, uint32(helperBase)), "simd helper frame offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "simd helper frame address")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextHelperTableOffset), "simd helper table")
			must(a.Ldr(a32.R12, a32.R1, embedded32.HelperSIMDOffset), "simd helper target")
			must(a.Blx(a32.R12), "simd helper call")
			must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.SIMDFrameTrapOffset), "simd helper trap")
			must(a.MovImm32(a32.R1, 0), "simd helper trap zero")
			must(a.Cmp(a32.R0, a32.R1), "simd helper trap compare")
			helperOK := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "simd helper trap cell")
			must(a.Str(a32.R0, a32.R1, 0), "simd helper trap publish")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "simd helper trap return address restore")
			must(a.MovImm32(a32.R12, frame), "simd helper trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "simd helper trap frame release")
			must(a.MovImm32(a32.R0, 0), "simd helper trap result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("arm32: simd helper trap branch out of range")
			}
			if len(op.Results) == 0 {
				break
			}
			if len(op.Results) != 1 {
				return nil, fmt.Errorf("arm32: simd helper result arity %d", len(op.Results))
			}
			result := op.Results[0]
			width, _ := shared.MixedValueSlots(result.Type)
			resultBase := uint16(embedded32.SIMDFrameScalarOutOffset)
			if result.Type == wasm.V128 {
				resultBase = embedded32.SIMDFrameOutOffset
			}
			for i := uint8(0); i < width; i++ {
				must(a.Ldr(a32.R0, a32.SP, helperBase+resultBase+uint16(i)*4), "simd helper result load")
				must(a.Str(a32.R0, a32.SP, off(result.Slot)+uint16(i)*4), "simd helper result store")
			}
		case shared.MixedI64Helper:
			must(a.MovImm32(a32.R0, op.HelperOp), "i64 helper op")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.I64FrameOpOffset), "i64 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+uint16(i)*4), "i64 helper left load")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.I64FrameALoOffset+uint16(i)*4), "i64 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.MovImm32(a32.R0, 0), "i64 helper input high zero")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.I64FrameAHiOffset), "i64 helper input high store")
			}
			if op.Arity == 2 {
				for i := uint16(0); i < 2; i++ {
					must(a.Ldr(a32.R0, a32.SP, off(op.Right)+i*4), "i64 helper right load")
					must(a.Str(a32.R0, a32.SP, helperBase+embedded32.I64FrameBLoOffset+i*4), "i64 helper right store")
				}
			}
			must(a.MovReg(a32.R0, a32.SP), "i64 helper frame base")
			must(a.MovImm32(a32.R1, uint32(helperBase)), "i64 helper frame offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "i64 helper frame address")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextHelperTableOffset), "i64 helper table")
			must(a.Ldr(a32.R12, a32.R1, embedded32.HelperI64Offset), "i64 helper target")
			must(a.Blx(a32.R12), "i64 helper call")
			must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.I64FrameTrapOffset), "i64 helper trap")
			must(a.MovImm32(a32.R1, 0), "i64 helper trap zero")
			must(a.Cmp(a32.R0, a32.R1), "i64 helper trap compare")
			helperOK := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "i64 helper trap cell")
			must(a.Str(a32.R0, a32.R1, 0), "i64 helper trap publish")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "i64 helper trap return address restore")
			must(a.MovImm32(a32.R12, frame), "i64 helper trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "i64 helper trap frame release")
			must(a.MovImm32(a32.R0, 0), "i64 helper trap result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("arm32: i64 helper trap branch out of range")
			}
			resultOffset := uint16(embedded32.I64FrameOutLoOffset)
			if op.Width == 1 {
				resultOffset = embedded32.I64FrameI32OutOffset
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Ldr(a32.R0, a32.SP, helperBase+resultOffset+uint16(i)*4), "i64 helper result load")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "i64 helper result store")
			}
		case shared.MixedF32Helper:
			must(a.MovImm32(a32.R0, op.HelperOp), "f32 helper op")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F32FrameOpOffset), "f32 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+uint16(i)*4), "f32 helper left load")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F32FrameALoOffset+uint16(i)*4), "f32 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.MovImm32(a32.R0, 0), "f32 helper input high zero")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F32FrameAHiOffset), "f32 helper input high store")
			}
			if op.Arity == 2 {
				must(a.Ldr(a32.R0, a32.SP, off(op.Right)), "f32 helper right load")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F32FrameBLoOffset), "f32 helper right store")
			}
			must(a.MovReg(a32.R0, a32.SP), "f32 helper frame base")
			must(a.MovImm32(a32.R1, uint32(helperBase)), "f32 helper frame offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "f32 helper frame address")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextHelperTableOffset), "f32 helper table")
			must(a.Ldr(a32.R12, a32.R1, embedded32.HelperF32Offset), "f32 helper target")
			must(a.Blx(a32.R12), "f32 helper call")
			must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.F32FrameTrapOffset), "f32 helper trap")
			must(a.MovImm32(a32.R1, 0), "f32 helper trap zero")
			must(a.Cmp(a32.R0, a32.R1), "f32 helper trap compare")
			helperOK := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "f32 helper trap cell")
			must(a.Str(a32.R0, a32.R1, 0), "f32 helper trap publish")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "f32 helper trap return address restore")
			must(a.MovImm32(a32.R12, frame), "f32 helper trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "f32 helper trap frame release")
			must(a.MovImm32(a32.R0, 0), "f32 helper trap result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("arm32: f32 helper trap branch out of range")
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.F32FrameOutLoOffset+uint16(i)*4), "f32 helper result load")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "f32 helper result store")
			}
		case shared.MixedF64Helper:
			must(a.MovImm32(a32.R0, op.HelperOp), "f64 helper op")
			must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F64FrameOpOffset), "f64 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Ldr(a32.R0, a32.SP, off(op.Left)+uint16(i)*4), "f64 helper left load")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F64FrameALoOffset+uint16(i)*4), "f64 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.MovImm32(a32.R0, 0), "f64 helper input high zero")
				must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F64FrameAHiOffset), "f64 helper input high store")
			}
			if op.Arity == 2 {
				for i := uint16(0); i < 2; i++ {
					must(a.Ldr(a32.R0, a32.SP, off(op.Right)+i*4), "f64 helper right load")
					must(a.Str(a32.R0, a32.SP, helperBase+embedded32.F64FrameBLoOffset+i*4), "f64 helper right store")
				}
			}
			must(a.MovReg(a32.R0, a32.SP), "f64 helper frame base")
			must(a.MovImm32(a32.R1, uint32(helperBase)), "f64 helper frame offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "f64 helper frame address")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextHelperTableOffset), "f64 helper table")
			must(a.Ldr(a32.R12, a32.R1, embedded32.HelperF64Offset), "f64 helper target")
			must(a.Blx(a32.R12), "f64 helper call")
			must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.F64FrameTrapOffset), "f64 helper trap")
			must(a.MovImm32(a32.R1, 0), "f64 helper trap zero")
			must(a.Cmp(a32.R0, a32.R1), "f64 helper trap compare")
			helperOK := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "f64 helper trap cell")
			must(a.Str(a32.R0, a32.R1, 0), "f64 helper trap publish")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "f64 helper trap return address restore")
			must(a.MovImm32(a32.R12, frame), "f64 helper trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "f64 helper trap frame release")
			must(a.MovImm32(a32.R0, 0), "f64 helper trap result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("arm32: f64 helper trap branch out of range")
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Ldr(a32.R0, a32.SP, helperBase+embedded32.F64FrameOutLoOffset+uint16(i)*4), "f64 helper result load")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)+uint16(i)*4), "f64 helper result store")
			}
		case shared.MixedMemoryLoad:
			width, resultWords, signed, ok := embedded32.ScalarLoadInfo(embedded32.ScalarLoadOp(op.MemoryOp))
			if !ok {
				return nil, fmt.Errorf("arm32: invalid mixed scalar load %d", op.MemoryOp)
			}
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory load address")
			must(a.MovImm32(a32.R1, op.MemoryOffset), "memory load static offset")
			must(a.Adds(a32.R0, a32.R0, a32.R1), "memory load effective address")
			traps := []int{a.FarBcond(a32.CondCS)}
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory load length")
			must(a.MovImm32(a32.R2, width), "memory load width")
			must(a.Cmp(a32.R1, a32.R2), "memory load short compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Sub(a32.R1, a32.R1, a32.R2), "memory load bound")
			must(a.Cmp(a32.R1, a32.R0), "memory load bounds compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory load base")
			must(a.Add(a32.R1, a32.R1, a32.R0), "memory load pointer")
			switch width {
			case 1:
				if signed {
					must(a.Ldrsb(a32.R2, a32.R1, 0), "memory load8 signed")
				} else {
					must(a.Ldrb(a32.R2, a32.R1, 0), "memory load8 unsigned")
				}
			case 2:
				if signed {
					must(a.Ldrsh(a32.R2, a32.R1, 0), "memory load16 signed")
				} else {
					must(a.Ldrh(a32.R2, a32.R1, 0), "memory load16 unsigned")
				}
			case 4:
				must(a.Ldr(a32.R2, a32.R1, 0), "memory load32")
			case 8:
				must(a.Ldr(a32.R2, a32.R1, 0), "memory load64 low")
				must(a.Ldr(a32.R3, a32.R1, 4), "memory load64 high")
			}
			must(a.Str(a32.R2, a32.SP, off(op.Dst)), "memory load result low")
			if resultWords == 2 {
				if width < 8 {
					if signed {
						must(a.AsrImm(a32.R3, a32.R2, 31), "memory load sign high")
					} else {
						must(a.MovImm32(a32.R3, 0), "memory load zero high")
					}
				}
				must(a.Str(a32.R3, a32.SP, off(op.Dst)+4), "memory load result high")
			}
			done := a.Branch()
			trap := a.Len()
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "memory load trap cell")
			must(a.MovImm32(a32.R0, uint32(embedded32.TrapMemoryOutOfBounds)), "memory load trap")
			must(a.Str(a32.R0, a32.R1, 0), "memory load trap write")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "memory load trap return address restore")
			must(a.MovImm32(a32.R12, frame), "memory load trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "memory load trap frame release")
			must(a.MovImm32(a32.R0, 0), "memory load trap result")
			a.Ret()
			a.Align4()
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed memory load success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("arm32: mixed memory load trap branch out of range")
				}
			}
		case shared.MixedMemoryStore:
			width, _, ok := embedded32.ScalarStoreInfo(embedded32.ScalarStoreOp(op.MemoryOp))
			if !ok {
				return nil, fmt.Errorf("arm32: invalid mixed scalar store %d", op.MemoryOp)
			}
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory store address")
			must(a.MovImm32(a32.R1, op.MemoryOffset), "memory store static offset")
			must(a.Adds(a32.R0, a32.R0, a32.R1), "memory store effective address")
			traps := []int{a.FarBcond(a32.CondCS)}
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory store length")
			must(a.MovImm32(a32.R2, width), "memory store width")
			must(a.Cmp(a32.R1, a32.R2), "memory store short compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Sub(a32.R1, a32.R1, a32.R2), "memory store bound")
			must(a.Cmp(a32.R1, a32.R0), "memory store bounds compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory store base")
			must(a.Add(a32.R1, a32.R1, a32.R0), "memory store pointer")
			must(a.Ldr(a32.R2, a32.SP, off(op.Right)), "memory store value low")
			switch width {
			case 1:
				must(a.Strb(a32.R2, a32.R1, 0), "memory store8")
			case 2:
				must(a.Strh(a32.R2, a32.R1, 0), "memory store16")
			case 4:
				must(a.Str(a32.R2, a32.R1, 0), "memory store32")
			case 8:
				must(a.Ldr(a32.R3, a32.SP, off(op.Right)+4), "memory store value high")
				must(a.Str(a32.R2, a32.R1, 0), "memory store64 low")
				must(a.Str(a32.R3, a32.R1, 4), "memory store64 high")
			}
			done := a.Branch()
			trap := a.Len()
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "memory store trap cell")
			must(a.MovImm32(a32.R0, uint32(embedded32.TrapMemoryOutOfBounds)), "memory store trap")
			must(a.Str(a32.R0, a32.R1, 0), "memory store trap write")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "memory store trap return address restore")
			must(a.MovImm32(a32.R12, frame), "memory store trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "memory store trap frame release")
			must(a.MovImm32(a32.R0, 0), "memory store trap result")
			a.Ret()
			a.Align4()
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed memory store success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("arm32: mixed memory store trap branch out of range")
				}
			}
		case shared.MixedMemoryInit:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory.init destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "memory.init source offset")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "memory.init count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextDataSegmentsBaseOffset), "memory.init descriptor base")
			must(a.MovImm32(a32.R12, op.Target*embedded32.DataSegmentABIBytes), "memory.init descriptor offset")
			must(a.Add(a32.R3, a32.R3, a32.R12), "memory.init descriptor address")
			must(a.Ldr(a32.LR, a32.R3, embedded32.DataSegmentDroppedOffset), "memory.init dropped flag")
			must(a.MovImm32(a32.R12, 0), "memory.init zero")
			must(a.Cmp(a32.LR, a32.R12), "memory.init dropped compare")
			available := a.FarBcond(a32.CondEQ)
			must(a.MovImm32(a32.LR, 0), "memory.init dropped length")
			lengthReady := a.Branch()
			availableTarget := a.Len()
			must(a.Ldr(a32.LR, a32.R3, embedded32.DataSegmentLengthOffset), "memory.init data length")
			lengthTarget := a.Len()
			if !a.PatchFarBranch(available, availableTarget) || !a.PatchBranch(lengthReady, lengthTarget) {
				return nil, fmt.Errorf("arm32: mixed memory.init data length branch out of range")
			}
			must(a.Cmp(a32.LR, a32.R2), "memory.init source size")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.LR, a32.LR, a32.R2), "memory.init source bound")
			must(a.Cmp(a32.LR, a32.R1), "memory.init source compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.LR, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.init memory length")
			must(a.Cmp(a32.LR, a32.R2), "memory.init destination size")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Sub(a32.LR, a32.LR, a32.R2), "memory.init destination bound")
			must(a.Cmp(a32.LR, a32.R0), "memory.init destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, a32.R3, embedded32.DataSegmentBaseOffset), "memory.init data base")
			must(a.Add(a32.R3, a32.R3, a32.R1), "memory.init source")
			must(a.Ldr(a32.LR, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.init memory base")
			must(a.Add(a32.R0, a32.R0, a32.LR), "memory.init destination pointer")
			loop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "memory.init done")
			copied := a.FarBcond(a32.CondEQ)
			must(a.Ldrb(a32.LR, a32.R3, 0), "memory.init load")
			must(a.Strb(a32.LR, a32.R0, 0), "memory.init store")
			must(a.MovImm32(a32.R12, 1), "memory.init step")
			must(a.Add(a32.R3, a32.R3, a32.R12), "memory.init source advance")
			must(a.Add(a32.R0, a32.R0, a32.R12), "memory.init destination advance")
			must(a.Sub(a32.R2, a32.R2, a32.R12), "memory.init count decrement")
			must(a.MovImm32(a32.R12, 0), "memory.init restore zero")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(copied, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed memory.init loop out of range")
			}
			done := a.Branch()
			if err := emitMemoryTrap(done, traps, "memory.init"); err != nil {
				return nil, err
			}
		case shared.MixedDataDrop:
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextDataSegmentsBaseOffset), "data.drop descriptor base")
			must(a.MovImm32(a32.R1, op.Target*embedded32.DataSegmentABIBytes), "data.drop descriptor offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "data.drop descriptor address")
			must(a.MovImm32(a32.R1, 1), "data.drop value")
			must(a.Str(a32.R1, a32.R0, embedded32.DataSegmentDroppedOffset), "data.drop store")
		case shared.MixedMemoryCopy:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory.copy destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "memory.copy source")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "memory.copy count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.copy length")
			must(a.Cmp(a32.R3, a32.R2), "memory.copy size compare")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.R3, a32.R3, a32.R2), "memory.copy bound")
			must(a.Cmp(a32.R3, a32.R0), "memory.copy destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Cmp(a32.R3, a32.R1), "memory.copy source compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.copy base")
			must(a.Add(a32.R0, a32.R0, a32.R3), "memory.copy destination pointer")
			must(a.Add(a32.R1, a32.R1, a32.R3), "memory.copy source pointer")
			must(a.MovImm32(a32.R12, 0), "memory.copy zero")
			must(a.Cmp(a32.R0, a32.R1), "memory.copy direction")
			forward := a.FarBcond(a32.CondLS)
			must(a.Add(a32.R0, a32.R0, a32.R2), "memory.copy backward destination")
			must(a.Add(a32.R1, a32.R1, a32.R2), "memory.copy backward source")
			backLoop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "memory.copy backward done")
			backDone := a.FarBcond(a32.CondEQ)
			must(a.MovImm32(a32.R3, 1), "memory.copy backward step")
			must(a.Sub(a32.R0, a32.R0, a32.R3), "memory.copy destination decrement")
			must(a.Sub(a32.R1, a32.R1, a32.R3), "memory.copy source decrement")
			must(a.Ldrb(a32.R12, a32.R1, 0), "memory.copy backward load")
			must(a.Strb(a32.R12, a32.R0, 0), "memory.copy backward store")
			must(a.Sub(a32.R2, a32.R2, a32.R3), "memory.copy backward count")
			must(a.MovImm32(a32.R12, 0), "memory.copy restore zero")
			back := a.Branch()
			if !a.PatchBranch(back, backLoop) {
				return nil, fmt.Errorf("arm32: mixed memory.copy backward loop out of range")
			}
			forwardTarget := a.Len()
			if !a.PatchFarBranch(forward, forwardTarget) {
				return nil, fmt.Errorf("arm32: mixed memory.copy direction branch out of range")
			}
			forwardLoop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "memory.copy forward done")
			forwardDone := a.FarBcond(a32.CondEQ)
			must(a.Ldrb(a32.R12, a32.R1, 0), "memory.copy forward load")
			must(a.Strb(a32.R12, a32.R0, 0), "memory.copy forward store")
			must(a.MovImm32(a32.R3, 1), "memory.copy forward step")
			must(a.Add(a32.R0, a32.R0, a32.R3), "memory.copy destination advance")
			must(a.Add(a32.R1, a32.R1, a32.R3), "memory.copy source advance")
			must(a.Sub(a32.R2, a32.R2, a32.R3), "memory.copy forward count")
			must(a.MovImm32(a32.R12, 0), "memory.copy restore zero")
			forwardBack := a.Branch()
			if !a.PatchBranch(forwardBack, forwardLoop) {
				return nil, fmt.Errorf("arm32: mixed memory.copy forward loop out of range")
			}
			finishCopy := a.Len()
			if !a.PatchFarBranch(backDone, finishCopy) || !a.PatchFarBranch(forwardDone, finishCopy) {
				return nil, fmt.Errorf("arm32: mixed memory.copy done branch out of range")
			}
			done := a.Branch()
			if err := emitMemoryTrap(done, traps, "memory.copy"); err != nil {
				return nil, err
			}
		case shared.MixedMemoryFill:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory.fill destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "memory.fill value")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "memory.fill count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.fill length")
			must(a.Cmp(a32.R3, a32.R2), "memory.fill size compare")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.R3, a32.R3, a32.R2), "memory.fill bound")
			must(a.Cmp(a32.R3, a32.R0), "memory.fill destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.fill base")
			must(a.Add(a32.R0, a32.R0, a32.R3), "memory.fill destination pointer")
			must(a.MovImm32(a32.R12, 0), "memory.fill zero")
			loop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "memory.fill done")
			filled := a.FarBcond(a32.CondEQ)
			must(a.Strb(a32.R1, a32.R0, 0), "memory.fill store")
			must(a.MovImm32(a32.R3, 1), "memory.fill step")
			must(a.Add(a32.R0, a32.R0, a32.R3), "memory.fill advance")
			must(a.Sub(a32.R2, a32.R2, a32.R3), "memory.fill count decrement")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(filled, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed memory.fill loop out of range")
			}
			done := a.Branch()
			if err := emitMemoryTrap(done, traps, "memory.fill"); err != nil {
				return nil, err
			}
		case shared.MixedMemorySize:
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.size length")
			must(a.LsrImm(a32.R0, a32.R0, 16), "memory.size pages")
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "memory.size result")
		case shared.MixedMemoryGrow:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "memory.grow delta")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow current")
			must(a.MovReg(a32.R2, a32.R1), "memory.grow old")
			must(a.LsrImm(a32.R2, a32.R2, 16), "memory.grow old pages")
			must(a.LsrImm(a32.R3, a32.R0, 16), "memory.grow delta overflow")
			must(a.MovImm32(a32.R12, 0), "memory.grow zero")
			must(a.Cmp(a32.R3, a32.R12), "memory.grow delta compare")
			fails := []int{a.FarBcond(a32.CondNE)}
			must(a.LslImm(a32.R0, a32.R0, 16), "memory.grow delta bytes")
			must(a.Adds(a32.R0, a32.R1, a32.R0), "memory.grow new length")
			fails = append(fails, a.FarBcond(a32.CondCS))
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryMaximumOffset), "memory.grow maximum")
			must(a.Cmp(a32.R3, a32.R0), "memory.grow maximum compare")
			fails = append(fails, a.FarBcond(a32.CondCC))
			must(a.Str(a32.R0, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow publish length")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.grow base")
			must(a.Add(a32.R1, a32.R3, a32.R1), "memory.grow clear start")
			must(a.Add(a32.R3, a32.R3, a32.R0), "memory.grow clear end")
			loop := a.Len()
			must(a.Cmp(a32.R1, a32.R3), "memory.grow clear compare")
			cleared := a.FarBcond(a32.CondEQ)
			must(a.Str(a32.R12, a32.R1, 0), "memory.grow clear word")
			must(a.MovImm32(a32.R0, 4), "memory.grow clear step")
			must(a.Add(a32.R1, a32.R1, a32.R0), "memory.grow clear advance")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(cleared, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed memory.grow clear branch out of range")
			}
			done := a.Branch()
			fail := a.Len()
			must(a.MovImm32(a32.R2, 0xffffffff), "memory.grow failure result")
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed memory.grow done branch out of range")
			}
			for _, at := range fails {
				if !a.PatchFarBranch(at, fail) {
					return nil, fmt.Errorf("arm32: mixed memory.grow failure branch out of range")
				}
			}
			must(a.Str(a32.R2, a32.SP, off(op.Dst)), "memory.grow result")
		case shared.MixedGlobalGet, shared.MixedGlobalSet:
			if uint64(op.Target)+uint64(op.Width) > 1024 {
				return nil, fmt.Errorf("arm32: mixed global slot %d width %d exceeds direct displacement", op.Target, op.Width)
			}
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextGlobalsBaseOffset), "global base")
			for i := uint8(0); i < op.Width; i++ {
				globalOffset := uint16((op.Target + uint32(i)) * 4)
				if op.Kind == shared.MixedGlobalGet {
					must(a.Ldr(a32.R1, a32.R0, globalOffset), "global.get")
					must(a.Str(a32.R1, a32.SP, off(op.Dst)+uint16(i)*4), "global.get result")
				} else {
					must(a.Ldr(a32.R1, a32.SP, off(op.Left)+uint16(i)*4), "global.set value")
					must(a.Str(a32.R1, a32.R0, globalOffset), "global.set")
				}
			}
		case shared.MixedTableGet, shared.MixedTableSet:
			if op.Target != 0 {
				return nil, fmt.Errorf("arm32: mixed table index %d is not supported", op.Target)
			}
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "table element index")
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTableOffset), "table descriptor")
			must(a.Ldr(a32.R2, a32.R1, embedded32.TableABILengthOffset), "table length")
			must(a.Cmp(a32.R0, a32.R2), "table bounds compare")
			outOfBounds := a.FarBcond(a32.CondCS)
			must(a.Ldr(a32.R2, a32.R1, embedded32.TableABIEntriesBaseOffset), "table entries")
			must(a.LslImm(a32.R3, a32.R0, 2), "table entry offset")
			must(a.Add(a32.R2, a32.R2, a32.R3), "table entry address")
			if op.Kind == shared.MixedTableGet {
				must(a.Ldr(a32.R0, a32.R2, 0), "table.get")
				must(a.Str(a32.R0, a32.SP, off(op.Dst)), "table.get result")
			} else {
				must(a.Ldr(a32.R0, a32.SP, off(op.Right)), "table.set value")
				must(a.Str(a32.R0, a32.R2, 0), "table.set")
			}
			done := a.Branch()
			trap := a.Len()
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "table trap cell")
			must(a.MovImm32(a32.R0, uint32(embedded32.TrapTableOutOfBounds)), "table trap")
			must(a.Str(a32.R0, a32.R1, 0), "table trap write")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "table trap return address restore")
			must(a.MovImm32(a32.R12, frame), "table trap frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "table trap frame release")
			must(a.MovImm32(a32.R0, 0), "table trap result")
			a.Ret()
			a.Align4()
			finish := a.Len()
			if !a.PatchBranch(done, finish) || !a.PatchFarBranch(outOfBounds, trap) {
				return nil, fmt.Errorf("arm32: mixed table branch out of range")
			}
		case shared.MixedTableInit:
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "table.init destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "table.init source offset")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "table.init count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextTableOffset), "table.init table descriptor")
			must(a.Ldr(a32.LR, a32.R3, embedded32.TableABILengthOffset), "table.init table length")
			must(a.Cmp(a32.LR, a32.R2), "table.init destination size")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.LR, a32.LR, a32.R2), "table.init destination bound")
			must(a.Cmp(a32.LR, a32.R0), "table.init destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, a32.R3, embedded32.TableABIElementSegmentsBaseOffset), "table.init element descriptor base")
			must(a.MovImm32(a32.R12, op.Target*embedded32.DataSegmentABIBytes), "table.init element descriptor offset")
			must(a.Add(a32.R3, a32.R3, a32.R12), "table.init element descriptor address")
			must(a.Ldr(a32.LR, a32.R3, embedded32.DataSegmentDroppedOffset), "table.init dropped flag")
			must(a.MovImm32(a32.R12, 0), "table.init zero")
			must(a.Cmp(a32.LR, a32.R12), "table.init dropped compare")
			available := a.FarBcond(a32.CondEQ)
			must(a.MovImm32(a32.LR, 0), "table.init dropped length")
			lengthReady := a.Branch()
			availableTarget := a.Len()
			must(a.Ldr(a32.LR, a32.R3, embedded32.DataSegmentLengthOffset), "table.init element length")
			lengthTarget := a.Len()
			if !a.PatchFarBranch(available, availableTarget) || !a.PatchBranch(lengthReady, lengthTarget) {
				return nil, fmt.Errorf("arm32: mixed table.init element length branch out of range")
			}
			must(a.Cmp(a32.LR, a32.R2), "table.init source size")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Sub(a32.LR, a32.LR, a32.R2), "table.init source bound")
			must(a.Cmp(a32.LR, a32.R1), "table.init source compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, a32.R3, embedded32.DataSegmentBaseOffset), "table.init payload base")
			must(a.LslImm(a32.R12, a32.R1, 2), "table.init source byte offset")
			must(a.Add(a32.R3, a32.R3, a32.R12), "table.init source pointer")
			must(a.Ldr(a32.LR, armContextReg, embedded32.ContextTableOffset), "table.init table descriptor restore")
			must(a.Ldr(a32.LR, a32.LR, embedded32.TableABIEntriesBaseOffset), "table.init entries")
			must(a.LslImm(a32.R12, a32.R0, 2), "table.init destination byte offset")
			must(a.Add(a32.LR, a32.LR, a32.R12), "table.init destination pointer")
			must(a.MovImm32(a32.R12, 0), "table.init loop zero")
			loop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "table.init done")
			copied := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R0, a32.R3, 0), "table.init load")
			must(a.Str(a32.R0, a32.LR, 0), "table.init store")
			must(a.MovImm32(a32.R0, 4), "table.init pointer step")
			must(a.Add(a32.R3, a32.R3, a32.R0), "table.init source advance")
			must(a.Add(a32.LR, a32.LR, a32.R0), "table.init destination advance")
			must(a.MovImm32(a32.R0, 1), "table.init count step")
			must(a.Sub(a32.R2, a32.R2, a32.R0), "table.init count decrement")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(copied, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed table.init loop out of range")
			}
			done := a.Branch()
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.init bounds")
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed table.init success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("arm32: mixed table.init trap branch out of range")
				}
			}
		case shared.MixedElemDrop:
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextTableOffset), "elem.drop table descriptor")
			must(a.Ldr(a32.R0, a32.R0, embedded32.TableABIElementSegmentsBaseOffset), "elem.drop descriptor base")
			must(a.MovImm32(a32.R1, op.Target*embedded32.DataSegmentABIBytes), "elem.drop descriptor offset")
			must(a.Add(a32.R0, a32.R0, a32.R1), "elem.drop descriptor address")
			must(a.MovImm32(a32.R1, 1), "elem.drop value")
			must(a.Str(a32.R1, a32.R0, embedded32.DataSegmentDroppedOffset), "elem.drop store")
		case shared.MixedTableSize:
			if op.Target != 0 {
				return nil, fmt.Errorf("arm32: mixed table.size index %d is not supported", op.Target)
			}
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextTableOffset), "table.size descriptor")
			must(a.Ldr(a32.R0, a32.R0, embedded32.TableABILengthOffset), "table.size length")
			must(a.Str(a32.R0, a32.SP, off(op.Dst)), "table.size result")
		case shared.MixedTableGrow:
			if op.Target != 0 {
				return nil, fmt.Errorf("arm32: mixed table.grow index %d is not supported", op.Target)
			}
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextTableOffset), "table.grow descriptor")
			must(a.Ldr(a32.R1, a32.R0, embedded32.TableABILengthOffset), "table.grow old length")
			must(a.Ldr(a32.R2, a32.SP, off(op.Right)), "table.grow delta")
			must(a.Adds(a32.R2, a32.R1, a32.R2), "table.grow new length")
			fails := []int{a.FarBcond(a32.CondCS)}
			must(a.Ldr(a32.R3, a32.R0, embedded32.TableABIMaximumOffset), "table.grow maximum")
			must(a.Cmp(a32.R3, a32.R2), "table.grow maximum compare")
			fails = append(fails, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, a32.R0, embedded32.TableABIEntriesBaseOffset), "table.grow entries")
			must(a.LslImm(a32.R12, a32.R1, 2), "table.grow old offset")
			must(a.Add(a32.R3, a32.R3, a32.R12), "table.grow fill pointer")
			must(a.Ldr(a32.R12, a32.SP, off(op.Right)), "table.grow fill count")
			must(a.Ldr(a32.LR, a32.SP, off(op.Left)), "table.grow fill value")
			loop := a.Len()
			must(a.MovImm32(a32.R0, 0), "table.grow zero")
			must(a.Cmp(a32.R12, a32.R0), "table.grow fill done")
			filled := a.FarBcond(a32.CondEQ)
			must(a.Str(a32.LR, a32.R3, 0), "table.grow fill store")
			must(a.MovImm32(a32.R0, 4), "table.grow fill step")
			must(a.Add(a32.R3, a32.R3, a32.R0), "table.grow fill advance")
			must(a.MovImm32(a32.R0, 1), "table.grow count step")
			must(a.Sub(a32.R12, a32.R12, a32.R0), "table.grow count decrement")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(filled, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed table.grow fill branch out of range")
			}
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextTableOffset), "table.grow descriptor restore")
			must(a.Str(a32.R2, a32.R0, embedded32.TableABILengthOffset), "table.grow publish length")
			done := a.Branch()
			fail := a.Len()
			must(a.MovImm32(a32.R1, 0xffffffff), "table.grow failure result")
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed table.grow done branch out of range")
			}
			for _, branch := range fails {
				if !a.PatchFarBranch(branch, fail) {
					return nil, fmt.Errorf("arm32: mixed table.grow failure branch out of range")
				}
			}
			must(a.Str(a32.R1, a32.SP, off(op.Dst)), "table.grow result")
		case shared.MixedTableFill:
			if op.Target != 0 {
				return nil, fmt.Errorf("arm32: mixed table.fill index %d is not supported", op.Target)
			}
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "table.fill destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "table.fill value")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "table.fill count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextTableOffset), "table.fill descriptor")
			must(a.Ldr(a32.LR, a32.R3, embedded32.TableABILengthOffset), "table.fill length")
			must(a.Cmp(a32.LR, a32.R2), "table.fill size compare")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.LR, a32.LR, a32.R2), "table.fill bound")
			must(a.Cmp(a32.LR, a32.R0), "table.fill destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, a32.R3, embedded32.TableABIEntriesBaseOffset), "table.fill entries")
			must(a.LslImm(a32.R12, a32.R0, 2), "table.fill destination offset")
			must(a.Add(a32.R3, a32.R3, a32.R12), "table.fill destination pointer")
			must(a.MovImm32(a32.R12, 0), "table.fill zero")
			loop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "table.fill done")
			filled := a.FarBcond(a32.CondEQ)
			must(a.Str(a32.R1, a32.R3, 0), "table.fill store")
			must(a.MovImm32(a32.LR, 4), "table.fill pointer step")
			must(a.Add(a32.R3, a32.R3, a32.LR), "table.fill advance")
			must(a.MovImm32(a32.LR, 1), "table.fill count step")
			must(a.Sub(a32.R2, a32.R2, a32.LR), "table.fill count decrement")
			back := a.Branch()
			if !a.PatchBranch(back, loop) || !a.PatchFarBranch(filled, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed table.fill loop out of range")
			}
			done := a.Branch()
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.fill bounds")
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed table.fill success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("arm32: mixed table.fill trap branch out of range")
				}
			}
		case shared.MixedTableCopy:
			if op.Target != 0 || op.Lane != 0 {
				return nil, fmt.Errorf("arm32: mixed table.copy indexes %d/%d are not supported", op.Target, op.Lane)
			}
			must(a.Ldr(a32.R0, a32.SP, off(op.Left)), "table.copy destination")
			must(a.Ldr(a32.R1, a32.SP, off(op.Right)), "table.copy source")
			must(a.Ldr(a32.R2, a32.SP, off(op.Third)), "table.copy count")
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextTableOffset), "table.copy descriptor")
			must(a.Ldr(a32.R3, a32.R3, embedded32.TableABILengthOffset), "table.copy length")
			must(a.Cmp(a32.R3, a32.R2), "table.copy size compare")
			traps := []int{a.FarBcond(a32.CondCC)}
			must(a.Sub(a32.R3, a32.R3, a32.R2), "table.copy bound")
			must(a.Cmp(a32.R3, a32.R0), "table.copy destination compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Cmp(a32.R3, a32.R1), "table.copy source compare")
			traps = append(traps, a.FarBcond(a32.CondCC))
			must(a.Ldr(a32.R3, armContextReg, embedded32.ContextTableOffset), "table.copy descriptor restore")
			must(a.Ldr(a32.R3, a32.R3, embedded32.TableABIEntriesBaseOffset), "table.copy entries")
			must(a.LslImm(a32.R12, a32.R0, 2), "table.copy destination offset")
			must(a.Add(a32.R0, a32.R3, a32.R12), "table.copy destination pointer")
			must(a.LslImm(a32.R12, a32.R1, 2), "table.copy source offset")
			must(a.Add(a32.R1, a32.R3, a32.R12), "table.copy source pointer")
			must(a.MovImm32(a32.R12, 0), "table.copy zero")
			must(a.Cmp(a32.R0, a32.R1), "table.copy direction")
			forward := a.FarBcond(a32.CondLS)
			must(a.LslImm(a32.LR, a32.R2, 2), "table.copy backward byte count")
			must(a.Add(a32.R0, a32.R0, a32.LR), "table.copy backward destination")
			must(a.Add(a32.R1, a32.R1, a32.LR), "table.copy backward source")
			backLoop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "table.copy backward done")
			backDone := a.FarBcond(a32.CondEQ)
			must(a.MovImm32(a32.LR, 4), "table.copy backward step")
			must(a.Sub(a32.R0, a32.R0, a32.LR), "table.copy destination decrement")
			must(a.Sub(a32.R1, a32.R1, a32.LR), "table.copy source decrement")
			must(a.Ldr(a32.LR, a32.R1, 0), "table.copy backward load")
			must(a.Str(a32.LR, a32.R0, 0), "table.copy backward store")
			must(a.MovImm32(a32.LR, 1), "table.copy count step")
			must(a.Sub(a32.R2, a32.R2, a32.LR), "table.copy backward count")
			back := a.Branch()
			if !a.PatchBranch(back, backLoop) {
				return nil, fmt.Errorf("arm32: mixed table.copy backward loop out of range")
			}
			forwardTarget := a.Len()
			if !a.PatchFarBranch(forward, forwardTarget) {
				return nil, fmt.Errorf("arm32: mixed table.copy direction branch out of range")
			}
			forwardLoop := a.Len()
			must(a.Cmp(a32.R2, a32.R12), "table.copy forward done")
			forwardDone := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.LR, a32.R1, 0), "table.copy forward load")
			must(a.Str(a32.LR, a32.R0, 0), "table.copy forward store")
			must(a.MovImm32(a32.LR, 4), "table.copy forward step")
			must(a.Add(a32.R0, a32.R0, a32.LR), "table.copy destination advance")
			must(a.Add(a32.R1, a32.R1, a32.LR), "table.copy source advance")
			must(a.MovImm32(a32.LR, 1), "table.copy count step")
			must(a.Sub(a32.R2, a32.R2, a32.LR), "table.copy forward count")
			forwardBack := a.Branch()
			if !a.PatchBranch(forwardBack, forwardLoop) {
				return nil, fmt.Errorf("arm32: mixed table.copy forward loop out of range")
			}
			finishCopy := a.Len()
			if !a.PatchFarBranch(backDone, finishCopy) || !a.PatchFarBranch(forwardDone, finishCopy) {
				return nil, fmt.Errorf("arm32: mixed table.copy done branch out of range")
			}
			done := a.Branch()
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.copy bounds")
			finish := a.Len()
			if !a.PatchBranch(done, finish) {
				return nil, fmt.Errorf("arm32: mixed table.copy success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("arm32: mixed table.copy trap branch out of range")
				}
			}
		case shared.MixedBranchZero, shared.MixedBranchNonzero:
			must(a.Ldr(a32.R0, a32.SP, off(op.Third)), "branch condition")
			must(a.MovImm32(a32.R1, 0), "branch zero")
			must(a.Cmp(a32.R0, a32.R1), "branch compare")
			cond := a32.CondEQ
			if op.Kind == shared.MixedBranchNonzero {
				cond = a32.CondNE
			}
			branches = append(branches, mixedBranchPatch{at: a.FarBcond(cond), label: op.Label, conditional: true})
		case shared.MixedJump:
			branches = append(branches, mixedBranchPatch{at: a.Branch(), label: op.Label})
		case shared.MixedPollCancellation:
			must(a.Ldr(a32.R0, armContextReg, embedded32.ContextCancelCellOffset), "loop cancel cell")
			must(a.Ldr(a32.R0, a32.R0, 0), "loop cancel value")
			must(a.MovImm32(a32.R1, 0), "loop cancel zero")
			must(a.Cmp(a32.R0, a32.R1), "loop cancel compare")
			loopClear := a.FarBcond(a32.CondEQ)
			must(a.Ldr(a32.R1, armContextReg, embedded32.ContextTrapCellOffset), "loop cancel trap cell")
			must(a.MovImm32(a32.R0, uint32(embedded32.TrapCanceled)), "loop cancel trap")
			must(a.Str(a32.R0, a32.R1, 0), "loop cancel trap write")
			must(a.Ldr(a32.LR, a32.SP, saveOffset), "loop cancel return address restore")
			must(a.MovImm32(a32.R12, frame), "loop cancel frame size")
			must(a.Add(a32.SP, a32.SP, a32.R12), "loop cancel frame release")
			must(a.MovImm32(a32.R0, 0), "loop cancel result")
			a.Ret()
			a.Align4()
			if !a.PatchFarBranch(loopClear, a.Len()) {
				return nil, fmt.Errorf("arm32: mixed loop cancellation branch out of range")
			}
		case shared.MixedCall, shared.MixedCallIndirect:
			if op.Kind == shared.MixedCallIndirect {
				must(a.Ldr(a32.R0, a32.SP, off(op.Third)), "indirect table index")
				must(a.Ldr(a32.R3, armContextReg, embedded32.ContextTableOffset), "indirect table descriptor")
				must(a.Ldr(a32.R1, a32.R3, embedded32.TableABILengthOffset), "indirect table length")
				must(a.Cmp(a32.R0, a32.R1), "indirect table bounds compare")
				outOfBounds := a.FarBcond(a32.CondCS)
				must(a.Ldr(a32.R1, a32.R3, embedded32.TableABIEntriesBaseOffset), "indirect table entries")
				must(a.LslImm(a32.R2, a32.R0, 2), "indirect table offset")
				must(a.Add(a32.R1, a32.R1, a32.R2), "indirect table entry address")
				must(a.Ldr(a32.R1, a32.R1, 0), "indirect table entry")
				must(a.MovImm32(a32.R2, 0), "indirect null zero")
				must(a.Cmp(a32.R1, a32.R2), "indirect null compare")
				null := a.FarBcond(a32.CondEQ)
				must(a.MovImm32(a32.R2, 1), "indirect function bias")
				must(a.Sub(a32.R1, a32.R1, a32.R2), "indirect function index")
				must(a.Ldr(a32.R0, a32.R3, embedded32.TableABIFunctionTypesBaseOffset), "indirect function types")
				must(a.LslImm(a32.R2, a32.R1, 2), "indirect function type offset")
				must(a.Add(a32.R0, a32.R0, a32.R2), "indirect function type address")
				must(a.Ldr(a32.R0, a32.R0, 0), "indirect actual type")
				must(a.MovImm32(a32.R2, op.Target), "indirect expected type")
				must(a.Cmp(a32.R0, a32.R2), "indirect type compare")
				typeMismatch := a.FarBcond(a32.CondNE)
				must(a.Ldr(a32.R0, a32.R3, embedded32.TableABIFunctionEntriesBaseOffset), "indirect function entries")
				must(a.LslImm(a32.R2, a32.R1, 2), "indirect function entry offset")
				must(a.Add(a32.R0, a32.R0, a32.R2), "indirect function entry address")
				must(a.Ldr(a32.R0, a32.R0, 0), "indirect call target")
				must(a.Str(a32.R0, a32.SP, indirectTargetOffset), "indirect call target save")
				resolved := a.Branch()
				outOfBoundsTarget := emitTrapReturn(embedded32.TrapTableOutOfBounds, "indirect table bounds")
				nullTarget := emitTrapReturn(embedded32.TrapIndirectCallNull, "indirect null")
				typeMismatchTarget := emitTrapReturn(embedded32.TrapIndirectCallTypeMismatch, "indirect type")
				resolvedTarget := a.Len()
				if !a.PatchBranch(resolved, resolvedTarget) || !a.PatchFarBranch(outOfBounds, outOfBoundsTarget) || !a.PatchFarBranch(null, nullTarget) || !a.PatchFarBranch(typeMismatch, typeMismatchTarget) {
					return nil, fmt.Errorf("arm32: mixed indirect resolution branch out of range")
				}
			} else if relocSink == nil {
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
			if op.Kind == shared.MixedCallIndirect {
				must(a.Ldr(a32.R12, a32.SP, indirectTargetOffset), "indirect call target restore")
				must(a.Blx(a32.R12), "indirect call")
			} else {
				at := a.Call()
				*relocSink = append(*relocSink, callReloc{at: at, target: int(op.Target)})
			}
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

	labels[len(plan.Ops)] = a.Len()
	for _, branch := range branches {
		if branch.label < 0 || branch.label >= len(labels) {
			return nil, fmt.Errorf("arm32: invalid mixed branch label %d", branch.label)
		}
		var ok bool
		if branch.conditional {
			ok = a.PatchFarBranch(branch.at, labels[branch.label])
		} else {
			ok = a.PatchBranch(branch.at, labels[branch.label])
		}
		if !ok {
			return nil, fmt.Errorf("arm32: mixed structured branch out of range")
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
