package wasm

func (v *funcValidator) proposalStep(in *Instruction) (bool, error) {
	switch in.Kind {
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

func (v *funcValidator) stepCallRef(in Instruction) error {
	ft := v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
	if ft == nil {
		return v.verr(ErrUnknownType, "call_ref")
	}
	callee, err := v.pop()
	if err != nil {
		return err
	}
	wantTyped := RefVal(Ref(false, IndexedHeap(TypeIdx{Index: in.Index}), false))
	if !callee.unknown && !v.subtype(callee.t, wantTyped) && !v.subtype(callee.t, FuncRef) {
		return v.verr(ErrTypeMismatch, "call_ref callee")
	}
	if err := v.popAll(ft.Params); err != nil {
		return err
	}
	if in.Kind == InstrReturnCallRef {
		if !v.sameValTypes(ft.Results, v.ctrls[0].out) {
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
		lt, err := v.label(uint32(c.Label))
		if err != nil {
			return err
		}
		var payload []ValType
		if c.Kind == CatchTag || c.Kind == CatchRef {
			if int(c.Tag) >= v.m.TagCount() {
				return v.verr(ErrUnknownTag, "catch")
			}
			ft, ok := v.tagFuncType(uint32(c.Tag))
			if !ok {
				return v.verr(ErrUnknownTag, "catch")
			}
			payload = append(payload, ft.Params...)
		}
		if c.Kind == CatchRef || c.Kind == CatchAllRef {
			payload = append(payload, RefVal(AbsRef(HeapExn)))
		}
		if c.Kind == CatchAll && len(lt) != 0 {
			return v.verr(ErrTypeMismatch, "catch_all label must expect no values")
		}
		if len(payload) != len(lt) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
		for i := range payload {
			if !v.subtype(payload[i], lt[i]) {
				return v.verr(ErrTypeMismatch, "catch payload label mismatch")
			}
		}
	}
	if err := v.pushCtrl(ctrlBlock, ins, outs); err != nil {
		return err
	}
	for _, child := range in.Body().Instrs {
		if err := v.step(&child); err != nil {
			return err
		}
	}
	_, err = v.popCtrl()
	return err
}

func (v *moduleValidator) tagFuncType(idx uint32) (*CompType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternTag {
			if n == idx {
				ft := v.funcTypeFromTypeIdx(im.Type.Tag.Type)
				return ft, ft != nil
			}
			n++
		}
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
		addr, err := v.checkSharedMemArg(in.MemArg(), 2)
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
		addr, err := v.checkSharedMemArg(in.MemArg(), natural)
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
	if eff, ok := atomicLoadEffects[in.Kind]; ok {
		addr, err := v.checkSharedMemArg(in.MemArg(), eff.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(eff.t)
		return nil
	}
	if eff, ok := atomicStoreEffects[in.Kind]; ok {
		addr, err := v.checkSharedMemArg(in.MemArg(), eff.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(eff.t); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	if in.Kind == InstrAtomicRmw {
		eff := atomicRmwEffect(in.AtomicOp)
		addr, err := v.checkSharedMemArg(in.MemArg(), eff.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(eff.t); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(eff.t)
		return nil
	}
	if in.Kind == InstrAtomicCmpxchg {
		eff := atomicCmpxchgEffect(in.AtomicOp)
		addr, err := v.checkSharedMemArg(in.MemArg(), eff.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(eff.t); err != nil {
			return err
		}
		if err := v.popExpect(eff.t); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(eff.t)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

var atomicLoadEffects = map[InstrKind]memeff{InstrI32AtomicLoad: {I32, 2}, InstrI64AtomicLoad: {I64, 3}, InstrI32AtomicLoad8U: {I32, 0}, InstrI32AtomicLoad16U: {I32, 1}, InstrI64AtomicLoad8U: {I64, 0}, InstrI64AtomicLoad16U: {I64, 1}, InstrI64AtomicLoad32U: {I64, 2}}
var atomicStoreEffects = map[InstrKind]memeff{InstrI32AtomicStore: {I32, 2}, InstrI64AtomicStore: {I64, 3}, InstrI32AtomicStore8: {I32, 0}, InstrI32AtomicStore16: {I32, 1}, InstrI64AtomicStore8: {I64, 0}, InstrI64AtomicStore16: {I64, 1}, InstrI64AtomicStore32: {I64, 2}}

func atomicRmwEffect(op uint32) memeff {
	if op == 0 {
		op = 30
	}
	pos := (op - 30) % 7
	if pos == 0 {
		return memeff{I32, 2}
	}
	if pos == 1 {
		return memeff{I64, 3}
	}
	if pos == 2 || pos == 3 {
		return memeff{I32, pos - 2}
	}
	return memeff{I64, pos - 4}
}
func atomicCmpxchgEffect(op uint32) memeff {
	if op == 0 {
		op = 72
	}
	switch op {
	case 72:
		return memeff{I32, 2}
	case 73:
		return memeff{I64, 3}
	case 74, 75:
		return memeff{I32, op - 74}
	default:
		return memeff{I64, op - 76}
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
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, in.Kind.String()+" expects a reference operand")
		}
		target, ok := v.descriptorTargetRefType(in.Cast.TargetNullable, in.HeapType(), false)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor target reftype")
		}
		if !x.unknown && !v.descriptorCompatible(x.t.Ref, target.Ref) {
			return v.verr(ErrTypeMismatch, "target does not match operand type")
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
			if !desc.unknown && desc.t.Kind != ValRef {
				return v.verr(ErrTypeMismatch, "descriptor operand")
			}
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, "ref.cast expects a reference operand")
		}
		if !x.unknown && in.Kind == InstrRefCastDescEq && !v.descriptorCompatible(x.t.Ref, target.Ref) {
			return v.verr(ErrTypeMismatch, "target does not match operand type")
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
		if st.Metadata.Descriptor == nil {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown && x.t.Kind != ValRef {
			return v.verr(ErrTypeMismatch, "expected a reference operand")
		}
		if !x.unknown && !v.refSubtype(x.t.Ref, Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)) {
			return v.verr(ErrTypeMismatch, "ref.get_desc target")
		}
		v.push(RefVal(Ref(false, IndexedHeap(*st.Metadata.Descriptor), true)))
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
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(fields[in.Index2].Storage, in.Kind != InstrStructGet))
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
		if f.Mut != Var {
			return v.verr(ErrTypeMismatch, "immutable field")
		}
		if err := v.popExpect(storageValType(f.Storage, false)); err != nil {
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
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(f.Storage, in.Kind != InstrArrayGet))
		return nil
	case InstrArraySet:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.set")
		}
		if f.Mut != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if err := v.popExpect(storageValType(f.Storage, false)); err != nil {
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
		if !x.unknown && (x.t.Kind != ValRef || !v.heapSubtype(x.t.Ref.Heap, AbsHeap(HeapArray))) {
			return v.verr(ErrTypeMismatch, "array.len")
		}
		v.push(I32)
		return nil
	case InstrArrayFill:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.fill")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(storageValType(f.Storage, false)); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayCopy:
		_, _, okDst := v.arrayField(TypeIdx{Index: in.Index})
		_, _, okSrc := v.arrayField(TypeIdx{Index: in.Index2})
		if !okDst || !okSrc {
			return v.verr(ErrUnknownType, "array.copy")
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
		if _, _, ok := v.arrayField(TypeIdx{Index: in.Index}); !ok {
			return v.verr(ErrUnknownType, "array.init_data")
		}
		if int(in.Index2) >= len(v.m.Data) {
			return v.verr(ErrInvalidDataCount, "array.init_data")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayInitElem:
		if _, _, ok := v.arrayField(TypeIdx{Index: in.Index}); !ok {
			return v.verr(ErrUnknownType, "array.init_elem")
		}
		if int(in.Index2) >= len(v.m.Elements) {
			return v.verr(ErrUnknownTable, "array.init_elem")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
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
	if (in.Kind == InstrStructNew || in.Kind == InstrStructNewDefault) && st.Metadata.Descriptor != nil {
		return v.verr(ErrTypeMismatch, "use struct.new_desc for descriptor-bearing struct")
	}
	if in.Kind == InstrStructNewDefault || in.Kind == InstrStructNewDefaultDesc {
		for _, f := range fields {
			if !f.Storage.Packed && !valTypeDefaultable(storageValType(f.Storage, false)) {
				return v.verr(ErrTypeMismatch, "field not defaultable")
			}
		}
	} else {
		for i := len(fields) - 1; i >= 0; i-- {
			if err := v.popExpect(storageValType(fields[i].Storage, false)); err != nil {
				return err
			}
		}
	}
	if in.Kind == InstrStructNewDesc || in.Kind == InstrStructNewDefaultDesc {
		if st.Metadata.Descriptor == nil {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		want := RefVal(Ref(false, IndexedHeap(*st.Metadata.Descriptor), true))
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
	elem := storageValType(f.Storage, false)
	switch in.Kind {
	case InstrArrayNew:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(elem); err != nil {
			return err
		}
	case InstrArrayNewDefault:
		if !f.Storage.Packed && !valTypeDefaultable(elem) {
			return v.verr(ErrTypeMismatch, "element not defaultable")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewFixed:
		for i := uint32(0); i < in.Index2; i++ {
			if err := v.popExpect(elem); err != nil {
				return err
			}
		}
	case InstrArrayNewData:
		if int(in.Index2) >= len(v.m.Data) {
			return v.verr(ErrInvalidDataCount, "array.new_data")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewElem:
		if int(in.Index2) >= len(v.m.Elements) {
			return v.verr(ErrUnknownTable, "array.new_elem")
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
	if labelRef.Kind != ValRef {
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
	if !x.unknown && (x.t.Kind != ValRef || !v.refSubtype(x.t.Ref, rt1)) {
		return v.verr(ErrTypeMismatch, "br_on_cast operand")
	}
	branchTypes := append([]ValType(nil), lt...)
	if in.Kind == InstrBrOnCastFail {
		if !v.subtype(RefVal(rt1), labelRef) {
			return v.verr(ErrTypeMismatch, "rt1 does not match label rt")
		}
		branchTypes[len(branchTypes)-1] = RefVal(rt1)
		if err := v.popAll(branchTypes[:len(branchTypes)-1]); err != nil {
			return err
		}
		v.push(RefVal(rt2))
		return nil
	}
	if !v.subtype(RefVal(rt2), labelRef) {
		return v.verr(ErrTypeMismatch, "rt2 does not match label rt")
	}
	branchTypes[len(branchTypes)-1] = RefVal(rt2)
	if err := v.popAll(branchTypes[:len(branchTypes)-1]); err != nil {
		return err
	}
	v.push(RefVal(rt1))
	return nil
}

var simdAll = func() map[InstrKind]struct{} {
	m := map[InstrKind]struct{}{InstrV128Const: {}, InstrV128Store: {}}
	for k := range simdLoads {
		m[k] = struct{}{}
	}
	for k := range simdMemLane {
		m[k] = struct{}{}
	}
	for k := range simdSplat {
		m[k] = struct{}{}
	}
	for k := range simdExtract {
		m[k] = struct{}{}
	}
	for k := range simdReplace {
		m[k] = struct{}{}
	}
	for k := range simdUnary {
		m[k] = struct{}{}
	}
	for k := range simdBinary {
		m[k] = struct{}{}
	}
	for k := range simdTernary {
		m[k] = struct{}{}
	}
	for k := range simdShift {
		m[k] = struct{}{}
	}
	m[InstrV128AnyTrue] = struct{}{}
	m[InstrI8x16AllTrue] = struct{}{}
	m[InstrI16x8AllTrue] = struct{}{}
	m[InstrI32x4AllTrue] = struct{}{}
	m[InstrI64x2AllTrue] = struct{}{}
	m[InstrI8x16Bitmask] = struct{}{}
	m[InstrI16x8Bitmask] = struct{}{}
	m[InstrI32x4Bitmask] = struct{}{}
	m[InstrI64x2Bitmask] = struct{}{}
	m[InstrV128Bitselect] = struct{}{}
	m[InstrI8x16RelaxedLaneselect] = struct{}{}
	m[InstrI16x8RelaxedLaneselect] = struct{}{}
	m[InstrI32x4RelaxedLaneselect] = struct{}{}
	m[InstrI64x2RelaxedLaneselect] = struct{}{}
	return m
}()

func (v *funcValidator) stepSIMD(in Instruction) error {
	e := simdEffects[in.Kind]
	if e.laneLimit != 0 && in.Lane >= e.laneLimit {
		return v.verr(ErrTypeMismatch, "simd lane out of range")
	}
	switch e.cat {
	case simdEffLoad:
		addr, err := v.checkMemArg(in.MemArg(), e.align)
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
		addr, err := v.checkMemArg(in.MemArg(), e.align)
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
		addr, err := v.checkMemArg(in.MemArg(), e.align)
		if err != nil {
			return err
		}
		if err := v.popExpect(V128); err != nil {
			return err
		}
		return v.popExpect(addr)
	case simdEffSplat:
		if err := v.popExpect(e.scalar); err != nil {
			return err
		}
		v.push(V128)
		return nil
	case simdEffExtract:
		if err := v.popExpect(V128); err != nil {
			return err
		}
		v.push(e.scalar)
		return nil
	case simdEffReplace:
		if err := v.popExpect(e.scalar); err != nil {
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
	scalar    ValType
	align     uint32
	laneLimit LaneIdx
}

var simdEffects [numInstrKinds]simdEffect

var simdLoads = map[InstrKind]memeff{InstrV128Load: {V128, 4}, InstrV128Load8x8S: {V128, 3}, InstrV128Load8x8U: {V128, 3}, InstrV128Load16x4S: {V128, 3}, InstrV128Load16x4U: {V128, 3}, InstrV128Load32x2S: {V128, 3}, InstrV128Load32x2U: {V128, 3}, InstrV128Load8Splat: {V128, 0}, InstrV128Load16Splat: {V128, 1}, InstrV128Load32Splat: {V128, 2}, InstrV128Load64Splat: {V128, 3}, InstrV128Load32Zero: {V128, 2}, InstrV128Load64Zero: {V128, 3}}
var simdMemLane = map[InstrKind]memeff{InstrV128Load8Lane: {V128, 0}, InstrV128Load16Lane: {V128, 1}, InstrV128Load32Lane: {V128, 2}, InstrV128Load64Lane: {V128, 3}, InstrV128Store8Lane: {V128, 0}, InstrV128Store16Lane: {V128, 1}, InstrV128Store32Lane: {V128, 2}, InstrV128Store64Lane: {V128, 3}}

// Lane immediates are decoded as raw bytes; validation enforces each shape's
// lane count so unsupported SIMD still obeys proposal validation boundaries.
var simdLaneLimits = map[InstrKind]LaneIdx{
	InstrI8x16ExtractLaneS: 16, InstrI8x16ExtractLaneU: 16, InstrI8x16ReplaceLane: 16,
	InstrI16x8ExtractLaneS: 8, InstrI16x8ExtractLaneU: 8, InstrI16x8ReplaceLane: 8,
	InstrI32x4ExtractLane: 4, InstrI32x4ReplaceLane: 4, InstrF32x4ExtractLane: 4, InstrF32x4ReplaceLane: 4,
	InstrI64x2ExtractLane: 2, InstrI64x2ReplaceLane: 2, InstrF64x2ExtractLane: 2, InstrF64x2ReplaceLane: 2,
	InstrV128Load8Lane: 16, InstrV128Store8Lane: 16,
	InstrV128Load16Lane: 8, InstrV128Store16Lane: 8,
	InstrV128Load32Lane: 4, InstrV128Store32Lane: 4,
	InstrV128Load64Lane: 2, InstrV128Store64Lane: 2,
}

var simdSplat = map[InstrKind]ValType{InstrI8x16Splat: I32, InstrI16x8Splat: I32, InstrI32x4Splat: I32, InstrI64x2Splat: I64, InstrF32x4Splat: F32, InstrF64x2Splat: F64}
var simdExtract = map[InstrKind]ValType{InstrI8x16ExtractLaneS: I32, InstrI8x16ExtractLaneU: I32, InstrI16x8ExtractLaneS: I32, InstrI16x8ExtractLaneU: I32, InstrI32x4ExtractLane: I32, InstrI64x2ExtractLane: I64, InstrF32x4ExtractLane: F32, InstrF64x2ExtractLane: F64}
var simdReplace = map[InstrKind]ValType{InstrI8x16ReplaceLane: I32, InstrI16x8ReplaceLane: I32, InstrI32x4ReplaceLane: I32, InstrI64x2ReplaceLane: I64, InstrF32x4ReplaceLane: F32, InstrF64x2ReplaceLane: F64}
var simdShift = map[InstrKind]struct{}{InstrI8x16Shl: {}, InstrI8x16ShrS: {}, InstrI8x16ShrU: {}, InstrI16x8Shl: {}, InstrI16x8ShrS: {}, InstrI16x8ShrU: {}, InstrI32x4Shl: {}, InstrI32x4ShrS: {}, InstrI32x4ShrU: {}, InstrI64x2Shl: {}, InstrI64x2ShrS: {}, InstrI64x2ShrU: {}}
var simdUnary = map[InstrKind]struct{}{InstrI8x16Swizzle: {}, InstrV128Not: {}, InstrF32x4DemoteF64x2Zero: {}, InstrF64x2PromoteLowF32x4: {}, InstrI8x16Abs: {}, InstrI8x16Neg: {}, InstrI8x16Popcnt: {}, InstrI16x8ExtaddPairwiseI8x16S: {}, InstrI16x8ExtaddPairwiseI8x16U: {}, InstrI32x4ExtaddPairwiseI16x8S: {}, InstrI32x4ExtaddPairwiseI16x8U: {}, InstrF32x4Ceil: {}, InstrF32x4Floor: {}, InstrF32x4Trunc: {}, InstrF32x4Nearest: {}, InstrF64x2Ceil: {}, InstrF64x2Floor: {}, InstrF64x2Trunc: {}, InstrF64x2Nearest: {}, InstrI16x8Abs: {}, InstrI16x8Neg: {}, InstrI32x4Abs: {}, InstrI32x4Neg: {}, InstrI64x2Abs: {}, InstrI64x2Neg: {}, InstrI64x2ExtendLowI32x4S: {}, InstrI64x2ExtendHighI32x4S: {}, InstrI64x2ExtendLowI32x4U: {}, InstrI64x2ExtendHighI32x4U: {}, InstrF32x4Abs: {}, InstrF32x4Neg: {}, InstrF32x4Sqrt: {}, InstrF64x2Abs: {}, InstrF64x2Neg: {}, InstrF64x2Sqrt: {}, InstrI32x4TruncSatF32x4S: {}, InstrI32x4TruncSatF32x4U: {}, InstrF32x4ConvertI32x4S: {}, InstrF32x4ConvertI32x4U: {}, InstrI32x4TruncSatF64x2SZero: {}, InstrI32x4TruncSatF64x2UZero: {}, InstrF64x2ConvertLowI32x4S: {}, InstrF64x2ConvertLowI32x4U: {}, InstrI32x4RelaxedTruncF32x4S: {}, InstrI32x4RelaxedTruncF32x4U: {}, InstrI32x4RelaxedTruncZeroF64x2S: {}, InstrI32x4RelaxedTruncZeroF64x2U: {}, InstrI16x8ExtendLowI8x16S: {}, InstrI16x8ExtendHighI8x16S: {}, InstrI16x8ExtendLowI8x16U: {}, InstrI16x8ExtendHighI8x16U: {}, InstrI32x4ExtendLowI16x8S: {}, InstrI32x4ExtendHighI16x8S: {}, InstrI32x4ExtendLowI16x8U: {}, InstrI32x4ExtendHighI16x8U: {}}
var simdBinary = map[InstrKind]struct{}{InstrI8x16Shuffle: {}, InstrI8x16RelaxedSwizzle: {}, InstrV128And: {}, InstrV128Andnot: {}, InstrV128Or: {}, InstrV128Xor: {}, InstrI8x16Eq: {}, InstrI8x16Ne: {}, InstrI8x16LtS: {}, InstrI8x16LtU: {}, InstrI8x16GtS: {}, InstrI8x16GtU: {}, InstrI8x16LeS: {}, InstrI8x16LeU: {}, InstrI8x16GeS: {}, InstrI8x16GeU: {}, InstrI16x8Eq: {}, InstrI16x8Ne: {}, InstrI16x8LtS: {}, InstrI16x8LtU: {}, InstrI16x8GtS: {}, InstrI16x8GtU: {}, InstrI16x8LeS: {}, InstrI16x8LeU: {}, InstrI16x8GeS: {}, InstrI16x8GeU: {}, InstrI32x4Eq: {}, InstrI32x4Ne: {}, InstrI32x4LtS: {}, InstrI32x4LtU: {}, InstrI32x4GtS: {}, InstrI32x4GtU: {}, InstrI32x4LeS: {}, InstrI32x4LeU: {}, InstrI32x4GeS: {}, InstrI32x4GeU: {}, InstrF32x4Eq: {}, InstrF32x4Ne: {}, InstrF32x4Lt: {}, InstrF32x4Gt: {}, InstrF32x4Le: {}, InstrF32x4Ge: {}, InstrF64x2Eq: {}, InstrF64x2Ne: {}, InstrF64x2Lt: {}, InstrF64x2Gt: {}, InstrF64x2Le: {}, InstrF64x2Ge: {}, InstrI8x16NarrowI16x8S: {}, InstrI8x16NarrowI16x8U: {}, InstrI8x16Shl: {}, InstrI8x16ShrS: {}, InstrI8x16ShrU: {}, InstrI8x16Add: {}, InstrI8x16AddSatS: {}, InstrI8x16AddSatU: {}, InstrI8x16Sub: {}, InstrI8x16SubSatS: {}, InstrI8x16SubSatU: {}, InstrI8x16MinS: {}, InstrI8x16MinU: {}, InstrI8x16MaxS: {}, InstrI8x16MaxU: {}, InstrI8x16AvgrU: {}, InstrI16x8Q15mulrSatS: {}, InstrI16x8NarrowI32x4S: {}, InstrI16x8NarrowI32x4U: {}, InstrI16x8Shl: {}, InstrI16x8ShrS: {}, InstrI16x8ShrU: {}, InstrI16x8Add: {}, InstrI16x8AddSatS: {}, InstrI16x8AddSatU: {}, InstrI16x8Sub: {}, InstrI16x8SubSatS: {}, InstrI16x8SubSatU: {}, InstrI16x8Mul: {}, InstrI16x8MinS: {}, InstrI16x8MinU: {}, InstrI16x8MaxS: {}, InstrI16x8MaxU: {}, InstrI16x8AvgrU: {}, InstrI16x8ExtmulLowI8x16S: {}, InstrI16x8ExtmulHighI8x16S: {}, InstrI16x8ExtmulLowI8x16U: {}, InstrI16x8ExtmulHighI8x16U: {}, InstrI32x4Add: {}, InstrI32x4Sub: {}, InstrI32x4Mul: {}, InstrI32x4MinS: {}, InstrI32x4MinU: {}, InstrI32x4MaxS: {}, InstrI32x4MaxU: {}, InstrI32x4DotI16x8S: {}, InstrI32x4ExtmulLowI16x8S: {}, InstrI32x4ExtmulHighI16x8S: {}, InstrI32x4ExtmulLowI16x8U: {}, InstrI32x4ExtmulHighI16x8U: {}, InstrI64x2Add: {}, InstrI64x2Sub: {}, InstrI64x2Mul: {}, InstrI64x2ExtmulLowI32x4S: {}, InstrI64x2ExtmulHighI32x4S: {}, InstrI64x2ExtmulLowI32x4U: {}, InstrI64x2ExtmulHighI32x4U: {}, InstrI64x2Eq: {}, InstrI64x2Ne: {}, InstrI64x2LtS: {}, InstrI64x2GtS: {}, InstrI64x2LeS: {}, InstrI64x2GeS: {}, InstrF32x4Add: {}, InstrF32x4Sub: {}, InstrF32x4Mul: {}, InstrF32x4Div: {}, InstrF32x4Min: {}, InstrF32x4Max: {}, InstrF32x4Pmin: {}, InstrF32x4Pmax: {}, InstrF64x2Add: {}, InstrF64x2Sub: {}, InstrF64x2Mul: {}, InstrF64x2Div: {}, InstrF64x2Min: {}, InstrF64x2Max: {}, InstrF64x2Pmin: {}, InstrF64x2Pmax: {}, InstrF32x4RelaxedMin: {}, InstrF32x4RelaxedMax: {}, InstrF64x2RelaxedMin: {}, InstrF64x2RelaxedMax: {}, InstrI16x8RelaxedQ15mulrS: {}, InstrI16x8RelaxedDotI8x16I7x16S: {}}
var simdTernary = map[InstrKind]struct{}{InstrF32x4RelaxedMadd: {}, InstrF32x4RelaxedNmadd: {}, InstrF64x2RelaxedMadd: {}, InstrF64x2RelaxedNmadd: {}, InstrI32x4RelaxedDotI8x16I7x16AddS: {}}

func init() {
	for k, eff := range simdLoads {
		simdEffects[k] = simdEffect{cat: simdEffLoad, align: eff.align}
	}
	simdEffects[InstrV128Store] = simdEffect{cat: simdEffStore}
	for k, eff := range simdMemLane {
		cat := simdEffMemLoadLane
		if k >= InstrV128Store8Lane && k <= InstrV128Store64Lane {
			cat = simdEffMemStoreLane
		}
		simdEffects[k] = simdEffect{cat: cat, align: eff.align}
	}
	for k, scalar := range simdSplat {
		simdEffects[k] = simdEffect{cat: simdEffSplat, scalar: scalar}
	}
	for k, scalar := range simdExtract {
		simdEffects[k] = simdEffect{cat: simdEffExtract, scalar: scalar}
	}
	for k, scalar := range simdReplace {
		simdEffects[k] = simdEffect{cat: simdEffReplace, scalar: scalar}
	}
	for k := range simdShift {
		simdEffects[k] = simdEffect{cat: simdEffShift}
	}
	for k := range simdUnary {
		simdEffects[k] = simdEffect{cat: simdEffUnary}
	}
	simdEffects[InstrI8x16Swizzle] = simdEffect{cat: simdEffBinary}
	for k := range simdBinary {
		if _, isShift := simdShift[k]; isShift {
			continue
		}
		simdEffects[k] = simdEffect{cat: simdEffBinary}
	}
	for k := range simdTernary {
		simdEffects[k] = simdEffect{cat: simdEffTernary}
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
	for k, limit := range simdLaneLimits {
		eff := simdEffects[k]
		eff.laneLimit = limit
		simdEffects[k] = eff
	}
}

// SIMDValidationInstructionKinds returns an immutable snapshot of the SIMD
// instruction kinds admitted by wasm validation. It is intended for downstream
// support/admission parity checks; callers receive a copy so the validator's
// internal tables cannot be mutated.
func SIMDValidationInstructionKinds() map[InstrKind]struct{} {
	out := make(map[InstrKind]struct{}, len(simdAll))
	for k := range simdAll {
		out[k] = struct{}{}
	}
	return out
}
