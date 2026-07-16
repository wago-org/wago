package wago

import (
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const maxGCExternConversions = 8

type gcExternConversionKind uint8

const (
	gcExternConversionForeign gcExternConversionKind = iota + 1
	gcExternConversionData
)

type gcExternConversionEntry struct {
	kind       gcExternConversionKind
	anyWord    uint64
	externWord uint64
	ref        gc.Ref
	rootSlot   uint32
	hasRoot    bool
}

// gcExternConversionState owns the finite identity bridge used by exact staged
// any.convert_extern/extern.convert_any products. Public externref tokens remain
// reference-store values. Converted data refs receive separate internal opaque
// extern words, and foreign externrefs receive separate internal any words; the
// two representations are never exposed as public GCRef tokens or scanned as
// compact collector refs.
type gcExternConversionState struct {
	mu        sync.Mutex
	store     *referenceStore
	collector *gc.Collector
	entries   [maxGCExternConversions]gcExternConversionEntry
	count     uint8
	closed    bool
}

func newGCExternConversionState(store *referenceStore, collector *gc.Collector) (*gcExternConversionState, error) {
	if store == nil {
		return nil, fmt.Errorf("GC extern conversion requires a reference store")
	}
	if collector == nil {
		return nil, fmt.Errorf("GC extern conversion requires a collector")
	}
	return &gcExternConversionState{store: store, collector: collector}, nil
}

func (s *gcExternConversionState) anyFromExtern(extern uint64) (uint64, error) {
	if s == nil {
		return 0, fmt.Errorf("GC extern conversion state is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, fmt.Errorf("GC extern conversion state is closed")
	}
	if extern == 0 {
		return 0, nil
	}
	for i := uint8(0); i < s.count; i++ {
		entry := &s.entries[i]
		switch entry.kind {
		case gcExternConversionData:
			if entry.externWord == extern {
				return uint64(entry.ref), nil
			}
		case gcExternConversionForeign:
			if entry.externWord == extern {
				return entry.anyWord, nil
			}
		}
	}
	if _, ok := s.store.resolveExternref(extern); !ok {
		return 0, fmt.Errorf("invalid or foreign externref token")
	}
	if s.count == maxGCExternConversions {
		return 0, fmt.Errorf("GC extern conversion capacity %d exhausted", maxGCExternConversions)
	}
	anyWord, err := s.newOpaqueWordLocked(extern)
	if err != nil {
		return 0, err
	}
	s.entries[s.count] = gcExternConversionEntry{kind: gcExternConversionForeign, anyWord: anyWord, externWord: extern}
	s.count++
	return anyWord, nil
}

func (s *gcExternConversionState) externFromAny(anyWord uint64) (uint64, error) {
	if s == nil {
		return 0, fmt.Errorf("GC extern conversion state is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, fmt.Errorf("GC extern conversion state is closed")
	}
	if anyWord == 0 {
		return 0, nil
	}
	for i := uint8(0); i < s.count; i++ {
		entry := &s.entries[i]
		if entry.kind == gcExternConversionForeign && entry.anyWord == anyWord {
			return entry.externWord, nil
		}
	}
	if anyWord>>32 != 0 {
		return 0, fmt.Errorf("invalid or forged internal anyref word %#x", anyWord)
	}
	ref := gc.Ref(uint32(anyWord))
	if !ref.IsI31() {
		if !ref.IsObj() {
			return 0, fmt.Errorf("invalid internal anyref word %#x", anyWord)
		}
		if _, err := s.collector.ObjectType(ref); err != nil {
			return 0, fmt.Errorf("convert internal anyref: %w", err)
		}
	}
	for i := uint8(0); i < s.count; i++ {
		entry := &s.entries[i]
		if entry.kind == gcExternConversionData && entry.ref == ref {
			return entry.externWord, nil
		}
	}
	if s.count == maxGCExternConversions {
		return 0, fmt.Errorf("GC extern conversion capacity %d exhausted", maxGCExternConversions)
	}
	externWord, err := s.newOpaqueWordLocked(anyWord)
	if err != nil {
		return 0, err
	}
	entry := gcExternConversionEntry{kind: gcExternConversionData, externWord: externWord, ref: ref}
	if ref.IsObj() {
		slot, err := s.collector.NewCheckedTableSlot(ref)
		if err != nil {
			return 0, fmt.Errorf("root converted GC object: %w", err)
		}
		entry.rootSlot, entry.hasRoot = slot, true
	}
	s.entries[s.count] = entry
	s.count++
	return externWord, nil
}

func (s *gcExternConversionState) isForeignAny(anyWord uint64) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("GC extern conversion state is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, fmt.Errorf("GC extern conversion state is closed")
	}
	for i := uint8(0); i < s.count; i++ {
		entry := &s.entries[i]
		if entry.kind == gcExternConversionForeign && entry.anyWord == anyWord {
			return true, nil
		}
	}
	return false, nil
}

func (s *gcExternConversionState) newOpaqueWordLocked(disallow uint64) (uint64, error) {
	for {
		word, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create GC extern conversion identity: %w", err)
		}
		if word>>32 == 0 || word == disallow {
			continue
		}
		collision := false
		for i := uint8(0); i < s.count; i++ {
			entry := &s.entries[i]
			if word == entry.anyWord || word == entry.externWord {
				collision = true
				break
			}
		}
		if !collision {
			return word, nil
		}
	}
}

func (s *gcExternConversionState) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var first error
	for i := uint8(0); i < s.count; i++ {
		entry := &s.entries[i]
		if entry.hasRoot {
			if err := s.collector.SetTableSlot(entry.rootSlot, gc.Null()); err != nil && first == nil {
				first = err
			}
		}
		*entry = gcExternConversionEntry{}
	}
	s.count = 0
	s.store = nil
	s.collector = nil
	return first
}
