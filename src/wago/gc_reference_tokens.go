// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (s *referenceStore) issueGCRef(source *Instance, ref gc.Ref, required ValueTypeDescriptor) (uint64, error) {
	return s.issueGCRefMode(source, ref, required, false)
}

// Callback arguments may originate in a logically closed native producer held
// by the current invocation. The callback generation and physical root retain
// keep this narrow publication path separate from public result publication.
func (s *referenceStore) issueCallbackGCRef(caller instanceHostModule, ref gc.Ref, required ValueTypeDescriptor) (uint64, error) {
	if caller.scope == nil || caller.generation == 0 || !caller.valid() || caller.in == nil || caller.in.refStore != s {
		return 0, fmt.Errorf("GC callback reference is outside its active host scope")
	}
	return s.issueGCRefMode(caller.in, ref, required, true)
}

func (s *referenceStore) issueGCRefMode(source *Instance, ref gc.Ref, required ValueTypeDescriptor, retained bool) (uint64, error) {
	if !collectorObjectTokensBuilt || source == nil || ref.IsNull() || !ref.IsObj() {
		return 0, fmt.Errorf("invalid non-null GC result")
	}
	if source.c == nil {
		return 0, fmt.Errorf("public GC result ownership is outside exact collector execution")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := source.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := source.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	return s.issueGCRefLockedMode(source, state, ref, required, retained)
}

// issueGCRefLocked publishes a retained public token while the caller holds
// native execution, the collector domain, and state.mu, in that order.
func (s *referenceStore) issueGCRefLocked(source *Instance, state *gcPublicState, ref gc.Ref, required ValueTypeDescriptor) (uint64, error) {
	return s.issueGCRefLockedMode(source, state, ref, required, false)
}

func (s *referenceStore) issueGCRefLockedMode(source *Instance, state *gcPublicState, ref gc.Ref, required ValueTypeDescriptor, retained bool) (uint64, error) {
	if !collectorObjectTokensBuilt || source == nil || ref.IsNull() || !ref.IsObj() {
		return 0, fmt.Errorf("invalid non-null GC result")
	}
	if source.c == nil {
		return 0, fmt.Errorf("public GC result ownership is outside exact collector execution")
	}
	var admittedArray, legacyStruct bool
	if wasmGCBuilt {
		arrayProduct := source.c.stagedGCArrayProduct()
		admittedArray = arrayProduct == stagedGCArrayProductNumericDefault || arrayProduct == stagedGCArrayProductNumericFixed ||
			arrayProduct == stagedGCArrayProductPackedData || arrayProduct == stagedGCArrayProductReferenceElements ||
			arrayProduct == stagedGCArrayProductNewData || arrayProduct == stagedGCArrayProductNewElem ||
			rv64LegacyGCProductsForImage && (rv64GCArrayFixedOfficial(source.c) || rv64GCArrayDefaultOfficial(source.c) || rv64GCArrayPackedData(source.c) || rv64GCArrayReference(source.c))
		legacyStruct = source.c.stagedGCStructProduct() == stagedGCStructBasic || rv64LegacyGCProductsForImage && rv64GCStructBasic(source.c)
	}
	generic := source.c.needsExactNativeGCRoots() && source.c.genericGCFrameRoots() != nil
	if !legacyStruct && !admittedArray && !generic {
		return 0, fmt.Errorf("public GC result ownership is outside exact collector execution")
	}
	slots := state.resultSlots()
	ownerIndex := slots.nextResultSlot()
	if source.gc == nil {
		return 0, fmt.Errorf("public GC result has no live collector")
	}
	typeID, err := source.gc.ObjectType(ref)
	if err != nil {
		return 0, fmt.Errorf("public GC result object %#x: %w", uint32(ref), err)
	}
	localType, ok := source.gcLocalType(typeID)
	exact, ownedException := ownedExceptionExactValueType(source.gc, ref, typeID)
	if !ownedException {
		if !ok || int(localType) >= len(source.c.Types) {
			return 0, fmt.Errorf("public GC result canonical type %d has no producer-local identity", typeID)
		}
		kind := source.c.Types[localType].Kind
		if legacyStruct && kind != CompositeTypeStruct {
			return 0, fmt.Errorf("public GC result type %d is not a struct", typeID)
		}
		if admittedArray && !generic && kind != CompositeTypeArray {
			return 0, fmt.Errorf("public GC result type %d is not an array", typeID)
		}
		if generic && kind != CompositeTypeStruct && kind != CompositeTypeArray {
			return 0, fmt.Errorf("public GC result type %d is not a struct or array", typeID)
		}
		exact = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: localType}}}
	}
	if required.Kind != ValueTypeReference || !source.gcRefMatchesValueType(ref, required) {
		return 0, fmt.Errorf("public GC result type %d does not match its exact structural result type", localType)
	}

	s.mu.Lock()
	_, registered := s.instances[source]
	s.mu.Unlock()
	if !registered || !source.retainResourceRootMode(retained) {
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	rollbackRoot := true
	defer func() {
		if rollbackRoot {
			source.releaseResourceRoot()
		}
	}()

	slot, err := slots.root(source.gc, ref, ownerIndex)
	if err != nil {
		return 0, fmt.Errorf("root public GC result: %w", err)
	}

	s.mu.Lock()
	if _, registered = s.instances[source]; !registered {
		s.mu.Unlock()
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	token, err := s.newTokenLocked()
	if err == nil {
		s.insertObjectTokenLocked(gcRefTokenEntry{
			gcResultRoot: gcResultRoot{token: token, slot: slot, ownerIndex: ownerIndex, owner: source},
			ref:          ref, exact: exact, domainType: typeID,
		})
		slots.publish(ownerIndex, token)
	}
	s.mu.Unlock()
	if err != nil {
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, err
	}
	rollbackRoot = false
	return token, nil
}

func (s *referenceStore) releaseGCRef(source *Instance, token uint64) error {
	if token == 0 {
		return nil
	}
	if !collectorObjectTokensBuilt {
		return fmt.Errorf("invalid or stale GC reference token")
	}
	s.mu.Lock()
	entry, ok := s.objectTokensLocked()[token]
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
	unlockNative := lockNativeExecutionForHostAccess()
	lockedDomain := source.lockGCCollector()
	state.mu.Lock()
	s.mu.Lock()
	entry, ok = s.objectTokensLocked()[token]
	ownerIndex := entry.ownerIndex
	slots := state.resultSlots()
	if !ok || entry.owner != source || slots == nil || slots.resultTokenCount == 0 || !slots.matches(ownerIndex, token, entry.slot) {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source.gc == nil {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("GC reference token collector is unavailable")
	}
	if err := source.gc.SetGlobalSlot(entry.slot, gc.Null()); err != nil {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("release GC reference token: %w", err)
	}
	delete(s.objectTokensLocked(), token)
	slots.clear(ownerIndex)
	s.mu.Unlock()
	state.mu.Unlock()
	unlockGCCollector(lockedDomain)
	unlockNative()
	source.releaseResourceRoot()
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && !s.hasObjectTokensLocked() {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	if topology != nil {
		topology.gate.Unlock()
	}
	releaseReferenceEntries(release)
	return nil
}

func (s *referenceStore) gcRefExactType(token uint64) (ValueTypeDescriptor, *Instance, uint32, bool) {
	s.mu.Lock()
	entry, ok := s.objectTokensLocked()[token]
	s.mu.Unlock()
	if !ok {
		return ValueTypeDescriptor{}, nil, 0, false
	}
	return entry.exact, entry.owner, entry.slot, true
}

func lockGCPublicStates(a, b *gcPublicState) (first, second *gcPublicState) {
	if a == b {
		a.mu.Lock()
		return a, nil
	}
	first, second = a, b
	if uintptr(unsafe.Pointer(first)) > uintptr(unsafe.Pointer(second)) {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return first, second
}

func unlockGCPublicStates(first, second *gcPublicState) {
	if second != nil {
		second.mu.Unlock()
	}
	first.mu.Unlock()
}

func (s *referenceStore) stageGCRefArgument(target *Instance, token uint64, required ValueTypeDescriptor) (gc.Ref, error) {
	if !collectorObjectTokensBuilt || s == nil || target == nil || target.gc == nil || target.c == nil || token == 0 {
		return gc.Null(), fmt.Errorf("invalid GC reference token")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	s.mu.Lock()
	entry, ok := s.objectTokensLocked()[token]
	s.mu.Unlock()
	if !ok || entry.owner == nil {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	owner := entry.owner
	ownerState := owner.existingPublicGCState()
	if ownerState == nil {
		return gc.Null(), fmt.Errorf("GC reference token owner state is unavailable")
	}
	targetState := target.publicGCState()
	firstState, secondState := lockGCPublicStates(ownerState, targetState)
	defer unlockGCPublicStates(firstState, secondState)

	s.mu.Lock()
	current, ok := s.objectTokensLocked()[token]
	ownerRecord := s.instances[owner]
	targetRecord := s.instances[target]
	s.mu.Unlock()
	ownerIndex := current.ownerIndex
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || !ownerState.resultSlots().matches(ownerIndex, token, entry.slot) {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC reference token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC reference token does not match the required structural argument type")
	}
	if err := targetState.argumentRoots().stage(target.gc, current.ref); err != nil {
		return gc.Null(), fmt.Errorf("root GC reference argument: %w", err)
	}
	return current.ref, nil
}

// publishGCRefGlobal authenticates one public token and installs its compact
// reference into an existing checked global-root slot. The caller holds native
// execution, which prevents token retirement between authentication and the
// subsequent native-cell write.
func (s *referenceStore) publishGCRefGlobal(target *Instance, token uint64, required ValueTypeDescriptor, slot uint32) (gc.Ref, error) {
	if !collectorObjectTokensBuilt || s == nil || target == nil || target.gc == nil || target.c == nil || token == 0 {
		return gc.Null(), fmt.Errorf("invalid GC reference token")
	}
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	s.mu.Lock()
	entry, ok := s.objectTokensLocked()[token]
	s.mu.Unlock()
	if !ok || entry.owner == nil {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	owner := entry.owner
	ownerState := owner.existingPublicGCState()
	if ownerState == nil {
		return gc.Null(), fmt.Errorf("GC reference token owner state is unavailable")
	}
	targetState := target.publicGCState()
	firstState, secondState := lockGCPublicStates(ownerState, targetState)
	defer unlockGCPublicStates(firstState, secondState)

	s.mu.Lock()
	current, ok := s.objectTokensLocked()[token]
	ownerRecord := s.instances[owner]
	targetRecord := s.instances[target]
	s.mu.Unlock()
	ownerIndex := current.ownerIndex
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || !ownerState.resultSlots().matches(ownerIndex, token, entry.slot) {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC reference token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC reference token does not match the required structural global type")
	}
	if err := target.gc.SetGlobalSlot(slot, current.ref); err != nil {
		return gc.Null(), fmt.Errorf("root GC reference global: %w", err)
	}
	return current.ref, nil
}

func (in *Instance) rootGCHostArguments(token gcHostActivationToken, dispatch uint32, args []uint64) error {
	if !wasmGCBuilt {
		return nil
	}
	state, index := gcHostActivationRootState(token)
	if in == nil || state == nil || in.gc == nil || in.refStore == nil {
		return nil
	}
	var types []ValType
	if dispatch&hostFuncRefDispatchBit != 0 {
		binding, ok := in.boundHostFuncRef(dispatch)
		if !ok {
			return fmt.Errorf("GC host argument owner is unavailable")
		}
		owner := binding.owner
		owner.mu.Lock()
		collector, domain := owner.binding.gcDomain()
		if !owner.allowsGCBridge() || owner.closed || collector != in.gc || domain == 0 {
			owner.mu.Unlock()
			return fmt.Errorf("GC host argument owner is outside the active collector domain")
		}
		owner.mu.Unlock()
		types = binding.sig.Params
	} else {
		pluginSig, ok := in.pluginGCHostSignature(dispatch)
		if !ok {
			return nil
		}
		if !in.refStore.ownsGCCollector(in.gc) {
			return fmt.Errorf("Runtime plugin GC host import is outside the calling instance's Runtime collector domain")
		}
		types = pluginSig.Params
	}

	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			slot += 2
			continue
		}
		if slot >= len(args) {
			return fmt.Errorf("missing GC host argument slot %d", slot)
		}
		if typ == ValAnyRef || typ == ValI31Ref {
			bits := args[slot]
			ref := gc.Ref(uint32(bits))
			if bits != 0 && (uint64(ref) != bits || (!ref.IsObj() && !ref.IsI31())) {
				return fmt.Errorf("invalid compact GC host argument %d", i)
			}
			if ref.IsObj() {
				roots := &state.ensureParkedRoots(index).arguments
				if int(roots.count) >= len(roots.slots) {
					return fmt.Errorf("GC host object argument count exceeds %d", len(roots.slots))
				}
				if err := roots.add(in.gc, ref, gc.RootPublicToken); err != nil {
					return fmt.Errorf("root GC host argument %d: %w", i, err)
				}
			}
		}
		slot++
	}
	return nil
}

func (s *referenceStore) stageGCHostResult(target *Instance, parked gcHostActivationToken, token uint64, required ValueTypeDescriptor) (gc.Ref, error) {
	parkedState, activation := gcHostActivationRootState(parked)
	if !collectorObjectTokensBuilt || s == nil || target == nil || target.gc == nil || target.c == nil || parkedState == nil || token == 0 {
		return gc.Null(), fmt.Errorf("invalid GC host result token")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	s.mu.Lock()
	entry, ok := s.objectTokensLocked()[token]
	s.mu.Unlock()
	if !ok || entry.owner == nil {
		return gc.Null(), fmt.Errorf("invalid or stale GC host result token")
	}
	owner := entry.owner
	ownerState := owner.existingPublicGCState()
	if ownerState == nil {
		return gc.Null(), fmt.Errorf("GC host result token owner state is unavailable")
	}
	targetState := target.publicGCState()
	firstState, secondState := lockGCPublicStates(ownerState, targetState)
	defer unlockGCPublicStates(firstState, secondState)

	s.mu.Lock()
	current, ok := s.objectTokensLocked()[token]
	ownerRecord := s.instances[owner]
	targetRecord := s.instances[target]
	s.mu.Unlock()
	ownerIndex := current.ownerIndex
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || !ownerState.resultSlots().matches(ownerIndex, token, entry.slot) {
		return gc.Null(), fmt.Errorf("invalid or stale GC host result token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC host result token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC host result token does not match the required structural result type")
	}
	// Multiple public roots can park on the same producer control frame. The
	// dispatch token identifies this callback's exact slot, including holes
	// reused after another root resumes out of suspension order.
	if parkedState != targetState || !targetState.hasGCHostActivation(activation) {
		return gc.Null(), fmt.Errorf("GC host result has no active parked frame")
	}
	roots := &targetState.ensureParkedRoots(activation).results
	if int(roots.count) >= len(roots.slots) {
		return gc.Null(), fmt.Errorf("GC host result count exceeds %d", len(roots.slots))
	}
	if err := roots.add(target.gc, current.ref, gc.RootForeignInstance); err != nil {
		return gc.Null(), fmt.Errorf("root GC host result: %w", err)
	}
	return current.ref, nil
}

func (in *Instance) clearGCHostResultRoots(token gcHostActivationToken) {
	if !collectorObjectTokensBuilt {
		return
	}
	state, index := gcHostActivationRootState(token)
	if in == nil || state == nil || in.gc == nil || int(index) >= gcHostActivationLimit {
		return
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	roots := state.parkedRoots(index)
	if roots == nil {
		return
	}
	if i, err := roots.arguments.clear(in.gc); err != nil {
		panic(gcHelperFailuref("clear GC host argument root %d: %w", i, err))
	}
	if i, err := roots.results.clear(in.gc); err != nil {
		panic(gcHelperFailuref("clear GC host result root %d: %w", i, err))
	}
}

func (in *Instance) clearGCRefArgumentRoots() {
	if !collectorObjectTokensBuilt {
		return
	}
	state := in.existingPublicGCState()
	if state == nil || in.gc == nil {
		return
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.argumentRoots().clear(in.gc)
}

// ReleaseGCRef releases one non-null GC result token issued by this producer.
// It is valid after Instance.Close while the token retains the producer's
// collector; null releases are no-ops. Non-null releases fail with
// ErrPermissionDenied while callback-scoped guest storage is borrowed. Stale,
// foreign-store, and cross-producer tokens reject without changing either owner.
func (in *Instance) ReleaseGCRef(ref GCRef) error {
	if ref.token == 0 {
		return nil
	}
	if in == nil {
		return fmt.Errorf("release GC reference token on nil instance")
	}
	if in.guestStorageBorrowed() {
		return fmt.Errorf("release GC reference token is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	in.lifeMu.Lock()
	store := in.refStore
	in.lifeMu.Unlock()
	if store == nil {
		return fmt.Errorf("instance has no GC reference token store")
	}
	if managedFunctionLifetimeBuilt {
		defer drainManagedFunctionStoreReclaims(store)
	}
	return store.releaseGCRef(in, ref.token)
}

// ReleaseExnRef releases an owned exception token issued by this instance.
// It has the same lifetime and ownership checks as ReleaseGCRef.
func (in *Instance) ReleaseExnRef(ref ExnRef) error {
	return in.ReleaseGCRef(GCRef(ref))
}
