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
	uses       uint8
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
	for i := range s.entries {
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
	for i := range s.entries {
		if s.entries[i].kind == 0 {
			s.entries[i] = gcExternConversionEntry{kind: gcExternConversionForeign, anyWord: anyWord, externWord: extern}
			s.count++
			return anyWord, nil
		}
	}
	return 0, fmt.Errorf("GC extern conversion capacity %d exhausted", maxGCExternConversions)
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
	for i := range s.entries {
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
	for i := range s.entries {
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
	for i := range s.entries {
		if s.entries[i].kind == 0 {
			s.entries[i] = entry
			s.count++
			return externWord, nil
		}
	}
	if entry.hasRoot {
		_ = s.collector.SetTableSlot(entry.rootSlot, gc.Null())
	}
	return 0, fmt.Errorf("GC extern conversion capacity %d exhausted", maxGCExternConversions)
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
	for i := range s.entries {
		entry := &s.entries[i]
		if entry.kind == gcExternConversionForeign && entry.anyWord == anyWord {
			return true, nil
		}
	}
	return false, nil
}

// replaceExtern transfers one extern-table slot from oldWord to newWord. Public
// store tokens require no collector root. Internal data-conversion words retain
// one bounded checked root per distinct live table value and are reclaimed when
// their final slot is overwritten.
func (s *gcExternConversionState) replaceExtern(oldWord, newWord uint64) error {
	if s == nil {
		return fmt.Errorf("GC extern conversion state is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("GC extern conversion state is closed")
	}
	if oldWord == newWord {
		return s.validateExternWordLocked(newWord)
	}
	newEntry, err := s.dataEntryForExternLocked(newWord)
	if err != nil {
		return err
	}
	oldEntry, err := s.dataEntryForExternLocked(oldWord)
	if err != nil {
		return err
	}
	if oldEntry != nil && oldEntry.uses == 0 {
		return fmt.Errorf("GC extern conversion ownership underflow")
	}
	if oldEntry != nil && oldEntry.uses == 1 && oldEntry.hasRoot {
		if err := s.collector.SetTableSlot(oldEntry.rootSlot, gc.Null()); err != nil {
			return err
		}
	}
	if newEntry != nil {
		if newEntry.uses == ^uint8(0) {
			return fmt.Errorf("GC extern conversion ownership overflow")
		}
		newEntry.uses++
	}
	if oldEntry != nil {
		oldEntry.uses--
		if oldEntry.uses == 0 {
			*oldEntry = gcExternConversionEntry{}
			s.count--
		}
	}
	return nil
}

func (s *gcExternConversionState) validateExternWordLocked(word uint64) error {
	_, err := s.dataEntryForExternLocked(word)
	return err
}

func (s *gcExternConversionState) dataEntryForExternLocked(word uint64) (*gcExternConversionEntry, error) {
	if word == 0 {
		return nil, nil
	}
	for i := range s.entries {
		entry := &s.entries[i]
		if entry.kind == gcExternConversionData && entry.externWord == word {
			return entry, nil
		}
	}
	if _, ok := s.store.resolveExternref(word); !ok {
		return nil, fmt.Errorf("invalid or foreign externref token")
	}
	return nil, nil
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
		for i := range s.entries {
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
	for i := range s.entries {
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
