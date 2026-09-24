package wago

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// WasmFunc is a resolved local Wasm export ready for repeated calls.
// It caches export lookup, signature layout, and the native entry address. Like
// Instance, it is not safe for concurrent calls: calls reuse the instance's
// argument and result buffers, and returned results remain valid only until the
// next call on that instance. Invoke must not race Instance.Close.
type WasmFunc struct {
	in                  *Instance
	export              string
	entry               uintptr
	directEntry         uintptr
	directLinMem        uintptr
	paramSlots          int
	resultSlots         int
	paramTypes          []ValType
	resultTypes         []ValType
	paramExact          []ValueTypeDescriptor
	resultExact         []ValueTypeDescriptor
	paramWide           []bool
	hasReferenceParams  bool
	hasReferenceResults bool
	gcMaintenance       bool
	scalarWideMask      uint8
	scalarFast          bool
	scalarResultWide    bool
	resultWide          []bool
	privateFast         bool
	isolatedFast        bool
	directIsolated      bool
	directIntFast       bool
	directFloatFast     bool
	directMixedInfo     uint8
	directIntLight      bool
	directIntBounded    bool
	directIntMode       preparedIntCallMode
	directIntCall       wruntime.PreparedIntCall
	directGate          *invocationGate
	hostPrepared        *wruntime.PreparedHostScalarCall
	hostMemBase         uintptr
	hostContextVersion  uint64 // last independent native context bound for this handle
	hostActivation      hostLoopActivation
	hostFixed           wruntime.FixedScalarHostCall
}

type preparedIntCallMode uint8

const (
	preparedIntCallNone preparedIntCallMode = iota
	preparedIntCallBlock
	preparedIntCallPrebound
)

// tryDirectGate uses the gate resolved with the function. Resource publication
// revokes this exact atomic word before sharing native state.
func (fn *WasmFunc) tryDirectGate() bool {
	gate := fn.directGate
	if gate == nil || !gate.state.CompareAndSwap(0, invocationGateHeld|invocationGateFast) {
		return false
	}
	if !fn.in.preparedFastStateValid() {
		gate.Unlock()
		return false
	}
	return true
}

func (c *Compiled) directPreparedAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedEntry(c.InternalEntry[local])
}

func (c *Compiled) directPreparedLightAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedLightEntry(c.InternalEntry[local])
}

func (c *Compiled) directPreparedBoundedAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedBoundedEntry(c.InternalEntry[local])
}

func preparedDirectIntSignature(sig FuncSig) bool {
	if len(sig.Params) > preparedDirectWideMaxArgs || len(sig.Results) > 4 || len(sig.Results) > 2 && !preparedDirectWideSupported {
		return false
	}
	for _, typ := range sig.Params {
		if typ != ValI32 && typ != ValI64 {
			return false
		}
	}
	for _, typ := range sig.Results {
		if typ != ValI32 && typ != ValI64 {
			return false
		}
	}
	return true
}

func preparedDirectFloatSignature(sig FuncSig) bool {
	if len(sig.Params) > 4 || len(sig.Results) > 4 {
		return false
	}
	for _, typ := range sig.Params {
		if typ != ValF32 && typ != ValF64 {
			return false
		}
	}
	for _, typ := range sig.Results {
		if typ != ValF32 && typ != ValF64 {
			return false
		}
	}
	return true
}

func preparedDirectMixedSignature(sig FuncSig) bool {
	if len(sig.Params) > 4 || len(sig.Results) > 2 {
		return false
	}
	for _, typ := range sig.Params {
		if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
			return false
		}
	}
	for _, typ := range sig.Results {
		if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
			return false
		}
	}
	return !preparedDirectIntSignature(sig) && !preparedDirectFloatSignature(sig)
}

func directMixedFloatMask(params []ValType) uint8 {
	var mask uint8
	for i, typ := range params {
		if typ == ValF32 || typ == ValF64 {
			mask |= 1 << i
		}
	}
	return mask
}

const (
	// The tagged encoding fits in invokeCache.scalarWideMask without enlarging
	// Instance. Mixed entries never consume its ordinary integer-width mask.
	directMixedParamMask = uint8(0x0f)
	directMixedResultFP  = uint8(1 << 4)
	directMixedResult1FP = uint8(1 << 5)
	directMixedEnabled   = uint8(1 << 7)
)

func encodeDirectMixedInfo(sig FuncSig) uint8 {
	info := directMixedEnabled | directMixedFloatMask(sig.Params)
	if len(sig.Results) > 0 && (sig.Results[0] == ValF32 || sig.Results[0] == ValF64) {
		info |= directMixedResultFP
	}
	if len(sig.Results) > 1 && (sig.Results[1] == ValF32 || sig.Results[1] == ValF64) {
		info |= directMixedResult1FP
	}
	return info
}

// WasmFunc resolves a locally-defined function export once. The returned
// handle is the like-for-like counterpart of runtimes whose exported-function
// lookup occurs outside the timed invocation loop. Re-exported imports continue
// to use Invoke because their target instance may differ.
func (in *Instance) WasmFunc(export string) (*WasmFunc, error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: resolve Wasm function: %w", err)
	}
	defer in.endInvocation()
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	if ic.li < 0 {
		return nil, fmt.Errorf("wago: resolve Wasm function %q: re-exported imports must use Invoke", export)
	}
	if in.c == nil || ic.li >= len(in.c.Entry) || ic.li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("wago: resolve Wasm function %q: local function index %d is out of range", export, ic.li)
	}
	sig := in.c.Funcs[ic.li]
	params, results, err := exactFuncSignatureView(sig, in.c.Types)
	if err != nil {
		return nil, fmt.Errorf("wago: resolve Wasm function %q exact signature: %w", export, err)
	}
	paramWide := append([]bool(nil), ic.slotWide[:ic.paramSlots]...)
	resultWide := append([]bool(nil), ic.slotWide[ic.paramSlots:]...)
	scalarFast := preparedScalarFastEnabled &&
		!hasReferenceValType(sig.Params) &&
		!hasReferenceValType(sig.Results)
	var scalarWideMask uint8
	if scalarFast && ic.paramSlots <= 4 {
		slot := 0
		for _, typ := range sig.Params {
			if typ == ValV128 {
				scalarWideMask |= 3 << slot
				slot += 2
			} else {
				if isWideValType(typ) {
					scalarWideMask |= 1 << slot
				}
				slot++
			}
		}
	}
	fn := &WasmFunc{
		in:                  in,
		export:              export,
		entry:               in.base + uintptr(in.c.Entry[ic.li]),
		paramSlots:          ic.paramSlots,
		resultSlots:         ic.resultSlots,
		scalarWideMask:      scalarWideMask,
		scalarFast:          scalarFast,
		scalarResultWide:    ic.resultSlots == 1 && resultWide[0],
		paramTypes:          append([]ValType(nil), sig.Params...),
		resultTypes:         append([]ValType(nil), sig.Results...),
		paramExact:          append([]ValueTypeDescriptor(nil), params...),
		resultExact:         append([]ValueTypeDescriptor(nil), results...),
		paramWide:           paramWide,
		hasReferenceParams:  hasReferenceValType(sig.Params),
		hasReferenceResults: hasReferenceValType(sig.Results),
		gcMaintenance:       in.gc != nil && (in.c.genericGCBoundaryCollectionSafe() || in.c.hasGCRefGlobals()),
		resultWide:          resultWide,
	}
	if scalarFast && preparedCallEnabled && preparedPrivateEntryEnabled {
		entryMode := in.preparedEntryMode()
		if entryMode != preparedEntryGeneral {
			fn.privateFast = true
			fn.isolatedFast = preparedIsolatedEntryEnabled && entryMode == preparedEntryIsolated
		}
		if preparedDirectIntSupported && preparedDirectIntEnabled && preparedDirectIntSignature(sig) && in.c.directPreparedAt(ic.li) &&
			(ic.resultSlots <= 1 || preparedDirectPairSupported && in.c.directPreparedBoundedAt(ic.li)) &&
			(!preparedDirectWideSupported || ic.paramSlots <= 4 && ic.resultSlots <= 2 || in.c.directPreparedBoundedAt(ic.li)) {
			// directPreparedAt is the compiler proof that this internal entry is
			// memory-free. Re-evaluate only the bounds-mode exclusion; every other
			// ownership and lifecycle exclusion remains in force.
			directMode := entryMode
			if directMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased {
				directMode = in.preparedMemoryFreeEntryMode()
			}
			fn.directIsolated = preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated
			if fn.directIsolated || (preparedDirectIntPrivateSupported && directMode == preparedEntryPrivate) {
				fn.directIntFast = true
				if fn.directIsolated {
					fn.directGate = &in.ensurePluginState().invokeMu
				}
				fn.directIntLight = in.c.directPreparedLightAt(ic.li)
				fn.directIntBounded = in.c.directPreparedBoundedAt(ic.li)
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
				fn.initDirectIntCall()
			}
		}
		if preparedDirectFloatSupported && preparedDirectIntEnabled && preparedDirectFloatSignature(sig) &&
			in.c.directPreparedAt(ic.li) && in.c.directPreparedBoundedAt(ic.li) {
			directMode := entryMode
			if directMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased {
				directMode = in.preparedMemoryFreeEntryMode()
			}
			if preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated {
				fn.directIsolated = true
				fn.directFloatFast = true
				fn.directGate = &in.ensurePluginState().invokeMu
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
			}
		}
		if preparedDirectFloatSupported && preparedDirectIntEnabled && preparedDirectMixedSignature(sig) &&
			in.c.directPreparedAt(ic.li) && in.c.directPreparedBoundedAt(ic.li) {
			directMode := entryMode
			if directMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased {
				directMode = in.preparedMemoryFreeEntryMode()
			}
			if preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated {
				fn.directIsolated = true
				fn.directMixedInfo = encodeDirectMixedInfo(sig)
				fn.directGate = &in.ensurePluginState().invokeMu
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
			}
		}
	}
	return fn, nil
}

// Invoke calls the resolved export. Arguments and results use the same raw slot
// representation and lifetime rules as Instance.Invoke.
func (fn *WasmFunc) Invoke(args ...uint64) ([]uint64, error) {
	if fn != nil && fn.in != nil && len(args) == fn.paramSlots {
		if fn.directIntFast {
			if preparedDirectWideSupported && (len(args) > 4 || fn.resultSlots > 2) {
				return fn.invokeDirectIntWide(args)
			}
			switch len(args) {
			case 0:
				return fn.invokeDirectIntFixed(0, 0, 0, 0)
			case 1:
				return fn.invokeDirectIntFixed(args[0], 0, 0, 0)
			case 2:
				return fn.invokeDirectIntFixed(args[0], args[1], 0, 0)
			case 3:
				return fn.invokeDirectIntFixed(args[0], args[1], args[2], 0)
			case 4:
				return fn.invokeDirectIntFixed(args[0], args[1], args[2], args[3])
			}
		}
		if preparedDirectFloatSupported && fn.directFloatFast {
			return fn.invokeDirectFloat(args)
		}
		if preparedDirectFloatSupported && fn.directMixedInfo != 0 {
			return fn.invokeDirectMixed(args)
		}
		if fn.scalarFast {
			return fn.invokeScalar(args)
		}
	}
	return fn.invokeGeneral(args)
}

type preparedInvocationLease struct {
	in    *Instance
	state *instancePluginState
	gc    gcInvocationLease
}

// When a scalar call has no collector or imported GC domain, keep its
// invocation ownership in a small lease instead of copying the full GC-domain
// view on entry and release.
type preparedScalarNoGCLease struct {
	in    *Instance
	state *instancePluginState
}

func (in *Instance) lockPreparedScalarNoGC() preparedScalarNoGCLease {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	state.invocationID = newInvocationID()
	return preparedScalarNoGCLease{in: in, state: state}
}

func (l preparedScalarNoGCLease) unlock() {
	if l.in.importsFuncrefStorage() || l.in.table != nil {
		l.in.reconcileFuncrefRoots()
	}
	l.state.invocationID = 0
	l.state.invokeMu.Unlock()
}

func (in *Instance) lockPreparedInvocation() preparedInvocationLease {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	id := newInvocationID()
	state.invocationID = id
	return preparedInvocationLease{in: in, state: state, gc: in.lockGCInvocation(id)}
}

func (in *Instance) lockPreparedSessionInvocation() preparedInvocationLease {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	state.invocationID = newInvocationID()
	return preparedInvocationLease{state: state}
}

func (l preparedInvocationLease) unlock() {
	l.gc.unlock()
	if l.in != nil && (l.in.importsFuncrefStorage() || l.in.table != nil) {
		l.in.reconcileFuncrefRoots()
	}
	l.state.invocationID = 0
	l.state.invokeMu.Unlock()
}

// ARM64 avoids copying the complete GC-domain lease through the scalar call
// boundary. Keep the existing by-value admission for other call shapes.
func (in *Instance) lockPreparedScalarInvocationInto(l *preparedInvocationLease) {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	id := newInvocationID()
	state.invocationID = id
	l.in, l.state, l.gc = in, state, in.lockGCInvocation(id)
}

func (l *preparedInvocationLease) unlockScalarInvocation() {
	l.gc.unlock()
	if l.in != nil && (l.in.importsFuncrefStorage() || l.in.table != nil) {
		l.in.reconcileFuncrefRoots()
	}
	l.state.invocationID = 0
	l.state.invokeMu.Unlock()
}

func (fn *WasmFunc) invokeGeneral(args []uint64) ([]uint64, error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: invoke closed Wasm function")
	}
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	defer in.endInvocation()
	// Prepared calls share the same instance buffers and Runtime GC domain as
	// Invoke. Publish an invocation identity under the instance gate so host
	// callbacks can suspend the domain lease, and retain that lease through public
	// reference-result tokenization.
	preparedLease := in.lockPreparedInvocation()
	defer preparedLease.unlock()
	return fn.invokeGeneralAdmitted(args)
}

func (fn *WasmFunc) invokeGeneralAdmitted(args []uint64) ([]uint64, error) {
	in := fn.in
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, err
	}
	if fn.hasReferenceParams {
		defer in.clearGCRefArgumentRoots()
		if err := in.marshalPublicReferenceArgs(fn.export, args, fn.paramTypes, fn.paramExact); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, fn.paramTypes)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if in.syncMode {
		if err := in.callNativeSync(fn.entry); err != nil {
			return nil, err
		}
	} else {
		prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
		err := in.callNativeAsync(fn.entry, prepared)
		if err != nil {
			return nil, err
		}
		if len(in.hostLog) != 0 {
			if err := in.replayHostLog(); err != nil {
				return nil, err
			}
		}
	}
	if err := in.reconcileGCGlobalRoots(); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	if fn.resultSlots == 1 {
		if fn.resultWide[0] {
			out[0] = binary.LittleEndian.Uint64(in.results)
		} else {
			out[0] = uint64(binary.LittleEndian.Uint32(in.results))
		}
		if fn.hasReferenceResults {
			if err := in.translatePublicReferenceResults(fn.export, out, fn.resultTypes, fn.resultExact); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	for i, wide := range fn.resultWide {
		off := i * 8
		if wide {
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	if fn.hasReferenceResults {
		if err := in.translatePublicReferenceResults(fn.export, out, fn.resultTypes, fn.resultExact); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// callScalarHostPrepared keeps validation and the immutable host-call layout on
// the resolved function, but acquires native ownership anew for every Invoke.
// The per-call binding is still required: another entry or a host callback may
// have changed the instance context while this function was idle.
func (fn *WasmFunc) callScalarHostPrepared() error {
	in := fn.in
	entry, err := fn.beginNativeEntry()
	if err != nil {
		return err
	}
	defer in.unlockNativeEntry(entry)
	base := in.jm.LinMemBase()
	if fn.hostPrepared == nil || fn.hostMemBase != base {
		rawSlots, ok := in.syncHosts[0].typedScalarSlots()
		if !ok {
			return fmt.Errorf("wago: invalid fixed scalar host signature")
		}
		fn.hostPrepared, err = in.eng.PrepareHostScalarFixedCall(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm, in.trap, in.results, in.ctrl, rawSlots)
		if err != nil {
			return err
		}
		fn.hostMemBase = base
		fn.hostActivation = hostLoopActivation{
			root:                        in,
			ctrl:                        offHeapSlicePtr(in.ctrl),
			state:                       in.ensurePluginState(),
			entryNativeMu:               entry.local,
			parkedNativeContextReusable: in.gc == nil && !in.c.threadedMemory0(),
		}
		if preparedHostFixedEnabled {
			switch in.syncHosts[0].scalarKind {
			case syncHostTypedI32:
				fn.hostFixed = fn.hostActivation.dispatchSingleTypedI32FixedPortal
			case syncHostTypedI32x2:
				fn.hostFixed = fn.hostActivation.dispatchSingleTypedI32x2FixedPortal
			}
		}
	} else {
		if err := in.jm.RebindTrapCell(in.trap); err != nil {
			return err
		}
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
	}
	restoreInvocationContext := bindHostInvocationParent(in, nil)
	defer restoreInvocationContext()
	//lint:ignore SA1012 nil selects the helper's background context only when atomic waits are present.
	stopWaitContext := in.publishAtomicWaitContext(nil)
	defer stopWaitContext()
	// Unlike a caller-owned session, each ordinary Invoke has a new invocation
	// identity. Foreign host dispatch must not reuse the previous call's cache.
	if fn.hostActivation.invocation.id != 0 {
		fn.hostActivation.invocation = hostInvocationContext{}
	}
	return in.callPreparedHostSyncAdmitted(fn.hostPrepared, fn.hostFixed, &fn.hostActivation)
}

func (fn *WasmFunc) invokeScalar(args []uint64) ([]uint64, error) {
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	defer in.endInvocation()
	if in.gc == nil && in.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain) == 0 {
		preparedLease := in.lockPreparedScalarNoGC()
		defer preparedLease.unlock()
		return fn.invokeScalarAdmitted(args)
	}
	if preparedScalarInPlaceLease {
		var preparedLease preparedInvocationLease
		in.lockPreparedScalarInvocationInto(&preparedLease)
		defer preparedLease.unlockScalarInvocation()
		return fn.invokeScalarAdmitted(args)
	}
	preparedLease := in.lockPreparedInvocation()
	defer preparedLease.unlock()
	return fn.invokeScalarAdmitted(args)
}

func (fn *WasmFunc) invokeScalarAdmitted(args []uint64) ([]uint64, error) {
	in := fn.in
	if fn.gcMaintenance || !in.preparedFastStateValid() {
		return fn.invokeGeneralAdmitted(args)
	}
	if len(args) <= 4 {
		put := func(slot int) {
			bits := args[slot]
			if fn.scalarWideMask&(1<<slot) == 0 {
				bits = uint64(uint32(bits))
			}
			binary.LittleEndian.PutUint64(in.serArgs[slot*8:], bits)
		}
		switch len(args) {
		case 4:
			put(3)
			fallthrough
		case 3:
			put(2)
			fallthrough
		case 2:
			put(1)
			fallthrough
		case 1:
			put(0)
		}
	} else {
		marshalPublicScalarSlotsByWidth(nativeUint64Slots(in.serArgs), args, fn.paramWide)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if in.syncMode {
		var err error
		if in.gc == nil && in.usesIndependentExecution() && in.hasSingleDirectTypedScalarHost() {
			err = fn.callScalarHostPrepared()
		} else {
			err = in.callNativeSync(fn.entry)
		}
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		if fn.isolatedFast {
			err = in.callPreparedIsolated(fn.entry, in.trap)
		} else if fn.privateFast {
			err = in.callPreparedPrivate(fn.entry, in.trap)
		} else {
			prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
			err = in.callNativeAsync(fn.entry, prepared)
		}
		if err != nil {
			return nil, err
		}
		if len(in.hostLog) != 0 {
			if err := in.replayHostLog(); err != nil {
				return nil, err
			}
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	decodePublicScalarSlots(out, nativeUint64Slots(in.results), fn.resultWide)
	return out, nil
}

// invokeScalarHostReserved is the host-capable counterpart to
// the ordinary scalar entry. PreparedSession already owns and has bound the
// native execution context, while callNativeSyncAdmitted retains the complete
// host park/resume and panic/trap protocol.
func (fn *WasmFunc) invokeScalarHostReserved(args []uint64, prepared *wruntime.PreparedHostScalarCall, fixed wruntime.FixedScalarHostCall, activation *hostLoopActivation) ([]uint64, error) {
	in := fn.in
	if len(args) <= 4 {
		put := func(slot int) {
			bits := args[slot]
			if fn.scalarWideMask&(1<<slot) == 0 {
				bits = uint64(uint32(bits))
			}
			binary.LittleEndian.PutUint64(in.serArgs[slot*8:], bits)
		}
		switch len(args) {
		case 4:
			put(3)
			fallthrough
		case 3:
			put(2)
			fallthrough
		case 2:
			put(1)
			fallthrough
		case 1:
			put(0)
		}
	} else {
		marshalPublicScalarSlotsByWidth(nativeUint64Slots(in.serArgs), args, fn.paramWide)
	}
	if err := in.callNativeSyncAdmitted(fn.entry, in.trap, nil, prepared, fixed, activation, nil); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	decodePublicScalarSlots(out, nativeUint64Slots(in.results), fn.resultWide)
	return out, nil
}
