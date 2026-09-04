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

func (f *fn) gcKnownArrayIndexInBounds(object, index *elem) (indexValue, length uint32, ok bool) {
	if !f.gcRefFactsEnabled() || !gcKnownArrayBoundsEnabled {
		return 0, 0, false
	}
	i, constant := gcKnownI32Const(index)
	fact := f.gcRefFact(object)
	length, known := fact.KnownArrayLength()
	return i, length, constant && known && fact.Nullability() == shared.GCKnownNonNull && i < length
}

func gcRefFact(e *elem) shared.GCRefFact {
	if e == nil || e.kind != ekValue || !e.st.hasGCRoot() {
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

func (f *fn) gcRefFactsEnabled() bool {
	return f.opt(optGCRefFacts)
}

func (f *fn) gcLoadForwarding() bool {
	return f.gcRefFactsEnabled() && gcLoadForwardingEnabled
}

func (f *fn) gcRefFact(e *elem) shared.GCRefFact {
	if !f.gcRefFactsEnabled() {
		return shared.GCRefFact{}
	}
	return gcRefFact(e)
}

func putGCRefFact(st *storage, fact shared.GCRefFact) {
	if st == nil {
		return
	}
	if st.kind == stConst && st.cval == 0 && fact.Nullability() == shared.GCKnownNull {
		st.setGCRoot(true)
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
	st.setGCRoot(true)
	st.cval = int64(bits)
	if arrayLen == 0 {
		return
	}
	switch st.kind {
	case stLocalRef, stLocalReg:
		st.slot = uint32(arrayLen)
	default:
		st.idx = uint32(arrayLen)
	}
}

func (f *fn) markGCRefFact(e *elem, fact shared.GCRefFact) {
	if e == nil || e.kind != ekValue {
		return
	}
	e.st.setGCRoot(true)
	if !f.gcRefFactsEnabled() {
		return
	}
	putGCRefFact(&e.st, fact)
}

func gcHeapClassMatches(source shared.GCHeapClass, target wasm.AbsHeapType) (match, known bool) {
	if target == wasm.HeapNone || target == wasm.HeapNoFunc || target == wasm.HeapNoExtern {
		return false, true
	}
	if source == shared.GCHeapUnknown {
		return false, false
	}
	switch target {
	case wasm.HeapAny:
		switch source {
		case shared.GCHeapAny, shared.GCHeapEq, shared.GCHeapI31, shared.GCHeapStruct, shared.GCHeapArray:
			return true, true
		case shared.GCHeapFunc, shared.GCHeapExtern:
			return false, true
		}
	case wasm.HeapEq:
		switch source {
		case shared.GCHeapEq, shared.GCHeapI31, shared.GCHeapStruct, shared.GCHeapArray:
			return true, true
		case shared.GCHeapAny:
			return false, false
		case shared.GCHeapFunc, shared.GCHeapExtern:
			return false, true
		}
	case wasm.HeapI31:
		switch source {
		case shared.GCHeapI31:
			return true, true
		case shared.GCHeapAny, shared.GCHeapEq:
			return false, false
		default:
			return false, true
		}
	case wasm.HeapStruct:
		switch source {
		case shared.GCHeapStruct:
			return true, true
		case shared.GCHeapAny, shared.GCHeapEq:
			return false, false
		default:
			return false, true
		}
	case wasm.HeapArray:
		switch source {
		case shared.GCHeapArray:
			return true, true
		case shared.GCHeapAny, shared.GCHeapEq:
			return false, false
		default:
			return false, true
		}
	case wasm.HeapFunc:
		return source == shared.GCHeapFunc, true
	case wasm.HeapExtern:
		return source == shared.GCHeapExtern, true
	}
	return false, false
}

func (f *fn) gcRefFactMatchesTarget(fact shared.GCRefFact, heap int64, nullable, exactTarget bool) (match, known bool) {
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
		source := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: actual}), exactTarget)
		target := wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), exactTarget)
		return f.m.ReferenceTypeSubtype(source, target), true
	}
	if exactTarget {
		return false, false
	}
	return gcHeapClassMatches(fact.HeapClass(), wasm.AbsHeapType(byte(heap)&0x7f))
}

func (f *fn) gcRefFactMatchesHeap(fact shared.GCRefFact, heap int64, nullable bool) (match, known bool) {
	return f.gcRefFactMatchesTarget(fact, heap, nullable, false)
}

func gcHeapClassForValType(m *wasm.Module, typ wasm.ValType) shared.GCHeapClass {
	if typ.Kind() != wasm.ValRef {
		return shared.GCHeapUnknown
	}
	heap := typ.Ref().Heap()
	if heap.Kind() == wasm.HeapAbs {
		switch heap.Abs() {
		case wasm.HeapAny:
			return shared.GCHeapAny
		case wasm.HeapEq:
			return shared.GCHeapEq
		case wasm.HeapI31:
			return shared.GCHeapI31
		case wasm.HeapStruct:
			return shared.GCHeapStruct
		case wasm.HeapArray:
			return shared.GCHeapArray
		case wasm.HeapFunc, wasm.HeapNoFunc:
			return shared.GCHeapFunc
		case wasm.HeapExtern, wasm.HeapNoExtern:
			return shared.GCHeapExtern
		}
		return shared.GCHeapUnknown
	}
	var index uint32
	switch heap.Kind() {
	case wasm.HeapTypeIndex:
		idx := heap.Type()
		if idx.Rec {
			return shared.GCHeapUnknown
		}
		index = idx.Index
	case wasm.HeapDefType:
		group, member, _, ok := heap.Def()
		if !ok || m == nil || int(group) >= len(m.Types) || member >= uint32(len(m.Types[group].SubTypes)) {
			return shared.GCHeapUnknown
		}
		for i := uint32(0); i < group; i++ {
			index += uint32(len(m.Types[i].SubTypes))
		}
		index += member
	default:
		return shared.GCHeapUnknown
	}
	if m != nil {
		want := index
		for i := range m.Types {
			if want >= uint32(len(m.Types[i].SubTypes)) {
				want -= uint32(len(m.Types[i].SubTypes))
				continue
			}
			switch m.Types[i].SubTypes[want].Comp.Kind {
			case wasm.CompStruct:
				return shared.GCHeapStruct
			case wasm.CompArray:
				return shared.GCHeapArray
			case wasm.CompFunc:
				return shared.GCHeapFunc
			}
			break
		}
	}
	return shared.GCHeapUnknown
}

func zeroGCRefFactForValType(m *wasm.Module, typ wasm.ValType) shared.GCRefFact {
	if typ.Kind() != wasm.ValRef {
		return shared.GCRefFact{}
	}
	return shared.NewGCRefFact(shared.GCKnownNull, gcHeapClassForValType(m, typ))
}

func (f *fn) declaredGCRefFact(typ wasm.ValType) shared.GCRefFact {
	if !f.gcRefFactsEnabled() || typ.Kind() != wasm.ValRef {
		return shared.GCRefFact{}
	}
	nullability := shared.GCNullUnknown
	if !typ.Ref().Nullable() {
		nullability = shared.GCKnownNonNull
	}
	fact := shared.NewGCRefFact(nullability, gcHeapClassForValType(f.m, typ))
	heap := typ.Ref().Heap()
	var index uint32
	switch heap.Kind() {
	case wasm.HeapTypeIndex:
		idx := heap.Type()
		if idx.Rec {
			return fact
		}
		index = idx.Index
	case wasm.HeapDefType:
		group, member, _, ok := heap.Def()
		if !ok || f.m == nil || int(group) >= len(f.m.Types) || member >= uint32(len(f.m.Types[group].SubTypes)) {
			return fact
		}
		for i := uint32(0); i < group; i++ {
			index += uint32(len(f.m.Types[i].SubTypes))
		}
		index += member
	default:
		return fact
	}
	if target, ok := f.stagedGCType(index); ok && target.Final {
		fact = fact.WithExactType(index, f.gcHeapClassForType(index))
		if int(index) < len(f.gcTypeLayouts) {
			fact = fact.WithPointerFree(f.gcTypeLayouts[index].PointerFree)
		}
	}
	return fact
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
	if f.gcRefFactsEnabled() {
		fact := f.constructorGCRefFact(typeIndex, arrayLength)
		f.markGCRefFact(f.s.back(), fact)
		f.stats.peep("gc-fact-exact")
	}
}

func (f *fn) markExactGCType(e *elem, typeIndex uint32) {
	fact := f.gcRefFact(e)
	fact = fact.WithExactType(typeIndex, fact.HeapClass()).WithNullability(shared.GCKnownNonNull)
	f.markGCRefFact(e, fact)
}

func (f *fn) markTopExactGCType(typeIndex uint32) {
	if f.gcRefFactsEnabled() {
		fact := f.gcRefFact(f.s.back()).WithExactType(typeIndex, f.gcHeapClassForType(typeIndex)).WithNullability(shared.GCKnownNonNull)
		f.markGCRefFact(f.s.back(), fact)
	}
}

func (f *fn) seedFinalGCParameterTypes(params []wasm.ValType, recBase, recLength uint32) {
	if !f.gcRefFactsEnabled() {
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
	if !f.gcRefFactsEnabled() || local < 0 || local >= len(f.localGCRefFacts) {
		return
	}
	if fact := f.localGCRefFacts[local]; !fact.IsZero() {
		f.markGCRefFact(e, fact)
	}
}

func (f *fn) setLocalExactGCType(local int, source *elem) (shared.GCRefFact, bool) {
	if !f.gcRefFactsEnabled() || local < 0 || local >= len(f.localGCRefFacts) {
		return shared.GCRefFact{}, false
	}
	fact := f.gcRefFact(source)
	f.localGCRefFacts[local] = fact
	_, exact := fact.ExactType()
	return fact, exact
}

func (f *fn) clearLocalExactGCTypes() {
	if f.gcRefFactsEnabled() {
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
	if !f.gcRefFactsEnabled() || len(f.localGCRefFacts) == 0 {
		return nil
	}
	b := f.newGCRefFactBuf()
	copy(b, f.localGCRefFacts)
	return b
}

func (f *fn) mergeGCRefFactsInto(target *[]shared.GCRefFact) {
	if !f.gcRefFactsEnabled() {
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
	if !f.gcRefFactsEnabled() {
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

func (f *fn) invalidateLoopModifiedGCRefFacts(modified []uint16) {
	isModified := func(local int) bool {
		if local < f.localBase {
			return false
		}
		return loopSetsLocal(modified, uint32(local-f.localBase))
	}
	// A loop header is a join with a backedge. Mutable field observations from
	// straight-line entry can never dominate a later iteration. Immutable cached
	// results survive only when both their object and result locals are invariant.
	if f.gcLastField.valid && (!f.gcLastField.immutable || f.gcLastField.local < 0 || f.gcLastField.resultLocal < 0 || isModified(f.gcLastField.local) || isModified(f.gcLastField.resultLocal)) {
		f.gcLastField.valid = false
	}
	f.invalidateGCResolvedObject()
	if !f.gcRefFactsEnabled() {
		return
	}
	for _, local := range modified {
		idx := int(local) + f.localBase
		if idx >= 0 && idx < len(f.localGCRefFacts) {
			f.localGCRefFacts[idx] = shared.GCRefFact{}
			f.invalidateGCLoadFactsForLocal(idx)
		}
	}
	// A previous iteration may publish an otherwise invariant constructor without
	// assigning its local. Identity/type facts remain valid, but freshness does not.
	for i, fact := range f.localGCRefFacts {
		if fact.Freshness() == shared.GCFreshUnpublished {
			f.localGCRefFacts[i] = fact.WithFreshness(shared.GCPublished)
		}
	}
}

func (f *fn) publishGCIdentity(identity uint32) {
	if !f.gcRefFactsEnabled() || identity == 0 {
		return
	}
	for i, fact := range f.localGCRefFacts {
		if fact.Identity() == identity {
			f.localGCRefFacts[i] = fact.WithFreshness(shared.GCPublished)
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue {
			fact := f.gcRefFact(e)
			if fact.Identity() == identity {
				putGCRefFact(&e.st, fact.WithFreshness(shared.GCPublished))
			}
		}
	}
	if f.gcLastField.identity == identity && !f.gcLastField.immutable {
		f.gcLastField.valid = false
	}
}

func (f *fn) publishGCRef(e *elem) { f.publishGCIdentity(f.gcRefFact(e).Identity()) }

func (f *fn) publishGCStoredChild(parent, child *elem) {
	parentIdentity := f.gcRefFact(parent).Identity()
	childIdentity := f.gcRefFact(child).Identity()
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
	if !f.gcRefFactsEnabled() {
		return
	}
	for i, fact := range f.localGCRefFacts {
		if fact.Freshness() == shared.GCFreshUnpublished {
			f.localGCRefFacts[i] = fact.WithFreshness(shared.GCPublished)
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.kind == ekValue {
			fact := f.gcRefFact(e)
			if fact.Freshness() == shared.GCFreshUnpublished {
				putGCRefFact(&e.st, fact.WithFreshness(shared.GCPublished))
			}
		}
	}
	if !f.gcLastField.immutable {
		f.gcLastField.valid = false
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
	if !f.gcLoadForwarding() {
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
	if !f.gcLoadForwarding() {
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
		return e.st.index(), true
	case stReg:
		if e.st.slot > 0 {
			return e.st.slotIndex() - 1, true
		}
	}
	return 0, false
}

func markGCLocalProvenance(e *elem, local int) {
	if e != nil && e.kind == ekValue && e.st.kind == stReg && local >= 0 {
		e.st.slot = uint32(local + 1)
	}
}

func (f *fn) tryForwardGCArrayLen(typeIndex uint32) bool {
	if !f.gcLoadForwarding() {
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
	if f.gcLoadForwarding() && f.gcLastArrayLen.valid {
		f.gcLastArrayLen.pending = f.s.back()
	}
}

func (f *fn) tryForwardGCImmutableStructGet(typeIndex, fieldIndex uint32) bool {
	if !f.gcLoadForwarding() {
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
	if f.gcLoadForwarding() && f.gcLastField.valid {
		f.gcLastField.pending = f.s.back()
	}
}

func (f *fn) pushGCCachedLocal(local int) *elem {
	var value *elem
	if pr, _, ok := f.pinReg(local); ok {
		f.recoverLocal(local)
		value = f.pushValue(storage{kind: stLocalReg, typ: f.localType[local], reg: pr, idx: uint32(local)})
	} else {
		value = f.pushValue(storage{kind: stLocalRef, typ: f.localType[local], idx: uint32(local)})
	}
	value.st.setGCRoot(f.gcFrameLocal(local))
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
		f.markGCRefFact(result, last.constFact)
	}
	last.pending = result
	f.stats.peep("gc-struct-set-get-forward")
	return true
}

func (f *fn) recordGCConstructorConstant(typeIndex, fieldIndex uint32, immutable bool, value, result *elem) {
	if value == nil || result == nil || value.kind != ekValue || value.st.kind != stConst {
		return
	}
	identity := f.gcRefFact(result).Identity()
	if identity == 0 {
		return
	}
	f.gcLastField = gcStructFieldFact{
		valid: true, fromStore: true, immutable: immutable, hasConst: true, local: -1, resultLocal: -1, identity: identity,
		typeIndex: typeIndex, fieldIndex: fieldIndex, constType: value.st.typ,
		constBits: value.st.cval, constFact: f.gcRefFact(value),
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
	last.constFact = f.gcRefFact(value)
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
	if !f.gcRefFactsEnabled() {
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
	if !f.gcRefFactsEnabled() {
		return
	}
	local, ok := gcLocalProvenance(object)
	if !ok || local < 0 || local >= len(f.localGCRefFacts) {
		return
	}
	fact := f.localGCRefFacts[local].WithNullability(shared.GCKnownNonNull)
	f.localGCRefFacts[local] = fact
	f.markGCRefFact(object, fact)
	f.stats.peep("gc-deref-nonnull-refine")
}

func (f *fn) refineTopLocalExactGCType(typeIndex uint32) {
	if !f.gcRefFactsEnabled() {
		return
	}
	e := f.s.back()
	if e == nil || e.kind != ekValue || (e.st.kind != stLocalRef && e.st.kind != stLocalReg) {
		return
	}
	local := e.st.index()
	if local >= 0 && local < len(f.localGCRefFacts) {
		fact := f.localGCRefFacts[local].WithExactType(typeIndex, f.gcHeapClassForType(typeIndex)).WithNullability(shared.GCKnownNonNull)
		f.localGCRefFacts[local] = fact
		putGCRefFact(&e.st, fact)
	}
}
