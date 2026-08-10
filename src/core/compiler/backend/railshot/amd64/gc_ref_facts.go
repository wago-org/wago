//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type gcArrayLenFact struct {
	valid     bool
	local     int
	typeIndex uint32
}

type gcStructFieldFact struct {
	valid      bool
	fromStore  bool
	local      int
	typeIndex  uint32
	fieldIndex uint32
}

type gcResolvedObject struct {
	valid         bool
	local         int
	typeIndex     uint32
	requiredBytes uint32
	reg           Reg
}

// Exact non-null GC reference facts are deliberately compact and local. A
// storage's cval is otherwise unused for reference-valued registers/local reads;
// type+1 keeps zero as "unknown". Facts never cache raw heap pointers and are
// therefore valid across collection. Structured control boundaries clear the
// local table instead of building SSA/phi state.

func exactGCType(e *elem) (uint32, bool) {
	if e == nil || e.kind != ekValue || !e.st.gcRoot || e.st.cval <= 0 {
		return 0, false
	}
	return uint32(e.st.cval - 1), true
}

func markExactGCType(e *elem, typeIndex uint32) {
	if e == nil || e.kind != ekValue {
		return
	}
	e.st.gcRoot = true
	e.st.cval = int64(typeIndex) + 1
}

func (f *fn) markTopExactGCType(typeIndex uint32) {
	if exactGCRefFactsEnabled {
		markExactGCType(f.s.back(), typeIndex)
	}
}

func (f *fn) seedFinalGCParameterTypes(params []wasm.ValType) {
	if !exactGCRefFactsEnabled {
		return
	}
	for local, typ := range params {
		if local >= len(f.localExactGCType) || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() {
			continue
		}
		heap := typ.Ref().Heap()
		var index uint32
		switch heap.Kind() {
		case wasm.HeapTypeIndex:
			index = heap.Type().Index
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
			f.localExactGCType[local] = index + 1
		}
	}
}

func (f *fn) markLocalGetExactGCType(e *elem, local int) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localExactGCType) {
		return
	}
	if encoded := f.localExactGCType[local]; encoded != 0 {
		markExactGCType(e, encoded-1)
	}
}

func (f *fn) setLocalExactGCType(local int, source *elem) (uint32, bool) {
	if !exactGCRefFactsEnabled || local < 0 || local >= len(f.localExactGCType) {
		return 0, false
	}
	typeIndex, ok := exactGCType(source)
	if ok {
		f.localExactGCType[local] = typeIndex + 1
		return typeIndex, true
	}
	f.localExactGCType[local] = 0
	return 0, false
}

func (f *fn) clearLocalExactGCTypes() {
	if exactGCRefFactsEnabled {
		clear(f.localExactGCType)
	}
	f.gcLastArrayLen.valid = false
	f.gcLastField.valid = false
	f.invalidateGCResolvedObject()
}

func (f *fn) invalidateGCLoadFactsForLocal(local int) {
	if f.gcLastArrayLen.valid && f.gcLastArrayLen.local == local {
		f.gcLastArrayLen.valid = false
	}
	if f.gcLastField.valid && f.gcLastField.local == local {
		f.gcLastField.valid = false
	}
	if f.gcResolved.valid && f.gcResolved.local == local {
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) invalidateGCMutableLoadFacts() {
	f.gcLastField.valid = false
	f.invalidateGCResolvedObject()
}

// prepareGCResolvedObject is the central straight-line invalidation gate. Only
// leaves needed to carry an unchanged local into another GC operation and the
// GC prefix itself retain the transient address. Every other opcode drops it
// before lowering, conservatively covering arithmetic fixed-register uses,
// memory/runtime operations, control transfers, exceptions, and unknown work.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	switch op {
	case 0x1a, // drop is register-neutral
		0x20,                   // local.get
		0x41, 0x42, 0x43, 0x44, // scalar constants used by array indexes
		0xfb: // decoded subopcode performs the second-stage allowlist
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
	case 2, 3, 4, 5, // struct.get/get_s/get_u/set
		11, 12, 13, 14, 15: // array.get/get_s/get_u/set/len
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
	// Restrict the transient cache to registers untouched by tiny leaf stubs and
	// x86 fixed-role scalar operations. Do not spill a Wasm value merely to cache
	// a derived address: register pressure rejects the optimization instead.
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

func (f *fn) observeGCArrayLen(typeIndex uint32) {
	local, knownType, ok := f.topExactGCLocal()
	if !ok || knownType != typeIndex {
		f.gcLastArrayLen.valid = false
		return
	}
	if last := &f.gcLastArrayLen; last.valid && last.local == local && last.typeIndex == typeIndex {
		f.stats.peep("gc-array-len-repeat")
	}
	f.gcLastArrayLen = gcArrayLenFact{valid: true, local: local, typeIndex: typeIndex}
}

func (f *fn) observeGCStructGet(typeIndex, fieldIndex uint32) {
	local, knownType, ok := f.topExactGCLocal()
	if !ok || knownType != typeIndex {
		f.gcLastField.valid = false
		return
	}
	last := &f.gcLastField
	if last.valid && last.local == local && last.typeIndex == typeIndex && last.fieldIndex == fieldIndex {
		if last.fromStore {
			f.stats.peep("gc-struct-set-get")
		} else {
			f.stats.peep("gc-struct-get-repeat")
		}
	}
	f.gcLastField = gcStructFieldFact{valid: true, local: local, typeIndex: typeIndex, fieldIndex: fieldIndex}
}

func (f *fn) observeGCStructSet(object *elem, typeIndex, fieldIndex uint32) {
	f.gcLastField.valid = false
	local, ok := gcLocalProvenance(object)
	if !ok {
		return
	}
	if local < 0 || local >= len(f.localExactGCType) || f.localExactGCType[local] != typeIndex+1 {
		return
	}
	f.gcLastField = gcStructFieldFact{valid: true, fromStore: true, local: local, typeIndex: typeIndex, fieldIndex: fieldIndex}
}

func (f *fn) topExactGCLocal() (local int, typeIndex uint32, ok bool) {
	if !exactGCRefFactsEnabled {
		return 0, 0, false
	}
	e := f.s.back()
	var hasLocal bool
	local, hasLocal = gcLocalProvenance(e)
	if !hasLocal {
		return 0, 0, false
	}
	if local < 0 || local >= len(f.localExactGCType) {
		return 0, 0, false
	}
	encoded := f.localExactGCType[local]
	if encoded == 0 {
		return 0, 0, false
	}
	return local, encoded - 1, true
}

func (f *fn) refineTopLocalExactGCType(typeIndex uint32) {
	if !exactGCRefFactsEnabled {
		return
	}
	e := f.s.back()
	if e == nil || e.kind != ekValue {
		return
	}
	if e.st.kind != stLocalRef && e.st.kind != stLocalReg {
		return
	}
	local := e.st.idx
	if local >= 0 && local < len(f.localExactGCType) {
		f.localExactGCType[local] = typeIndex + 1
	}
}
