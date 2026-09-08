package wasm

// IsCoreAtomicInstructionKind reports whether kind belongs to the core threads
// proposal's linear-memory atomic instruction family. GC struct atomics share
// the binary prefix but deliberately remain behind their own feature boundary.
func IsCoreAtomicInstructionKind(kind InstrKind) bool {
	switch kind {
	case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32, InstrMemoryAtomicWait64,
		InstrAtomicFence,
		InstrI32AtomicLoad, InstrI64AtomicLoad, InstrI32AtomicLoad8U,
		InstrI32AtomicLoad16U, InstrI64AtomicLoad8U, InstrI64AtomicLoad16U,
		InstrI64AtomicLoad32U,
		InstrI32AtomicStore, InstrI64AtomicStore, InstrI32AtomicStore8,
		InstrI32AtomicStore16, InstrI64AtomicStore8, InstrI64AtomicStore16,
		InstrI64AtomicStore32,
		InstrAtomicRmw, InstrAtomicCmpxchg:
		return true
	default:
		return false
	}
}

func (v *funcValidator) proposalStep(in *Instruction) (bool, error) {
	switch in.Kind {
	case InstrThrow:
		return true, v.stepThrow(*in)
	case InstrThrowRef:
		if err := v.popExpect(RefVal(AbsRef(HeapExn))); err != nil {
			return true, err
		}
		v.unreachable()
		return true, nil
	case InstrTryTable:
		return true, v.stepTryTable(*in)
	case InstrCallRef, InstrReturnCallRef:
		return true, v.stepCallRef(*in)
	case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32, InstrMemoryAtomicWait64, InstrAtomicFence,
		InstrI32AtomicLoad, InstrI64AtomicLoad, InstrI32AtomicLoad8U, InstrI32AtomicLoad16U,
		InstrI64AtomicLoad8U, InstrI64AtomicLoad16U, InstrI64AtomicLoad32U,
		InstrI32AtomicStore, InstrI64AtomicStore, InstrI32AtomicStore8, InstrI32AtomicStore16,
		InstrI64AtomicStore8, InstrI64AtomicStore16, InstrI64AtomicStore32,
		InstrAtomicRmw, InstrAtomicCmpxchg:
		return true, v.stepAtomic(*in)
	case InstrStructNew, InstrStructNewDefault, InstrStructNewDesc, InstrStructNewDefaultDesc,
		InstrStructGet, InstrStructGetS, InstrStructGetU, InstrStructAtomicGet, InstrStructAtomicGetS, InstrStructAtomicGetU, InstrStructSet,
		InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem,
		InstrArrayGet, InstrArrayGetS, InstrArrayGetU, InstrArraySet, InstrArrayLen, InstrArrayFill, InstrArrayCopy, InstrArrayInitData, InstrArrayInitElem,
		InstrRefGetDesc, InstrRefTest, InstrRefCast, InstrRefTestDesc, InstrRefCastDescEq, InstrBrOnCast, InstrBrOnCastFail,
		InstrAnyConvertExtern, InstrExternConvertAny, InstrRefI31, InstrI31GetS, InstrI31GetU:
		return true, v.stepGC(*in)
	}
	if in.Kind < numInstrKinds && simdEffects[in.Kind].cat != simdNone {
		return true, v.stepSIMD(*in)
	}
	return false, nil
}

func (v *funcValidator) stepThrow(in Instruction) error {
	ft, ok := v.tagFuncType(in.Index)
	if !ok {
		return v.verr(ErrUnknownTag, "throw")
	}
	if err := v.popAll(ft.Params); err != nil {
		return err
	}
	v.unreachable()
	return nil
}

func (v *funcValidator) stepCallRef(in Instruction) error {
	ft := v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
	if ft == nil {
		return v.verr(ErrUnknownType, "call_ref")
	}
	callee, err := v.pop()
	if err != nil {
		return err
	}
	wantTyped := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))
	if !callee.unknown && !v.subtype(callee.t, wantTyped) {
		// call_ref requires a reference to the selected function type. Nullable
		// typed references remain valid and trap dynamically when null; abstract
		// funcref has no exact callable signature.
		return v.verr(ErrTypeMismatch, "call_ref callee")
	}
	if err := v.popAll(ft.Params); err != nil {
		return err
	}
	if in.Kind == InstrReturnCallRef {
		if !v.matchValTypes(ft.Results, v.ctrls[0].out) {
			return v.verr(ErrTypeMismatch, "return_call_ref")
		}
		v.unreachable()
	} else {
		v.pushAll(ft.Results)
	}
	return nil
}

func (v *funcValidator) stepTryTable(in Instruction) error {
	ins, outs, err := v.blockSig(in.BlockType())
	if err != nil {
		return err
	}
	for _, c := range in.Catches() {
		if err := v.validateCatchPayload(c); err != nil {
			return err
		}
	}
	if err := v.pushCtrl(ctrlTry, ins, outs); err != nil {
		return err
	}
	for _, child := range in.Body().Instrs {
		if err := v.step(&child); err != nil {
			return err
		}
	}
	fr, err := v.popCtrl()
	if err == nil && fr.unreachable {
		// A try_table whose body has no normal completion leaves its parent path
		// unreachable; catches branch directly to their declared outer labels.
		v.unreachable()
	}
	return err
}

func (v *funcValidator) validateCatchPayload(c Catch) error {
	lt, err := v.label(uint32(c.Label))
	if err != nil {
		return err
	}
	var params []ValType
	if c.Kind == CatchTag || c.Kind == CatchRef {
		if int(c.Tag) >= (len(v.importsOfKind(ExternTag)) + len(v.m.Tags)) {
			return v.verr(ErrUnknownTag, "catch")
		}
		ft, ok := v.tagFuncType(uint32(c.Tag))
		if !ok {
			return v.verr(ErrUnknownTag, "catch")
		}
		params = ft.Params
	}
	hasRef := c.Kind == CatchRef || c.Kind == CatchAllRef
	if c.Kind == CatchAll && len(lt) != 0 {
		return v.verr(ErrTypeMismatch, "catch_all label must expect no values")
	}
	payloadLen := len(params)
	if hasRef {
		payloadLen++
	}
	if payloadLen != len(lt) {
		return v.verr(ErrTypeMismatch, "catch payload label mismatch")
	}
	for i := range params {
		if !v.subtype(params[i], lt[i]) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
	}
	if hasRef {
		// Reference catches materialize a non-null exception reference. The target
		// label may widen it to nullable exnref, but not vice versa.
		exn := RefVal(Ref(false, AbsHeap(HeapExn), false))
		if !v.subtype(exn, lt[len(params)]) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
	}
	return nil
}

func (v *moduleValidator) tagFuncType(idx uint32) (*CompType, bool) {
	indexes := v.importsOfKind(ExternTag)
	n := uint32(len(indexes))
	if idx < n {
		im := &v.m.Imports[indexes[idx]]
		ft := v.funcTypeFromTypeIdx(im.Type.TagType().Type)
		return ft, ft != nil
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Tags) {
		return nil, false
	}
	ft := v.funcTypeFromTypeIdx(v.m.Tags[local].Type)
	return ft, ft != nil
}

func (v *funcValidator) stepAtomic(in Instruction) error {
	if in.Kind == InstrAtomicFence {
		return nil
	}
	if in.Kind == InstrMemoryAtomicNotify {
		addr, err := v.checkAtomicMemArg(in.MemArg(), 2)
		if err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if in.Kind == InstrMemoryAtomicWait32 || in.Kind == InstrMemoryAtomicWait64 {
		natural := uint32(2)
		if in.Kind == InstrMemoryAtomicWait64 {
			natural = 3
		}
		addr, err := v.checkAtomicMemArg(in.MemArg(), natural)
		if err != nil {
			return err
		}
		if err := v.popExpect(I64); err != nil {
			return err
		}
		want := I32
		if in.Kind == InstrMemoryAtomicWait64 {
			want = I64
		}
		if err := v.popExpect(want); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if eff, ok := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicLoad, in.Kind); ok {
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(eff.typ.valType())
		return nil
	}
	if eff, ok := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicStore, in.Kind); ok {
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(eff.typ.valType()); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	if in.Kind == InstrAtomicRmw {
		eff := atomicRmwEffect(in.AtomicOp)
		typ := eff.typ.valType()
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(typ)
		return nil
	}
	if in.Kind == InstrAtomicCmpxchg {
		eff := atomicCmpxchgEffect(in.AtomicOp)
		typ := eff.typ.valType()
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(typ)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

type atomicEffect struct {
	typ   effectValue
	align uint8
}

var atomicLoadEffects = [...]atomicEffect{
	{typ: effectI32, align: 2},
	{typ: effectI64, align: 3},
	{typ: effectI32},
	{typ: effectI32, align: 1},
	{typ: effectI64},
	{typ: effectI64, align: 1},
	{typ: effectI64, align: 2},
}

func lookupAtomicEffect(table []atomicEffect, first, kind InstrKind) (atomicEffect, bool) {
	index := uint32(kind) - uint32(first)
	if index >= uint32(len(table)) {
		return atomicEffect{}, false
	}
	return table[index], true
}

func atomicRmwEffect(op uint32) atomicEffect {
	if op == 0 {
		op = 30
	}
	pos := (op - 30) % 7
	if pos == 0 {
		return atomicEffect{typ: effectI32, align: 2}
	}
	if pos == 1 {
		return atomicEffect{typ: effectI64, align: 3}
	}
	if pos == 2 || pos == 3 {
		return atomicEffect{typ: effectI32, align: uint8(pos - 2)}
	}
	return atomicEffect{typ: effectI64, align: uint8(pos - 4)}
}

func atomicCmpxchgEffect(op uint32) atomicEffect {
	if op == 0 {
		op = 72
	}
	switch op {
	case 72:
		return atomicEffect{typ: effectI32, align: 2}
	case 73:
		return atomicEffect{typ: effectI64, align: 3}
	case 74, 75:
		return atomicEffect{typ: effectI32, align: uint8(op - 74)}
	default:
		return atomicEffect{typ: effectI64, align: uint8(op - 76)}
	}
}

func (v *funcValidator) stepGC(in Instruction) error {
	switch in.Kind {
	case InstrRefI31:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		v.push(RefVal(Ref(false, AbsHeap(HeapI31), false)))
		return nil
	case InstrI31GetS, InstrI31GetU:
		if err := v.popExpect(I31Ref); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrAnyConvertExtern:
		if err := v.popExpect(ExternRef); err != nil {
			return err
		}
		v.push(AnyRef)
		return nil
	case InstrExternConvertAny:
		if err := v.popExpect(AnyRef); err != nil {
			return err
		}
		v.push(ExternRef)
		return nil
	case InstrRefTest, InstrRefTestDesc:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, in.Kind.String()+" expects a reference operand")
		}
		target, ok := v.descriptorTargetRefType(in.Cast.TargetNullable, in.HeapType(), false)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor target reftype")
		}
		if !x.unknown {
			compatible := v.refTestCompatible(x.t.Ref(), target.Ref())
			if in.Kind == InstrRefTestDesc {
				compatible = v.descriptorCompatible(x.t.Ref(), target.Ref())
			}
			if !compatible {
				return v.verr(ErrTypeMismatch, "target does not match operand type")
			}
		}
		v.push(I32)
		return nil
	case InstrRefCast, InstrRefCastDescEq:
		target, ok := v.descriptorTargetRefType(in.Cast.TargetNullable, in.HeapType(), in.Cast.SourceNullable)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor target reftype")
		}
		if in.Kind == InstrRefCastDescEq {
			desc, err := v.pop()
			if err != nil {
				return err
			}
			if !desc.unknown && desc.t.Kind() != ValRef {
				return v.verr(ErrTypeMismatch, "descriptor operand")
			}
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "ref.cast expects a reference operand")
		}
		if !x.unknown {
			compatible := v.refTestCompatible(x.t.Ref(), target.Ref())
			if in.Kind == InstrRefCastDescEq {
				compatible = v.descriptorCompatible(x.t.Ref(), target.Ref())
			}
			if !compatible {
				return v.verr(ErrTypeMismatch, "target does not match operand type")
			}
		}
		v.push(target)
		return nil
	case InstrBrOnCast, InstrBrOnCastFail:
		return v.stepBrOnCast(in)
	case InstrRefGetDesc:
		_, st, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "ref.get_desc")
		}
		descriptor, present := st.Metadata.Descriptor.Get()
		if !present {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "expected a reference operand")
		}
		if !x.unknown && !v.refSubtype(x.t.Ref(), Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)) {
			return v.verr(ErrTypeMismatch, "ref.get_desc target")
		}
		v.push(RefVal(Ref(false, IndexedHeap(descriptor), true)))
		return nil
	case InstrStructNew, InstrStructNewDefault, InstrStructNewDesc, InstrStructNewDefaultDesc:
		return v.stepStructNew(in)
	case InstrStructGet, InstrStructGetS, InstrStructGetU, InstrStructAtomicGet, InstrStructAtomicGetS, InstrStructAtomicGetU:
		fields, _, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "struct.get")
		}
		if int(in.Index2) >= len(fields) {
			return v.verr(ErrTypeMismatch, "unknown field")
		}
		f := fields[in.Index2]
		packedGet := packedFieldGet(in.Kind)
		if f.Storage().Packed() != packedGet {
			return v.verr(ErrTypeMismatch, "field storage does not match struct.get variant")
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(f.Storage(), packedGet))
		return nil
	case InstrStructSet:
		fields, _, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "struct.set")
		}
		if int(in.Index2) >= len(fields) {
			return v.verr(ErrTypeMismatch, "unknown field")
		}
		f := fields[in.Index2]
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable field")
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem:
		return v.stepArrayNew(in)
	case InstrArrayGet, InstrArrayGetS, InstrArrayGetU:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.get")
		}
		packedGet := packedFieldGet(in.Kind)
		if f.Storage().Packed() != packedGet {
			return v.verr(ErrTypeMismatch, "field storage does not match array.get variant")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(f.Storage(), packedGet))
		return nil
	case InstrArraySet:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.set")
		}
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayLen:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && (x.t.Kind() != ValRef || !v.heapSubtype(x.t.Ref().Heap(), AbsHeap(HeapArray))) {
			return v.verr(ErrTypeMismatch, "array.len")
		}
		v.push(I32)
		return nil
	case InstrArrayFill:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.fill")
		}
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayCopy:
		dst, _, okDst := v.arrayField(TypeIdx{Index: in.Index})
		src, _, okSrc := v.arrayField(TypeIdx{Index: in.Index2})
		if !okDst || !okSrc {
			return v.verr(ErrUnknownType, "array.copy")
		}
		if dst.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		storageMatches := false
		if dst.Storage().Packed() || src.Storage().Packed() {
			storageMatches = dst.Storage().Packed() && src.Storage().Packed() && dst.Storage().Pack() == src.Storage().Pack()
		} else {
			storageMatches = v.subtype(src.Storage().Val(), dst.Storage().Val())
		}
		if !storageMatches {
			return v.verr(ErrTypeMismatch, "array types do not match")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index2}), false))); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayInitData:
		field, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.init_data")
		}
		if field.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if !field.Storage().Packed() && field.Storage().Val().Kind() == ValRef {
			return v.verr(ErrTypeMismatch, "array type is not numeric or vector")
		}
		if err := v.checkDataIndex(in.Index2, "array.init_data"); err != nil {
			return err
		}
		for range 3 {
			if err := v.popExpect(I32); err != nil {
				return err
			}
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayInitElem:
		field, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.init_elem")
		}
		if field.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if field.Storage().Packed() || field.Storage().Val().Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "array.init_elem destination is not a reference array")
		}
		elemRef, err := v.elemRefType(in.Index2)
		if err != nil {
			return err
		}
		if !v.refSubtype(elemRef, field.Storage().Val().Ref()) {
			return v.verr(ErrTypeMismatch, "array.init_elem element type")
		}
		for range 3 {
			if err := v.popExpect(I32); err != nil {
				return err
			}
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

func (v *funcValidator) stepStructNew(in Instruction) error {
	fields, st, ok := v.structFields(TypeIdx{Index: in.Index})
	if !ok {
		return v.verr(ErrUnknownType, "struct.new")
	}
	if (in.Kind == InstrStructNew || in.Kind == InstrStructNewDefault) && st.Metadata.Descriptor.Present() {
		return v.verr(ErrTypeMismatch, "use struct.new_desc for descriptor-bearing struct")
	}
	if in.Kind == InstrStructNewDefault || in.Kind == InstrStructNewDefaultDesc {
		for _, f := range fields {
			if !f.Storage().Packed() && !valTypeDefaultable(storageValType(f.Storage(), false)) {
				return v.verr(ErrTypeMismatch, "field not defaultable")
			}
		}
	} else {
		for i := len(fields) - 1; i >= 0; i-- {
			if err := v.popExpect(storageValType(fields[i].Storage(), false)); err != nil {
				return err
			}
		}
	}
	if in.Kind == InstrStructNewDesc || in.Kind == InstrStructNewDefaultDesc {
		descriptor, present := st.Metadata.Descriptor.Get()
		if !present {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		want := RefVal(Ref(false, IndexedHeap(descriptor), true))
		if err := v.popExpect(want); err != nil {
			return err
		}
	}
	v.push(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	return nil
}

func (v *funcValidator) stepArrayNew(in Instruction) error {
	f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
	if !ok {
		return v.verr(ErrUnknownType, "array.new")
	}
	elem := storageValType(f.Storage(), false)
	switch in.Kind {
	case InstrArrayNew:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(elem); err != nil {
			return err
		}
	case InstrArrayNewDefault:
		if !f.Storage().Packed() && !valTypeDefaultable(elem) {
			return v.verr(ErrTypeMismatch, "element not defaultable")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewFixed:
		available := len(v.vals) - v.top().height
		if uint64(in.Index2) > uint64(available) && !v.top().unreachable {
			return v.verr(ErrTypeMismatch, "stack underflow")
		}
		count := available
		if uint64(in.Index2) < uint64(count) {
			count = int(in.Index2)
		} else if uint64(in.Index2) > uint64(available) {
			// The remaining operands are polymorphic bottom values. Validate only
			// the concrete stack suffix so an untrusted u32 count cannot turn a
			// tiny unreachable body into billions of validation iterations.
			count = available
		}
		for range count {
			if err := v.popExpect(elem); err != nil {
				return err
			}
		}
	case InstrArrayNewData:
		if !f.Storage().Packed() && f.Storage().Val().Kind() == ValRef {
			return v.verr(ErrTypeMismatch, "array type is not numeric or vector")
		}
		if err := v.checkDataIndex(in.Index2, "array.new_data"); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewElem:
		if f.Storage().Packed() || f.Storage().Val().Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "array.new_elem destination is not a reference array")
		}
		elemRef, err := v.elemRefType(in.Index2)
		if err != nil {
			return err
		}
		if !v.refSubtype(elemRef, f.Storage().Val().Ref()) {
			return v.verr(ErrTypeMismatch, "array.new_elem element type")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	}
	v.push(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	return nil
}

func (v *funcValidator) stepBrOnCast(in Instruction) error {
	lt, err := v.label(in.Index)
	if err != nil {
		return err
	}
	if len(lt) == 0 {
		return v.verr(ErrTypeMismatch, "label type too short")
	}
	labelRef := lt[len(lt)-1]
	if labelRef.Kind() != ValRef {
		return v.verr(ErrTypeMismatch, "label must end with a reftype")
	}
	rt1 := Ref(in.Cast.SourceNullable, in.HeapType(), false)
	rt2 := Ref(in.Cast.TargetNullable, in.HeapType2(), false)
	if !v.refSubtype(rt2, rt1) {
		return v.verr(ErrTypeMismatch, "rt2 does not match rt1")
	}
	x, err := v.pop()
	if err != nil {
		return err
	}
	if !x.unknown && (x.t.Kind() != ValRef || !v.refSubtype(x.t.Ref(), rt1)) {
		return v.verr(ErrTypeMismatch, "br_on_cast operand")
	}
	// A nullable target consumes null on the successful cast edge. The failed
	// edge is therefore known non-null even when the declared source is nullable.
	// When the target is non-null, null remains a possible failed value and the
	// source nullability is preserved.
	failed := rt1
	if rt2.Nullable() {
		failed = failed.WithNullable(false)
	}
	prefix := lt[:len(lt)-1]
	if in.Kind == InstrBrOnCastFail {
		if !v.subtype(RefVal(failed), labelRef) {
			return v.verr(ErrTypeMismatch, "failed source does not match label rt")
		}
		if err := v.popAll(prefix); err != nil {
			return err
		}
		v.pushAll(prefix)
		v.push(RefVal(rt2))
		return nil
	}
	if !v.subtype(RefVal(rt2), labelRef) {
		return v.verr(ErrTypeMismatch, "rt2 does not match label rt")
	}
	if err := v.popAll(prefix); err != nil {
		return err
	}
	v.pushAll(prefix)
	v.push(RefVal(failed))
	return nil
}

func (v *funcValidator) stepSIMD(in Instruction) error {
	e := simdEffects[in.Kind]
	if e.laneLimit != 0 && in.Lane >= e.laneLimit {
		return v.verr(ErrTypeMismatch, "simd lane out of range")
	}
	switch e.cat {
	case simdEffLoad:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffStore:
		addr, err := v.checkMemArg(in.MemArg(), 4)
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		return v.popExpect(addr)
	case simdEffMemLoadLane:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffMemStoreLane:
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		return v.popExpect(addr)
	case simdEffSplat:
		if err := v.popExpect(e.scalar.valType()); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffExtract:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(e.scalar.valType())
		return nil
	case simdEffReplace:
		if err := v.popExpect(e.scalar.valType()); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffShift:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffUnary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffBinary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffTernary:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdPopV128PushI32:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case simdBitselect:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdConst:
		v.push(V128)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

type simdEffectCat uint8

const (
	simdNone simdEffectCat = iota
	simdEffLoad
	simdEffStore
	simdEffMemLoadLane
	simdEffMemStoreLane
	simdEffSplat
	simdEffExtract
	simdEffReplace
	simdEffShift
	simdEffUnary
	simdEffBinary
	simdEffTernary
	simdPopV128PushI32
	simdBitselect
	simdConst
)

type simdEffect struct {
	cat       simdEffectCat
	scalar    effectValue
	align     uint8
	laneLimit LaneIdx
}

var simdEffects [numInstrKinds]simdEffect

type simdMemEffect struct {
	kind  InstrKind
	align uint8
}

type simdScalarEffect struct {
	kind   InstrKind
	scalar effectValue
}

type simdLaneLimit struct {
	kind  InstrKind
	limit LaneIdx
}

var simdLoads = [...]simdMemEffect{
	{kind: InstrV128Load, align: 4},
	{kind: InstrV128Load8x8S, align: 3},
	{kind: InstrV128Load8x8U, align: 3},
	{kind: InstrV128Load16x4S, align: 3},
	{kind: InstrV128Load16x4U, align: 3},
	{kind: InstrV128Load32x2S, align: 3},
	{kind: InstrV128Load32x2U, align: 3},
	{kind: InstrV128Load8Splat, align: 0},
	{kind: InstrV128Load16Splat, align: 1},
	{kind: InstrV128Load32Splat, align: 2},
	{kind: InstrV128Load64Splat, align: 3},
	{kind: InstrV128Load32Zero, align: 2},
	{kind: InstrV128Load64Zero, align: 3},
}

var simdMemLane = [...]simdMemEffect{
	{kind: InstrV128Load8Lane, align: 0},
	{kind: InstrV128Load16Lane, align: 1},
	{kind: InstrV128Load32Lane, align: 2},
	{kind: InstrV128Load64Lane, align: 3},
	{kind: InstrV128Store8Lane, align: 0},
	{kind: InstrV128Store16Lane, align: 1},
	{kind: InstrV128Store32Lane, align: 2},
	{kind: InstrV128Store64Lane, align: 3},
}

var simdLaneLimits = [...]simdLaneLimit{
	{kind: InstrI8x16ExtractLaneS, limit: 16},
	{kind: InstrI8x16ExtractLaneU, limit: 16},
	{kind: InstrI8x16ReplaceLane, limit: 16},
	{kind: InstrI16x8ExtractLaneS, limit: 8},
	{kind: InstrI16x8ExtractLaneU, limit: 8},
	{kind: InstrI16x8ReplaceLane, limit: 8},
	{kind: InstrI32x4ExtractLane, limit: 4},
	{kind: InstrI32x4ReplaceLane, limit: 4},
	{kind: InstrF32x4ExtractLane, limit: 4},
	{kind: InstrF32x4ReplaceLane, limit: 4},
	{kind: InstrI64x2ExtractLane, limit: 2},
	{kind: InstrI64x2ReplaceLane, limit: 2},
	{kind: InstrF64x2ExtractLane, limit: 2},
	{kind: InstrF64x2ReplaceLane, limit: 2},
	{kind: InstrV128Load8Lane, limit: 16},
	{kind: InstrV128Store8Lane, limit: 16},
	{kind: InstrV128Load16Lane, limit: 8},
	{kind: InstrV128Store16Lane, limit: 8},
	{kind: InstrV128Load32Lane, limit: 4},
	{kind: InstrV128Store32Lane, limit: 4},
	{kind: InstrV128Load64Lane, limit: 2},
	{kind: InstrV128Store64Lane, limit: 2},
}

var simdSplat = [...]simdScalarEffect{
	{kind: InstrI8x16Splat, scalar: effectI32},
	{kind: InstrI16x8Splat, scalar: effectI32},
	{kind: InstrI32x4Splat, scalar: effectI32},
	{kind: InstrI64x2Splat, scalar: effectI64},
	{kind: InstrF32x4Splat, scalar: effectF32},
	{kind: InstrF64x2Splat, scalar: effectF64},
}

var simdExtract = [...]simdScalarEffect{
	{kind: InstrI8x16ExtractLaneS, scalar: effectI32},
	{kind: InstrI8x16ExtractLaneU, scalar: effectI32},
	{kind: InstrI16x8ExtractLaneS, scalar: effectI32},
	{kind: InstrI16x8ExtractLaneU, scalar: effectI32},
	{kind: InstrI32x4ExtractLane, scalar: effectI32},
	{kind: InstrI64x2ExtractLane, scalar: effectI64},
	{kind: InstrF32x4ExtractLane, scalar: effectF32},
	{kind: InstrF64x2ExtractLane, scalar: effectF64},
}

var simdReplace = [...]simdScalarEffect{
	{kind: InstrI8x16ReplaceLane, scalar: effectI32},
	{kind: InstrI16x8ReplaceLane, scalar: effectI32},
	{kind: InstrI32x4ReplaceLane, scalar: effectI32},
	{kind: InstrI64x2ReplaceLane, scalar: effectI64},
	{kind: InstrF32x4ReplaceLane, scalar: effectF32},
	{kind: InstrF64x2ReplaceLane, scalar: effectF64},
}

var simdShift = [...]InstrKind{
	InstrI8x16Shl,
	InstrI8x16ShrS,
	InstrI8x16ShrU,
	InstrI16x8Shl,
	InstrI16x8ShrS,
	InstrI16x8ShrU,
	InstrI32x4Shl,
	InstrI32x4ShrS,
	InstrI32x4ShrU,
	InstrI64x2Shl,
	InstrI64x2ShrS,
	InstrI64x2ShrU,
}

var simdUnary = [...]InstrKind{
	InstrI8x16Swizzle,
	InstrV128Not,
	InstrF32x4DemoteF64x2Zero,
	InstrF64x2PromoteLowF32x4,
	InstrI8x16Abs,
	InstrI8x16Neg,
	InstrI8x16Popcnt,
	InstrI16x8ExtaddPairwiseI8x16S,
	InstrI16x8ExtaddPairwiseI8x16U,
	InstrI32x4ExtaddPairwiseI16x8S,
	InstrI32x4ExtaddPairwiseI16x8U,
	InstrF32x4Ceil,
	InstrF32x4Floor,
	InstrF32x4Trunc,
	InstrF32x4Nearest,
	InstrF64x2Ceil,
	InstrF64x2Floor,
	InstrF64x2Trunc,
	InstrF64x2Nearest,
	InstrI16x8Abs,
	InstrI16x8Neg,
	InstrI32x4Abs,
	InstrI32x4Neg,
	InstrI64x2Abs,
	InstrI64x2Neg,
	InstrI64x2ExtendLowI32x4S,
	InstrI64x2ExtendHighI32x4S,
	InstrI64x2ExtendLowI32x4U,
	InstrI64x2ExtendHighI32x4U,
	InstrF32x4Abs,
	InstrF32x4Neg,
	InstrF32x4Sqrt,
	InstrF64x2Abs,
	InstrF64x2Neg,
	InstrF64x2Sqrt,
	InstrI32x4TruncSatF32x4S,
	InstrI32x4TruncSatF32x4U,
	InstrF32x4ConvertI32x4S,
	InstrF32x4ConvertI32x4U,
	InstrI32x4TruncSatF64x2SZero,
	InstrI32x4TruncSatF64x2UZero,
	InstrF64x2ConvertLowI32x4S,
	InstrF64x2ConvertLowI32x4U,
	InstrI32x4RelaxedTruncF32x4S,
	InstrI32x4RelaxedTruncF32x4U,
	InstrI32x4RelaxedTruncZeroF64x2S,
	InstrI32x4RelaxedTruncZeroF64x2U,
	InstrI16x8ExtendLowI8x16S,
	InstrI16x8ExtendHighI8x16S,
	InstrI16x8ExtendLowI8x16U,
	InstrI16x8ExtendHighI8x16U,
	InstrI32x4ExtendLowI16x8S,
	InstrI32x4ExtendHighI16x8S,
	InstrI32x4ExtendLowI16x8U,
	InstrI32x4ExtendHighI16x8U,
}

var simdBinary = [...]InstrKind{
	InstrI8x16Shuffle,
	InstrI8x16RelaxedSwizzle,
	InstrV128And,
	InstrV128Andnot,
	InstrV128Or,
	InstrV128Xor,
	InstrI8x16Eq,
	InstrI8x16Ne,
	InstrI8x16LtS,
	InstrI8x16LtU,
	InstrI8x16GtS,
	InstrI8x16GtU,
	InstrI8x16LeS,
	InstrI8x16LeU,
	InstrI8x16GeS,
	InstrI8x16GeU,
	InstrI16x8Eq,
	InstrI16x8Ne,
	InstrI16x8LtS,
	InstrI16x8LtU,
	InstrI16x8GtS,
	InstrI16x8GtU,
	InstrI16x8LeS,
	InstrI16x8LeU,
	InstrI16x8GeS,
	InstrI16x8GeU,
	InstrI32x4Eq,
	InstrI32x4Ne,
	InstrI32x4LtS,
	InstrI32x4LtU,
	InstrI32x4GtS,
	InstrI32x4GtU,
	InstrI32x4LeS,
	InstrI32x4LeU,
	InstrI32x4GeS,
	InstrI32x4GeU,
	InstrF32x4Eq,
	InstrF32x4Ne,
	InstrF32x4Lt,
	InstrF32x4Gt,
	InstrF32x4Le,
	InstrF32x4Ge,
	InstrF64x2Eq,
	InstrF64x2Ne,
	InstrF64x2Lt,
	InstrF64x2Gt,
	InstrF64x2Le,
	InstrF64x2Ge,
	InstrI8x16NarrowI16x8S,
	InstrI8x16NarrowI16x8U,
	InstrI8x16Shl,
	InstrI8x16ShrS,
	InstrI8x16ShrU,
	InstrI8x16Add,
	InstrI8x16AddSatS,
	InstrI8x16AddSatU,
	InstrI8x16Sub,
	InstrI8x16SubSatS,
	InstrI8x16SubSatU,
	InstrI8x16MinS,
	InstrI8x16MinU,
	InstrI8x16MaxS,
	InstrI8x16MaxU,
	InstrI8x16AvgrU,
	InstrI16x8Q15mulrSatS,
	InstrI16x8NarrowI32x4S,
	InstrI16x8NarrowI32x4U,
	InstrI16x8Shl,
	InstrI16x8ShrS,
	InstrI16x8ShrU,
	InstrI16x8Add,
	InstrI16x8AddSatS,
	InstrI16x8AddSatU,
	InstrI16x8Sub,
	InstrI16x8SubSatS,
	InstrI16x8SubSatU,
	InstrI16x8Mul,
	InstrI16x8MinS,
	InstrI16x8MinU,
	InstrI16x8MaxS,
	InstrI16x8MaxU,
	InstrI16x8AvgrU,
	InstrI16x8ExtmulLowI8x16S,
	InstrI16x8ExtmulHighI8x16S,
	InstrI16x8ExtmulLowI8x16U,
	InstrI16x8ExtmulHighI8x16U,
	InstrI32x4Add,
	InstrI32x4Sub,
	InstrI32x4Mul,
	InstrI32x4MinS,
	InstrI32x4MinU,
	InstrI32x4MaxS,
	InstrI32x4MaxU,
	InstrI32x4DotI16x8S,
	InstrI32x4ExtmulLowI16x8S,
	InstrI32x4ExtmulHighI16x8S,
	InstrI32x4ExtmulLowI16x8U,
	InstrI32x4ExtmulHighI16x8U,
	InstrI64x2Add,
	InstrI64x2Sub,
	InstrI64x2Mul,
	InstrI64x2ExtmulLowI32x4S,
	InstrI64x2ExtmulHighI32x4S,
	InstrI64x2ExtmulLowI32x4U,
	InstrI64x2ExtmulHighI32x4U,
	InstrI64x2Eq,
	InstrI64x2Ne,
	InstrI64x2LtS,
	InstrI64x2GtS,
	InstrI64x2LeS,
	InstrI64x2GeS,
	InstrF32x4Add,
	InstrF32x4Sub,
	InstrF32x4Mul,
	InstrF32x4Div,
	InstrF32x4Min,
	InstrF32x4Max,
	InstrF32x4Pmin,
	InstrF32x4Pmax,
	InstrF64x2Add,
	InstrF64x2Sub,
	InstrF64x2Mul,
	InstrF64x2Div,
	InstrF64x2Min,
	InstrF64x2Max,
	InstrF64x2Pmin,
	InstrF64x2Pmax,
	InstrF32x4RelaxedMin,
	InstrF32x4RelaxedMax,
	InstrF64x2RelaxedMin,
	InstrF64x2RelaxedMax,
	InstrI16x8RelaxedQ15mulrS,
	InstrI16x8RelaxedDotI8x16I7x16S,
}

var simdTernary = [...]InstrKind{
	InstrF32x4RelaxedMadd,
	InstrF32x4RelaxedNmadd,
	InstrF64x2RelaxedMadd,
	InstrF64x2RelaxedNmadd,
	InstrI32x4RelaxedDotI8x16I7x16AddS,
}

func init() {
	for _, effect := range simdLoads {
		simdEffects[effect.kind] = simdEffect{cat: simdEffLoad, align: effect.align}
	}
	simdEffects[InstrV128Store] = simdEffect{cat: simdEffStore}
	for _, effect := range simdMemLane {
		cat := simdEffMemLoadLane
		if effect.kind >= InstrV128Store8Lane && effect.kind <= InstrV128Store64Lane {
			cat = simdEffMemStoreLane
		}
		simdEffects[effect.kind] = simdEffect{cat: cat, align: effect.align}
	}
	for _, effect := range simdSplat {
		simdEffects[effect.kind] = simdEffect{cat: simdEffSplat, scalar: effect.scalar}
	}
	for _, effect := range simdExtract {
		simdEffects[effect.kind] = simdEffect{cat: simdEffExtract, scalar: effect.scalar}
	}
	for _, effect := range simdReplace {
		simdEffects[effect.kind] = simdEffect{cat: simdEffReplace, scalar: effect.scalar}
	}
	for _, kind := range simdShift {
		simdEffects[kind] = simdEffect{cat: simdEffShift}
	}
	for _, kind := range simdUnary {
		simdEffects[kind] = simdEffect{cat: simdEffUnary}
	}
	simdEffects[InstrI8x16Swizzle] = simdEffect{cat: simdEffBinary}
	for _, kind := range simdBinary {
		if simdEffects[kind].cat == simdEffShift {
			continue
		}
		simdEffects[kind] = simdEffect{cat: simdEffBinary}
	}
	for _, kind := range simdTernary {
		simdEffects[kind] = simdEffect{cat: simdEffTernary}
	}
	for _, k := range [...]InstrKind{
		InstrV128AnyTrue,
		InstrI8x16AllTrue, InstrI16x8AllTrue, InstrI32x4AllTrue, InstrI64x2AllTrue,
		InstrI8x16Bitmask, InstrI16x8Bitmask, InstrI32x4Bitmask, InstrI64x2Bitmask,
	} {
		simdEffects[k] = simdEffect{cat: simdPopV128PushI32}
	}
	for _, k := range [...]InstrKind{
		InstrV128Bitselect,
		InstrI8x16RelaxedLaneselect, InstrI16x8RelaxedLaneselect, InstrI32x4RelaxedLaneselect, InstrI64x2RelaxedLaneselect,
	} {
		simdEffects[k] = simdEffect{cat: simdBitselect}
	}
	simdEffects[InstrV128Const] = simdEffect{cat: simdConst}
	for _, lane := range simdLaneLimits {
		effect := simdEffects[lane.kind]
		effect.laneLimit = lane.limit
		simdEffects[lane.kind] = effect
	}
}

// IsSIMDValidationInstructionKind reports whether wasm validation admits kind
// as a SIMD instruction. It is allocation-free and shares the validator's
// compact effect table.
func IsSIMDValidationInstructionKind(kind InstrKind) bool {
	return kind < numInstrKinds && simdEffects[kind].cat != simdNone
}

// SIMDValidationInstructionKinds returns an immutable snapshot of the SIMD
// instruction kinds admitted by wasm validation. It is intended for downstream
// support/admission parity checks; callers receive a copy so the validator's
// internal tables cannot be mutated.
func SIMDValidationInstructionKinds() map[InstrKind]struct{} {
	out := make(map[InstrKind]struct{}, 268)
	for kind, effect := range simdEffects {
		if effect.cat != simdNone {
			out[InstrKind(kind)] = struct{}{}
		}
	}
	return out
}
