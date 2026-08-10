//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type gcArrayLenFact struct {
	valid       bool
	local       int
	resultLocal int
	pending     *elem
	identity    uint32
	typeIndex   uint32
}

type gcStructFieldFact struct {
	valid       bool
	fromStore   bool
	immutable   bool
	hasConst    bool
	local       int
	resultLocal int
	pending     *elem
	identity    uint32
	typeIndex   uint32
	fieldIndex  uint32
	constType   machineType
	constBits   int64
	constFact   shared.GCRefFact
}

type gcResolvedObject struct {
	valid         bool
	local         int
	typeIndex     uint32
	requiredBytes uint32
	reg           Reg
}

// Semantic compact-reference facts and transient raw-address certificates are
// intentionally separate. GCRefFact survives collection when tied to the same
// compact identity; gcResolvedObject never does.

func gcKnownI32Const(e *elem) (uint32, bool) {
	if e == nil || e.kind != ekValue || e.st.kind != stConst || e.st.typ != mtI32 {
		return 0, false
	}
	return uint32(e.st.cval), true
}

func gcKnownArrayIndexInBounds(object, index *elem) (indexValue, length uint32, ok bool) {
	if !gcKnownArrayBoundsEnabled {
		return 0, 0, false
	}
	i, constant := gcKnownI32Const(index)
	fact := gcRefFact(object)
	length, known := fact.KnownArrayLength()
	return i, length, constant && known && fact.Nullability() == shared.GCKnownNonNull && i < length
}

func gcRefFact(e *elem) shared.GCRefFact {
	if !exactGCRefFactsEnabled || e == nil || e.kind != ekValue || !e.st.gcRoot {
		return shared.GCRefFact{}
	}
	if e.st.kind == stConst && e.st.cval == 0 {
		return shared.NewGCRefFact(shared.GCKnownNull, shared.GCHeapUnknown)
	}
	arrayLen := uint64(0)
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		if e.st.slot > 0 {
			arrayLen = uint64(e.st.slot)
		}
	default:
		if e.st.idx > 0 {
			arrayLen = uint64(e.st.idx)
		}
	}
	return shared.GCRefFactFromPacked(uint64(e.st.cval), arrayLen)
}

func putGCRefFact(st *storage, fact shared.GCRefFact) {
	if st == nil {
		return
	}
	if st.kind == stConst && st.cval == 0 && fact.Nullability() == shared.GCKnownNull {
		st.gcRoot = true
		return
	}
	// Clear the bounded array-length payload before replacing a fact. The
	// alternate integer remains available for local identity/provenance.
	switch st.kind {
	case stLocalRef, stLocalReg:
		st.slot = 0
	case stReg, stSlot:
		st.idx = 0
	}
	if fact.IsZero() {
		st.cval = 0
		return
	}
	bits, arrayLen := fact.Packed()
	st.gcRoot = true
	st.cval = int64(bits)
	if arrayLen == 0 {
		return
	}
	switch st.kind {
	case stLocalRef, stLocalReg:
		st.slot = int(arrayLen)
	default:
		st.idx = int(arrayLen)
	}
}

func exactGCType(e *elem) (uint32, bool) { return gcRefFact(e).ExactType() }

func markGCRefFact(e *elem, fact shared.GCRefFact) {
	if e == nil || e.kind != ekValue {
		return
	}
	e.st.gcRoot = true
	if !exactGCRefFactsEnabled {
		return
	}
	putGCRefFact(&e.st, fact)
}

func (f *fn) gcRefFactMatchesHeap(fact shared.GCRefFact, heap int64, nullable bool) (match, known bool) {
	if fact.Nullability() == shared.GCKnownNull {
		return nullable, true
	}
	if fact.Nullability() != shared.GCKnownNonNull {
		return false, false
	}
	if heap >= 0 {
		actual, exact := fact.ExactType()
		if !exact || f.m == nil {
			return false, false
		}
		source := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: actual}), false)
		target := wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false)
		return f.m.ReferenceTypeSubtype(source, target), true
	}
	switch wasm.AbsHeapType(byte(heap) & 0x7f) {
	case wasm.HeapAny:
		return fact.HeapClass() != shared.GCHeapFunc && fact.HeapClass() != shared.GCHeapExtern, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapEq:
		switch fact.HeapClass() {
		case shared.GCHeapI31, shared.GCHeapStruct, shared.GCHeapArray:
			return true, true
		case shared.GCHeapFunc, shared.GCHeapExtern:
			return false, true
		}
	case wasm.HeapI31:
		return fact.HeapClass() == shared.GCHeapI31, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapStruct:
		return fact.HeapClass() == shared.GCHeapStruct, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapArray:
		return fact.HeapClass() == shared.GCHeapArray, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapFunc:
		return fact.HeapClass() == shared.GCHeapFunc, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapExtern:
		return fact.HeapClass() == shared.GCHeapExtern, fact.HeapClass() != shared.GCHeapUnknown
	case wasm.HeapNone, wasm.HeapNoFunc, wasm.HeapNoExtern:
		return false, true
	}
	return false, false
}

func (f *fn) gcHeapClassForType(typeIndex uint32) shared.GCHeapClass {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok && layout.Type != nil {
		return shared.GCHeapStruct
	}
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok && layout.Type != nil {
		return shared.GCHeapArray
	}
	if typ, ok := f.stagedGCType(typeIndex); ok {
		switch typ.Comp.Kind {
		case wasm.CompStruct:
			return shared.GCHeapStruct
		case wasm.CompArray:
			return shared.GCHeapArray
		case wasm.CompFunc:
			return shared.GCHeapFunc
		}
	}
	return shared.GCHeapUnknown
}

func (f *fn) nextGCIdentity() uint32 {
	if f.nextGCRefIdentity >= shared.MaxGCRefFactIdentity {
		return 0
	}
	f.nextGCRefIdentity++
	return f.nextGCRefIdentity
}

func (f *fn) constructorGCRefFact(typeIndex uint32, arrayLength *uint32) shared.GCRefFact {
	fact := shared.ExactGCRefFact(typeIndex, f.nextGCIdentity(), f.gcHeapClassForType(typeIndex)).
		WithFreshness(shared.GCFreshUnpublished)
	if int(typeIndex) < len(f.gcTypeLayouts) {
		fact = fact.WithPointerFree(f.gcTypeLayouts[typeIndex].PointerFree)
	}
	if arrayLength != nil {
		fact = fact.WithKnownArrayLength(*arrayLength)
	}
	return fact
}

func (f *fn) markTopConstructorGCRefFact(typeIndex uint32, arrayLength *uint32) {
	if exactGCRefFactsEnabled {
		fact := f.constructorGCRefFact(typeIndex, arrayLength)
		markGCRefFact(f.s.back(), fact)
		f.stats.peep("gc-fact-exact")
	}
}

func markExactGCType(e *elem, typeIndex uint32) {
	fact := gcRefFact(e)
	fact = fact.WithExactType(typeIndex, fact.HeapClass()).WithNullability(shared.GCKnownNonNull)
	markGCRefFact(e, fact)
}

func (f *fn) markTopExactGCType(typeIndex uint32) {
	if exactGCRefFactsEnabled {
		fact := gcRefFact(f.s.back()).WithExactType(typeIndex, f.gcHeapClassForType(typeIndex)).WithNullability(shared.GCKnownNonNull)
		markGCRefFact(f.s.back(), fact)
	}
}

func (f *fn) seedFinalGCParameterTypes(params []wasm.ValType, recBase, recLength uint32) {
	if !exactGCRefFactsEnabled {
		return
	}
	for local, typ := range params {
		if local >= len(f.localGCRefFacts) || typ.Kind() != wasm.ValRef {
			continue
		}
		nullability := shared.GCNullUnknown
		if !typ.Ref().Nullable() {
			nullability = shared.GCKnownNonNull
		}
		heap := typ.Ref().Heap()
		var index uint32
		switch heap.Kind() {
		case wasm.HeapTypeIndex:
			typeIndex := heap.Type()
			index = typeIndex.Index
			if typeIndex.Rec {
				if typeIndex.Index >= recLength || recBase > ^uint32(0)-typeIndex.Index {
					continue
				}
				index = recBase + typeIndex.Index
			}
		case wasm.HeapDefType:
			group, member, _, valid := heap.Def()
			if !valid || int(group) >= len(f.m.Types) || member >= uint32(len(f.m.Types[group].SubTypes)) {
				continue
			}
			for i := uint32(0); i < group; i++ {
				index += uint32(len(f.m.Types[i].SubTypes))
			}
			index += member
		default:
			continue
		}
		if target, ok := f.stagedGCType(index); ok && target.Final && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray) {
			fact := shared.ExactGCRefFact(index, 0, f.gcHeapClassForType(index)).WithNullability(nullability).WithFreshness(shared.GCPublished)
			if int(index) < len(f.gcTypeLayouts) {
				fact = fact.WithPointerFree(f.gcTypeLayouts[index].PointerFree)
			}
			f.localGCRefFacts[local] = fact
		}
	}
}

func (f *fn) markLocalGetExactGCType(e *elem, local int) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localGCRefFacts) {
		return
	}
	if fact := f.localGCRefFacts[local]; !fact.IsZero() {
		markGCRefFact(e, fact)
	}
}

func (f *fn) setLocalExactGCType(local int, source *elem) (shared.GCRefFact, bool) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localGCRefFacts) {
		return shared.GCRefFact{}, false
	}
	fact := gcRefFact(source)
	f.localGCRefFacts[local] = fact
	_, exact := fact.ExactType()
	return fact, exact
}

func (f *fn) clearLocalExactGCTypes() {
	if exactGCRefFactsEnabled {
		clear(f.localGCRefFacts)
	}
	f.gcLastArrayLen.valid = false
	f.gcLastField.valid = false
	f.invalidateGCResolvedObject()
}

func (f *fn) newGCRefFactBuf() []shared.GCRefFact {
	n := len(f.localGCRefFacts)
	for i := len(f.gcFactPool) - 1; i >= 0; i-- {
		b := f.gcFactPool[i]
		if cap(b) < n {
			continue
		}
		last := len(f.gcFactPool) - 1
		f.gcFactPool[i] = f.gcFactPool[last]
		f.gcFactPool[last] = nil
		f.gcFactPool = f.gcFactPool[:last]
		return b[:n]
	}
	return make([]shared.GCRefFact, n)
}

func (f *fn) freeGCRefFactBuf(b []shared.GCRefFact) {
	if cap(b) >= len(f.localGCRefFacts) && len(f.localGCRefFacts) != 0 {
		clear(b)
		f.gcFactPool = append(f.gcFactPool, b[:cap(b)])
	}
}

func (f *fn) snapshotGCRefFacts() []shared.GCRefFact {
	if !exactGCRefFactsEnabled || len(f.localGCRefFacts) == 0 {
		return nil
	}
	b := f.newGCRefFactBuf()
	copy(b, f.localGCRefFacts)
	return b
}

func (f *fn) mergeGCRefFactsInto(target *[]shared.GCRefFact) {
	if !exactGCRefFactsEnabled {
		return
	}
	if *target == nil {
		*target = f.snapshotGCRefFacts()
		return
	}
	for i := range f.localGCRefFacts {
		(*target)[i] = shared.MergeGCRefFacts((*target)[i], f.localGCRefFacts[i])
	}
}

func (f *fn) installGCRefFacts(source []shared.GCRefFact) {
	if !exactGCRefFactsEnabled {
		return
	}
	if source == nil {
		clear(f.localGCRefFacts)
	} else {
		copy(f.localGCRefFacts, source)
	}
	f.gcLastArrayLen.valid = false
	f.gcLastField.valid = false
	f.invalidateGCResolvedObject()
}

func (f *fn) invalidateLoopModifiedGCRefFacts(modified map[uint32]bool) {
	if !exactGCRefFactsEnabled {
		return
	}
	for local := range modified {
		idx := int(local) + f.localBase
		if idx >= 0 && idx < len(f.localGCRefFacts) {
			f.localGCRefFacts[idx] = shared.GCRefFact{}
			f.invalidateGCLoadFactsForLocal(idx)
		}
	}
	f.invalidateGCResolvedObject()
}

func (f *fn) publishGCIdentity(identity uint32) {
	if !exactGCRefFactsEnabled || identity == 0 {
		return
	}
	for i, fact := range f.localGCRefFacts {
		if fact.Identity() == identity {
			f.localGCRefFacts[i] = fact.WithFreshness(shared.GCPublished)
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue {
			fact := gcRefFact(e)
			if fact.Identity() == identity {
				putGCRefFact(&e.st, fact.WithFreshness(shared.GCPublished))
			}
		}
	}
	if f.gcLastField.identity == identity && !f.gcLastField.immutable {
		f.gcLastField.valid = false
	}
}

func (f *fn) publishGCRef(e *elem) { f.publishGCIdentity(gcRefFact(e).Identity()) }

func (f *fn) publishGCStoredChild(parent, child *elem) {
	parentIdentity := gcRefFact(parent).Identity()
	childIdentity := gcRefFact(child).Identity()
	if childIdentity != 0 && childIdentity != parentIdentity {
		f.publishGCIdentity(childIdentity)
	}
}

func (f *fn) recordGCBarrierState(state shared.GCBarrierState) {
	switch state {
	case shared.GCBarrierNoBarrier:
		f.stats.peep("gc-barrier-none")
	case shared.GCBarrierYoungParent:
		f.stats.peep("gc-barrier-young-parent")
	case shared.GCBarrierKnownOldChild:
		f.stats.peep("gc-barrier-known-old-child")
	case shared.GCBarrierExistingCard:
		f.stats.peep("gc-barrier-existing-card")
	case shared.GCBarrierCardMark:
		f.stats.peep("gc-barrier-card-mark")
	case shared.GCBarrierSlowBarrier:
		f.stats.peep("gc-barrier-slow")
	}
}

func (f *fn) publishAllFreshGCRefs() {
	if !exactGCRefFactsEnabled {
		return
	}
	for i, fact := range f.localGCRefFacts {
		if fact.Freshness() == shared.GCFreshUnpublished {
			f.localGCRefFacts[i] = fact.WithFreshness(shared.GCPublished)
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue {
			fact := gcRefFact(e)
			if fact.Freshness() == shared.GCFreshUnpublished {
				putGCRefFact(&e.st, fact.WithFreshness(shared.GCPublished))
			}
		}
	}
	if !f.gcLastField.immutable {
		f.gcLastField.valid = false
	}
}

func (f *fn) invalidateGCGenerationFacts() {
	if !exactGCRefFactsEnabled {
		return
	}
	for i, fact := range f.localGCRefFacts {
		f.localGCRefFacts[i] = fact.WithGeneration(shared.GCGenerationUnknown)
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue {
			fact := gcRefFact(e)
			if !fact.IsZero() {
				putGCRefFact(&e.st, fact.WithGeneration(shared.GCGenerationUnknown))
			}
		}
	}
}

func (f *fn) invalidateGCLoadFactsForLocal(local int) {
	if f.gcLastArrayLen.valid && (f.gcLastArrayLen.local == local || f.gcLastArrayLen.resultLocal == local) {
		f.gcLastArrayLen.valid = false
	}
	if f.gcLastField.valid && (f.gcLastField.local == local || f.gcLastField.resultLocal == local) {
		f.gcLastField.valid = false
	}
	if f.gcResolved.valid && f.gcResolved.local == local {
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) prepareGCLoadResultCapture(op byte) {
	if !gcLoadForwardingEnabled {
		f.gcLastArrayLen.pending = nil
		f.gcLastField.pending = nil
		return
	}
	if op == 0x21 || op == 0x22 { // local.set / local.tee may name the just-produced value.
		return
	}
	f.gcLastArrayLen.pending = nil
	f.gcLastField.pending = nil
}

func (f *fn) captureGCLoadResultLocal(value *elem, local int) {
	if !gcLoadForwardingEnabled {
		f.invalidateGCLoadFactsForLocal(local)
		return
	}
	array := f.gcLastArrayLen
	captureArray := array.valid && array.pending == value && array.local != local
	field := f.gcLastField
	captureField := field.valid && field.pending == value && field.local != local
	f.invalidateGCLoadFactsForLocal(local)
	if captureArray {
		array.resultLocal, array.pending = local, nil
		f.gcLastArrayLen = array
		f.stats.peep("gc-load-cache-capture")
	}
	if captureField {
		field.resultLocal, field.pending = local, nil
		f.gcLastField = field
		f.stats.peep("gc-load-cache-capture")
	}
}

func (f *fn) invalidateGCMutableLoadFacts() {
	if !f.gcLastField.immutable {
		f.gcLastField.valid = false
	}
	f.invalidateGCResolvedObject()
}

// prepareGCResolvedObject is the central straight-line invalidation gate. Only
// leaves needed to carry an unchanged local into another GC operation and the
// GC prefix itself retain the transient address. Every other opcode drops it.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	switch op {
	case 0x1a, 0x20, 0x41, 0x42, 0x43, 0x44, 0xfb:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) prepareGCResolvedFB(sub uint32) {
	if !f.gcResolved.valid {
		return
	}
	switch sub {
	case 2, 3, 4, 5, 11, 12, 13, 14, 15:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) invalidateGCResolvedObject() {
	if !f.gcResolved.valid {
		return
	}
	f.pinned = f.pinned.remove(f.gcResolved.reg)
	f.gcResolved = gcResolvedObject{}
}

func (f *fn) gcResolvedRegister() Reg {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range [...]Reg{RBP, R12, R13, R14} {
		if f.regUser[reg] == nil && !block.has(reg) {
			return reg
		}
	}
	return regNone
}

func gcLocalProvenance(e *elem) (int, bool) {
	if e == nil || e.kind != ekValue {
		return 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return e.st.idx, true
	case stReg:
		if e.st.slot > 0 {
			return e.st.slot - 1, true
		}
	}
	return 0, false
}

func markGCLocalProvenance(e *elem, local int) {
	if e != nil && e.kind == ekValue && e.st.kind == stReg && local >= 0 {
		e.st.slot = local + 1
	}
}

func (f *fn) tryForwardGCArrayLen(typeIndex uint32) bool {
	if !gcLoadForwardingEnabled {
		return false
	}
	last := &f.gcLastArrayLen
	if !last.valid || last.resultLocal < 0 || last.typeIndex != typeIndex {
		return false
	}
	local, fact, ok := f.topGCLocalFact()
	if !ok || local != last.local || (last.identity != 0 && fact.Identity() != last.identity) {
		return false
	}
	f.dropValue()
	f.pushGCCachedLocal(last.resultLocal)
	f.stats.peep("gc-array-len-repeat")
	f.stats.peep("gc-array-len-repeat-elide")
	return true
}

func (f *fn) observeGCArrayLen(typeIndex uint32) {
	local, fact, ok := f.topGCLocalFact()
	knownType, exact := fact.ExactType()
	if !ok || !exact || knownType != typeIndex {
		f.gcLastArrayLen.valid = false
		return
	}
	if last := &f.gcLastArrayLen; last.valid && last.local == local && last.identity == fact.Identity() && last.typeIndex == typeIndex {
		f.stats.peep("gc-array-len-repeat")
	}
	f.gcLastArrayLen = gcArrayLenFact{valid: true, local: local, resultLocal: -1, identity: fact.Identity(), typeIndex: typeIndex}
}

func (f *fn) recordGCArrayLenResult() {
	if gcLoadForwardingEnabled && f.gcLastArrayLen.valid {
		f.gcLastArrayLen.pending = f.s.back()
	}
}

func (f *fn) tryForwardGCImmutableStructGet(typeIndex, fieldIndex uint32) bool {
	if !gcLoadForwardingEnabled {
		return false
	}
	last := &f.gcLastField
	if !last.valid || !last.immutable || last.resultLocal < 0 || last.typeIndex != typeIndex || last.fieldIndex != fieldIndex {
		return false
	}
	local, fact, ok := f.topGCLocalFact()
	if !ok || local != last.local || (last.identity != 0 && fact.Identity() != last.identity) {
		return false
	}
	f.dropValue()
	f.pushGCCachedLocal(last.resultLocal)
	f.stats.peep("gc-struct-get-repeat")
	f.stats.peep("gc-struct-get-repeat-elide")
	return true
}

func (f *fn) observeGCStructGet(typeIndex, fieldIndex uint32, immutable bool) {
	local, fact, ok := f.topGCLocalFact()
	knownType, exact := fact.ExactType()
	if !ok || !exact || knownType != typeIndex {
		f.gcLastField.valid = false
		return
	}
	if last := &f.gcLastField; last.valid && last.local == local && last.identity == fact.Identity() && last.typeIndex == typeIndex && last.fieldIndex == fieldIndex {
		if last.fromStore {
			f.stats.peep("gc-struct-set-get")
		} else {
			f.stats.peep("gc-struct-get-repeat")
		}
	}
	f.gcLastField = gcStructFieldFact{valid: true, immutable: immutable, local: local, resultLocal: -1, identity: fact.Identity(), typeIndex: typeIndex, fieldIndex: fieldIndex}
}

func (f *fn) recordGCStructGetResult() {
	if gcLoadForwardingEnabled && f.gcLastField.valid {
		f.gcLastField.pending = f.s.back()
	}
}

func (f *fn) pushGCCachedLocal(local int) *elem {
	var value *elem
	if pr, _, ok := f.pinReg(local); ok {
		f.recoverLocal(local)
		value = f.pushValue(storage{kind: stLocalReg, typ: f.localType[local], reg: pr, idx: local})
	} else {
		value = f.pushValue(storage{kind: stLocalRef, typ: f.localType[local], idx: local})
	}
	value.st.gcRoot = f.gcFrameLocal(local)
	f.markLocalGetExactGCType(value, local)
	return value
}

func (f *fn) tryForwardGCStructSetGet(typeIndex, fieldIndex uint32) bool {
	last := &f.gcLastField
	if !last.valid || !last.fromStore || !last.hasConst || last.typeIndex != typeIndex || last.fieldIndex != fieldIndex {
		return false
	}
	local, fact, ok := f.topGCLocalFact()
	if !ok || (last.local >= 0 && local != last.local) || fact.Identity() == 0 || fact.Identity() != last.identity {
		return false
	}
	f.dropValue()
	result := f.pushValue(storage{kind: stConst, typ: last.constType, cval: last.constBits})
	if !last.constFact.IsZero() {
		markGCRefFact(result, last.constFact)
	}
	last.pending = result
	f.stats.peep("gc-struct-set-get-forward")
	return true
}

func (f *fn) recordGCConstructorConstant(typeIndex, fieldIndex uint32, immutable bool, value, result *elem) {
	if value == nil || result == nil || value.kind != ekValue || value.st.kind != stConst {
		return
	}
	identity := gcRefFact(result).Identity()
	if identity == 0 {
		return
	}
	f.gcLastField = gcStructFieldFact{
		valid: true, fromStore: true, immutable: immutable, hasConst: true, local: -1, resultLocal: -1, identity: identity,
		typeIndex: typeIndex, fieldIndex: fieldIndex, constType: value.st.typ,
		constBits: value.st.cval, constFact: gcRefFact(value),
	}
}

func (f *fn) recordGCStructSetConstant(value *elem) {
	last := &f.gcLastField
	if !last.valid || !last.fromStore || value == nil || value.kind != ekValue || value.st.kind != stConst {
		return
	}
	last.hasConst = true
	last.constType = value.st.typ
	last.constBits = value.st.cval
	last.constFact = gcRefFact(value)
}

func (f *fn) observeGCStructSet(object *elem, typeIndex, fieldIndex uint32) {
	if f.gcLastField.valid && f.gcLastField.immutable {
		return
	}
	f.gcLastField.valid = false
	local, ok := gcLocalProvenance(object)
	if !ok || local < 0 || local >= len(f.localGCRefFacts) {
		return
	}
	fact := f.localGCRefFacts[local]
	knownType, exact := fact.ExactType()
	if !exact || knownType != typeIndex || fact.Freshness() != shared.GCFreshUnpublished || fact.Identity() == 0 {
		return
	}
	f.gcLastField = gcStructFieldFact{valid: true, fromStore: true, local: local, resultLocal: -1, identity: fact.Identity(), typeIndex: typeIndex, fieldIndex: fieldIndex}
}

func (f *fn) topGCLocalFact() (local int, fact shared.GCRefFact, ok bool) {
	if !exactGCRefFactsEnabled {
		return 0, shared.GCRefFact{}, false
	}
	e := f.s.back()
	local, ok = gcLocalProvenance(e)
	if !ok || local < 0 || local >= len(f.localGCRefFacts) {
		return 0, shared.GCRefFact{}, false
	}
	fact = f.localGCRefFacts[local]
	return local, fact, !fact.IsZero()
}

func (f *fn) topExactGCLocal() (local int, typeIndex uint32, ok bool) {
	local, fact, ok := f.topGCLocalFact()
	if !ok {
		return 0, 0, false
	}
	typeIndex, ok = fact.ExactType()
	return local, typeIndex, ok
}

func (f *fn) refineGCDereferencedObject(object *elem) {
	if !exactGCRefFactsEnabled {
		return
	}
	local, ok := gcLocalProvenance(object)
	if !ok || local < 0 || local >= len(f.localGCRefFacts) {
		return
	}
	fact := f.localGCRefFacts[local].WithNullability(shared.GCKnownNonNull)
	f.localGCRefFacts[local] = fact
	markGCRefFact(object, fact)
	f.stats.peep("gc-deref-nonnull-refine")
}

func (f *fn) refineTopLocalExactGCType(typeIndex uint32) {
	if !exactGCRefFactsEnabled {
		return
	}
	e := f.s.back()
	if e == nil || e.kind != ekValue || (e.st.kind != stLocalRef && e.st.kind != stLocalReg) {
		return
	}
	local := e.st.idx
	if local >= 0 && local < len(f.localGCRefFacts) {
		fact := f.localGCRefFacts[local].WithExactType(typeIndex, f.gcHeapClassForType(typeIndex)).WithNullability(shared.GCKnownNonNull)
		f.localGCRefFacts[local] = fact
		putGCRefFact(&e.st, fact)
	}
}
