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
	plan, err := shared.BuildMixedPlanWithModuleResolvers(ft, locals, body, func(index uint32) (*wasm.CompType, bool) {
		return m.FuncSignature(index)
	}, func(index uint32) (wasm.ValType, bool, uint32, bool, bool) {
		return shared.EmbeddedGlobalLocation(m, index)
	}, func(index uint32) (*wasm.CompType, bool) {
		return m.TypeFunc(index)
	}, func(index uint32) (wasm.ValType, bool) {
		return shared.EmbeddedTableValueType(m, index)
	})
	if err != nil {
		return nil, nil, err
	}
	for i := range plan.Ops {
		op := &plan.Ops[i]
		if (op.Kind == shared.MixedMemoryInit || op.Kind == shared.MixedDataDrop) && uint64(op.Target) >= uint64(len(m.Data)) {
			return nil, nil, fmt.Errorf("riscv32: invalid mixed data segment %d", op.Target)
		}
		if (op.Kind == shared.MixedTableInit || op.Kind == shared.MixedElemDrop) && uint64(op.Target) >= uint64(len(m.Elements)) {
			return nil, nil, fmt.Errorf("riscv32: invalid mixed element segment %d", op.Target)
		}
		if op.Kind == shared.MixedTableInit && op.Lane != 0 {
			return nil, nil, fmt.Errorf("riscv32: invalid mixed table.init table %d", op.Lane)
		}
		if op.Kind == shared.MixedCallIndirect {
			if typ, ok := shared.EmbeddedTableValueType(m, op.Lane); !ok || typ != wasm.FuncRef {
				return nil, nil, fmt.Errorf("riscv32: invalid mixed indirect table %d", op.Lane)
			}
			typeID, ok := shared.EmbeddedFunctionTypeID(m, op.Target)
			if !ok {
				return nil, nil, fmt.Errorf("riscv32: invalid mixed indirect type %d", op.Target)
			}
			op.Target = typeID
			continue
		}
		if op.Kind != shared.MixedCall {
			continue
		}
		imported := uint32(m.ImportedFuncCount())
		if op.Target < imported {
			op.Kind = shared.MixedCallImport
			continue
		}
		localTarget := op.Target - imported
		if uint64(localTarget) >= uint64(len(m.Code)) {
			return nil, nil, fmt.Errorf("riscv32: mixed call target %d is unavailable", op.Target)
		}
		targetType, ok := m.LocalFuncType(int(localTarget))
		if !ok || (!usesMixedModuleCompiler(targetType, m.Code[localTarget].Locals.Runs) && !homogeneousFunction(targetType, m.Code[localTarget].Locals.Runs, wasm.I32, true)) {
			return nil, nil, fmt.Errorf("riscv32: mixed call target %d does not use a compatible module ABI", op.Target)
		}
		op.Target = localTarget
	}
	var relocs []callReloc
	code, err := emitMixedPlan(plan, &relocs)
	return code, relocs, err
}

func emitMixedPlan(plan *shared.MixedPlan, relocSink *[]callReloc) ([]byte, error) {
	maxOutgoingSlots := uint16(0)
	for _, op := range plan.Ops {
		if op.Kind != shared.MixedCall && op.Kind != shared.MixedCallImport && op.Kind != shared.MixedCallIndirect {
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
	helperBase := valueBase + dataBytes
	indirectBytes := int32(0)
	for _, op := range plan.Ops {
		if op.Kind == shared.MixedCallIndirect || op.Kind == shared.MixedCallImport {
			indirectBytes = 4
			break
		}
	}
	indirectTargetOffset := helperBase + helperBytes
	saveOffset := indirectTargetOffset + indirectBytes
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
	emitTrapReturn := func(kind embedded32.Trap, name string) int {
		at := a.Len()
		must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), name+" trap cell")
		a.MovImm32(rv.T0, uint32(kind))
		must(a.Sw(rv.T0, rv.T1, 0), name+" trap write")
		must(a.Lw(rv.RA, rv.SP, saveOffset), name+" return address restore")
		must(a.Addi(rv.SP, rv.SP, frame), name+" frame release")
		a.MovImm32(rv.A0, 0)
		a.Ret()
		return at
	}
	emitMemoryTrap := func(done int, traps []int, name string) error {
		trap := a.Len()
		must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), name+" trap cell")
		a.MovImm32(rv.T0, uint32(embedded32.TrapMemoryOutOfBounds))
		must(a.Sw(rv.T0, rv.T1, 0), name+" trap write")
		must(a.Lw(rv.RA, rv.SP, saveOffset), name+" trap return address restore")
		must(a.Addi(rv.SP, rv.SP, frame), name+" trap frame release")
		a.MovImm32(rv.A0, 0)
		a.Ret()
		finish := a.Len()
		if !a.PatchFarJump(done, finish) {
			return fmt.Errorf("riscv32: mixed %s success branch out of range", name)
		}
		for _, branch := range traps {
			if !a.PatchFarBranch(branch, trap) {
				return fmt.Errorf("riscv32: mixed %s trap branch out of range", name)
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
		case shared.MixedI32Eqz, shared.MixedI32Eq, shared.MixedI32Ne,
			shared.MixedI32LtS, shared.MixedI32LtU, shared.MixedI32GtS, shared.MixedI32GtU,
			shared.MixedI32LeS, shared.MixedI32LeU, shared.MixedI32GeS, shared.MixedI32GeU:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 compare left")
			if op.Kind == shared.MixedI32Eqz {
				a.Seqz(rv.T2, rv.T0)
			} else {
				must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i32 compare right")
				switch op.Kind {
				case shared.MixedI32Eq:
					a.Sub(rv.T2, rv.T0, rv.T1)
					a.Seqz(rv.T2, rv.T2)
				case shared.MixedI32Ne:
					a.Sub(rv.T2, rv.T0, rv.T1)
					a.Snez(rv.T2, rv.T2)
				case shared.MixedI32LtS:
					a.Slt(rv.T2, rv.T0, rv.T1)
				case shared.MixedI32LtU:
					a.Sltu(rv.T2, rv.T0, rv.T1)
				case shared.MixedI32GtS:
					a.Slt(rv.T2, rv.T1, rv.T0)
				case shared.MixedI32GtU:
					a.Sltu(rv.T2, rv.T1, rv.T0)
				case shared.MixedI32LeS:
					a.Slt(rv.T2, rv.T1, rv.T0)
					a.Xori(rv.T2, rv.T2, 1)
				case shared.MixedI32LeU:
					a.Sltu(rv.T2, rv.T1, rv.T0)
					a.Xori(rv.T2, rv.T2, 1)
				case shared.MixedI32GeS:
					a.Slt(rv.T2, rv.T0, rv.T1)
					a.Xori(rv.T2, rv.T2, 1)
				case shared.MixedI32GeU:
					a.Sltu(rv.T2, rv.T0, rv.T1)
					a.Xori(rv.T2, rv.T2, 1)
				}
			}
			must(a.Sw(rv.T2, rv.SP, off(op.Dst)), "i32 compare result")
		case shared.MixedI32Clz, shared.MixedI32Ctz, shared.MixedI32Popcnt:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 count value")
			a.MovImm32(rv.T1, 0)
			if op.Kind == shared.MixedI32Popcnt {
				loop := a.Len()
				done := a.Bcond(rv.T0, rv.Zero, rv.CondEQ)
				a.Andi(rv.T2, rv.T0, 1)
				a.Add(rv.T1, rv.T1, rv.T2)
				a.Srli(rv.T0, rv.T0, 1)
				back := a.Jal(rv.Zero)
				if !a.PatchJAL21(back, loop) || !a.PatchBranch13(done, a.Len()) {
					return nil, fmt.Errorf("riscv32: mixed i32 popcnt branch out of range")
				}
			} else {
				zero := a.Bcond(rv.T0, rv.Zero, rv.CondEQ)
				loop := a.Len()
				var done int
				if op.Kind == shared.MixedI32Clz {
					done = a.Bcond(rv.T0, rv.Zero, rv.CondLT)
					a.Slli(rv.T0, rv.T0, 1)
				} else {
					a.Andi(rv.T2, rv.T0, 1)
					done = a.Bcond(rv.T2, rv.Zero, rv.CondNE)
					a.Srli(rv.T0, rv.T0, 1)
				}
				a.Addi(rv.T1, rv.T1, 1)
				back := a.Jal(rv.Zero)
				if !a.PatchJAL21(back, loop) {
					return nil, fmt.Errorf("riscv32: mixed i32 count loop out of range")
				}
				finish := a.Len()
				overZero := a.Jal(rv.Zero)
				zeroCase := a.Len()
				a.MovImm32(rv.T1, 32)
				end := a.Len()
				if !a.PatchBranch13(zero, zeroCase) || !a.PatchBranch13(done, finish) || !a.PatchJAL21(overZero, end) {
					return nil, fmt.Errorf("riscv32: mixed i32 count finish branch out of range")
				}
			}
			must(a.Sw(rv.T1, rv.SP, off(op.Dst)), "i32 count result")
		case shared.MixedI32DivS, shared.MixedI32DivU, shared.MixedI32RemS, shared.MixedI32RemU:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 division left")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i32 division right")
			zero := a.FarBcond(rv.T1, rv.Zero, rv.CondEQ, branchScratch)
			overflow := -1
			if op.Kind == shared.MixedI32DivS {
				a.MovImm32(rv.T2, 0x80000000)
				notMinimum := a.FarBcond(rv.T0, rv.T2, rv.CondNE, branchScratch)
				a.MovImm32(rv.T2, 0xffffffff)
				overflow = a.FarBcond(rv.T1, rv.T2, rv.CondEQ, branchScratch)
				if !a.PatchFarBranch(notMinimum, a.Len()) {
					return nil, fmt.Errorf("riscv32: mixed i32 division overflow skip out of range")
				}
			}
			switch op.Kind {
			case shared.MixedI32DivS:
				a.Div(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32DivU:
				a.Divu(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32RemS:
				a.Rem(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32RemU:
				a.Remu(rv.T2, rv.T0, rv.T1)
			}
			must(a.Sw(rv.T2, rv.SP, off(op.Dst)), "i32 division result")
			done := a.FarJump(rv.Zero, branchScratch)
			zeroTarget := emitTrapReturn(embedded32.TrapIntegerDivideByZero, "i32 division zero")
			overflowTarget := a.Len()
			if overflow >= 0 {
				overflowTarget = emitTrapReturn(embedded32.TrapIntegerOverflow, "i32 division overflow")
			}
			finish := a.Len()
			if !a.PatchFarJump(done, finish) || !a.PatchFarBranch(zero, zeroTarget) || (overflow >= 0 && !a.PatchFarBranch(overflow, overflowTarget)) {
				return nil, fmt.Errorf("riscv32: mixed i32 division branch out of range")
			}
		case shared.MixedI32Shl, shared.MixedI32ShrS, shared.MixedI32ShrU, shared.MixedI32Rotl, shared.MixedI32Rotr:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 shift value")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "i32 shift count")
			switch op.Kind {
			case shared.MixedI32Shl:
				a.Sll(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32ShrS:
				a.Sra(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32ShrU:
				a.Srl(rv.T2, rv.T0, rv.T1)
			case shared.MixedI32Rotl:
				a.Neg(rv.T3, rv.T1)
				a.Sll(rv.T2, rv.T0, rv.T1)
				a.Srl(rv.T3, rv.T0, rv.T3)
				a.Or(rv.T2, rv.T2, rv.T3)
			case shared.MixedI32Rotr:
				a.Neg(rv.T3, rv.T1)
				a.Srl(rv.T2, rv.T0, rv.T1)
				a.Sll(rv.T3, rv.T0, rv.T3)
				a.Or(rv.T2, rv.T2, rv.T3)
			}
			must(a.Sw(rv.T2, rv.SP, off(op.Dst)), "i32 shift result")
		case shared.MixedI32Extend8S, shared.MixedI32Extend16S:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "i32 sign extension value")
			if op.Kind == shared.MixedI32Extend8S {
				a.Sext8(rv.T0, rv.T0)
			} else {
				a.Sext16(rv.T0, rv.T0)
			}
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "i32 sign extension result")
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
		case shared.MixedSIMDHelper:
			a.MovImm32(rv.T0, op.HelperOp)
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameOpOffset), "simd helper op store")
			inputStart := 0
			if op.HasMemory {
				if len(op.Args) == 0 || op.Args[0].Type != wasm.I32 {
					return nil, fmt.Errorf("riscv32: simd memory helper has no i32 address")
				}
				must(a.Lw(rv.T0, rv.SP, off(op.Args[0].Slot)), "simd memory address")
				a.MovReg(rv.T1, rv.T0)
				a.MovImm32(rv.T2, op.MemoryOffset)
				a.Add(rv.T0, rv.T0, rv.T2)
				addressOK := a.FarBcond(rv.T0, rv.T1, rv.CondGEU, branchScratch)
				must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "simd memory overflow trap cell")
				a.MovImm32(rv.T0, uint32(embedded32.TrapMemoryOutOfBounds))
				must(a.Sw(rv.T0, rv.T1, 0), "simd memory overflow trap write")
				must(a.Lw(rv.RA, rv.SP, saveOffset), "simd memory overflow return address restore")
				must(a.Addi(rv.SP, rv.SP, frame), "simd memory overflow frame release")
				a.MovImm32(rv.A0, 0)
				a.Ret()
				if !a.PatchFarBranch(addressOK, a.Len()) {
					return nil, fmt.Errorf("riscv32: simd memory overflow branch out of range")
				}
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameAddressOffset), "simd memory address store")
				inputStart = 1
			}
			vectorInput := 0
			vectorBases := []int32{embedded32.SIMDFrameAOffset, embedded32.SIMDFrameBOffset, embedded32.SIMDFrameCOffset}
			for _, input := range op.Args[inputStart:] {
				width, _ := shared.MixedValueSlots(input.Type)
				if input.Type == wasm.V128 {
					for i := int32(0); i < 4; i++ {
						must(a.Lw(rv.T0, rv.SP, off(input.Slot)+i*4), "simd helper vector input load")
						must(a.Sw(rv.T0, rv.SP, helperBase+vectorBases[vectorInput]+i*4), "simd helper vector input store")
					}
					vectorInput++
				} else {
					for i := uint8(0); i < width; i++ {
						must(a.Lw(rv.T0, rv.SP, off(input.Slot)+int32(i)*4), "simd helper scalar input load")
						must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameScalarLoOffset+int32(i)*4), "simd helper scalar input store")
					}
					if width == 1 {
						must(a.Sw(rv.Zero, rv.SP, helperBase+embedded32.SIMDFrameScalarHiOffset), "simd helper scalar high store")
					}
				}
			}
			for i := int32(0); i < 4; i++ {
				a.MovImm32(rv.T0, op.Words[i])
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameImmediateOffset+i*4), "simd helper immediate store")
			}
			a.MovImm32(rv.T0, op.Lane)
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameLaneOffset), "simd helper lane store")
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "simd helper memory base")
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameMemoryBaseOffset), "simd helper memory base store")
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "simd helper memory length")
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameMemoryLenOffset), "simd helper memory length store")
			must(a.Addi(rv.A0, rv.SP, helperBase), "simd helper frame address")
			must(a.Lw(rv.A1, rvContextReg, embedded32.ContextHelperTableOffset), "simd helper table")
			must(a.Lw(rv.T0, rv.A1, embedded32.HelperSIMDOffset), "simd helper target")
			a.Blr(rv.T0)
			must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.SIMDFrameTrapOffset), "simd helper trap")
			helperOK := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "simd helper trap cell")
			must(a.Sw(rv.T0, rv.T1, 0), "simd helper trap publish")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "simd helper trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "simd helper trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("riscv32: simd helper trap branch out of range")
			}
			if len(op.Results) == 0 {
				break
			}
			if len(op.Results) != 1 {
				return nil, fmt.Errorf("riscv32: simd helper result arity %d", len(op.Results))
			}
			result := op.Results[0]
			width, _ := shared.MixedValueSlots(result.Type)
			resultBase := int32(embedded32.SIMDFrameScalarOutOffset)
			if result.Type == wasm.V128 {
				resultBase = embedded32.SIMDFrameOutOffset
			}
			for i := uint8(0); i < width; i++ {
				must(a.Lw(rv.T0, rv.SP, helperBase+resultBase+int32(i)*4), "simd helper result load")
				must(a.Sw(rv.T0, rv.SP, off(result.Slot)+int32(i)*4), "simd helper result store")
			}
		case shared.MixedI64Helper:
			a.MovImm32(rv.T0, op.HelperOp)
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.I64FrameOpOffset), "i64 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+int32(i)*4), "i64 helper left load")
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.I64FrameALoOffset+int32(i)*4), "i64 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.Sw(rv.Zero, rv.SP, helperBase+embedded32.I64FrameAHiOffset), "i64 helper input high store")
			}
			if op.Arity == 2 {
				for i := int32(0); i < 2; i++ {
					must(a.Lw(rv.T0, rv.SP, off(op.Right)+i*4), "i64 helper right load")
					must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.I64FrameBLoOffset+i*4), "i64 helper right store")
				}
			}
			must(a.Addi(rv.A0, rv.SP, helperBase), "i64 helper frame address")
			must(a.Lw(rv.A1, rvContextReg, embedded32.ContextHelperTableOffset), "i64 helper table")
			must(a.Lw(rv.T0, rv.A1, embedded32.HelperI64Offset), "i64 helper target")
			a.Blr(rv.T0)
			must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.I64FrameTrapOffset), "i64 helper trap")
			helperOK := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "i64 helper trap cell")
			must(a.Sw(rv.T0, rv.T1, 0), "i64 helper trap publish")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "i64 helper trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "i64 helper trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("riscv32: i64 helper trap branch out of range")
			}
			resultOffset := int32(embedded32.I64FrameOutLoOffset)
			if op.Width == 1 {
				resultOffset = embedded32.I64FrameI32OutOffset
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Lw(rv.T0, rv.SP, helperBase+resultOffset+int32(i)*4), "i64 helper result load")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "i64 helper result store")
			}
		case shared.MixedF32Helper:
			a.MovImm32(rv.T0, op.HelperOp)
			must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F32FrameOpOffset), "f32 helper op store")
			for i := uint8(0); i < op.InputWidth; i++ {
				must(a.Lw(rv.T0, rv.SP, off(op.Left)+int32(i)*4), "f32 helper left load")
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F32FrameALoOffset+int32(i)*4), "f32 helper left store")
			}
			if op.InputWidth == 1 {
				must(a.Sw(rv.Zero, rv.SP, helperBase+embedded32.F32FrameAHiOffset), "f32 helper input high store")
			}
			if op.Arity == 2 {
				must(a.Lw(rv.T0, rv.SP, off(op.Right)), "f32 helper right load")
				must(a.Sw(rv.T0, rv.SP, helperBase+embedded32.F32FrameBLoOffset), "f32 helper right store")
			}
			must(a.Addi(rv.A0, rv.SP, helperBase), "f32 helper frame address")
			must(a.Lw(rv.A1, rvContextReg, embedded32.ContextHelperTableOffset), "f32 helper table")
			must(a.Lw(rv.T0, rv.A1, embedded32.HelperF32Offset), "f32 helper target")
			a.Blr(rv.T0)
			must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.F32FrameTrapOffset), "f32 helper trap")
			helperOK := a.FarBcond(rv.T0, rv.Zero, rv.CondEQ, branchScratch)
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "f32 helper trap cell")
			must(a.Sw(rv.T0, rv.T1, 0), "f32 helper trap publish")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "f32 helper trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "f32 helper trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			if !a.PatchFarBranch(helperOK, a.Len()) {
				return nil, fmt.Errorf("riscv32: f32 helper trap branch out of range")
			}
			for i := uint8(0); i < op.Width; i++ {
				must(a.Lw(rv.T0, rv.SP, helperBase+embedded32.F32FrameOutLoOffset+int32(i)*4), "f32 helper result load")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)+int32(i)*4), "f32 helper result store")
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
		case shared.MixedMemoryLoad:
			width, resultWords, signed, ok := embedded32.ScalarLoadInfo(embedded32.ScalarLoadOp(op.MemoryOp))
			if !ok {
				return nil, fmt.Errorf("riscv32: invalid mixed scalar load %d", op.MemoryOp)
			}
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory load address")
			a.MovReg(rv.T2, rv.T0)
			a.MovImm32(rv.T1, op.MemoryOffset)
			a.Add(rv.T0, rv.T0, rv.T1)
			traps := []int{a.FarBcond(rv.T0, rv.T2, rv.CondLTU, branchScratch)}
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory load length")
			a.MovImm32(rv.T2, width)
			traps = append(traps, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, branchScratch))
			a.Sub(rv.T1, rv.T1, rv.T2)
			traps = append(traps, a.FarBcond(rv.T1, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory load base")
			a.Add(rv.T1, rv.T1, rv.T0)
			switch width {
			case 1:
				if signed {
					must(a.Lb(rv.T2, rv.T1, 0), "memory load8 signed")
				} else {
					must(a.Lbu(rv.T2, rv.T1, 0), "memory load8 unsigned")
				}
			case 2:
				if signed {
					must(a.Lh(rv.T2, rv.T1, 0), "memory load16 signed")
				} else {
					must(a.Lhu(rv.T2, rv.T1, 0), "memory load16 unsigned")
				}
			case 4:
				must(a.Lw(rv.T2, rv.T1, 0), "memory load32")
			case 8:
				must(a.Lw(rv.T2, rv.T1, 0), "memory load64 low")
				must(a.Lw(rv.T3, rv.T1, 4), "memory load64 high")
			}
			must(a.Sw(rv.T2, rv.SP, off(op.Dst)), "memory load result low")
			if resultWords == 2 {
				if width < 8 {
					if signed {
						must(a.Srai(rv.T3, rv.T2, 31), "memory load sign high")
					} else {
						a.MovImm32(rv.T3, 0)
					}
				}
				must(a.Sw(rv.T3, rv.SP, off(op.Dst)+4), "memory load result high")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := a.Len()
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "memory load trap cell")
			a.MovImm32(rv.T0, uint32(embedded32.TrapMemoryOutOfBounds))
			must(a.Sw(rv.T0, rv.T1, 0), "memory load trap write")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "memory load trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "memory load trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed memory load success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("riscv32: mixed memory load trap branch out of range")
				}
			}
		case shared.MixedMemoryStore:
			width, _, ok := embedded32.ScalarStoreInfo(embedded32.ScalarStoreOp(op.MemoryOp))
			if !ok {
				return nil, fmt.Errorf("riscv32: invalid mixed scalar store %d", op.MemoryOp)
			}
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory store address")
			a.MovReg(rv.T2, rv.T0)
			a.MovImm32(rv.T1, op.MemoryOffset)
			a.Add(rv.T0, rv.T0, rv.T1)
			traps := []int{a.FarBcond(rv.T0, rv.T2, rv.CondLTU, branchScratch)}
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory store length")
			a.MovImm32(rv.T2, width)
			traps = append(traps, a.FarBcond(rv.T1, rv.T2, rv.CondLTU, branchScratch))
			a.Sub(rv.T1, rv.T1, rv.T2)
			traps = append(traps, a.FarBcond(rv.T1, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory store base")
			a.Add(rv.T1, rv.T1, rv.T0)
			must(a.Lw(rv.T2, rv.SP, off(op.Right)), "memory store value low")
			switch width {
			case 1:
				must(a.Sb(rv.T2, rv.T1, 0), "memory store8")
			case 2:
				must(a.Sh(rv.T2, rv.T1, 0), "memory store16")
			case 4:
				must(a.Sw(rv.T2, rv.T1, 0), "memory store32")
			case 8:
				must(a.Lw(rv.T3, rv.SP, off(op.Right)+4), "memory store value high")
				must(a.Sw(rv.T2, rv.T1, 0), "memory store64 low")
				must(a.Sw(rv.T3, rv.T1, 4), "memory store64 high")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := a.Len()
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "memory store trap cell")
			a.MovImm32(rv.T0, uint32(embedded32.TrapMemoryOutOfBounds))
			must(a.Sw(rv.T0, rv.T1, 0), "memory store trap write")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "memory store trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "memory store trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed memory store success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("riscv32: mixed memory store trap branch out of range")
				}
			}
		case shared.MixedMemoryInit:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory.init destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "memory.init source offset")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "memory.init count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextDataSegmentsBaseOffset), "memory.init descriptor base")
			a.MovImm32(rv.T6, op.Target*embedded32.DataSegmentABIBytes)
			a.Add(rv.T3, rv.T3, rv.T6)
			must(a.Lw(rv.T5, rv.T3, embedded32.DataSegmentDroppedOffset), "memory.init dropped flag")
			available := a.Bcond(rv.T5, rv.Zero, rv.CondEQ)
			a.MovImm32(rv.T5, 0)
			lengthReady := a.Jal(rv.Zero)
			availableTarget := a.Len()
			must(a.Lw(rv.T5, rv.T3, embedded32.DataSegmentLengthOffset), "memory.init data length")
			lengthTarget := a.Len()
			if !a.PatchBranch13(available, availableTarget) || !a.PatchJAL21(lengthReady, lengthTarget) {
				return nil, fmt.Errorf("riscv32: mixed memory.init data length branch out of range")
			}
			traps := []int{a.FarBcond(rv.T5, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T5, rv.T5, rv.T2)
			traps = append(traps, a.FarBcond(rv.T5, rv.T1, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T5, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.init memory length")
			traps = append(traps, a.FarBcond(rv.T5, rv.T2, rv.CondLTU, branchScratch))
			a.Sub(rv.T5, rv.T5, rv.T2)
			traps = append(traps, a.FarBcond(rv.T5, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rv.T3, embedded32.DataSegmentBaseOffset), "memory.init data base")
			a.Add(rv.T3, rv.T3, rv.T1)
			must(a.Lw(rv.T5, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.init memory base")
			a.Add(rv.T0, rv.T0, rv.T5)
			loop := a.Len()
			copied := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Lbu(rv.T5, rv.T3, 0), "memory.init load")
			must(a.Sb(rv.T5, rv.T0, 0), "memory.init store")
			must(a.Addi(rv.T3, rv.T3, 1), "memory.init source advance")
			must(a.Addi(rv.T0, rv.T0, 1), "memory.init destination advance")
			must(a.Addi(rv.T2, rv.T2, -1), "memory.init count decrement")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(copied, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed memory.init loop out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			if err := emitMemoryTrap(done, traps, "memory.init"); err != nil {
				return nil, err
			}
		case shared.MixedDataDrop:
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextDataSegmentsBaseOffset), "data.drop descriptor base")
			a.MovImm32(rv.T1, op.Target*embedded32.DataSegmentABIBytes)
			a.Add(rv.T0, rv.T0, rv.T1)
			a.MovImm32(rv.T1, 1)
			must(a.Sw(rv.T1, rv.T0, embedded32.DataSegmentDroppedOffset), "data.drop store")
		case shared.MixedMemoryCopy:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory.copy destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "memory.copy source")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "memory.copy count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.copy length")
			traps := []int{a.FarBcond(rv.T3, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T3, rv.T3, rv.T2)
			traps = append(traps, a.FarBcond(rv.T3, rv.T0, rv.CondLTU, branchScratch))
			traps = append(traps, a.FarBcond(rv.T3, rv.T1, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.copy base")
			a.Add(rv.T0, rv.T0, rv.T3)
			a.Add(rv.T1, rv.T1, rv.T3)
			forward := a.FarBcond(rv.T1, rv.T0, rv.CondGEU, branchScratch)
			a.Add(rv.T0, rv.T0, rv.T2)
			a.Add(rv.T1, rv.T1, rv.T2)
			backLoop := a.Len()
			backDone := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Addi(rv.T0, rv.T0, -1), "memory.copy destination decrement")
			must(a.Addi(rv.T1, rv.T1, -1), "memory.copy source decrement")
			must(a.Lbu(rv.T5, rv.T1, 0), "memory.copy backward load")
			must(a.Sb(rv.T5, rv.T0, 0), "memory.copy backward store")
			must(a.Addi(rv.T2, rv.T2, -1), "memory.copy backward count")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, backLoop) {
				return nil, fmt.Errorf("riscv32: mixed memory.copy backward loop out of range")
			}
			forwardTarget := a.Len()
			if !a.PatchFarBranch(forward, forwardTarget) {
				return nil, fmt.Errorf("riscv32: mixed memory.copy direction branch out of range")
			}
			forwardLoop := a.Len()
			forwardDone := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Lbu(rv.T5, rv.T1, 0), "memory.copy forward load")
			must(a.Sb(rv.T5, rv.T0, 0), "memory.copy forward store")
			must(a.Addi(rv.T0, rv.T0, 1), "memory.copy destination advance")
			must(a.Addi(rv.T1, rv.T1, 1), "memory.copy source advance")
			must(a.Addi(rv.T2, rv.T2, -1), "memory.copy forward count")
			forwardBack := a.Jal(rv.Zero)
			if !a.PatchJAL21(forwardBack, forwardLoop) {
				return nil, fmt.Errorf("riscv32: mixed memory.copy forward loop out of range")
			}
			finishCopy := a.Len()
			if !a.PatchBranch13(backDone, finishCopy) || !a.PatchBranch13(forwardDone, finishCopy) {
				return nil, fmt.Errorf("riscv32: mixed memory.copy done branch out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			if err := emitMemoryTrap(done, traps, "memory.copy"); err != nil {
				return nil, err
			}
		case shared.MixedMemoryFill:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory.fill destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "memory.fill value")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "memory.fill count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.fill length")
			traps := []int{a.FarBcond(rv.T3, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T3, rv.T3, rv.T2)
			traps = append(traps, a.FarBcond(rv.T3, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.fill base")
			a.Add(rv.T0, rv.T0, rv.T3)
			loop := a.Len()
			filled := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Sb(rv.T1, rv.T0, 0), "memory.fill store")
			must(a.Addi(rv.T0, rv.T0, 1), "memory.fill advance")
			must(a.Addi(rv.T2, rv.T2, -1), "memory.fill count decrement")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(filled, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed memory.fill loop out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			if err := emitMemoryTrap(done, traps, "memory.fill"); err != nil {
				return nil, err
			}
		case shared.MixedMemorySize:
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.size length")
			a.Srli(rv.T0, rv.T0, 16)
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "memory.size result")
		case shared.MixedMemoryGrow:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "memory.grow delta")
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow current")
			a.MovReg(rv.T2, rv.T1)
			a.Srli(rv.T2, rv.T2, 16)
			a.Srli(rv.T3, rv.T0, 16)
			fails := []int{a.FarBcond(rv.T3, rv.Zero, rv.CondNE, branchScratch)}
			a.Slli(rv.T0, rv.T0, 16)
			a.Add(rv.T0, rv.T1, rv.T0)
			fails = append(fails, a.FarBcond(rv.T0, rv.T1, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryMaximumOffset), "memory.grow maximum")
			fails = append(fails, a.FarBcond(rv.T3, rv.T0, rv.CondLTU, branchScratch))
			must(a.Sw(rv.T0, rvContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow publish length")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.grow base")
			a.Add(rv.T1, rv.T3, rv.T1)
			a.Add(rv.T3, rv.T3, rv.T0)
			loop := a.Len()
			cleared := a.Bcond(rv.T1, rv.T3, rv.CondEQ)
			must(a.Sw(rv.Zero, rv.T1, 0), "memory.grow clear word")
			must(a.Addi(rv.T1, rv.T1, 4), "memory.grow clear advance")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(cleared, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed memory.grow clear branch out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			fail := a.Len()
			a.MovImm32(rv.T2, 0xffffffff)
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed memory.grow done branch out of range")
			}
			for _, at := range fails {
				if !a.PatchFarBranch(at, fail) {
					return nil, fmt.Errorf("riscv32: mixed memory.grow failure branch out of range")
				}
			}
			must(a.Sw(rv.T2, rv.SP, off(op.Dst)), "memory.grow result")
		case shared.MixedGlobalGet, shared.MixedGlobalSet:
			if op.Imported {
				if op.Target > 511 {
					return nil, fmt.Errorf("riscv32: imported global index %d exceeds direct displacement", op.Target)
				}
				must(a.Lw(rv.T0, rvContextReg, embedded32.ContextImportedGlobalsBaseOffset), "imported global directory")
				must(a.Lw(rv.T0, rv.T0, int32(op.Target*4)), "imported global cell")
			} else {
				if uint64(op.Target)+uint64(op.Width) > 512 {
					return nil, fmt.Errorf("riscv32: mixed global slot %d width %d exceeds direct displacement", op.Target, op.Width)
				}
				must(a.Lw(rv.T0, rvContextReg, embedded32.ContextGlobalsBaseOffset), "global base")
			}
			for i := uint8(0); i < op.Width; i++ {
				globalSlot := uint32(i)
				if !op.Imported {
					globalSlot += op.Target
				}
				globalOffset := int32(globalSlot * 4)
				if op.Kind == shared.MixedGlobalGet {
					must(a.Lw(rv.T1, rv.T0, globalOffset), "global.get")
					must(a.Sw(rv.T1, rv.SP, off(op.Dst)+int32(i)*4), "global.get result")
				} else {
					must(a.Lw(rv.T1, rv.SP, off(op.Left)+int32(i)*4), "global.set value")
					must(a.Sw(rv.T1, rv.T0, globalOffset), "global.set")
				}
			}
		case shared.MixedTableGet, shared.MixedTableSet:
			if op.Target != 0 {
				return nil, fmt.Errorf("riscv32: mixed table index %d is not supported", op.Target)
			}
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "table element index")
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTableOffset), "table descriptor")
			must(a.Lw(rv.T2, rv.T1, embedded32.TableABILengthOffset), "table length")
			outOfBounds := a.FarBcond(rv.T0, rv.T2, rv.CondGEU, branchScratch)
			must(a.Lw(rv.T2, rv.T1, embedded32.TableABIEntriesBaseOffset), "table entries")
			a.Slli(rv.T3, rv.T0, 2)
			a.Add(rv.T2, rv.T2, rv.T3)
			if op.Kind == shared.MixedTableGet {
				must(a.Lw(rv.T0, rv.T2, 0), "table.get")
				must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "table.get result")
			} else {
				must(a.Lw(rv.T0, rv.SP, off(op.Right)), "table.set value")
				must(a.Sw(rv.T0, rv.T2, 0), "table.set")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := a.Len()
			must(a.Lw(rv.T1, rvContextReg, embedded32.ContextTrapCellOffset), "table trap cell")
			a.MovImm32(rv.T0, uint32(embedded32.TrapTableOutOfBounds))
			must(a.Sw(rv.T0, rv.T1, 0), "table trap write")
			must(a.Lw(rv.RA, rv.SP, saveOffset), "table trap return address restore")
			must(a.Addi(rv.SP, rv.SP, frame), "table trap frame release")
			a.MovImm32(rv.A0, 0)
			a.Ret()
			finish := a.Len()
			if !a.PatchFarJump(done, finish) || !a.PatchFarBranch(outOfBounds, trap) {
				return nil, fmt.Errorf("riscv32: mixed table branch out of range")
			}
		case shared.MixedTableInit:
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "table.init destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "table.init source offset")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "table.init count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextTableOffset), "table.init table descriptor")
			must(a.Lw(rv.T4, rv.T3, embedded32.TableABILengthOffset), "table.init table length")
			traps := []int{a.FarBcond(rv.T4, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T4, rv.T4, rv.T2)
			traps = append(traps, a.FarBcond(rv.T4, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rv.T3, embedded32.TableABIElementSegmentsBaseOffset), "table.init element descriptor base")
			a.MovImm32(rv.T6, op.Target*embedded32.DataSegmentABIBytes)
			a.Add(rv.T3, rv.T3, rv.T6)
			must(a.Lw(rv.T5, rv.T3, embedded32.DataSegmentDroppedOffset), "table.init dropped flag")
			available := a.Bcond(rv.T5, rv.Zero, rv.CondEQ)
			a.MovImm32(rv.T5, 0)
			lengthReady := a.Jal(rv.Zero)
			availableTarget := a.Len()
			must(a.Lw(rv.T5, rv.T3, embedded32.DataSegmentLengthOffset), "table.init element length")
			lengthTarget := a.Len()
			if !a.PatchBranch13(available, availableTarget) || !a.PatchJAL21(lengthReady, lengthTarget) {
				return nil, fmt.Errorf("riscv32: mixed table.init element length branch out of range")
			}
			traps = append(traps, a.FarBcond(rv.T5, rv.T2, rv.CondLTU, branchScratch))
			a.Sub(rv.T5, rv.T5, rv.T2)
			traps = append(traps, a.FarBcond(rv.T5, rv.T1, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rv.T3, embedded32.DataSegmentBaseOffset), "table.init payload base")
			a.Slli(rv.T4, rv.T1, 2)
			a.Add(rv.T3, rv.T3, rv.T4)
			must(a.Lw(rv.T5, rvContextReg, embedded32.ContextTableOffset), "table.init table descriptor restore")
			must(a.Lw(rv.T5, rv.T5, embedded32.TableABIEntriesBaseOffset), "table.init entries")
			a.Slli(rv.T4, rv.T0, 2)
			a.Add(rv.T5, rv.T5, rv.T4)
			loop := a.Len()
			copied := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Lw(rv.T0, rv.T3, 0), "table.init load")
			must(a.Sw(rv.T0, rv.T5, 0), "table.init store")
			must(a.Addi(rv.T3, rv.T3, 4), "table.init source advance")
			must(a.Addi(rv.T5, rv.T5, 4), "table.init destination advance")
			must(a.Addi(rv.T2, rv.T2, -1), "table.init count decrement")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(copied, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed table.init loop out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.init bounds")
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed table.init success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("riscv32: mixed table.init trap branch out of range")
				}
			}
		case shared.MixedElemDrop:
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextTableOffset), "elem.drop table descriptor")
			must(a.Lw(rv.T0, rv.T0, embedded32.TableABIElementSegmentsBaseOffset), "elem.drop descriptor base")
			a.MovImm32(rv.T1, op.Target*embedded32.DataSegmentABIBytes)
			a.Add(rv.T0, rv.T0, rv.T1)
			a.MovImm32(rv.T1, 1)
			must(a.Sw(rv.T1, rv.T0, embedded32.DataSegmentDroppedOffset), "elem.drop store")
		case shared.MixedTableSize:
			if op.Target != 0 {
				return nil, fmt.Errorf("riscv32: mixed table.size index %d is not supported", op.Target)
			}
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextTableOffset), "table.size descriptor")
			must(a.Lw(rv.T0, rv.T0, embedded32.TableABILengthOffset), "table.size length")
			must(a.Sw(rv.T0, rv.SP, off(op.Dst)), "table.size result")
		case shared.MixedTableGrow:
			if op.Target != 0 {
				return nil, fmt.Errorf("riscv32: mixed table.grow index %d is not supported", op.Target)
			}
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextTableOffset), "table.grow descriptor")
			must(a.Lw(rv.T1, rv.T0, embedded32.TableABILengthOffset), "table.grow old length")
			must(a.Lw(rv.T2, rv.SP, off(op.Right)), "table.grow delta")
			a.Add(rv.T2, rv.T1, rv.T2)
			fails := []int{a.FarBcond(rv.T2, rv.T1, rv.CondLTU, branchScratch)}
			must(a.Lw(rv.T3, rv.T0, embedded32.TableABIMaximumOffset), "table.grow maximum")
			fails = append(fails, a.FarBcond(rv.T3, rv.T2, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rv.T0, embedded32.TableABIEntriesBaseOffset), "table.grow entries")
			a.Slli(rv.T4, rv.T1, 2)
			a.Add(rv.T3, rv.T3, rv.T4)
			must(a.Lw(rv.T4, rv.SP, off(op.Right)), "table.grow fill count")
			must(a.Lw(rv.T5, rv.SP, off(op.Left)), "table.grow fill value")
			loop := a.Len()
			filled := a.Bcond(rv.T4, rv.Zero, rv.CondEQ)
			must(a.Sw(rv.T5, rv.T3, 0), "table.grow fill store")
			must(a.Addi(rv.T3, rv.T3, 4), "table.grow fill advance")
			must(a.Addi(rv.T4, rv.T4, -1), "table.grow count decrement")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(filled, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed table.grow fill branch out of range")
			}
			must(a.Lw(rv.T0, rvContextReg, embedded32.ContextTableOffset), "table.grow descriptor restore")
			must(a.Sw(rv.T2, rv.T0, embedded32.TableABILengthOffset), "table.grow publish length")
			done := a.FarJump(rv.Zero, branchScratch)
			fail := a.Len()
			a.MovImm32(rv.T1, 0xffffffff)
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed table.grow done branch out of range")
			}
			for _, branch := range fails {
				if !a.PatchFarBranch(branch, fail) {
					return nil, fmt.Errorf("riscv32: mixed table.grow failure branch out of range")
				}
			}
			must(a.Sw(rv.T1, rv.SP, off(op.Dst)), "table.grow result")
		case shared.MixedTableFill:
			if op.Target != 0 {
				return nil, fmt.Errorf("riscv32: mixed table.fill index %d is not supported", op.Target)
			}
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "table.fill destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "table.fill value")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "table.fill count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextTableOffset), "table.fill descriptor")
			must(a.Lw(rv.T4, rv.T3, embedded32.TableABILengthOffset), "table.fill length")
			traps := []int{a.FarBcond(rv.T4, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T4, rv.T4, rv.T2)
			traps = append(traps, a.FarBcond(rv.T4, rv.T0, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rv.T3, embedded32.TableABIEntriesBaseOffset), "table.fill entries")
			a.Slli(rv.T4, rv.T0, 2)
			a.Add(rv.T3, rv.T3, rv.T4)
			loop := a.Len()
			filled := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Sw(rv.T1, rv.T3, 0), "table.fill store")
			must(a.Addi(rv.T3, rv.T3, 4), "table.fill advance")
			must(a.Addi(rv.T2, rv.T2, -1), "table.fill count decrement")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, loop) || !a.PatchBranch13(filled, a.Len()) {
				return nil, fmt.Errorf("riscv32: mixed table.fill loop out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.fill bounds")
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed table.fill success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("riscv32: mixed table.fill trap branch out of range")
				}
			}
		case shared.MixedTableCopy:
			if op.Target != 0 || op.Lane != 0 {
				return nil, fmt.Errorf("riscv32: mixed table.copy indexes %d/%d are not supported", op.Target, op.Lane)
			}
			must(a.Lw(rv.T0, rv.SP, off(op.Left)), "table.copy destination")
			must(a.Lw(rv.T1, rv.SP, off(op.Right)), "table.copy source")
			must(a.Lw(rv.T2, rv.SP, off(op.Third)), "table.copy count")
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextTableOffset), "table.copy descriptor")
			must(a.Lw(rv.T3, rv.T3, embedded32.TableABILengthOffset), "table.copy length")
			traps := []int{a.FarBcond(rv.T3, rv.T2, rv.CondLTU, branchScratch)}
			a.Sub(rv.T3, rv.T3, rv.T2)
			traps = append(traps, a.FarBcond(rv.T3, rv.T0, rv.CondLTU, branchScratch))
			traps = append(traps, a.FarBcond(rv.T3, rv.T1, rv.CondLTU, branchScratch))
			must(a.Lw(rv.T3, rvContextReg, embedded32.ContextTableOffset), "table.copy descriptor restore")
			must(a.Lw(rv.T3, rv.T3, embedded32.TableABIEntriesBaseOffset), "table.copy entries")
			a.Slli(rv.T4, rv.T0, 2)
			a.Add(rv.T0, rv.T3, rv.T4)
			a.Slli(rv.T4, rv.T1, 2)
			a.Add(rv.T1, rv.T3, rv.T4)
			forward := a.FarBcond(rv.T1, rv.T0, rv.CondGEU, branchScratch)
			a.Slli(rv.T4, rv.T2, 2)
			a.Add(rv.T0, rv.T0, rv.T4)
			a.Add(rv.T1, rv.T1, rv.T4)
			backLoop := a.Len()
			backDone := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Addi(rv.T0, rv.T0, -4), "table.copy destination decrement")
			must(a.Addi(rv.T1, rv.T1, -4), "table.copy source decrement")
			must(a.Lw(rv.T4, rv.T1, 0), "table.copy backward load")
			must(a.Sw(rv.T4, rv.T0, 0), "table.copy backward store")
			must(a.Addi(rv.T2, rv.T2, -1), "table.copy backward count")
			back := a.Jal(rv.Zero)
			if !a.PatchJAL21(back, backLoop) {
				return nil, fmt.Errorf("riscv32: mixed table.copy backward loop out of range")
			}
			forwardTarget := a.Len()
			if !a.PatchFarBranch(forward, forwardTarget) {
				return nil, fmt.Errorf("riscv32: mixed table.copy direction branch out of range")
			}
			forwardLoop := a.Len()
			forwardDone := a.Bcond(rv.T2, rv.Zero, rv.CondEQ)
			must(a.Lw(rv.T4, rv.T1, 0), "table.copy forward load")
			must(a.Sw(rv.T4, rv.T0, 0), "table.copy forward store")
			must(a.Addi(rv.T0, rv.T0, 4), "table.copy destination advance")
			must(a.Addi(rv.T1, rv.T1, 4), "table.copy source advance")
			must(a.Addi(rv.T2, rv.T2, -1), "table.copy forward count")
			forwardBack := a.Jal(rv.Zero)
			if !a.PatchJAL21(forwardBack, forwardLoop) {
				return nil, fmt.Errorf("riscv32: mixed table.copy forward loop out of range")
			}
			finishCopy := a.Len()
			if !a.PatchBranch13(backDone, finishCopy) || !a.PatchBranch13(forwardDone, finishCopy) {
				return nil, fmt.Errorf("riscv32: mixed table.copy done branch out of range")
			}
			done := a.FarJump(rv.Zero, branchScratch)
			trap := emitTrapReturn(embedded32.TrapTableOutOfBounds, "table.copy bounds")
			finish := a.Len()
			if !a.PatchFarJump(done, finish) {
				return nil, fmt.Errorf("riscv32: mixed table.copy success branch out of range")
			}
			for _, branch := range traps {
				if !a.PatchFarBranch(branch, trap) {
					return nil, fmt.Errorf("riscv32: mixed table.copy trap branch out of range")
				}
			}
		case shared.MixedBranchZero, shared.MixedBranchNonzero:
			must(a.Lw(rv.T0, rv.SP, off(op.Third)), "branch condition")
			cond := rv.CondEQ
			if op.Kind == shared.MixedBranchNonzero {
				cond = rv.CondNE
			}
			branches = append(branches, mixedBranchPatch{at: a.FarBcond(rv.T0, rv.Zero, cond, branchScratch), label: op.Label, conditional: true})
		case shared.MixedBranchEqualImmediate:
			must(a.Lw(rv.T0, rv.SP, off(op.Third)), "branch table selector")
			a.MovImm32(rv.T1, op.Target)
			branches = append(branches, mixedBranchPatch{at: a.FarBcond(rv.T0, rv.T1, rv.CondEQ, branchScratch), label: op.Label, conditional: true})
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
		case shared.MixedCall, shared.MixedCallImport, shared.MixedCallIndirect:
			if op.Kind == shared.MixedCallImport {
				if op.Target > 511 {
					return nil, fmt.Errorf("riscv32: import index %d exceeds direct displacement", op.Target)
				}
				must(a.Lw(rv.T0, rvContextReg, embedded32.ContextImportsBaseOffset), "import table")
				must(a.Lw(rv.T0, rv.T0, int32(op.Target*4)), "import target")
				must(a.Sw(rv.T0, rv.SP, indirectTargetOffset), "import target save")
			} else if op.Kind == shared.MixedCallIndirect {
				must(a.Lw(rv.T0, rv.SP, off(op.Third)), "indirect table index")
				must(a.Lw(rv.T3, rvContextReg, embedded32.ContextTableOffset), "indirect table descriptor")
				must(a.Lw(rv.T1, rv.T3, embedded32.TableABILengthOffset), "indirect table length")
				outOfBounds := a.FarBcond(rv.T0, rv.T1, rv.CondGEU, branchScratch)
				must(a.Lw(rv.T1, rv.T3, embedded32.TableABIEntriesBaseOffset), "indirect table entries")
				a.Slli(rv.T2, rv.T0, 2)
				a.Add(rv.T1, rv.T1, rv.T2)
				must(a.Lw(rv.T1, rv.T1, 0), "indirect table entry")
				null := a.FarBcond(rv.T1, rv.Zero, rv.CondEQ, branchScratch)
				must(a.Addi(rv.T1, rv.T1, -1), "indirect function index")
				must(a.Lw(rv.T0, rv.T3, embedded32.TableABIFunctionTypesBaseOffset), "indirect function types")
				a.Slli(rv.T2, rv.T1, 2)
				a.Add(rv.T0, rv.T0, rv.T2)
				must(a.Lw(rv.T0, rv.T0, 0), "indirect actual type")
				a.MovImm32(rv.T2, op.Target)
				typeMismatch := a.FarBcond(rv.T0, rv.T2, rv.CondNE, branchScratch)
				must(a.Lw(rv.T0, rv.T3, embedded32.TableABIFunctionEntriesBaseOffset), "indirect function entries")
				a.Slli(rv.T2, rv.T1, 2)
				a.Add(rv.T0, rv.T0, rv.T2)
				must(a.Lw(rv.T0, rv.T0, 0), "indirect call target")
				must(a.Sw(rv.T0, rv.SP, indirectTargetOffset), "indirect call target save")
				resolved := a.FarJump(rv.Zero, branchScratch)
				outOfBoundsTarget := emitTrapReturn(embedded32.TrapTableOutOfBounds, "indirect table bounds")
				nullTarget := emitTrapReturn(embedded32.TrapIndirectCallNull, "indirect null")
				typeMismatchTarget := emitTrapReturn(embedded32.TrapIndirectCallTypeMismatch, "indirect type")
				resolvedTarget := a.Len()
				if !a.PatchFarJump(resolved, resolvedTarget) || !a.PatchFarBranch(outOfBounds, outOfBoundsTarget) || !a.PatchFarBranch(null, nullTarget) || !a.PatchFarBranch(typeMismatch, typeMismatchTarget) {
					return nil, fmt.Errorf("riscv32: mixed indirect resolution branch out of range")
				}
			} else if relocSink == nil {
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
			if op.Kind == shared.MixedCallIndirect || op.Kind == shared.MixedCallImport {
				must(a.Lw(rv.T0, rv.SP, indirectTargetOffset), "indirect call target restore")
				a.Blr(rv.T0)
			} else {
				at := a.FarCall(branchScratch)
				*relocSink = append(*relocSink, callReloc{at: at, target: int(op.Target)})
			}
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
