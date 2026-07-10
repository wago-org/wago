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
	s.liveInstances++
	return nil
}

func (s *referenceStore) instanceClosed() {
	var release []*funcrefTokenEntry
	s.mu.Lock()
	if s.liveInstances > 0 {
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
	if !source.ownsLocalFuncrefDescriptor(descriptor) {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	if !source.retainFuncrefToken() {
		return 0, fmt.Errorf("funcref producer is closed")
	}
	token, err := s.newTokenLocked()
	if err != nil {
		source.releaseFuncrefToken()
		return 0, err
	}
	entry := &funcrefTokenEntry{token: token, descriptor: descriptor, owner: source}
	if s.byDescriptor == nil {
		s.byDescriptor = make(map[uint64]*funcrefTokenEntry)
		s.byToken = make(map[uint64]*funcrefTokenEntry)
	}
	s.byDescriptor[descriptor] = entry
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
		entry.owner.releaseFuncrefToken()
	}
}

func (in *Instance) ownsLocalFuncrefDescriptor(descriptor uint64) bool {
	if len(in.funcRefDescs) < 2*coreruntime.TableEntryBytes {
		return false
	}
	base := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0])))
	if descriptor < base+coreruntime.TableEntryBytes || descriptor >= base+uint64(len(in.funcRefDescs)) {
		return false
	}
	delta := descriptor - base
	if delta%coreruntime.TableEntryBytes != 0 {
		return false
	}
	funcIndex := int(delta/coreruntime.TableEntryBytes) - 1
	return funcIndex >= in.c.NumImports && funcIndex < len(in.c.FuncTypeID)
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

func (in *Instance) retainFuncrefToken() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed {
		return false
	}
	in.funcrefTokenRefs++
	return true
}

func (in *Instance) releaseFuncrefToken() {
	in.lifeMu.Lock()
	if in.funcrefTokenRefs > 0 {
		in.funcrefTokenRefs--
	}
	shouldRelease := in.closed && in.funcrefTokenRefs == 0 && !in.resourcesClosed
	in.lifeMu.Unlock()
	if shouldRelease {
		in.releaseResources()
	}
}
