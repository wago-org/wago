package wasm3

func (v *funcValidator) step(in Instruction) error {
	if v.constOnly && !isConstInstruction(in.Kind) {
		return v.verr(ErrConstExprRequired, in.Kind.String())
	}
	for _, t := range in.ValTypes {
		if err := v.validateValType(t); err != nil {
			return err
		}
	}
	if li, ok := loads[in.Kind]; ok {
		addr, err := v.checkMemArg(in.MemArg, li.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(li.t)
		return nil
	}
	if si, ok := stores[in.Kind]; ok {
		addr, err := v.checkMemArg(in.MemArg, si.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(si.t); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	switch in.Kind {
	case InstrUnreachable:
		v.unreachable()
	case InstrNop:
	case InstrBlock, InstrLoop:
		ins, outs, err := v.blockSig(in.BlockType)
		if err != nil {
			return err
		}
		kind := ctrlBlock
		if in.Kind == InstrLoop {
			kind = ctrlLoop
		}
		if err := v.pushCtrl(kind, ins, outs); err != nil {
			return err
		}
		for _, child := range in.Body.Instrs {
			if err := v.step(child); err != nil {
				return err
			}
		}
		_, err = v.popCtrl()
		return err
	case InstrIf:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		ins, outs, err := v.blockSig(in.BlockType)
		if err != nil {
			return err
		}
		baseVals := append([]val(nil), v.vals...)
		baseCtrls := append([]ctrlFrame(nil), v.ctrls...)
		if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
			return err
		}
		for _, child := range in.Then {
			if err := v.step(child); err != nil {
				return err
			}
		}
		_, err = v.popCtrl()
		if err != nil {
			return err
		}
		thenVals := append([]val(nil), v.vals...)
		v.vals = baseVals
		v.ctrls = baseCtrls
		if len(in.Else) > 0 {
			if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
				return err
			}
			for _, child := range in.Else {
				if err := v.step(child); err != nil {
					return err
				}
			}
			_, err = v.popCtrl()
			if err != nil {
				return err
			}
		} else if len(outs) != 0 || len(ins) != 0 {
			return v.verr(ErrTypeMismatch, "if without else")
		}
		if len(in.Else) > 0 && len(v.vals) != len(thenVals) {
			return v.verr(ErrTypeMismatch, "if branch heights")
		}
	case InstrBr:
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		if err := v.popAll(lt); err != nil {
			return err
		}
		v.unreachable()
	case InstrBrIf:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		if err := v.popAll(lt); err != nil {
			return err
		}
		v.pushAll(lt)
	case InstrBrTable:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		dt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		for _, l := range in.Indices {
			lt, err := v.label(l)
			if err != nil {
				return err
			}
			if len(lt) != len(dt) {
				return v.verr(ErrTypeMismatch, "br_table label arity")
			}
		}
		if err := v.popAll(dt); err != nil {
			return err
		}
		v.unreachable()
	case InstrReturn:
		if err := v.popAll(v.ctrls[0].out); err != nil {
			return err
		}
		v.unreachable()
	case InstrCall, InstrReturnCall:
		ft, ok := v.funcType(in.Index)
		if !ok {
			return v.verr(ErrUnknownFunc, "")
		}
		if err := v.popAll(ft.Params); err != nil {
			return err
		}
		if in.Kind == InstrReturnCall {
			if !sameValTypes(ft.Results, v.ctrls[0].out) {
				return v.verr(ErrTypeMismatch, "return_call")
			}
			v.unreachable()
		} else {
			v.pushAll(ft.Results)
		}
	case InstrCallIndirect, InstrReturnCallIndirect:
		ft := v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
		if ft == nil {
			return v.verr(ErrUnknownType, "call_indirect")
		}
		tt, ok := v.tableType(in.Index2)
		if !ok {
			return v.verr(ErrUnknownTable, "")
		}
		if !v.refSubtype(tt.Ref, AbsRef(HeapFunc)) {
			return v.verr(ErrTypeMismatch, "call_indirect table element type")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popAll(ft.Params); err != nil {
			return err
		}
		if in.Kind == InstrReturnCallIndirect {
			if !sameValTypes(ft.Results, v.ctrls[0].out) {
				return v.verr(ErrTypeMismatch, "return_call_indirect")
			}
			v.unreachable()
		} else {
			v.pushAll(ft.Results)
		}
	case InstrDrop:
		_, err := v.pop()
		return err
	case InstrSelect:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if len(in.ValTypes) == 1 {
			if err := v.popExpect(in.ValTypes[0]); err != nil {
				return err
			}
			if err := v.popExpect(in.ValTypes[0]); err != nil {
				return err
			}
			v.push(in.ValTypes[0])
		} else {
			a, err := v.pop()
			if err != nil {
				return err
			}
			b, err := v.pop()
			if err != nil {
				return err
			}
			if !a.unknown && !b.unknown && !equalValType(a.t, b.t) {
				return v.verr(ErrTypeMismatch, "select")
			}
			if a.unknown {
				v.vals = append(v.vals, b)
			} else {
				v.vals = append(v.vals, a)
			}
		}
	case InstrLocalGet:
		if int(in.Index) >= len(v.locals) {
			return v.verr(ErrUnknownLocal, "")
		}
		v.push(v.locals[in.Index])
	case InstrLocalSet:
		if int(in.Index) >= len(v.locals) {
			return v.verr(ErrUnknownLocal, "")
		}
		return v.popExpect(v.locals[in.Index])
	case InstrLocalTee:
		if int(in.Index) >= len(v.locals) {
			return v.verr(ErrUnknownLocal, "")
		}
		if err := v.popExpect(v.locals[in.Index]); err != nil {
			return err
		}
		v.push(v.locals[in.Index])
	case InstrGlobalGet:
		gt, ok := v.globalType(in.Index)
		if !ok {
			return v.verr(ErrUnknownGlobal, "")
		}
		if v.constOnly && (int(in.Index) >= v.m.ImportedGlobalCount() || gt.Mutable) {
			return v.verr(ErrConstExprRequired, "global.get")
		}
		v.push(gt.Type)
	case InstrGlobalSet:
		gt, ok := v.globalType(in.Index)
		if !ok {
			return v.verr(ErrUnknownGlobal, "")
		}
		if !gt.Mutable {
			return v.verr(ErrImmutableGlobal, "")
		}
		return v.popExpect(gt.Type)
	case InstrTableGet:
		tt, ok := v.tableType(in.Index)
		if !ok {
			return v.verr(ErrUnknownTable, "")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		v.push(RefVal(tt.Ref))
	case InstrTableSet:
		tt, ok := v.tableType(in.Index)
		if !ok {
			return v.verr(ErrUnknownTable, "")
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(I32)
	case InstrI32Const:
		v.push(I32)
	case InstrI64Const:
		v.push(I64)
	case InstrF32Const:
		v.push(F32)
	case InstrF64Const:
		v.push(F64)
	case InstrRefNull:
		if err := v.validateRefType(in.RefType); err != nil {
			return err
		}
		v.push(RefVal(in.RefType))
	case InstrRefFunc:
		if int(in.Index) >= v.m.FuncCount() {
			return v.verr(ErrUnknownFunc, "ref.func")
		}
		v.push(FuncRef)
	case InstrRefIsNull:
		_, err := v.pop()
		if err != nil {
			return err
		}
		v.push(I32)
	case InstrRefEq:
		if err := v.popExpect(EqRef); err != nil {
			return err
		}
		if err := v.popExpect(EqRef); err != nil {
			return err
		}
		v.push(I32)
	case InstrStringConst:
		if int(in.Index) >= len(v.m.StringRefs) {
			return v.verr(ErrTypeMismatch, "string.const index")
		}
		v.push(StringRef)
	case InstrRefAsNonNull:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, "ref.as_non_null")
		}
		if !x.unknown {
			x.t.Ref.Nullable = false
		}
		v.vals = append(v.vals, x)
	case InstrBrOnNull:
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, "br_on_null")
		}
		if err := v.popAll(lt); err != nil {
			return err
		}
		v.pushAll(lt)
		if !x.unknown {
			x.t.Ref.Nullable = false
		}
		v.vals = append(v.vals, x)
	case InstrBrOnNonNull:
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, "br_on_non_null")
		}
		if !x.unknown {
			x.t.Ref.Nullable = false
		}
		v.vals = append(v.vals, x)
		if err := v.popAll(lt); err != nil {
			return err
		}
		v.pushAll(lt)
	case InstrMemoryInit:
		if int(in.Index) >= len(v.m.Data) {
			return v.verr(ErrInvalidDataCount, "memory.init data index")
		}
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index2))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil { // length
			return err
		}
		if err := v.popExpect(I32); err != nil { // source offset in data segment
			return err
		}
		return v.popExpect(addr) // destination
	case InstrMemoryCopy:
		addrDst, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		addrSrc, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index2))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addrDst); err != nil { // length follows destination memory address type
			return err
		}
		if err := v.popExpect(addrSrc); err != nil {
			return err
		}
		return v.popExpect(addrDst)
	case InstrMemoryFill:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil { // length
			return err
		}
		if err := v.popExpect(I32); err != nil { // byte value
			return err
		}
		return v.popExpect(addr) // destination
	case InstrDataDrop:
		if int(in.Index) >= len(v.m.Data) {
			return v.verr(ErrInvalidDataCount, "data.drop")
		}
	case InstrTableInit:
		if int(in.Index) >= len(v.m.Elements) {
			return v.verr(ErrUnknownTable, "table.init elem")
		}
		if _, ok := v.tableType(in.Index2); !ok {
			return v.verr(ErrUnknownTable, "table.init table")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(I32)
	case InstrTableCopy:
		if _, ok := v.tableType(in.Index); !ok {
			return v.verr(ErrUnknownTable, "table.copy dst")
		}
		if _, ok := v.tableType(in.Index2); !ok {
			return v.verr(ErrUnknownTable, "table.copy src")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(I32)
	case InstrElemDrop:
		if int(in.Index) >= len(v.m.Elements) {
			return v.verr(ErrUnknownTable, "elem.drop")
		}
	case InstrTableSize:
		if _, ok := v.tableType(in.Index); !ok {
			return v.verr(ErrUnknownTable, "table.size")
		}
		v.push(I32)
	case InstrTableGrow:
		tt, ok := v.tableType(in.Index)
		if !ok {
			return v.verr(ErrUnknownTable, "table.grow")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		v.push(I32)
	case InstrTableFill:
		tt, ok := v.tableType(in.Index)
		if !ok {
			return v.verr(ErrUnknownTable, "table.fill")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(I32)
	default:
		if handled, err := v.proposalStep(in); handled || err != nil {
			return err
		}
		return v.stackEffect(in.Kind)
	}
	return nil
}

func isConstInstruction(k InstrKind) bool {
	switch k {
	case InstrI32Const, InstrI64Const, InstrF32Const, InstrF64Const, InstrRefNull, InstrRefFunc, InstrGlobalGet, InstrStructNewDefault, InstrArrayNewFixed, InstrStringConst:
		return true
	}
	return false
}
func sameValTypes(a, b []ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalValType(a[i], b[i]) {
			return false
		}
	}
	return true
}

func (v *funcValidator) stackEffect(k InstrKind) error {
	if t, ok := unary[k]; ok {
		if err := v.popExpect(t); err != nil {
			return err
		}
		v.push(t)
		return nil
	}
	if t, ok := binaryOps[k]; ok {
		if err := v.popExpect(t); err != nil {
			return err
		}
		if err := v.popExpect(t); err != nil {
			return err
		}
		v.push(t)
		return nil
	}
	if t, ok := compare[k]; ok {
		if err := v.popExpect(t); err != nil {
			return err
		}
		if err := v.popExpect(t); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if t, ok := test[k]; ok {
		if err := v.popExpect(t); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if eff, ok := conversions[k]; ok {
		if err := v.popExpect(eff.from); err != nil {
			return err
		}
		v.push(eff.to)
		return nil
	}
	if li, ok := loads[k]; ok {
		if err := v.checkMem(li.align); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		v.push(li.t)
		return nil
	}
	if si, ok := stores[k]; ok {
		if err := v.checkMem(si.align); err != nil {
			return err
		}
		if err := v.popExpect(si.t); err != nil {
			return err
		}
		return v.popExpect(I32)
	}
	switch k {
	case InstrMemorySize:
		addr, err := v.checkMemArg(MemArg{}, 0)
		if err != nil {
			return err
		}
		v.push(addr)
		return nil
	case InstrMemoryGrow:
		addr, err := v.checkMemArg(MemArg{}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(addr)
		return nil
	case InstrI32TruncSatF32S, InstrI32TruncSatF32U:
		if err := v.popExpect(F32); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrI32TruncSatF64S, InstrI32TruncSatF64U:
		if err := v.popExpect(F64); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrI64TruncSatF32S, InstrI64TruncSatF32U:
		if err := v.popExpect(F32); err != nil {
			return err
		}
		v.push(I64)
		return nil
	case InstrI64TruncSatF64S, InstrI64TruncSatF64U:
		if err := v.popExpect(F64); err != nil {
			return err
		}
		v.push(I64)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, k.String())
}
func (v *funcValidator) checkMem(align uint32) error {
	_, err := v.checkMemArg(MemArg{Align: align}, align)
	return err
}

func (v *funcValidator) checkMemArg(ma MemArg, natural uint32) (ValType, error) {
	idx := uint32(0)
	if ma.Mem != nil {
		idx = uint32(*ma.Mem)
	}
	mt, ok := v.memoryType(idx)
	if !ok {
		return ValType{}, v.verr(ErrUnknownMemory, "")
	}
	if ma.Align > natural {
		return ValType{}, v.verr(ErrInvalidAlignment, "")
	}
	if mt.Limits.Addr64 {
		return I64, nil
	}
	if ma.Offset > uint64(^uint32(0)) {
		return ValType{}, v.verr(ErrInvalidAlignment, "offset out of range for i32 memory")
	}
	return I32, nil
}

var unary = map[InstrKind]ValType{InstrI32Clz: I32, InstrI32Ctz: I32, InstrI32Popcnt: I32, InstrI64Clz: I64, InstrI64Ctz: I64, InstrI64Popcnt: I64, InstrF32Abs: F32, InstrF32Neg: F32, InstrF32Ceil: F32, InstrF32Floor: F32, InstrF32Trunc: F32, InstrF32Nearest: F32, InstrF32Sqrt: F32, InstrF64Abs: F64, InstrF64Neg: F64, InstrF64Ceil: F64, InstrF64Floor: F64, InstrF64Trunc: F64, InstrF64Nearest: F64, InstrF64Sqrt: F64, InstrI32Extend8S: I32, InstrI32Extend16S: I32, InstrI64Extend8S: I64, InstrI64Extend16S: I64, InstrI64Extend32S: I64}
var binaryOps = map[InstrKind]ValType{InstrI32Add: I32, InstrI32Sub: I32, InstrI32Mul: I32, InstrI32DivS: I32, InstrI32DivU: I32, InstrI32RemS: I32, InstrI32RemU: I32, InstrI32And: I32, InstrI32Or: I32, InstrI32Xor: I32, InstrI32Shl: I32, InstrI32ShrS: I32, InstrI32ShrU: I32, InstrI32Rotl: I32, InstrI32Rotr: I32, InstrI64Add: I64, InstrI64Sub: I64, InstrI64Mul: I64, InstrI64DivS: I64, InstrI64DivU: I64, InstrI64RemS: I64, InstrI64RemU: I64, InstrI64And: I64, InstrI64Or: I64, InstrI64Xor: I64, InstrI64Shl: I64, InstrI64ShrS: I64, InstrI64ShrU: I64, InstrI64Rotl: I64, InstrI64Rotr: I64, InstrF32Add: F32, InstrF32Sub: F32, InstrF32Mul: F32, InstrF32Div: F32, InstrF32Min: F32, InstrF32Max: F32, InstrF32Copysign: F32, InstrF64Add: F64, InstrF64Sub: F64, InstrF64Mul: F64, InstrF64Div: F64, InstrF64Min: F64, InstrF64Max: F64, InstrF64Copysign: F64}
var compare = map[InstrKind]ValType{InstrI32Eq: I32, InstrI32Ne: I32, InstrI32LtS: I32, InstrI32LtU: I32, InstrI32GtS: I32, InstrI32GtU: I32, InstrI32LeS: I32, InstrI32LeU: I32, InstrI32GeS: I32, InstrI32GeU: I32, InstrI64Eq: I64, InstrI64Ne: I64, InstrI64LtS: I64, InstrI64LtU: I64, InstrI64GtS: I64, InstrI64GtU: I64, InstrI64LeS: I64, InstrI64LeU: I64, InstrI64GeS: I64, InstrI64GeU: I64, InstrF32Eq: F32, InstrF32Ne: F32, InstrF32Lt: F32, InstrF32Gt: F32, InstrF32Le: F32, InstrF32Ge: F32, InstrF64Eq: F64, InstrF64Ne: F64, InstrF64Lt: F64, InstrF64Gt: F64, InstrF64Le: F64, InstrF64Ge: F64}
var test = map[InstrKind]ValType{InstrI32Eqz: I32, InstrI64Eqz: I64}

type conv struct{ from, to ValType }

var conversions = map[InstrKind]conv{InstrI32WrapI64: {I64, I32}, InstrI32TruncF32S: {F32, I32}, InstrI32TruncF32U: {F32, I32}, InstrI32TruncF64S: {F64, I32}, InstrI32TruncF64U: {F64, I32}, InstrI64ExtendI32S: {I32, I64}, InstrI64ExtendI32U: {I32, I64}, InstrI64TruncF32S: {F32, I64}, InstrI64TruncF32U: {F32, I64}, InstrI64TruncF64S: {F64, I64}, InstrI64TruncF64U: {F64, I64}, InstrF32ConvertI32S: {I32, F32}, InstrF32ConvertI32U: {I32, F32}, InstrF32ConvertI64S: {I64, F32}, InstrF32ConvertI64U: {I64, F32}, InstrF32DemoteF64: {F64, F32}, InstrF64ConvertI32S: {I32, F64}, InstrF64ConvertI32U: {I32, F64}, InstrF64ConvertI64S: {I64, F64}, InstrF64ConvertI64U: {I64, F64}, InstrF64PromoteF32: {F32, F64}, InstrI32ReinterpretF32: {F32, I32}, InstrI64ReinterpretF64: {F64, I64}, InstrF32ReinterpretI32: {I32, F32}, InstrF64ReinterpretI64: {I64, F64}}

type memeff struct {
	t     ValType
	align uint32
}

var loads = map[InstrKind]memeff{InstrI32Load: {I32, 2}, InstrI64Load: {I64, 3}, InstrF32Load: {F32, 2}, InstrF64Load: {F64, 3}, InstrI32Load8S: {I32, 0}, InstrI32Load8U: {I32, 0}, InstrI32Load16S: {I32, 1}, InstrI32Load16U: {I32, 1}, InstrI64Load8S: {I64, 0}, InstrI64Load8U: {I64, 0}, InstrI64Load16S: {I64, 1}, InstrI64Load16U: {I64, 1}, InstrI64Load32S: {I64, 2}, InstrI64Load32U: {I64, 2}}
var stores = map[InstrKind]memeff{InstrI32Store: {I32, 2}, InstrI64Store: {I64, 3}, InstrF32Store: {F32, 2}, InstrF64Store: {F64, 3}, InstrI32Store8: {I32, 0}, InstrI32Store16: {I32, 1}, InstrI64Store8: {I64, 0}, InstrI64Store16: {I64, 1}, InstrI64Store32: {I64, 2}}
