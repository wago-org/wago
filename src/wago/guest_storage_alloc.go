package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// GuestGCArrayAllocatorHostModule is the optional host-callback surface for
// allocating an exact caller-selected Wasm GC array result. The resultIndex is
// the host import's result position, not the raw ABI slot position.
//
// initialize runs while the new array is private and rooted. Its byte slice is
// the complete logical payload and is writable even when the caller selected an
// immutable array type. The slice expires when initialize returns. If initialize
// fails, no public result token is created. Wago rejects guest re-entry for the
// duration of this operation.
//
// The returned token is ephemeral to the active host call. Write it to the
// corresponding result slot during that call. Wago releases allocator-created
// result tokens after host-result translation has rooted the object for the
// parked Wasm frame. Hosts MUST NOT retain or reuse the token.
type GuestGCArrayAllocatorHostModule interface {
	HostModule
	NewGCArrayResult(resultIndex int, length uint32, initialize func([]byte, GuestGCArrayInfo) error) (uint64, error)
}

func (h instanceHostModule) NewGCArrayResult(resultIndex int, length uint32, initialize func([]byte, GuestGCArrayInfo) error) (uint64, error) {
	if !h.valid() || h.in == nil {
		return 0, fmt.Errorf("wago: GC result allocation is outside its active host callback: %w", ErrPermissionDenied)
	}
	if resultIndex < 0 || resultIndex >= len(h.exactResults) {
		return 0, fmt.Errorf("wago: host result index %d is out of range", resultIndex)
	}
	if h.ephemeralGCResults == nil {
		return 0, fmt.Errorf("wago: GC result allocation requires the active host dispatch: %w", ErrPermissionDenied)
	}
	required := h.exactResults[resultIndex]
	if required.Kind != ValueTypeReference || !required.Ref.Heap.Defined {
		return 0, fmt.Errorf("wago: host result %d is not a defined GC reference type", resultIndex)
	}
	localType := required.Ref.Heap.TypeIndex
	if h.in.c == nil || int(localType) >= len(h.in.c.Types) || int(localType) >= len(h.in.c.GCTypeDescs) {
		return 0, fmt.Errorf("wago: host result %d references unavailable type %d", resultIndex, localType)
	}
	defined := h.in.c.Types[localType]
	desc := h.in.c.GCTypeDescs[localType]
	if defined.Kind != CompositeTypeArray || desc.Kind != gc.KindArray {
		return 0, fmt.Errorf("wago: host result %d is not an array reference", resultIndex)
	}
	storage, ok := guestArrayStorage(desc.Elem)
	if !ok || storage == GuestGCArrayRef || storage == GuestGCArrayFuncRef || storage == GuestGCArrayExternRef {
		return 0, fmt.Errorf("wago: host result %d is not a raw-payload GC array", resultIndex)
	}
	if h.in.gc == nil {
		return 0, fmt.Errorf("wago: host result %d has no live GC collector", resultIndex)
	}
	if _, err := h.in.referenceStoreForBoundary(); err != nil {
		return 0, fmt.Errorf("wago: host result %d reference store: %w", resultIndex, err)
	}
	domainType, ok := h.in.gcDomainType(localType)
	if !ok {
		return 0, fmt.Errorf("wago: host result type %d has no Runtime-domain identity", localType)
	}

	endBorrow, err := beginGuestStorageBorrow(h.in)
	if err != nil {
		return 0, err
	}
	defer endBorrow()
	unlockNative := h.in.lockInstanceNativeStateForHostAccess()
	defer unlockNative()
	lockedDomain := h.in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := h.in.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := h.in.syncGenericGCGlobalRootsLocked(state); err != nil {
		return 0, err
	}
	state.frameRoots = h.in.gcCollectFrameRoots(state)
	ref, err := h.in.gc.NewArrayDefaultWithRoots(domainType, length, &state.frameRoots)
	if err != nil {
		return 0, fmt.Errorf("wago: allocate host GC array result: %w", err)
	}
	payload, err := h.in.gc.ArrayPayload(ref)
	if err != nil {
		return 0, fmt.Errorf("wago: host GC array result payload: %w", err)
	}
	info := GuestGCArrayInfo{Storage: storage, Length: length, Mutable: defined.Array.Mutable, TypeIndex: localType}
	if initialize != nil {
		if err := initialize(payload.Bytes, info); err != nil {
			return 0, err
		}
	}
	token, err := issueHostGCResultLocked(h.in, state, ref, required, localType, domainType)
	if err != nil {
		return 0, err
	}
	temps := h.ephemeralGCResults
	temps.setToken(temps.count, token)
	temps.count++
	return token, nil
}

// issueHostGCResultLocked is the callback-allocation counterpart of
// referenceStore.issueGCRef. The caller holds native execution, the collector
// domain, and state.mu so a newly allocated object cannot move between
// allocation and publication.
func issueHostGCResultLocked(source *Instance, state *gcPublicState, ref gc.Ref, required ValueTypeDescriptor, localType uint32, domainType gc.TypeID) (uint64, error) {
	if source == nil || source.refStore == nil || source.gc == nil || state == nil || ref.IsNull() || !ref.IsObj() {
		return 0, fmt.Errorf("wago: invalid host GC result")
	}
	if required.Kind != ValueTypeReference || !source.gcRefMatchesValueType(ref, required) {
		return 0, fmt.Errorf("wago: allocated GC result type %d does not match host result type", localType)
	}
	ownerIndex := state.nextResultSlot()

	s := source.refStore
	s.mu.Lock()
	_, registered := s.instances[source]
	s.mu.Unlock()
	if !registered || !source.retainResourceRoot() {
		return 0, fmt.Errorf("wago: public GC result producer is closed")
	}
	rootRetained := true
	defer func() {
		if rootRetained {
			source.releaseResourceRoot()
		}
	}()

	var slot uint32
	if ownerIndex == state.resultRootsMade {
		var err error
		slot, err = source.gc.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
		if err != nil {
			return 0, fmt.Errorf("wago: root host GC result: %w", err)
		}
		state.appendResultRootSlot(slot)
	} else {
		slot = state.resultRootSlot(ownerIndex)
		if err := source.gc.SetGlobalSlot(slot, ref); err != nil {
			return 0, fmt.Errorf("wago: root host GC result: %w", err)
		}
	}

	exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Exact: true,
		Heap:  HeapTypeDescriptor{Defined: true, TypeIndex: localType},
	}}
	s.mu.Lock()
	if _, registered = s.instances[source]; !registered {
		s.mu.Unlock()
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, fmt.Errorf("wago: public GC result producer is closed")
	}
	token, err := s.newTokenLocked()
	if err == nil {
		if s.gcByToken == nil {
			s.gcByToken = make(map[uint64]gcRefTokenEntry)
		}
		s.gcByToken[token] = gcRefTokenEntry{
			token: token, ref: ref, slot: slot, ownerIndex: ownerIndex,
			exact: exact, domainType: domainType, owner: source,
		}
		state.setResultToken(ownerIndex, token)
		state.resultTokenCount++
	}
	s.mu.Unlock()
	if err != nil {
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, err
	}
	rootRetained = false
	return token, nil
}
