//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"unsafe"
)

// Only constant construction uses this journal. Local cells transfer their
// roots and pins after Instance registration; imported cells already have a
// physical owner and retain their writes even if later construction fails.
type compactExternConstruction struct {
	collector *gc.Collector
	store     *referenceStore
	domain    *gcStoreDomain
	state     *compactExternCollector
	rootsLive []gc.Root
	cells     []compactExternConstructionCell
	extra     gc.RootSet
	parent    *gc.Slots
}
type compactExternConstructionCell struct {
	key   uintptr
	value compactExternPreparedWord
}

func newCompactExternConstruction(c *Compiled, collector *gc.Collector, store *referenceStore, domain *gcStoreDomain, parent *gc.Slots) *compactExternConstruction {
	if !compactExternConstructionNeeded(c) || collector == nil || store == nil {
		return nil
	}
	return &compactExternConstruction{collector: collector, store: store, domain: domain, parent: parent}
}
func compactExternConstructionNeeded(c *Compiled) bool {
	if c == nil || !c.compactExternFrameRoots() {
		return false
	}
	for index, g := range c.Globals {
		if g.Type == ValExternRef && (index < len(c.GlobalImports) || g.HasInitGlobal || len(g.InitExpr) != 0) || bytes.Contains(g.InitExpr, []byte{0xfb, 26}) || bytes.Contains(g.InitExpr, []byte{0xfb, 27}) {
			return true
		}
	}
	needsEntries := func(e ElemInit) bool {
		if normalizedElemRefType(e.RefType) != ValExternRef {
			return false
		}
		for _, value := range e.Values {
			if !value.Null && !value.RepeatPrevious {
				return true
			}
		}
		return false
	}
	for _, e := range c.Elems {
		if needsEntries(e) {
			return true
		}
	}
	for _, e := range c.passiveElems {
		if needsEntries(e) {
			return true
		}
	}
	return false
}
func (j *compactExternConstruction) ensureState() (*compactExternCollector, error) {
	if j.state != nil {
		return j.state, nil
	}
	state, err := compactExternCollectorFor(j.collector, j.store, j.domain)
	if err == nil {
		j.state = state
	}
	return state, err
}
func (j *compactExternConstruction) roots(fallback gc.RootSet) gc.RootSet {
	if j == nil {
		return fallback
	}
	j.extra = fallback
	return j
}
func (j *compactExternConstruction) RangeRoots(fn func(gc.RootSlot) bool) {
	if j.extra != nil {
		more := true
		j.extra.RangeRoots(func(slot gc.RootSlot) bool { more = fn(slot); return more })
		if !more {
			return
		}
	}
	for i := range j.rootsLive {
		if !fn(&j.rootsLive[i]) {
			return
		}
	}
}
func (j *compactExternConstruction) RangeRootRefs(sink gc.RootRefSink) bool {
	if j.extra != nil {
		if direct, ok := j.extra.(gc.DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
		} else {
			more := true
			j.extra.RangeRoots(func(slot gc.RootSlot) bool { more = sink.VisitRootRef(slot.GetRef()); return more })
			if !more {
				return false
			}
		}
	}
	for _, ref := range j.rootsLive {
		if !sink.VisitRootRef(gc.Ref(ref)) {
			return false
		}
	}
	return true
}
func (j *compactExternConstruction) track(ref gc.Ref) {
	if ref.IsNull() {
		return
	}
	j.rootsLive = append(j.rootsLive, gc.Root(ref))
	if j.parent != nil {
		*j.parent = append(*j.parent, &j.rootsLive[len(j.rootsLive)-1])
	}
}
func (j *compactExternConstruction) convert(sub uint32, value gcConstStackValue, roots gc.RootSet) (uint64, error) {
	if sub == 26 && value.kind != gcConstExternRef || sub == 27 && value.kind != gcConstCollectorRef {
		return 0, fmt.Errorf("compact extern constant carrier mismatch")
	}
	word := value.bits
	if sub == 27 {
		word = uint64(value.ref)
	}
	if word == 0 {
		return 0, nil
	}
	state, err := j.ensureState()
	if err != nil {
		return 0, err
	}
	if sub == 26 {
		ref, err := state.anyFromExtern(word, roots)
		if err != nil {
			return 0, err
		}
		j.track(ref)
		return uint64(ref), nil
	}
	word, err = state.externFromAny(value.ref, roots)
	if err != nil {
		return 0, err
	}
	if collector, ref := compactExternWordRoot(j.store, word); collector != nil {
		if collector != j.collector {
			return 0, fmt.Errorf("compact extern constant has a foreign collector")
		}
		j.track(ref)
	}
	return word, nil
}
func (j *compactExternConstruction) record(cell []byte, owner *Instance) error {
	if j == nil || len(cell) < 8 {
		return nil
	}
	word := binary.LittleEndian.Uint64(cell)
	if word == 0 && j.state == nil {
		return nil
	}
	state, err := j.ensureState()
	if err != nil {
		return err
	}
	key := uintptr(unsafe.Pointer(&cell[0]))
	if owner == nil {
		if host := state.hostGlobalForCell(key); host != nil {
			value, err := state.preparePersistent(word, j)
			if err != nil {
				return err
			}
			state.storeHostPrepared(&host.compactExternHostRootRow, key, &value, 0)
			binary.LittleEndian.PutUint64(cell, value.word)
			return nil
		}
		if host := state.hostTableForCell(key); host != nil {
			value, err := state.preparePersistent(word, j)
			if err != nil {
				return err
			}
			state.storePhysicalPrepared(nil, host, key, &value)
			binary.LittleEndian.PutUint64(cell, value.word)
			return nil
		}
	}
	if owner != nil {
		normalized, err := state.storePersistent(owner, key, word, j)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(cell, normalized)
		return nil
	}
	value, err := state.preparePersistent(word, j)
	if err != nil {
		return err
	}
	normalized, ref := value.word, value.ref
	for i := range j.cells {
		if j.cells[i].key == key {
			if old := j.cells[i].value.pin; old != 0 && !j.store.unpinExternref(old) {
				panic("wago: compact extern construction pin changed")
			}
			j.cells[i].value = value
			binary.LittleEndian.PutUint64(cell, normalized)
			j.track(ref)
			return nil
		}
	}
	j.cells = append(j.cells, compactExternConstructionCell{key: key, value: value})
	j.track(ref)
	binary.LittleEndian.PutUint64(cell, normalized)
	return nil
}
func (j *compactExternConstruction) bind(in *Instance) error {
	if j == nil || j.state == nil {
		return nil
	}
	if in == nil || in.gc != j.collector || in.refStore != j.store {
		return fmt.Errorf("compact extern construction lost its physical owner")
	}
	for i := range j.cells {
		cell := &j.cells[i]
		j.state.storePrepared(in, cell.key, &cell.value)
	}
	return nil
}
func (j *compactExternConstruction) close() {
	if j == nil {
		return
	}
	for i := range j.cells {
		if pin := j.cells[i].value.pin; pin != 0 && !j.store.unpinExternref(pin) {
			panic("wago: compact extern construction pin changed")
		}
	}
	j.cells = nil
	j.rootsLive = nil
	j.extra = nil
	j.parent = nil
	if j.state != nil {
		j.state.drain()
	}
}
