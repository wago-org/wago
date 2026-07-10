package wago

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// referenceStore owns public reference tokens. Runtime-created instances share
// one store; package-level Instantiate creates a private store lazily on the
// first non-null reference boundary, so scalar/null-only instances pay no store
// allocation. Externref objects live only in the Go-owned slots below; native
// code and mmap-backed Wasm state carry the generation-checked uint64 handle.
type referenceStore struct {
	mu sync.Mutex

	private       bool
	runtimeClosed bool
	liveInstances int
	instances     map[*Instance]struct{}
	byDescriptor  map[uint64]*funcrefTokenEntry
	byToken       map[uint64]*funcrefTokenEntry
	externKey     uint64
	externSeed    uint32
	externrefs    []externrefSlot
}

type funcrefTokenEntry struct {
	token      uint64
	descriptor uint64
	owner      *Instance
}

type externrefSlot struct {
	value      any
	generation uint32
}

func newReferenceStore(private bool) *referenceStore {
	return &referenceStore{private: private, runtimeClosed: private}
}

func (s *referenceStore) registerInstance(in *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed && !s.private {
		return fmt.Errorf("wago: reference store is closed")
	}
	if s.instances == nil {
		s.instances = make(map[*Instance]struct{})
	}
	if _, exists := s.instances[in]; !exists {
		s.instances[in] = struct{}{}
		s.liveInstances++
	}
	return nil
}

func (s *referenceStore) instanceClosed(in *Instance) {
	var release []*funcrefTokenEntry
	s.mu.Lock()
	if _, exists := s.instances[in]; exists {
		delete(s.instances, in)
		s.liveInstances--
	}
	if s.runtimeClosed && s.liveInstances == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseFuncrefEntries(release)
}

func (s *referenceStore) closeRuntime() {
	var release []*funcrefTokenEntry
	s.mu.Lock()
	s.runtimeClosed = true
	if s.liveInstances == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseFuncrefEntries(release)
}

func (s *referenceStore) issue(source *Instance, descriptor uint64) (uint64, error) {
	if descriptor == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.byDescriptor[descriptor]; entry != nil {
		return entry.token, nil
	}
	owner, canonical, ok := s.canonicalFuncrefOwnerLocked(source, descriptor)
	if !ok {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	if entry := s.byDescriptor[canonical]; entry != nil {
		return entry.token, nil
	}
	if !owner.retainResourceRoot() {
		return 0, fmt.Errorf("funcref producer is closed")
	}
	token, err := s.newTokenLocked()
	if err != nil {
		owner.releaseResourceRoot()
		return 0, err
	}
	entry := &funcrefTokenEntry{token: token, descriptor: canonical, owner: owner}
	if s.byDescriptor == nil {
		s.byDescriptor = make(map[uint64]*funcrefTokenEntry)
		s.byToken = make(map[uint64]*funcrefTokenEntry)
	}
	s.byDescriptor[canonical] = entry
	s.byToken[token] = entry
	return token, nil
}

func (s *referenceStore) resolve(token uint64) (uint64, bool) {
	if token == 0 {
		return 0, true
	}
	s.mu.Lock()
	entry := s.byToken[token]
	s.mu.Unlock()
	if entry == nil {
		return 0, false
	}
	return entry.descriptor, true
}

func (s *referenceStore) issueExternref(value any) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed && !s.private {
		return 0, fmt.Errorf("wago: reference store is closed")
	}
	if uint64(len(s.externrefs)) >= uint64(^uint32(0)) {
		return 0, fmt.Errorf("wago: externref store is full")
	}
	if s.externKey == 0 {
		key, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create externref store key: %w", err)
		}
		s.externKey = key
		s.externSeed = uint32(key>>32) | 1
	}
	index := uint32(len(s.externrefs)) + 1
	generation := s.externSeed + index - 1
	if generation == 0 {
		generation = 1
	}
	for {
		raw := uint64(generation)<<32 | uint64(index)
		token := bits.RotateLeft64(raw^s.externKey, 17)
		if token != 0 {
			s.externrefs = append(s.externrefs, externrefSlot{value: value, generation: generation})
			return token, nil
		}
		generation++
		if generation == 0 {
			generation = 1
		}
	}
}

func (s *referenceStore) resolveExternref(token uint64) (any, bool) {
	if token == 0 {
		return nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externKey == 0 {
		return nil, false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index := uint32(raw)
	generation := uint32(raw >> 32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return nil, false
	}
	slot := &s.externrefs[index-1]
	if slot.generation != generation {
		return nil, false
	}
	return slot.value, true
}

func (s *referenceStore) newTokenLocked() (uint64, error) {
	for {
		token, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create funcref token: %w", err)
		}
		if s.byToken[token] == nil {
			return token, nil
		}
	}
}

func randomNonzeroUint64() (uint64, error) {
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		if token := binary.LittleEndian.Uint64(buf[:]); token != 0 {
			return token, nil
		}
	}
}

func (s *referenceStore) releaseEntriesLocked() []*funcrefTokenEntry {
	var entries []*funcrefTokenEntry
	if len(s.byToken) != 0 {
		entries = make([]*funcrefTokenEntry, 0, len(s.byToken))
		for _, entry := range s.byToken {
			entries = append(entries, entry)
		}
	}
	s.byDescriptor = nil
	s.byToken = nil
	clear(s.externrefs)
	s.externrefs = nil
	s.externKey = 0
	s.externSeed = 0
	return entries
}

func releaseFuncrefEntries(entries []*funcrefTokenEntry) {
	for _, entry := range entries {
		entry.owner.releaseResourceRoot()
	}
}

func (s *referenceStore) canonicalFuncrefOwnerLocked(source *Instance, descriptor uint64) (*Instance, uint64, bool) {
	if fidx, ok := source.funcrefDescriptorIndex(descriptor); ok {
		if fidx >= source.c.NumImports {
			_, registered := s.instances[source]
			return source, descriptor, registered
		}
		if fidx >= len(source.c.Imports) {
			return nil, 0, false
		}
		ex, ok := source.imports[source.c.Imports[fidx]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil || ex.inst.refStore != s {
			return nil, 0, false
		}
		canonical, ok := ex.inst.localFuncrefDescriptor(ex.localIdx)
		if !ok {
			return nil, 0, false
		}
		off := (fidx + 1) * coreruntime.TableEntryBytes
		refSlot := binary.LittleEndian.Uint64(source.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
		if refSlot != canonical {
			return nil, 0, false
		}
		if s.byDescriptor[canonical] != nil {
			return ex.inst, canonical, true
		}
		_, registered := s.instances[ex.inst]
		return ex.inst, canonical, registered
	}
	for candidate := range s.instances {
		if candidate.ownsLocalFuncrefDescriptor(descriptor) {
			return candidate, descriptor, true
		}
	}
	return nil, 0, false
}

func (in *Instance) funcrefDescriptorIndex(descriptor uint64) (int, bool) {
	if len(in.funcRefDescs) < 2*coreruntime.TableEntryBytes {
		return 0, false
	}
	base := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0])))
	if descriptor < base+coreruntime.TableEntryBytes || descriptor >= base+uint64(len(in.funcRefDescs)) {
		return 0, false
	}
	delta := descriptor - base
	if delta%coreruntime.TableEntryBytes != 0 {
		return 0, false
	}
	funcIndex := int(delta/coreruntime.TableEntryBytes) - 1
	return funcIndex, funcIndex >= 0 && funcIndex < len(in.c.FuncTypeID)
}

func (in *Instance) ownsLocalFuncrefDescriptor(descriptor uint64) bool {
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	return ok && funcIndex >= in.c.NumImports
}

func (in *Instance) localFuncrefDescriptor(localIdx int) (uint64, bool) {
	fidx := in.c.NumImports + localIdx
	if localIdx < 0 || fidx < in.c.NumImports || fidx >= len(in.c.FuncTypeID) || len(in.funcRefDescs) == 0 {
		return 0, false
	}
	off := (fidx + 1) * coreruntime.TableEntryBytes
	if off+coreruntime.TableEntryBytes > len(in.funcRefDescs) {
		return 0, false
	}
	return uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[off]))), true
}

func (in *Instance) funcrefStoreForEgress() (*referenceStore, error) {
	return in.referenceStoreForBoundary()
}

func (in *Instance) referenceStoreForBoundary() (*referenceStore, error) {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed {
		return nil, fmt.Errorf("instance is closed")
	}
	if in.refStore == nil {
		store := newReferenceStore(true)
		if err := store.registerInstance(in); err != nil {
			return nil, err
		}
		in.refStore = store
	}
	return in.refStore, nil
}

func (in *Instance) retainResourceRoot() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed {
		return false
	}
	in.resourceRefs++
	return true
}

func (in *Instance) releaseResourceRoot() {
	in.lifeMu.Lock()
	if in.resourceRefs > 0 {
		in.resourceRefs--
	}
	shouldRelease := in.closed && in.resourceRefs == 0 && !in.resourcesClosed
	in.lifeMu.Unlock()
	if shouldRelease {
		in.releaseResources()
	}
}
