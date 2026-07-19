package wasm

// step validates one already-decoded instruction. in is taken by pointer: the
// Instruction struct is ~56 bytes and this is the validator's innermost hot path,
// so passing a value here shows up as runtime.duffcopy under profiling.
func (v *funcValidator) step(in *Instruction) error {
	if v.constOnly && !isConstInstruction(in.Kind) {
		return v.verr(ErrConstExprRequired, in.Kind.String())
	}
	for _, t := range in.ValTypes() {
		if err := v.validateValType(t); err != nil {
			return err
		}
	}
	if e := opEffects[in.Kind]; e.cat == effLoad {
		addr, err := v.checkMemArg(in.MemArg(), e.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(e.a)
		return nil
	} else if e.cat == effStore {
		addr, err := v.checkMemArg(in.MemArg(), e.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(e.a); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	switch in.Kind {
	case InstrUnreachable:
		v.unreachable()
	case InstrNop:
	case InstrBlock, InstrLoop:
		ins, outs, err := v.blockSig(in.BlockType())
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
		for _, child := range in.Body().Instrs {
			if err := v.step(&child); err != nil {
				return err
			}
		}
		_, err = v.popCtrl()
		return err
	case InstrIf:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		ins, outs, err := v.blockSig(in.BlockType())
		if err != nil {
			return err
		}
		baseVals := append([]val(nil), v.vals...)
		baseCtrls := append([]ctrlFrame(nil), v.ctrls...)
		if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
			return err
		}
		for _, child := range in.Then() {
			if err := v.step(&child); err != nil {
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
		if len(in.Else()) > 0 {
			if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
				return err
			}
			for _, child := range in.Else() {
				if err := v.step(&child); err != nil {
					return err
				}
			}
			_, err = v.popCtrl()
			if err != nil {
				return err
			}
		} else if !sameValTypes(ins, outs) {
			// With no else arm, the false path preserves the block inputs as the
			// expression results. Accept only the shape the IR builder can model
			// directly: identical input/output types.
			return v.verr(ErrTypeMismatch, "if without else")
		}
		if len(in.Else()) > 0 && len(v.vals) != len(thenVals) {
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
		payloadHeight := len(v.vals)
		for _, l := range in.Indices() {
			lt, err := v.label(l)
			if err != nil {
				return err
			}
			if len(lt) != len(dt) {
				return v.verr(ErrTypeMismatch, "br_table label arity")
			}
			// Every target consumes the same available branch payload. Restore the
			// values after each check so an unreachable-stack bottom can match
			// heterogeneous equal-arity labels without weakening reachable values.
			if err := v.popAll(lt); err != nil {
				return err
			}
			v.vals = v.vals[:payloadHeight]
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
		addr := I32
		if tt.Limits.Addr64 {
			addr = I64
		}
		if err := v.popExpect(addr); err != nil {
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
		// The typed-select immediate is a result type constrained by the core
		// spec to exactly one value type; len==0 is the untyped select form.
		if len(in.ValTypes()) > 1 {
			return v.verr(ErrTypeMismatch, "select type arity")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if len(in.ValTypes()) == 1 {
			if err := v.popExpect(in.ValTypes()[0]); err != nil {
				return err
			}
			if err := v.popExpect(in.ValTypes()[0]); err != nil {
				return err
			}
			v.push(in.ValTypes()[0])
		} else {
			a, err := v.pop()
			if err != nil {
				return err
			}
			b, err := v.pop()
			if err != nil {
				return err
			}
			// The implicit form is restricted to numeric and vector values. A
			// stack-polymorphic unknown still matches any permitted operand, but
			// must not hide a known reference operand.
			if (!a.unknown && !isImplicitSelectType(a.t)) || (!b.unknown && !isImplicitSelectType(b.t)) {
				return v.verr(ErrTypeMismatch, "implicit select operand type")
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
		t, ok := v.localType(in.Index)
		if !ok {
			return v.verr(ErrUnknownLocal, "")
		}
		v.push(t)
	case InstrLocalSet:
		t, ok := v.localType(in.Index)
		if !ok {
			return v.verr(ErrUnknownLocal, "")
		}
		return v.popExpect(t)
	case InstrLocalTee:
		t, ok := v.localType(in.Index)
		if !ok {
			return v.verr(ErrUnknownLocal, "")
		}
		if err := v.popExpect(t); err != nil {
			return err
		}
		v.push(t)
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
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(RefVal(tt.Ref))
	case InstrTableSet:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(addr)
	case InstrI32Const:
		v.push(I32)
	case InstrI64Const:
		v.push(I64)
	case InstrF32Const:
		v.push(F32)
	case InstrF64Const:
		v.push(F64)
	case InstrRefNull:
		if err := v.validateRefType(in.RefType()); err != nil {
			return err
		}
		v.push(RefVal(in.RefType()))
	case InstrRefFunc:
		if int(in.Index) >= v.m.FuncCount() {
			return v.verr(ErrUnknownFunc, "ref.func")
		}
		if !v.isDeclaredFunc(in.Index) {
			return v.verr(ErrUnknownFunc, "undeclared function reference")
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
		if err := v.checkDataIndex(in.Index, "memory.init"); err != nil {
			return err
		}
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index2))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil { // length in data segment bytes
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
		if err := v.popExpect(minAddrType(addrDst, addrSrc)); err != nil { // length
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
		if err := v.checkDataIndex(in.Index, "data.drop"); err != nil {
			return err
		}
	case InstrTableInit:
		// Element expressions were validated during the serial module phase. The
		// body check needs only their immutable declared/result reference type.
		elemRef, err := v.elemRefType(in.Index)
		if err != nil {
			return err
		}
		tt, ok := v.tableType(in.Index2)
		if !ok {
			return v.verr(ErrUnknownTable, "table.init table")
		}
		if !v.refSubtype(elemRef, tt.Ref) {
			return v.verr(ErrTypeMismatch, "table.init element type")
		}
		addr := tableAddrType(tt)
		if err := v.popExpect(I32); err != nil { // length in element-segment entries
			return err
		}
		if err := v.popExpect(I32); err != nil { // source offset in element segment
			return err
		}
		return v.popExpect(addr) // destination table offset
	case InstrTableCopy:
		addrDst, dst, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.copy dst")
		}
		addrSrc, src, err := v.tableAddrType(in.Index2)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.copy src")
		}
		if !v.refSubtype(src.Ref, dst.Ref) {
			return v.verr(ErrTypeMismatch, "table.copy element type")
		}
		if err := v.popExpect(minAddrType(addrDst, addrSrc)); err != nil {
			return err
		}
		if err := v.popExpect(addrSrc); err != nil {
			return err
		}
		return v.popExpect(addrDst)
	case InstrElemDrop:
		if v.direct != nil {
			if int(in.Index) >= len(v.direct.elements) {
				return v.verr(ErrUnknownTable, "elem.drop")
			}
		} else if int(in.Index) >= len(v.m.Elements) {
			return v.verr(ErrUnknownTable, "elem.drop")
		}
	case InstrTableSize:
		addr, _, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.size")
		}
		v.push(addr)
	case InstrTableGrow:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.grow")
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		v.push(addr)
	case InstrTableFill:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.fill")
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(addr)
	default:
		if handled, err := v.proposalStep(in); handled || err != nil {
			return err
		}
		return v.stackEffect(in)
	}
	return nil
}

func isConstInstruction(k InstrKind) bool {
	switch k {
	case InstrI32Const, InstrI64Const, InstrF32Const, InstrF64Const, InstrV128Const, InstrRefNull, InstrRefFunc, InstrGlobalGet, InstrStructNewDefault, InstrArrayNewFixed, InstrStringConst:
		return true
	}
	return false
}
func isImplicitSelectType(t ValType) bool {
	return t.Kind == ValNum || t.Kind == ValVec
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

func (v *funcValidator) stackEffect(in *Instruction) error {
	k := in.Kind
	if e := opEffects[k]; e.cat != effNone {
		switch e.cat {
		case effUnary:
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			v.push(e.a)
		case effBinary:
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			v.push(e.a)
		case effCompare:
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			v.push(I32)
		case effTest:
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			v.push(I32)
		case effConv:
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			v.push(e.b)
		case effLoad:
			if err := v.checkMem(e.align); err != nil {
				return err
			}
			if err := v.popExpect(I32); err != nil {
				return err
			}
			v.push(e.a)
		case effStore:
			if err := v.checkMem(e.align); err != nil {
				return err
			}
			if err := v.popExpect(e.a); err != nil {
				return err
			}
			return v.popExpect(I32)
		}
		return nil
	}
	switch k {
	case InstrMemorySize:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		v.push(addr)
		return nil
	case InstrMemoryGrow:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
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

func (v *funcValidator) checkDataIndex(idx uint32, op string) error {
	// Bulk-memory data instructions are guarded by the data count section. The
	// segment may have any mode; active segments are already dropped at runtime.
	if v.m.DataCount == nil || idx >= *v.m.DataCount || int(idx) >= len(v.m.Data) {
		return v.verr(ErrInvalidDataCount, op+" data index")
	}
	return nil
}

func tableAddrType(tt TableType) ValType { return TableAddrType(tt) }

func minAddrType(a, b ValType) ValType {
	if equalValType(a, I32) || equalValType(b, I32) {
		return I32
	}
	return I64
}

func (v *funcValidator) tableAddrType(idx uint32) (ValType, TableType, error) {
	tt, ok := v.tableType(idx)
	if !ok {
		return ValType{}, TableType{}, v.verr(ErrUnknownTable, "")
	}
	return tableAddrType(tt), tt, nil
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

func (v *funcValidator) checkSharedMemArg(ma MemArg, natural uint32) (ValType, error) {
	addr, err := v.checkMemArg(ma, natural)
	if err != nil {
		return ValType{}, err
	}
	idx := uint32(0)
	if ma.Mem != nil {
		idx = uint32(*ma.Mem)
	}
	mt, _ := v.memoryType(idx) // existence was checked by checkMemArg above.
	if mt == nil || !mt.Shared {
		// Atomic memory instructions are valid only for shared memories.
		return ValType{}, v.verr(ErrInvalidSharedMemory, "atomic memory instruction")
	}
	return addr, nil
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

// opEffect is a precomputed stack effect for the simple numeric/mem instructions,
// collapsing the per-instruction cascade of map lookups (unary → binaryOps →
// compare → test → conversions → loads → stores) into one array index — the
// hottest map cost in validation. Built once at init from those maps, which stay
// the single source of truth.
type opEffectCat uint8

const (
	effNone    opEffectCat = iota
	effUnary               // pop a; push a
	effBinary              // pop a; pop a; push a
	effCompare             // pop a; pop a; push i32
	effTest                // pop a; push i32
	effConv                // pop a; push b
	effLoad                // checkMem(align); pop i32; push a
	effStore               // checkMem(align); pop a; pop i32
)

type opEffect struct {
	cat   opEffectCat
	a, b  ValType
	align uint32
}

var opEffects [numInstrKinds]opEffect

func init() {
	for k, t := range unary {
		opEffects[k] = opEffect{cat: effUnary, a: t}
	}
	for k, t := range binaryOps {
		opEffects[k] = opEffect{cat: effBinary, a: t}
	}
	for k, t := range compare {
		opEffects[k] = opEffect{cat: effCompare, a: t}
	}
	for k, t := range test {
		opEffects[k] = opEffect{cat: effTest, a: t}
	}
	for k, c := range conversions {
		opEffects[k] = opEffect{cat: effConv, a: c.from, b: c.to}
	}
	for k, m := range loads {
		opEffects[k] = opEffect{cat: effLoad, a: m.t, align: m.align}
	}
	for k, m := range stores {
		opEffects[k] = opEffect{cat: effStore, a: m.t, align: m.align}
	}
}
