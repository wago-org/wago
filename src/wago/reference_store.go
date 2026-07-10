package wago

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// referenceStore owns public reference tokens. Runtime-created instances share
// one store; package-level Instantiate creates a private store lazily on the
// first non-null funcref result, so scalar-only instances pay no store allocation.
type referenceStore struct {
	mu sync.Mutex

	private       bool
	runtimeClosed bool
	liveInstances int
	instances     map[*Instance]struct{}
	byDescriptor  map[uint64]*funcrefTokenEntry
	byToken       map[uint64]*funcrefTokenEntry
}

type funcrefTokenEntry struct {
	token      uint64
	descriptor uint64
	owner      *Instance
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

func (s *referenceStore) newTokenLocked() (uint64, error) {
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, fmt.Errorf("create funcref token: %w", err)
		}
		token := binary.LittleEndian.Uint64(buf[:])
		if token != 0 && s.byToken[token] == nil {
			return token, nil
		}
	}
}

func (s *referenceStore) releaseEntriesLocked() []*funcrefTokenEntry {
	if len(s.byToken) == 0 {
		return nil
	}
	entries := make([]*funcrefTokenEntry, 0, len(s.byToken))
	for _, entry := range s.byToken {
		entries = append(entries, entry)
	}
	s.byDescriptor = nil
	s.byToken = nil
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
