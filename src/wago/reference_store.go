package wago

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
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
	liveInstances uint32
	liveObjects   uint32
	instances     map[*Instance]struct{}
	byIdentity    map[funcrefIdentity]*funcrefTokenEntry
	byToken       map[uint64]*funcrefTokenEntry
	gcByToken     map[uint64]gcRefTokenEntry
	externKey     uint64
	externSeed    uint32
	externrefs    []externrefSlot
}

type funcrefIdentity struct {
	descriptor uint64
	instance   *Instance
	localIdx   int
}

type funcrefTokenEntry struct {
	token      uint64
	descriptor uint64
	owner      *Instance
}

type gcRefTokenEntry struct {
	token uint64
	ref   gc.Ref
	slot  uint32
	exact ValueTypeDescriptor
	owner *Instance
}

// gcPublicState serializes collector access performed by the exact staged public
// result owner. One reusable collector slot bounds each instance to one live
// token while allowing release/reissue without growing root metadata.
type gcPublicState struct {
	mu          sync.Mutex
	token       uint64
	slot        uint32
	slotCreated bool
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
		if s.liveInstances == ^uint32(0) {
			return fmt.Errorf("wago: reference store has too many live instances")
		}
		s.instances[in] = struct{}{}
		s.liveInstances++
	}
	return nil
}

func (s *referenceStore) instanceClosed(in *Instance) {
	var release referenceTokenEntries
	hasRoots := in.hasResourceRoots()
	s.mu.Lock()
	if _, exists := s.instances[in]; exists {
		if !hasRoots {
			delete(s.instances, in)
		}
		s.liveInstances--
	}
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && len(s.gcByToken) == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) resourceOwnerReleased(in *Instance) {
	s.mu.Lock()
	delete(s.instances, in)
	s.mu.Unlock()
}

func (s *referenceStore) registerStoreObject() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed {
		return fmt.Errorf("wago: reference store is closed")
	}
	if s.liveObjects == ^uint32(0) {
		return fmt.Errorf("wago: reference store has too many live objects")
	}
	s.liveObjects++
	return nil
}

const hostFuncRefDispatchBit = uint32(1 << 31)

func (s *referenceStore) registerHostFuncRef(owner *HostFuncRef) (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed {
		return 0, fmt.Errorf("wago: reference store is closed")
	}
	if s.liveObjects == ^uint32(0) || len(s.externrefs) >= int(hostFuncRefDispatchBit-1) {
		return 0, fmt.Errorf("wago: reference store has too many live objects")
	}
	s.liveObjects++
	s.externrefs = append(s.externrefs, externrefSlot{value: owner})
	return uint32(len(s.externrefs)), nil
}

func (s *referenceStore) hostFuncRef(dispatch uint32) *HostFuncRef {
	index := dispatch &^ hostFuncRefDispatchBit
	if dispatch&hostFuncRefDispatchBit == 0 || index == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if uint64(index) > uint64(len(s.externrefs)) {
		return nil
	}
	owner, _ := s.externrefs[index-1].value.(*HostFuncRef)
	return owner
}

func (s *referenceStore) storeObjectClosed() {
	var release referenceTokenEntries
	s.mu.Lock()
	if s.liveObjects > 0 {
		s.liveObjects--
	}
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) closeRuntime() {
	var release referenceTokenEntries
	s.mu.Lock()
	s.runtimeClosed = true
	if s.liveInstances == 0 && s.liveObjects == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) issue(source *Instance, descriptor uint64) (uint64, error) {
	if descriptor == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil {
		return entry.token, nil
	}
	if source == nil {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	owner, canonical, ok := s.canonicalFuncrefOwnerLocked(source, descriptor)
	if !ok {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	identity, hasIdentity := source.funcrefFunctionIdentity(descriptor)
	if hasIdentity {
		if entry := s.byIdentity[identity]; entry != nil {
			return entry.token, nil
		}
	}
	if entry := s.byIdentity[funcrefIdentity{descriptor: canonical}]; entry != nil {
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
	if s.byIdentity == nil {
		s.byIdentity = make(map[funcrefIdentity]*funcrefTokenEntry)
		s.byToken = make(map[uint64]*funcrefTokenEntry)
	}
	s.byIdentity[funcrefIdentity{descriptor: canonical}] = entry
	if hasIdentity {
		s.byIdentity[identity] = entry
	}
	s.byToken[token] = entry
	if hostOwner := owner.hostFuncRefForDescriptor(canonical); hostOwner != nil && !hostOwner.markTokenLive(owner, canonical) {
		delete(s.byIdentity, funcrefIdentity{descriptor: canonical})
		if hasIdentity {
			delete(s.byIdentity, identity)
		}
		delete(s.byToken, token)
		owner.releaseResourceRoot()
		return 0, fmt.Errorf("host funcref owner closed during token issue")
	}
	return token, nil
}

func (in *Instance) publicGCState() *gcPublicState {
	plugin := in.ensurePluginState()
	state := plugin.gcPublic.Load()
	if state == nil {
		candidate := &gcPublicState{}
		if plugin.gcPublic.CompareAndSwap(nil, candidate) {
			state = candidate
		} else {
			state = plugin.gcPublic.Load()
		}
	}
	return state
}

func (in *Instance) existingPublicGCState() *gcPublicState {
	if in == nil {
		return nil
	}
	plugin := in.pluginState.Load()
	if plugin == nil {
		return nil
	}
	return plugin.gcPublic.Load()
}

func (s *referenceStore) issueGCRef(source *Instance, ref gc.Ref, required ValueTypeDescriptor) (uint64, error) {
	if source == nil || ref.IsNull() || !ref.IsObj() {
		return 0, fmt.Errorf("invalid non-null GC result")
	}
	if source.c == nil || (source.c.stagedGCStructProduct() != stagedGCStructBasic && source.c.stagedGCArrayProduct() != stagedGCArrayProductNumericFixed) {
		return 0, fmt.Errorf("public GC result ownership is outside an exact admitted struct/array product")
	}
	state := source.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.token != 0 {
		return 0, fmt.Errorf("public GC result owner already has one live token")
	}
	if source.gc == nil {
		return 0, fmt.Errorf("public GC result has no live collector")
	}
	typeID, err := source.gc.ObjectType(ref)
	if err != nil {
		return 0, fmt.Errorf("public GC result object: %w", err)
	}
	if int(typeID) >= len(source.c.Types) {
		return 0, fmt.Errorf("public GC result type %d is unavailable", typeID)
	}
	kind := source.c.Types[typeID].Kind
	if source.c.stagedGCStructProduct() == stagedGCStructBasic && kind != CompositeTypeStruct {
		return 0, fmt.Errorf("public GC result type %d is not a struct", typeID)
	}
	if source.c.stagedGCArrayProduct() == stagedGCArrayProductNumericFixed && kind != CompositeTypeArray {
		return 0, fmt.Errorf("public GC result type %d is not an array", typeID)
	}
	exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(typeID)},
	}}
	if required.Kind != ValueTypeReference || !valueTypeSubtype(exact, source.c.Types, required, source.c.Types) {
		return 0, fmt.Errorf("public GC result type %d does not match its exact structural result type", typeID)
	}

	s.mu.Lock()
	_, registered := s.instances[source]
	s.mu.Unlock()
	if !registered || !source.retainResourceRoot() {
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	rollbackRoot := true
	defer func() {
		if rollbackRoot {
			source.releaseResourceRoot()
		}
	}()

	if !state.slotCreated {
		slot, slotErr := source.gc.NewCheckedGlobalSlot(ref)
		if slotErr != nil {
			return 0, fmt.Errorf("root public GC result: %w", slotErr)
		}
		state.slot, state.slotCreated = slot, true
	} else if err := source.gc.SetGlobalSlot(state.slot, ref); err != nil {
		return 0, fmt.Errorf("root public GC result: %w", err)
	}

	s.mu.Lock()
	if _, registered = s.instances[source]; !registered {
		s.mu.Unlock()
		_ = source.gc.SetGlobalSlot(state.slot, gc.Null())
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	token, err := s.newTokenLocked()
	if err == nil {
		if s.gcByToken == nil {
			s.gcByToken = make(map[uint64]gcRefTokenEntry)
		}
		s.gcByToken[token] = gcRefTokenEntry{token: token, ref: ref, slot: state.slot, exact: exact, owner: source}
		state.token = token
	}
	s.mu.Unlock()
	if err != nil {
		_ = source.gc.SetGlobalSlot(state.slot, gc.Null())
		return 0, err
	}
	rollbackRoot = false
	return token, nil
}

func (s *referenceStore) releaseGCRef(source *Instance, token uint64) error {
	if token == 0 {
		return nil
	}
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source == nil || entry.owner != source {
		return fmt.Errorf("GC reference token belongs to a different producer or store")
	}
	state := source.existingPublicGCState()
	if state == nil {
		return fmt.Errorf("GC reference token owner state is unavailable")
	}
	state.mu.Lock()
	s.mu.Lock()
	entry, ok = s.gcByToken[token]
	if !ok || entry.owner != source || state.token != token || !state.slotCreated || state.slot != entry.slot {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source.gc == nil {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("GC reference token collector is unavailable")
	}
	if err := source.gc.SetGlobalSlot(entry.slot, gc.Null()); err != nil {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("release GC reference token: %w", err)
	}
	delete(s.gcByToken, token)
	state.token = 0
	s.mu.Unlock()
	state.mu.Unlock()
	source.releaseResourceRoot()
	var release referenceTokenEntries
	s.mu.Lock()
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && len(s.gcByToken) == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseReferenceEntries(release)
	return nil
}

func (s *referenceStore) gcRefExactType(token uint64) (ValueTypeDescriptor, *Instance, uint32, bool) {
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok {
		return ValueTypeDescriptor{}, nil, 0, false
	}
	return entry.exact, entry.owner, entry.slot, true
}

// ReleaseGCRef releases one non-null GC result token issued by this producer.
// It is valid after Instance.Close while the token retains the producer's
// collector; null releases are no-ops. Stale, foreign-store, and cross-producer
// tokens reject without changing either owner.
func (in *Instance) ReleaseGCRef(ref GCRef) error {
	if ref.token == 0 {
		return nil
	}
	if in == nil {
		return fmt.Errorf("release GC reference token on nil instance")
	}
	in.lifeMu.Lock()
	store := in.refStore
	in.lifeMu.Unlock()
	if store == nil {
		return fmt.Errorf("instance has no GC reference token store")
	}
	return store.releaseGCRef(in, ref.token)
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

func (s *referenceStore) tokenFuncrefExactType(token uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if token == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	s.mu.Lock()
	entry := s.byToken[token]
	var owner *Instance
	var descriptor uint64
	if entry != nil {
		owner, descriptor = entry.owner, entry.descriptor
	}
	s.mu.Unlock()
	return instanceFuncrefExactType(owner, descriptor)
}

func (s *referenceStore) descriptorFuncrefExactType(source *Instance, descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if source == nil || descriptor == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	s.mu.Lock()
	owner, canonical, ok := s.canonicalFuncrefOwnerLocked(source, descriptor)
	s.mu.Unlock()
	if !ok {
		return ValueTypeDescriptor{}, nil, false
	}
	return instanceFuncrefExactType(owner, canonical)
}

func instanceFuncrefExactType(owner *Instance, descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if owner == nil || owner.c == nil || descriptor == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	index, ok := owner.funcrefDescriptorIndex(descriptor)
	if !ok {
		return ValueTypeDescriptor{}, nil, false
	}
	exact, err := owner.c.functionRefExactType(uint32(index))
	if err != nil {
		return ValueTypeDescriptor{}, nil, false
	}
	return exact, owner.c.Types, true
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
			return 0, fmt.Errorf("create reference token: %w", err)
		}
		_, gcExists := s.gcByToken[token]
		if token>>32 != 0 && s.byToken[token] == nil && !gcExists {
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

type referenceTokenEntries struct {
	funcrefs []*funcrefTokenEntry
	gcRefs   []gcRefTokenEntry
}

func (s *referenceStore) releaseEntriesLocked() referenceTokenEntries {
	var entries referenceTokenEntries
	if len(s.byToken) != 0 {
		entries.funcrefs = make([]*funcrefTokenEntry, 0, len(s.byToken))
		for _, entry := range s.byToken {
			entries.funcrefs = append(entries.funcrefs, entry)
		}
	}
	if len(s.gcByToken) != 0 {
		entries.gcRefs = make([]gcRefTokenEntry, 0, len(s.gcByToken))
		for _, entry := range s.gcByToken {
			entries.gcRefs = append(entries.gcRefs, entry)
		}
	}
	s.byIdentity = nil
	s.byToken = nil
	s.gcByToken = nil
	clear(s.externrefs)
	s.externrefs = nil
	s.externKey = 0
	s.externSeed = 0
	return entries
}

func releaseReferenceEntries(entries referenceTokenEntries) {
	for _, entry := range entries.funcrefs {
		if hostOwner := entry.owner.hostFuncRefForDescriptor(entry.descriptor); hostOwner != nil {
			hostOwner.tokenReleased(entry.owner, entry.descriptor)
		}
		entry.owner.releaseResourceRoot()
	}
	for _, entry := range entries.gcRefs {
		state := entry.owner.existingPublicGCState()
		if state != nil {
			state.mu.Lock()
			if state.token == entry.token && state.slotCreated && state.slot == entry.slot {
				if entry.owner.gc != nil {
					_ = entry.owner.gc.SetGlobalSlot(entry.slot, gc.Null())
				}
				state.token = 0
			}
			state.mu.Unlock()
		}
		entry.owner.releaseResourceRoot()
	}
}

func (s *referenceStore) canonicalFuncrefOwnerLocked(source *Instance, descriptor uint64) (*Instance, uint64, bool) {
	if fidx, ok := source.funcrefDescriptorIndex(descriptor); ok {
		if fidx >= source.c.NumImports {
			_, registered := s.instances[source]
			return source, descriptor, registered
		}
		if fidx >= len(source.c.Imports) || fidx >= len(source.c.importFuncSigs) {
			return nil, 0, false
		}
		key := source.c.Imports[fidx]
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		refSlot := binary.LittleEndian.Uint64(source.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
		if ex, ok := source.imports[key].(*InstanceExport); ok {
			if ex == nil || ex.inst == nil || ex.inst.refStore != s || ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
				return nil, 0, false
			}
			entry := source.funcRefDescs[off : off+coreruntime.FuncRefDescBytes]
			expectedCode := uint64(ex.inst.base) + uint64(ex.inst.c.Entry[ex.localIdx])
			home := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:])
			home &^= abi.FuncRefInternalHomeTag | abi.FuncRefCrossInstanceHomeTag | abi.FuncRefLocalWrapperHomeTag
			if binary.LittleEndian.Uint64(entry[coreruntime.TableEntryCodePtrOffset:]) != expectedCode ||
				home != uint64(ex.inst.jm.LinMemBase()) ||
				binary.LittleEndian.Uint64(entry[coreruntime.TableEntrySigKeyOffset:]) != source.c.funcTypeKey(fidx) ||
				binary.LittleEndian.Uint64(entry[coreruntime.FuncRefContextOffset:]) != uint64(ex.inst.nativeContext) {
				return nil, 0, false
			}
			canonical, hasCanonical := ex.inst.localFuncrefDescriptor(ex.localIdx)
			if !hasCanonical {
				// The producer never needed a descriptor arena itself. The importer's
				// exact proxy becomes the store identity; token retention keeps this
				// importer physically live, and its function attachment retains the
				// producer code/context until the token is released.
				if refSlot != descriptor {
					return nil, 0, false
				}
				_, registered := s.instances[source]
				return source, descriptor, registered
			}
			if refSlot != canonical {
				return nil, 0, false
			}
			if s.byIdentity[funcrefIdentity{descriptor: canonical}] != nil {
				return ex.inst, canonical, true
			}
			_, registered := s.instances[ex.inst]
			return ex.inst, canonical, registered
		}
		hostOwner, ok := source.imports[key].(*HostFuncRef)
		if !ok || hostOwner == nil || hostOwner.store != s || refSlot != descriptor {
			return nil, 0, false
		}
		entry := source.funcRefDescs[off : off+coreruntime.TableEntryBytes]
		home := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:])
		home &^= abi.FuncRefInternalHomeTag | abi.FuncRefCrossInstanceHomeTag | abi.FuncRefLocalWrapperHomeTag
		if binary.LittleEndian.Uint64(entry[coreruntime.TableEntryCodePtrOffset:]) == 0 ||
			home != uint64(source.jm.LinMemBase()) ||
			binary.LittleEndian.Uint64(entry[coreruntime.TableEntrySigKeyOffset:]) != source.c.funcTypeKey(fidx) {
			return nil, 0, false
		}
		return hostOwner.canonicalDescriptor(source, descriptor, source.c.importFuncSigs[fidx])
	}
	for candidate := range s.instances {
		if candidate.ownsLocalFuncrefDescriptor(descriptor) {
			return candidate, descriptor, true
		}
	}
	return nil, 0, false
}

func (in *Instance) funcrefDescriptorIndex(descriptor uint64) (int, bool) {
	if len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
		return 0, false
	}
	base := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0])))
	if descriptor < base+coreruntime.FuncRefDescBytes || descriptor >= base+uint64(len(in.funcRefDescs)) {
		return 0, false
	}
	delta := descriptor - base
	if delta%coreruntime.FuncRefDescBytes != 0 {
		return 0, false
	}
	funcIndex := int(delta/coreruntime.FuncRefDescBytes) - 1
	return funcIndex, funcIndex >= 0 && funcIndex < len(in.c.FuncTypeID)
}

func (in *Instance) funcrefFunctionIdentity(descriptor uint64) (funcrefIdentity, bool) {
	fidx, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok {
		return funcrefIdentity{}, false
	}
	if fidx >= in.c.NumImports {
		return funcrefIdentity{instance: in, localIdx: fidx - in.c.NumImports}, true
	}
	if fidx >= len(in.c.Imports) {
		return funcrefIdentity{}, false
	}
	export, ok := in.imports[in.c.Imports[fidx]].(*InstanceExport)
	if !ok || export == nil || export.inst == nil || export.localIdx < 0 {
		return funcrefIdentity{}, false
	}
	return funcrefIdentity{instance: export.inst, localIdx: export.localIdx}, true
}

func (in *Instance) ownsLocalFuncrefDescriptor(descriptor uint64) bool {
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	return ok && funcIndex >= in.c.NumImports
}

// reachesFuncrefDescriptor reports whether descriptor is represented in this
// instance's function-index descriptor space. Imported InstanceExport entries
// may reuse a producer's canonical refSlot, while bare producers and HostFuncRef
// bindings use importer-owned proxy slots. Retaining this instance preserves the
// already-established function/host attachment chain for every form.
func (in *Instance) reachesFuncrefDescriptor(descriptor uint64) bool {
	if in == nil || descriptor == 0 || len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
		return false
	}
	for fidx := 0; fidx < len(in.c.FuncTypeID); fidx++ {
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
			return false
		}
		if binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]) == descriptor {
			return true
		}
	}
	return false
}

func (in *Instance) hostFuncRefForDescriptor(descriptor uint64) *HostFuncRef {
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok || funcIndex < 0 || funcIndex >= in.c.NumImports || funcIndex >= len(in.c.Imports) {
		return nil
	}
	owner, _ := in.imports[in.c.Imports[funcIndex]].(*HostFuncRef)
	return owner
}

func (in *Instance) localFuncrefDescriptor(localIdx int) (uint64, bool) {
	fidx := in.c.NumImports + localIdx
	if localIdx < 0 || fidx < in.c.NumImports || fidx >= len(in.c.FuncTypeID) || len(in.funcRefDescs) == 0 {
		return 0, false
	}
	off := (fidx + 1) * coreruntime.FuncRefDescBytes
	if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
		return 0, false
	}
	return uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[off]))), true
}

func (in *Instance) funcrefStoreForEgress() (*referenceStore, error) {
	return in.referenceStoreForBoundary()
}

// FuncRefMatchesFunction reports whether ref has the canonical identity of the
// function at index in this instance's Wasm function index space. It compares
// descriptor identity rather than opaque public token bits, so imported aliases
// and cross-instance references remain stable across store tokenization.
func (in *Instance) FuncRefMatchesFunction(ref FuncRef, index uint32) bool {
	if in == nil || ref.token == 0 {
		return false
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed || in.refStore == nil || int(index) >= len(in.c.FuncTypeID) {
		return false
	}
	descriptor, ok := in.refStore.resolve(ref.token)
	if !ok || descriptor == 0 {
		return false
	}
	actual := unsafe.Slice((*byte)(offHeapPtr(uintptr(descriptor))), coreruntime.TableEntryBytes)
	identity := binary.LittleEndian.Uint64(actual[coreruntime.TableEntryRefSlotOffset:])
	off := (int(index) + 1) * coreruntime.FuncRefDescBytes
	if identity == 0 || off < coreruntime.FuncRefDescBytes || off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
		return false
	}
	expected := binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
	return expected != 0 && identity == expected
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
	store := in.refStore
	in.lifeMu.Unlock()
	if shouldRelease {
		if store != nil {
			store.resourceOwnerReleased(in)
		}
		in.releaseResources()
	}
}

func (in *Instance) hasResourceRoots() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return in.resourceRefs != 0
}

func (in *Instance) hasPhysicalResources() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return !in.resourcesClosed
}
