//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	maxGCLocalTypeFacts          = 8
	maxGCLocalTypeFactSuperDepth = 64
)

type gcReferenceFactFlags uint8

const (
	gcReferenceFactValid gcReferenceFactFlags = 1 << iota
	gcReferenceFactNullable
	gcReferenceFactExact
	gcReferenceFactStored
)

// gcReferenceTypeFact is a compact, allocation-free description of a proved
// indexed reference value. Recursive-local type indexes are excluded because
// they need their defining recursion-group owner to resolve correctly.
type gcReferenceTypeFact struct {
	typeIndex uint32
	flags     gcReferenceFactFlags
}

func gcReferenceFact(typ wasm.ValType) (gcReferenceTypeFact, bool) {
	if typ.Kind() != wasm.ValRef {
		return gcReferenceTypeFact{}, false
	}
	ref := typ.Ref()
	heap := ref.Heap()
	if heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec {
		return gcReferenceTypeFact{}, false
	}
	flags := gcReferenceFactValid
	if ref.Nullable() {
		flags |= gcReferenceFactNullable
	}
	if ref.Exact() {
		flags |= gcReferenceFactExact
	}
	return gcReferenceTypeFact{typeIndex: heap.Type().Index, flags: flags}, true
}

func (fact gcReferenceTypeFact) present() bool {
	return fact.valid() || fact.flags&gcReferenceFactStored != 0
}

func (fact gcReferenceTypeFact) stored() bool {
	return fact.valid() || fact.flags&gcReferenceFactStored != 0
}

func (fact gcReferenceTypeFact) valid() bool {
	return fact.flags&gcReferenceFactValid != 0
}

func (fact gcReferenceTypeFact) provesCast(m *wasm.Module, sub uint32, target int64, exact bool) bool {
	if !fact.valid() || m == nil || target < 0 || uint64(target) > uint64(^uint32(0)) || sub != 22 && sub != 23 {
		return false
	}
	if sub == 22 && fact.flags&gcReferenceFactNullable != 0 {
		return false
	}
	targetIndex := uint32(target)
	if exact {
		return fact.flags&gcReferenceFactExact != 0 && fact.typeIndex == targetIndex
	}
	if fact.typeIndex == targetIndex {
		return true
	}
	return m.DeclaredTypeSubtype(fact.typeIndex, targetIndex, maxGCLocalTypeFactSuperDepth)
}

// gcLocalTypeFacts is a bounded straight-line map. Replacement is FIFO only
// after all eight entries are live. The common path scans at most eight scalar
// entries and never allocates.
type gcLocalTypeFact struct {
	local uint32
	fact  gcReferenceTypeFact
}

type gcLocalTypeFacts struct {
	entries [maxGCLocalTypeFacts]gcLocalTypeFact
	n       uint8
	next    uint8
}

func (facts *gcLocalTypeFacts) get(local int) (gcReferenceTypeFact, bool) {
	if local < 0 {
		return gcReferenceTypeFact{}, false
	}
	for i := uint8(0); i < facts.n; i++ {
		entry := facts.entries[i]
		if entry.local == uint32(local) {
			return entry.fact, entry.fact.present()
		}
	}
	return gcReferenceTypeFact{}, false
}

func (facts *gcLocalTypeFacts) set(local int, fact gcReferenceTypeFact) {
	if local < 0 || !fact.present() {
		facts.remove(local)
		return
	}
	for i := uint8(0); i < facts.n; i++ {
		if facts.entries[i].local == uint32(local) {
			facts.entries[i].fact = fact
			return
		}
	}
	entry := gcLocalTypeFact{local: uint32(local), fact: fact}
	if facts.n < maxGCLocalTypeFacts {
		facts.entries[facts.n] = entry
		facts.n++
		return
	}
	facts.entries[facts.next] = entry
	facts.next = (facts.next + 1) % maxGCLocalTypeFacts
}

func (facts *gcLocalTypeFacts) remove(local int) {
	if local < 0 {
		return
	}
	for i := uint8(0); i < facts.n; i++ {
		if facts.entries[i].local != uint32(local) {
			continue
		}
		facts.n--
		facts.entries[i] = facts.entries[facts.n]
		facts.entries[facts.n] = gcLocalTypeFact{}
		facts.next = 0
		return
	}
}

func (facts *gcLocalTypeFacts) clear() {
	facts.n = 0
	facts.next = 0
}

func (f *fn) gcLocalTypeFactsEnabled() bool {
	return f.gcStructHelpers && f.opt(optValueFacts)
}

func (f *fn) loadGCLocalTypeFact(local int) (gcReferenceTypeFact, bool) {
	if !f.gcLocalTypeFactsEnabled() {
		return gcReferenceTypeFact{}, false
	}
	fact, ok := f.gcLocalTypeFacts.get(local)
	if ok {
		f.stats.peep("gc-local-type-fact-load")
	}
	return fact, ok
}

func (f *fn) storeGCLocalTypeFact(local int, fact gcReferenceTypeFact) {
	if !f.gcLocalTypeFactsEnabled() {
		return
	}
	f.gcLocalTypeFacts.set(local, fact)
	if fact.valid() {
		f.stats.peep("gc-local-type-fact-store")
	}
}

func (f *fn) invalidateGCLocalTypeFact(local int) {
	if f.gcLocalTypeFactsEnabled() {
		f.gcLocalTypeFacts.remove(local)
	}
}

func (f *fn) clearGCLocalTypeFacts() {
	f.gcLocalTypeFacts.clear()
	f.gcPendingReference = gcReferenceTypeFact{}
}

func (f *fn) prepareGCLocalTypeFacts(op byte) {
	switch op {
	case 0x00, 0x02, 0x03, 0x04, 0x05, 0x08, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x1f:
		f.clearGCLocalTypeFacts()
		return
	case 0x21, 0x22, 0xfb:
		return
	default:
		f.gcPendingReference = gcReferenceTypeFact{}
	}
}

func (f *fn) setPendingGCReference(typ wasm.ValType) {
	if !f.gcLocalTypeFactsEnabled() {
		f.gcPendingReference = gcReferenceTypeFact{}
		return
	}
	fact, ok := gcReferenceFact(typ)
	if !ok {
		f.gcPendingReference = gcReferenceTypeFact{}
		return
	}
	f.gcPendingReference = fact
}

func (f *fn) setPendingGCCastReference(sub uint32, target int64, exact bool) {
	if !f.gcLocalTypeFactsEnabled() {
		f.gcPendingReference = gcReferenceTypeFact{}
		return
	}
	if target < 0 || uint64(target) > uint64(^uint32(0)) || sub != 22 && sub != 23 {
		f.gcPendingReference = gcReferenceTypeFact{}
		return
	}
	flags := gcReferenceFactValid
	if sub == 23 {
		flags |= gcReferenceFactNullable
	}
	if exact {
		flags |= gcReferenceFactExact
	}
	f.gcPendingReference = gcReferenceTypeFact{typeIndex: uint32(target), flags: flags}
}
