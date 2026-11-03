// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	goruntime "runtime"
)

// publishPublicGCScalarCertificate records immutable export, signature and
// entry facts after the ordinary Invoke cache has checked them. The live call
// still acquires the ordinary lifetime/gate and collector topology leases.
func (in *Instance) publishPublicGCScalarCertificate(ic *invokeCache, sig FuncSig) {
	if in == nil || ic == nil || ic.li < 0 || in.gc == nil || !in.syncMode ||
		in.c == nil || in.c.NumImports != 0 || in.c.usesAtomicWaitHelpers() ||
		len(sig.Params) != 0 || len(sig.Results) != 1 ||
		sig.Results[0] == ValV128 || hasReferenceValType(sig.Results) {
		return
	}
	fn := &WasmFunc{
		in:              in,
		export:          ic.export,
		entry:           in.base + uintptr(in.c.Entry[ic.li]),
		resultSlots:     1,
		resultWide:      append([]bool(nil), ic.slotWide...),
		gcMaintenance:   in.c.genericGCBoundaryCollectionSafe() || in.c.hasGCRefGlobals(),
		gcPrepared:      true,
		gcBoundarySafe:  in.c.genericGCBoundaryCollectionSafe(),
		gcHasRefGlobals: in.c.hasGCRefGlobals(),
		boundedWrapper:  ic.boundedWrapper,
	}
	if roots := in.c.genericGCFrameRoots(); roots != nil && ic.li < len(roots.deadInductionLayouts) && roots.deadInductionLayouts[ic.li] == ^uint64(0) {
		fn.gcLifetimeScalar = true
		fn.boundedWrapper = true
	}
	if !fn.gcMaintenance {
		return
	}
	// The public certificate may use the private gate only when collection is
	// already performed by the allocation helper with an exact native root
	// plan. Certificates that need boundary collection or root reconciliation
	// retain the ordinary checked route.
	fn.configurePrivateGCContext(!codeProfileEnabled && !fn.gcHasRefGlobals && len(in.hostLog) == 0 &&
		(!fn.gcBoundarySafe || in.c.genericGCFrameRoots() != nil && in.gc.CollectsOnAllocation()))
	in.ensurePluginState().numericCacheState().publicGCScalar.Store(fn)
}

func (in *Instance) publicGCScalarCertificate(export string) *WasmFunc {
	state := in.pluginState.Load()
	if state == nil {
		return nil
	}
	cache := state.numericCache.Load()
	if cache == nil {
		return nil
	}
	fn := cache.publicGCScalar.Load()
	if fn == nil || fn.in != in || !sameExportName(fn.export, export) {
		return nil
	}
	return fn
}

// invokePublicGCScalar is the checked public Invoke boundary for the narrow
// cached signature. It specializes orchestration only: execution uses the
// normal safe GC/native trampoline and the same live maintenance operations.
func (fn *WasmFunc) invokePublicGCScalar() (result []uint64, resultErr error) {
	if state := fn.tryBeginPrivateGCState(); state != nil {
		return fn.invokePrivatePublicGCScalar(state)
	}
	in := fn.in
	var lease preparedGCInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		if err := in.beginPreparedGCInvocation(&lease); err != nil {
			resultErr = fmt.Errorf("invoke %q: %w", fn.export, err)
			return
		}
		leaseOwned = true
		result, resultErr = fn.invokePublicGCScalarAdmitted()
	})
	if leaseOwned {
		cleanup.run(lease.unlockGCInvocation)
	}
	cleanup.finish()
	return result, resultErr
}

// invokePrivatePublicGCScalar runs the zero-parameter public certificate under
// the same revocable combined gate as the private one-parameter route. The
// certificate proof excludes boundary collection and post-call global work.
func (fn *WasmFunc) invokePrivatePublicGCScalar(state *instancePluginState) (out []uint64, err error) {
	in := fn.in
	defer in.endPrivateGCInvocation(state)
	if err = validateNativeGCEntry(in); err != nil {
		return nil, err
	}
	defer in.recoverBoundedNativeSyncPanic(&err)
	state.invocationID = newInvocationID()
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	if err = fn.callPrivateGCBounded(state); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out = in.resultVals[:1]
	if fn.resultWide[0] {
		out[0] = binary.LittleEndian.Uint64(in.results)
	} else {
		out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	}
	return out, nil
}

func (fn *WasmFunc) invokePublicGCScalarAdmitted() ([]uint64, error) {
	in := fn.in
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, err
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	executionErr := fn.callGCPrepared()
	if err := in.reconcileGCGlobalRootsAfter(executionErr); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:1]
	if fn.resultWide[0] {
		out[0] = binary.LittleEndian.Uint64(in.results)
	} else {
		out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	}
	return out, nil
}

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
	// Cold and revoked prepared entries must consume pending interruption
	// before a native helper can replace the trap word or charge an object.
	if err = clearPreparedInductionTrap(in); err != nil {
		return err
	}
	if activeHostInvocationBindings.Load() != 0 {
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
		// safepoint bits for the ordinary GC dispatcher while checking the
		// exact helper identity, root frame, and parked signature.
		mask := ^uint32((shared.GCDispatchPayloadMask &^ shared.GCHelperIDMask) | 1)
		if fn.gcLifetimeScalar {
			err = fn.hostPrepared.CallBoundedHelperOne(runtimebridge.GrantHostScalarCall(), mask, fn.boundedGCHelper(), activation.dispatch)
		} else {
			err = fn.hostPrepared.CallBoundedHelper(runtimebridge.GrantHostScalarCall(), mask, fn.boundedGCHelper(), activation.dispatch)
		}
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
	if l.gc.acquired {
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

// configurePrivateGCContext centralizes the immutable module and ownership
// proof used by both prepared handles and the public zero-parameter certificate.
func (fn *WasmFunc) configurePrivateGCContext(entryEligible bool) {
	in := fn.in
	fn.gcPrivateContext = entryEligible && fn.boundedWrapper && in.c.memoryCount() == 0 && !in.c.HasTable && len(in.c.Globals) == 0
	fn.gcRevocableContext = fn.gcPrivateContext && in.rt == nil && !fn.directIntFast && !fn.directFloatFast && fn.directMixedInfo == 0 && gcPrivateNumericSignatures(in.c)
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
	if !in.privateGCStateValid(state) || !fn.privateGCNativeContextValid(state) {
		in.endFastInvocation(state)
		return nil
	}
	return state
}

// The plugin state is published once. Check its live borrow bit and one
// consistent snapshot of execution flags after acquiring the shared gate.
func (in *Instance) privateGCStateValid(state *instancePluginState) bool {
	return in.refStore == nil && state.guestStorageBorrow.Load() == 0 &&
		in.executionFlags.Load()&(executionFlagIndependent|preparedFastBlocked) == executionFlagIndependent
}

// The prepared wrapper requires the cached native context.
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
	// Consume pending interruption before native helper dispatch can replace
	// the trap word, preserving pre-charge failure and later-call recovery.
	if err = clearPreparedInductionTrap(in); err != nil {
		return nil, err
	}
	return fn.invokePrivateGCScalarSlow(arg, state)
}

func (fn *WasmFunc) invokePrivateGCScalarSlow(arg uint64, state *instancePluginState) (out []uint64, err error) {
	in := fn.in
	defer in.recoverBoundedNativeSyncPanic(&err)
	// Internal collecting helpers may expose the invocation identity or host
	// parent while the native wrapper retains exclusive admission.
	state.invocationID = newInvocationID()
	if activeHostInvocationBindings.Load() != 0 {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}

	binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(arg)))
	err = fn.callPrivateGCBounded(state)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err
	}
	out = in.resultVals[:1]
	out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	return out, nil
}

func (fn *WasmFunc) callPrivateGCBounded(state *instancePluginState) error {
	if !fn.privateGCNativeContextValid(state) {
		return fn.callGCPrepared()
	}
	in := fn.in
	activation := hostLoopActivation{root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state}
	mask := ^uint32((shared.GCDispatchPayloadMask &^ shared.GCHelperIDMask) | 1)
	if fn.gcLifetimeScalar {
		return fn.hostPrepared.CallBoundedHelperOne(runtimebridge.GrantHostScalarCall(), mask, fn.boundedGCHelper(), activation.dispatch)
	}
	return fn.hostPrepared.CallBoundedHelper(runtimebridge.GrantHostScalarCall(), mask, fn.boundedGCHelper(), activation.dispatch)
}

func (fn *WasmFunc) boundedGCHelper() uint32 {
	if fn.gcLifetimeScalar {
		return gcStructDispatchBit | gcStructReserveDeadNative
	}
	return gcStructDispatchBit | gcStructReserveDeadBatch
}

// Called before external access acquires collector or native-context locks.
// Internal GC helpers retain the fast gate and must not revoke their own entry.
func (in *Instance) revokePrivateGCEntry() {
	if in != nil && in.gc != nil {
		if state := in.pluginState.Load(); state != nil {
			state.invokeMu.revokeFast()
		}
	}
}
