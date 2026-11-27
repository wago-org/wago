// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc && (amd64 || arm64)

package wago

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

type preparedGCScalarState struct {
	gcPrepared         bool
	gcScalar32         bool
	gcScalar32Plain    bool
	gcPrivateContext   bool
	gcRevocableContext bool
	gcBoundarySafe     bool
	gcHasRefGlobals    bool
}

func (fn *WasmFunc) prepareGCScalar() {
	in := fn.in
	fn.gcBoundarySafe = in.c.genericGCBoundaryCollectionSafe()
	fn.gcHasRefGlobals = in.c.hasGCRefGlobals()
	fn.gcPrepared = in.gc != nil && fn.scalarFast && in.c.NumImports == 0 && !in.c.usesAtomicWaitHelpers()
	fn.gcScalar32 = fn.gcPrepared && fn.gcMaintenance && in.syncMode && fn.paramSlots == 1 && fn.resultSlots == 1 && fn.scalarWideMask == 0 && !fn.scalarResultWide
	// The collector/config and compiled root plan are fixed before publication.
	// Profiling builds retain the live inherited-profile and maintenance path.
	fn.gcScalar32Plain = fn.gcScalar32 && !codeProfileEnabled && !fn.gcHasRefGlobals && len(in.hostLog) == 0 &&
		(!fn.gcBoundarySafe || in.c.genericGCFrameRoots() != nil && in.gc.CollectsOnAllocation())
	// These modules cannot replace resource-context pointers during execution.
	// The instance GC view itself is stable; collector contents remain live.
	fn.gcPrivateContext = fn.gcScalar32Plain && fn.boundedWrapper && in.c.memoryCount() == 0 && !in.c.HasTable && len(in.c.Globals) == 0
	// Preserve direct numeric ABI priority when GC types are otherwise unused.
	fn.gcRevocableContext = fn.gcPrivateContext && in.rt == nil && !fn.directIntFast && !fn.directFloatFast && fn.directMixedInfo == 0 && gcPrivateNumericSignatures(in.c)
}

func (fn *WasmFunc) hasPreparedGCScalar() bool   { return fn.gcPrepared }
func (fn *WasmFunc) hasPrivateGCContext() bool   { return fn.gcPrivateContext }
func (fn *WasmFunc) hasRevocableGCContext() bool { return fn.gcRevocableContext }

// callGCPrepared runs under the ordinary invocation and GC domain leases. It
// caches validated buffer/entry metadata. Certified private entries reuse
// unchanged bindings; each helper return retains full GC rebinding.
func (fn *WasmFunc) callGCPrepared() (err error) {
	in := fn.in
	locked, reuse, err := fn.beginNativeEntry()
	if err != nil {
		return err
	}
	defer in.unlockNativeEntry(locked)
	defer in.recoverBoundedNativeSyncPanic(&err)
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	base := in.jm.LinMemBase()
	if fn.hostPrepared == nil || fn.hostMemBase != base {
		fn.hostPrepared, err = in.eng.PrepareHostScalarCall(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm, in.trap, in.results, in.ctrl)
		if err != nil {
			return err
		}
		fn.hostMemBase = base
	} else if !reuse {
		if err = in.jm.RebindTrapCell(in.trap); err != nil {
			return err
		}
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
	}
	activation := hostLoopActivation{
		root: in, ctrl: offHeapSlicePtr(in.ctrl), state: in.ensurePluginState(), entryNativeMu: locked.local,
	}
	if fn.boundedWrapper {
		// Only compiler-proven bounded entries reach this path. Preserve the
		// safepoint bits for the ordinary GC dispatcher while checking tags,
		// helpers 42/43, root frame, and the two-argument/no-result ABI.
		mask := ^uint32((shared.GCDispatchPayloadMask &^ shared.GCHelperIDMask) | 1)
		err = fn.hostPrepared.CallBoundedHelper(runtimebridge.GrantHostScalarCall(), mask, gcStructDispatchBit|gcStructReserveDeadBatch, activation.dispatch)
	} else {
		err = fn.hostPrepared.Call(activation.dispatch, profileScalarFallback)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}

type preparedGCInvocationLease struct {
	preparedInvocationLease
	combined bool
}

// beginPreparedGCInvocation fills a zero-initialized lease. Ordinary combined
// admission preserves the existing lifetime and gate protocol; Runtime-owned
// instances and contention retain separate admission.
func (in *Instance) beginPreparedGCInvocation(l *preparedGCInvocationLease) error {
	state := in.pluginState.Load()
	combined := in.tryBeginOrdinaryInvocation(state)
	if combined && in.guestStorageBorrowed() {
		in.endOrdinaryInvocation(state)
		combined = false
	}
	if !combined {
		if err := in.beginDirectInvocation(); err != nil {
			return err
		}
		state = in.ensurePluginState()
		state.invokeMu.Lock()
	}
	l.in, l.state, l.combined = in, state, combined
	id := newInvocationID()
	state.invocationID = id
	if in.mayNeedGCInvocationLease() {
		l.gc = in.lockGCInvocation(id)
	}
	return nil
}

func (l *preparedGCInvocationLease) unlockGCInvocation() {
	if !l.combined {
		defer l.in.endDirectInvocation()
		l.unlockScalarInvocation()
		return
	}
	defer l.in.endOrdinaryInvocation(l.state)
	if l.gc.isAcquired() {
		l.gc.unlock()
	}
	if l.in.importsFuncrefStorage() || l.in.table != nil {
		l.in.reconcileFuncrefRoots()
	}
	l.state.invocationID = 0
}

// invokeGCScalar skips the generic scalar fast-path checks for a handle that
// already requires the GC boundary. Admission and collector leases are the
// same as invokeScalar; all boundary maintenance remains in the general body.
func (fn *WasmFunc) invokeGCScalar(args []uint64) ([]uint64, error) {
	var lease preparedGCInvocationLease
	if err := fn.in.beginPreparedGCInvocation(&lease); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	defer lease.unlockGCInvocation()
	if fn.gcScalar32Plain {
		in := fn.in
		binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(args[0])))
		if err := fn.callGCPrepared(); err != nil {
			return nil, err
		}
		// callGCPrepared keeps both instance and compiled code alive through
		// native execution. The result remains instance-owned like general Invoke.
		out := in.resultVals[:1]
		out[0] = uint64(binary.LittleEndian.Uint32(in.results))
		return out, nil
	}
	if fn.gcScalar32 {
		return fn.invokeGCScalar32Admitted(args[0])
	}
	return fn.invokeGeneralAdmitted(args)
}

// The handle has a single numeric 32-bit parameter/result and a synchronous GC
// entry. The ordinary invocation and collector leases are already held.
func (fn *WasmFunc) invokeGCScalar32Admitted(arg uint64) ([]uint64, error) {
	in := fn.in
	if codeProfileEnabled && (in.boundaryProfile() != nil || activeHostInvocationContext(in).profileSession() != nil) {
		return fn.invokeGeneralAdmitted([]uint64{arg})
	}
	// Module shape is immutable. Keep the collector policy and root-plan check
	// live so collection eligibility remains the same as the general boundary.
	if fn.gcBoundarySafe && (in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation()) {
		if err := in.collectGC(); err != nil {
			return nil, err
		}
	}
	binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(arg)))
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if err := fn.callGCPrepared(); err != nil {
		return nil, err
	}
	if fn.gcHasRefGlobals {
		if err := in.reconcileGCGlobalRoots(); err != nil {
			return nil, err
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:1]
	out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	return out, nil
}

// Private GC entry has no public reference values or arbitrary host callbacks.
// External collector/context access revokes the fast gate before mutation.
func gcPrivateNumericSignatures(c *Compiled) bool {
	for _, typ := range c.Types {
		if typ.Kind != CompositeTypeFunction {
			continue
		}
		for _, v := range typ.Params {
			if v.Kind == ValueTypeReference {
				return false
			}
		}
		for _, v := range typ.Results {
			if v.Kind == ValueTypeReference {
				return false
			}
		}
	}
	return true
}

func (fn *WasmFunc) tryBeginPrivateGCState() *instancePluginState {
	if !fn.gcRevocableContext {
		return nil
	}
	in := fn.in
	state := in.pluginState.Load()
	if state == nil || state.invokeMu.state.shared != &in.invocationState.word || !in.invocationState.word.CompareAndSwap(0, instanceFastAdmission) {
		return nil
	}
	if !in.privateGCStateValid(state) ||
		!fn.hasPreparedDeadInduction() && !fn.privateGCNativeContextValid(state) {
		in.endFastInvocation(state)
		return nil
	}
	return state
}

func (fn *WasmFunc) tryInvokeRevocableGCScalar(arg uint64) ([]uint64, error, bool) {
	state := fn.tryBeginPrivateGCState()
	if state == nil {
		return nil, nil, false
	}
	if fn.hasPreparedDeadInduction() {
		if out, err, handled := fn.tryInvokePreparedDeadInduction(arg, state); handled {
			return out, err, true
		}
	}
	// A failed charge leaves admission held. The ordinary driver owns cleanup.
	out, err := fn.invokePrivateGCScalar(arg, state)
	return out, err, true
}

// The plugin state is published once. Check its live borrow bit and one
// consistent snapshot of execution flags after acquiring the shared gate.
func (in *Instance) privateGCStateValid(state *instancePluginState) bool {
	return in.refStore == nil && state.guestStorageBorrow.Load() == 0 &&
		in.executionFlags.Load()&(executionFlagIndependent|preparedFastBlocked) == executionFlagIndependent
}

// The Go-only induction charge needs collector ownership but no native binding.
// Check the cached native context only when execution must use the wrapper.
func (fn *WasmFunc) privateGCNativeContextValid(state *instancePluginState) bool {
	version := state.nativeContextVersion.Load()
	return fn.hostPrepared != nil && version != 0 && version != ^uint64(0) && fn.hostContextVersion == version && fn.hostMemBase == fn.in.jm.LinMemBase()
}

func (in *Instance) endPrivateGCInvocation(state *instancePluginState) {
	state.invocationID = 0
	in.endFastInvocation(state)
}

// The revocable gate proves the warmed private context remains exclusively
// owned through native execution and every internal collecting helper.
func (fn *WasmFunc) invokePrivateGCScalar(arg uint64, state *instancePluginState) (out []uint64, err error) {
	in := fn.in
	defer in.endPrivateGCInvocation(state)
	if err = validateNativeGCEntry(in); err != nil {
		return nil, err
	}
	return fn.invokePrivateGCScalarSlow(arg, state)
}

func (fn *WasmFunc) invokePrivateGCScalarSlow(arg uint64, state *instancePluginState) (out []uint64, err error) {
	in := fn.in
	defer in.recoverBoundedNativeSyncPanic(&err)
	// Only helper dispatch can expose an invocation identity or host parent.
	// The successful induction reduction cannot invoke helpers or host code.
	state.invocationID = newInvocationID()
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}

	binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(arg)))
	if !fn.privateGCNativeContextValid(state) {
		err = fn.callGCPrepared()
	} else {
		activation := hostLoopActivation{root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state}
		mask := ^uint32((shared.GCDispatchPayloadMask &^ shared.GCHelperIDMask) | 1)
		err = fn.hostPrepared.CallBoundedHelper(runtimebridge.GrantHostScalarCall(), mask, gcStructDispatchBit|gcStructReserveDeadBatch, activation.dispatch)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err
	}
	out = in.resultVals[:1]
	out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	return out, nil
}
