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
	goruntime "runtime"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedEarlyI32LightSupported = goruntime.Compiler == "gc" && goruntime.GOARCH == "arm64" && goruntime.GOOS == "linux"

const preparedEarlyZeroArgSupported = (goruntime.Compiler == "tinygo" && goruntime.GOARCH == "arm64") ||
	((goruntime.Compiler == "gc" || goruntime.Compiler == "tinygo") && goruntime.GOARCH == "amd64")

const preparedZeroArgForImage = preparedEarlyZeroArgSupported && (preparedZeroArgNarrowResultForImage || preparedZeroArgWideResultForImage)

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
	paramWidthClass     scalarSlotWidthClass
	resultWidthClass    scalarSlotWidthClass
	hasReferenceParams  bool
	hasReferenceResults bool
	gcMaintenance       bool
	preparedGCScalarState
	scalarWideMask   uint8
	scalarFast       bool
	scalarResultWide bool
	preparedGCInductionState
	resultWide            []bool
	boundedWrapper        bool
	isolatedZeroArgResult bool
	directI32LightOneArg  bool
	boundedBulkLimit      uint32
	boundedBulkArgument   uint8
	privateFast           bool
	isolatedFast          bool
	directIsolated        bool
	directIntFast         bool
	directIntOneArg       bool
	directFloatFast       bool
	directMixedInfo       uint8
	directIntLight        bool
	directIntBounded      bool
	directIntMode         preparedIntCallMode
	boundedNumericHost    bool
	guardedMemoryGrow     bool
	directIntCall         wruntime.PreparedIntCall
	directGate            *invocationGate
	hostPrepared          *wruntime.PreparedHostScalarCall
	hostMemBase           uintptr
	hostContextVersion    uint64 // last independent native context bound for this handle
	hostActivation        hostLoopActivation
	hostFixed             wruntime.FixedScalarHostCall
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
	held, valid := fn.tryDirectGateState()
	if held && !valid {
		fn.directGate.Unlock()
	}
	return valid
}

// tryDirectGateState separates inlineable acquisition from the uncommon
// release of an invalid fast state. A held gate always belongs to the caller,
// including when valid is false. Validate only after acquiring the same word
// that resource publication revokes.
func (fn *WasmFunc) tryDirectGateState() (held, valid bool) {
	gate := fn.directGate
	if gate == nil || !gate.state.CompareAndSwap(0, invocationGateHeld|invocationGateFast) {
		return false, false
	}
	return true, fn.in.preparedFastStateValid()
}

func (c *Compiled) directPreparedAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedEntry(c.InternalEntry[local])
}

func (c *Compiled) directPreparedLightAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedLightEntry(c.InternalEntry[local])
}

// The bounded-work bit also covers a small scalar wrapper that cannot use the
// register entry; callers must check directPreparedAt separately when needed.
func (c *Compiled) directPreparedBoundedAt(local int) bool {
	return c != nil && local >= 0 && local < len(c.InternalEntry) && directPreparedBoundedEntry(c.InternalEntry[local])
}

func fixedDirectArguments(args []uint64) (a0, a1, a2, a3 uint64) {
	if maxFixedDirectArgsForImage >= 1 && len(args) >= 1 {
		a0 = args[0]
	}
	if maxFixedDirectArgsForImage >= 2 && len(args) >= 2 {
		a1 = args[1]
	}
	if maxFixedDirectArgsForImage >= 3 && len(args) >= 3 {
		a2 = args[2]
	}
	if maxFixedDirectArgsForImage >= 4 && len(args) >= 4 {
		a3 = args[3]
	}
	return a0, a1, a2, a3
}

const directFloatForImage = directFloatFourForImage || directFloatOctForImage
const directFloatOrMixedForImage = directFloatForImage || directMixedForImage
const directWideArityForImage = directWideFiveForImage || directOctResultsForImage
const directFixedOrPairForImage = directIntFixedForImage || directPairResultsForImage
const directIntegerForImage = directFixedOrPairForImage || directWideArityForImage
const directIntBoundedForImage = directIntBoundedFullForImage || directIntBoundedLightForImage
const directIntUnboundedForImage = directIntUnboundedFullForImage || directIntUnboundedLightForImage

const (
	fixedIntVoidResult uint8 = 1 << iota
	fixedIntI32Result
	fixedIntI64Result
)

const (
	mixedVoidResult uint8 = 1 << iota
	mixedSingleResult
	mixedMultipleResults
)

func fixedIntArgumentIsNarrow(lane, wideMask uint8) bool {
	return fixedIntI32ArgsForImage&lane != 0 && (fixedIntI64ArgsForImage&lane == 0 || wideMask&lane == 0)
}

func narrowFixedIntArguments(a0, a1, a2, a3 uint64) (uint64, uint64, uint64, uint64) {
	if fixedIntI32ArgsForImage&1 != 0 {
		a0 = uint64(uint32(a0))
	}
	if fixedIntI32ArgsForImage&2 != 0 {
		a1 = uint64(uint32(a1))
	}
	if fixedIntI32ArgsForImage&4 != 0 {
		a2 = uint64(uint32(a2))
	}
	if fixedIntI32ArgsForImage&8 != 0 {
		a3 = uint64(uint32(a3))
	}
	return a0, a1, a2, a3
}

func storeFixedIntResult(out []uint64, result uint64, wide bool) {
	if fixedIntResultsForImage&(fixedIntI32Result|fixedIntI64Result) != 0 &&
		(fixedIntResultsForImage&fixedIntVoidResult == 0 || len(out) == 1) {
		if fixedIntResultsForImage&fixedIntI64Result == 0 ||
			fixedIntResultsForImage&fixedIntI32Result != 0 && !wide {
			result = uint64(uint32(result))
		}
		out[0] = result
	}
}

func directBankValue(bits uint64, slot int, widths []bool, narrowMask, wideMask uint8) uint64 {
	if narrowMask == 0 {
		return bits
	}
	if wideMask == 0 {
		return uint64(uint32(bits))
	}
	lane := uint8(1 << slot)
	if (narrowMask == 0xff || narrowMask&lane != 0) &&
		(wideMask != 0xff && wideMask&lane == 0 || !widths[slot]) {
		return uint64(uint32(bits))
	}
	return bits
}

func useBoundedDirectInt(bounded bool) bool {
	return directIntBoundedForImage && (!directIntUnboundedForImage || bounded)
}

// A pair-only image can select its return ABI at link time.
func (fn *WasmFunc) usesDirectIntPairABI() bool {
	return directPairResultsForImage && (!directIntFixedForImage || fn.resultSlots == 2)
}

func preparedDirectIntegerArityForImage(params, results int) bool {
	if !standaloneBareImage {
		return true
	}
	switch {
	case results > 5:
		return directOctResultsForImage
	case params > 4 || results > 2:
		return directWideFiveForImage
	case results == 2:
		return directPairResultsForImage && params <= maxFixedDirectArgsForImage
	default:
		return directIntFixedForImage && params <= maxFixedDirectArgsForImage
	}
}

func preparedDirectFloatSignatureForImage(sig FuncSig) bool {
	if len(sig.Params) > 4 || len(sig.Results) > 4 {
		return directFloatOctForImage && preparedDirectFloatSignature(sig)
	}
	return directFloatFourForImage && preparedDirectFloatSignature(sig)
}

func preparedDirectIntSignature(sig FuncSig) bool {
	if len(sig.Params) > preparedDirectWideMaxArgs || len(sig.Results) > preparedDirectWideMaxArgs || len(sig.Results) > 2 && !preparedDirectWideSupported {
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
	if len(sig.Params) > 8 || len(sig.Results) > 8 {
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
	if len(sig.Params) > 4 || len(sig.Results) > 4 {
		return false
	}
	for _, typ := range sig.Params {
		if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
			return false
		}
	}
	gp, fp := 0, 0
	for _, typ := range sig.Results {
		if typ != ValI32 && typ != ValI64 && typ != ValF32 && typ != ValF64 {
			return false
		}
		if typ == ValF32 || typ == ValF64 {
			fp++
		} else {
			gp++
		}
	}
	if len(sig.Results) > 2 && (gp == 0 || fp == 0 || gp > 2 || fp > 2) {
		return false
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
	directMixedResult2FP = uint8(1 << 6)
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
	if len(sig.Results) > 2 && (sig.Results[2] == ValF32 || sig.Results[2] == ValF64) {
		info |= directMixedResult2FP
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
	paramWide := ic.slotWide[:ic.paramSlots]
	resultWide := ic.slotWide[ic.paramSlots:]
	if wideInvocationSlotsForImage {
		paramWide = append([]bool(nil), paramWide...)
		resultWide = append([]bool(nil), resultWide...)
	}
	scalarFast := preparedScalarFastEnabled &&
		!ic.hasFuncRefParams && !ic.hasFuncRefResults
	var scalarWideMask uint8
	if wideInvocationSlotsForImage && scalarFast && ic.paramSlots <= 4 {
		slot := 0
		for _, typ := range sig.Params {
			if vectorInvocationForImage && typ == ValV128 {
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
		paramSlots:          int(ic.paramSlots),
		resultSlots:         int(ic.resultSlots),
		scalarWideMask:      scalarWideMask,
		scalarFast:          scalarFast,
		scalarResultWide:    ic.resultSlots == 1 && resultWide[0],
		boundedNumericHost:  scalarFast && in.preparedBoundedNumericEligible(),
		paramTypes:          append([]ValType(nil), sig.Params...),
		resultTypes:         append([]ValType(nil), sig.Results...),
		paramExact:          append([]ValueTypeDescriptor(nil), params...),
		resultExact:         append([]ValueTypeDescriptor(nil), results...),
		paramWide:           paramWide,
		paramWidthClass:     ic.paramWidthClass,
		resultWidthClass:    ic.resultWidthClass,
		hasReferenceParams:  ic.hasFuncRefParams,
		hasReferenceResults: ic.hasFuncRefResults,
		gcMaintenance:       collectorObjectTokensBuilt && in.gc != nil && (in.c.genericGCBoundaryCollectionSafe() || in.c.hasGCRefGlobals()),
		resultWide:          resultWide,
		boundedWrapper:      in.c.directPreparedBoundedAt(ic.li) || in.isolatedNativeScalarLeaf(),
		boundedBulkLimit:    uint32(in.c.boundedBulkArgumentPlan(ic.li) >> 32),
		boundedBulkArgument: uint8(in.c.boundedBulkArgumentPlan(ic.li)),
	}
	fn.guardedMemoryGrow = guardedGrowthForImage && guardedMemoryGrowEntry(in.c.InternalEntry[ic.li])
	if scalarFast && preparedCallEnabled && preparedPrivateEntryEnabled {
		entryMode := in.preparedEntryMode()
		if entryMode != preparedEntryGeneral {
			fn.privateFast = true
			fn.isolatedFast = preparedIsolatedEntryEnabled && entryMode == preparedEntryIsolated
		}
		if directIntegerForImage && preparedDirectIntSupported && preparedDirectIntEnabled && preparedDirectIntSignature(sig) && in.c.directPreparedAt(ic.li) &&
			preparedDirectIntegerArityForImage(int(ic.paramSlots), int(ic.resultSlots)) &&
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
				fn.directIntLight = in.c.directPreparedLightAt(ic.li)
				fn.directIntBounded = in.c.directPreparedBoundedAt(ic.li)
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
				if directIntBoundedForImage {
					fn.initDirectIntCall()
				}
			}
		}
		if directFloatForImage && preparedDirectIntEnabled && preparedDirectFloatSignatureForImage(sig) &&
			in.c.directPreparedAt(ic.li) && in.c.directPreparedBoundedAt(ic.li) {
			directMode := entryMode
			if directMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased {
				directMode = in.preparedMemoryFreeEntryMode()
			}
			if preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated {
				fn.directIsolated = true
				fn.directFloatFast = true
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
			}
		}
		if directMixedForImage && preparedDirectIntEnabled && preparedDirectMixedSignature(sig) &&
			in.c.directPreparedAt(ic.li) && in.c.directPreparedBoundedAt(ic.li) {
			directMode := entryMode
			if directMode == preparedEntryGeneral && in.c.boundsMode == BoundsChecksSignalsBased {
				directMode = in.preparedMemoryFreeEntryMode()
			}
			if preparedIsolatedEntryEnabled && directMode == preparedEntryIsolated {
				fn.directIsolated = true
				fn.directMixedInfo = encodeDirectMixedInfo(sig)
				fn.directEntry = in.base + uintptr(internalEntryOffset(in.c.InternalEntry[ic.li]))
				fn.directLinMem = in.jm.LinMemBase()
			}
		}
		if fn.isolatedFast || fn.directIsolated {
			fn.directGate = &in.ensurePluginState().invokeMu
		}
	}
	fn.prepareGCScalar()
	fn.prepareGCInductions(ic.li)
	fn.directIntOneArg = preparedIntOneArgForImage && !codeProfileEnabled && !fn.guardedMemoryGrow && fn.paramSlots == 1 &&
		!fn.hasPreparedI31Induction() && (!preparedDirectWideSupported || fn.resultSlots <= 2)
	if preparedEarlyI32LightSupported {
		fn.directIntOneArg = fn.directIntOneArg && fn.directIntFast
		fn.directI32LightOneArg = preparedUnboundedLightI32OneArgForImage && fn.directIntOneArg && fn.directIsolated && fn.directIntLight &&
			!fn.directIntBounded && fn.directIntMode == preparedIntCallNone &&
			fn.scalarWideMask == 0 && fn.resultSlots == 1 && !fn.scalarResultWide
	}
	if preparedZeroArgForImage {
		fn.isolatedZeroArgResult = !codeProfileEnabled && !fn.guardedMemoryGrow && fn.paramSlots == 0 &&
			fn.resultSlots == 1 && fn.isolatedFast && fn.scalarFast && !fn.gcMaintenance &&
			!in.syncMode && !fn.directIntFast && !fn.directFloatFast && fn.directMixedInfo == 0 && len(in.hostLog) == 0
	}
	return fn, nil
}

// Invoke calls the resolved export. Arguments and results use the same raw slot
// representation and lifetime rules as Instance.Invoke.
//
//go:inline
func (fn *WasmFunc) Invoke(args ...uint64) ([]uint64, error) {
	if fn != nil {
		if preparedIntOneArgForImage && fn.directIntOneArg && (preparedEarlyI32LightSupported || fn.directIntFast) && len(args) == 1 {
			return fn.invokeDirectIntFixed(args[0], 0, 0, 0)
		}
		if preparedZeroArgForImage && fn.isolatedZeroArgResult && len(args) == 0 {
			return fn.invokeIsolatedZeroArg()
		}
		if guardedGrowthForImage && fn.guardedMemoryGrow {
			return fn.invokeGuardedMemoryGrow(args)
		}
	}
	return fn.invoke(args)
}

// The resolved function has no arguments or host callbacks. Acquire the
// same revocable reservation as the ordinary isolated async wrapper.
func (fn *WasmFunc) invokeIsolatedZeroArg() ([]uint64, error) {
	if !fn.tryBeginFastInvocation() {
		return fn.invoke(nil)
	}
	in := fn.in
	err := in.callPreparedIsolated(fn.entry, in.trap, true, fn.boundedWrapper || fn.boundedBulkArguments(nil))
	if err != nil {
		in.endFastInvocation(in.pluginState.Load())
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:1]
	if preparedZeroArgWideResultForImage && (!preparedZeroArgNarrowResultForImage || fn.scalarResultWide) {
		out[0] = binary.LittleEndian.Uint64(in.results)
	} else {
		out[0] = uint64(binary.LittleEndian.Uint32(in.results))
	}
	in.endFastInvocation(in.pluginState.Load())
	return out, nil
}

func (fn *WasmFunc) invokeGuardedMemoryGrow(args []uint64) ([]uint64, error) {
	defer fn.in.commitGuardedGrowth()
	return fn.invoke(args)
}

func (fn *WasmFunc) invoke(args []uint64) ([]uint64, error) {
	if codeProfileEnabled && fn != nil && fn.in.boundaryProfile() != nil {
		return fn.invokeGeneral(args)
	}
	if fn != nil && fn.in != nil && len(args) == fn.paramSlots {
		if fn.hasPreparedI31Induction() {
			if out, err, handled := fn.tryInvokePreparedI31(args[0]); handled {
				return out, err
			}
		}
		if directIntegerForImage && fn.directIntFast {
			if directWideArityForImage && (len(args) > 4 || fn.resultSlots > 2) {
				return fn.invokeDirectIntWide(args)
			}
			if directFixedOrPairForImage && len(args) <= maxFixedDirectArgsForImage {
				return fn.invokeDirectIntFixed(fixedDirectArguments(args))
			}
		}
		// Revocable GC contexts exclude direct integer entries, so the common
		// numeric call can avoid this check without changing GC admission.
		if fn.hasRevocableGCContext() {
			if out, err, handled := fn.tryInvokeRevocableGCScalar(args[0]); handled {
				return out, err
			}
		}
		if directFloatForImage && fn.directFloatFast {
			return fn.invokeDirectFloat(args)
		}
		if directMixedForImage && fn.directMixedInfo != 0 {
			return fn.invokeDirectMixed(args)
		}
		if fn.scalarFast {
			if fn.hasPreparedGCScalar() && fn.gcMaintenance {
				return fn.invokeGCScalar(args)
			}
			if fn.boundedNumericHost {
				if out, err, ok := fn.tryInvokeBoundedNumeric(args); ok {
					return out, err
				}
			}
			if len(args) == 1 && fn.resultSlots == 1 && fn.scalarWideMask == 0 && !fn.scalarResultWide && fn.hostPrepared != nil && fn.hostFixed != nil {
				if out, err, ok := fn.tryInvokeScalarHost1(args); ok {
					return out, err
				}
			}
			return fn.invokeScalar(args)
		}
	}
	return fn.invokeGeneral(args)
}

func (fn *WasmFunc) tryInvokeScalarHost1(args []uint64) (result []uint64, resultErr error, admitted bool) {
	in := fn.in
	if in.hasNativeCollector() || fn.gcMaintenance || !in.usesIndependentExecution() || !in.preparedFastStateValid() {
		return nil, nil, false
	}
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err), true
	}
	var lease preparedScalarNoGCLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		lease = in.lockPreparedScalarNoGC()
		leaseOwned = true
		if !in.usesIndependentExecution() || !in.preparedFastStateValid() {
			return
		}
		admitted = true
		binary.LittleEndian.PutUint64(in.serArgs, uint64(uint32(args[0])))
		if len(in.hostLog) > 0 {
			binary.LittleEndian.PutUint32(in.hostLog, 0)
		}
		if resultErr = fn.callScalarHostPrepared(); resultErr != nil {
			return
		}
		goruntime.KeepAlive(in)
		goruntime.KeepAlive(in.c)
		result = in.resultVals[:1]
		result[0] = uint64(binary.LittleEndian.Uint32(in.results))
	})
	if leaseOwned {
		cleanup.run(lease.unlock)
	}
	cleanup.run(in.endDirectInvocation)
	cleanup.finish()
	return result, resultErr, admitted
}

// invokeGeneralAfterDirectAdmission completes a revoked direct-entry fallback.
// The caller has already acquired one direct lifetime unit; keep its prepared
// lease and lifecycle release in the same first-panic journal as the admitted
// general body.
func (fn *WasmFunc) invokeGeneralAfterDirectAdmission(args []uint64) (result []uint64, resultErr error) {
	in := fn.in
	var lease preparedInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		lease = in.lockPreparedInvocation()
		leaseOwned = true
		result, resultErr = fn.invokeGeneralAdmitted(args)
	})
	if leaseOwned {
		cleanup.run(lease.unlock)
	}
	cleanup.run(in.endDirectInvocation)
	cleanup.finish()
	return result, resultErr
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

func (in *Instance) lockPreparedScalarNoGC() (lease preparedScalarNoGCLease) {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	lease.in, lease.state = in, state
	returned := false
	defer func() {
		if returned {
			return
		}
		var cleanup resourceCleanup
		cleanup.panicked, cleanup.value = true, recover()
		cleanup.run(func() { state.setInvocationIdentity(0) })
		cleanup.run(state.invokeMu.Unlock)
		cleanup.finish()
	}()
	state.setInvocationIdentity(newInvocationID())
	returned = true
	return lease
}

func (l preparedScalarNoGCLease) unlock() {
	var cleanup resourceCleanup
	if l.in.importsFuncrefStorage() || l.in.table != nil {
		cleanup.run(l.in.reconcileFuncrefRoots)
	}
	cleanup.run(func() { l.state.setInvocationIdentity(0) })
	cleanup.run(l.state.invokeMu.Unlock)
	cleanup.finish()
}

func (in *Instance) lockPreparedInvocation() (lease preparedInvocationLease) {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	lease.in, lease.state = in, state
	returned := false
	defer func() {
		if returned {
			return
		}
		var cleanup resourceCleanup
		cleanup.panicked, cleanup.value = true, recover()
		cleanup.run(lease.gc.unlock)
		cleanup.run(func() { state.setInvocationIdentity(0) })
		cleanup.run(state.invokeMu.Unlock)
		cleanup.finish()
	}()
	id := newInvocationID()
	state.setInvocationIdentity(id)
	lease.gc = in.lockGCInvocation(id)
	returned = true
	return lease
}

func (in *Instance) lockPreparedSessionInvocation() preparedInvocationLease {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	state.setInvocationIdentity(newInvocationID())
	return preparedInvocationLease{state: state}
}

func (l preparedInvocationLease) unlock() {
	var cleanup resourceCleanup
	l.gc.release(&cleanup)
	if l.in != nil && (l.in.importsFuncrefStorage() || l.in.table != nil) {
		cleanup.run(l.in.reconcileFuncrefRoots)
	}
	cleanup.run(func() { l.state.setInvocationIdentity(0) })
	cleanup.run(l.state.invokeMu.Unlock)
	cleanup.finish()
}

// ARM64 avoids copying the complete GC-domain lease through the scalar call
// boundary. Keep the existing by-value admission for other call shapes.
func (in *Instance) lockPreparedScalarInvocationInto(l *preparedInvocationLease) {
	state := in.ensurePluginState()
	state.invokeMu.Lock()
	l.in, l.state = in, state
	returned := false
	defer func() {
		if returned {
			return
		}
		var cleanup resourceCleanup
		cleanup.panicked, cleanup.value = true, recover()
		l.gc.release(&cleanup)
		cleanup.run(func() { state.setInvocationIdentity(0) })
		cleanup.run(state.invokeMu.Unlock)
		cleanup.finish()
	}()
	id := newInvocationID()
	state.setInvocationIdentity(id)
	if in.mayNeedGCInvocationLease() {
		l.gc = in.lockGCInvocation(id)
	} else {
		l.gc = gcInvocationLease{}
	}
	returned = true
}

func (l *preparedInvocationLease) unlockScalarInvocation() {
	var cleanup resourceCleanup
	if l.gc.isAcquired() {
		l.gc.release(&cleanup)
	}
	if l.in != nil && (l.in.importsFuncrefStorage() || l.in.table != nil) {
		cleanup.run(l.in.reconcileFuncrefRoots)
	}
	cleanup.run(func() { l.state.setInvocationIdentity(0) })
	cleanup.run(l.state.invokeMu.Unlock)
	cleanup.finish()
}

func (fn *WasmFunc) invokeGeneral(args []uint64) (result []uint64, resultErr error) {
	if fn == nil || fn.in == nil {
		return nil, fmt.Errorf("wago: invoke closed Wasm function")
	}
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	// Prepared calls share the same instance buffers and Runtime GC domain as
	// Invoke. Publish an invocation identity under the instance gate so host
	// callbacks can suspend the domain lease, and retain that lease through public
	// reference-result tokenization.
	var preparedLease preparedInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		preparedLease = in.lockPreparedInvocation()
		leaseOwned = true
		if codeProfileEnabled && in.boundaryProfile() != nil {
			span, restore := in.beginProfileInvocation(fn.export)
			defer restore()
			completed := false
			defer finishProfileBoundary(span, &resultErr, &completed)
			result, resultErr = fn.invokeGeneralAdmitted(args)
			completed = true
			return
		}
		result, resultErr = fn.invokeGeneralAdmitted(args)
	})
	if leaseOwned {
		cleanup.run(preparedLease.unlock)
	}
	cleanup.run(in.endInvocation)
	cleanup.finish()
	return result, resultErr
}

func (fn *WasmFunc) invokeGeneralAdmitted(args []uint64) (result []uint64, resultErr error) {
	if !referenceSignaturesBuilt {
		return fn.invokeGeneralAdmittedBody(args, nil)
	}
	var argumentOwners railshotPublicFuncrefArguments
	var cleanup resourceCleanup
	cleanup.run(func() { result, resultErr = fn.invokeGeneralAdmittedBody(args, &argumentOwners) })
	cleanup.run(func() { railshotReleasePublicFuncrefArguments(argumentOwners) })
	cleanup.finish()
	return result, resultErr
}

func (fn *WasmFunc) invokeGeneralAdmittedBody(args []uint64, argumentOwners *railshotPublicFuncrefArguments) ([]uint64, error) {
	in := fn.in
	if len(args) != fn.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", fn.export, fn.paramSlots, len(args))
	}
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, err
	}
	if referenceSignaturesBuilt && fn.hasReferenceParams {
		if collectorObjectTokensBuilt {
			defer in.clearGCRefArgumentRoots()
		}
		var err error
		if *argumentOwners, err = railshotBeginPublicFuncrefArguments(in); err != nil {
			return nil, fmt.Errorf("%s reference argument owners: %w", fn.export, err)
		}
		if err := in.marshalPublicReferenceArgs(fn.export, args, fn.paramTypes, fn.paramExact, argumentOwners); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, fn.paramTypes)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	var executionErr error
	if syncHostControlForImage && in.syncMode {
		if fn.hasPreparedGCScalar() && (!codeProfileEnabled || in.boundaryProfile() == nil && activeHostInvocationContext(in).profileSession() == nil) {
			executionErr = fn.callGCPrepared()
		} else {
			executionErr = in.callNativeSync(fn.entry)
		}
	} else {
		prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
		executionErr = in.callNativeAsync(fn.entry, prepared)
		if hostFunctionImportsBuilt && executionErr == nil && len(in.hostLog) != 0 {
			executionErr = in.replayHostLog()
		}
	}
	if err := in.reconcileGCGlobalRootsAfter(executionErr); err != nil {
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
		if referenceSignaturesBuilt && fn.hasReferenceResults {
			if err := in.translatePublicReferenceResults(fn.export, out, fn.resultTypes, fn.resultExact); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	for i, wide := range fn.resultWide {
		off := i * 8
		if wideInvocationSlotsForImage && wide {
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	if referenceSignaturesBuilt && fn.hasReferenceResults {
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
	entry, reuse, err := fn.beginNativeEntry()
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
		if (in.boundedHostSegments() || in.boundedFixedSharedHostPortal()) && preparedHostFixedEnabled {
			if err := fn.hostPrepared.EnableBoundedSegments(runtimebridge.GrantHostScalarCall()); err != nil {
				return err
			}
		}
		fn.hostMemBase = base
		fn.hostActivation = hostLoopActivation{
			root:                        in,
			ctrl:                        offHeapSlicePtr(in.ctrl),
			state:                       in.ensurePluginState(),
			entryNativeMu:               entry.local,
			parkedNativeContextReusable: in.parkedHostControlReusable(),
		}
		if preparedHostFixedEnabled {
			switch in.syncHosts[0].scalarKind {
			case syncHostTypedI32:
				fn.hostFixed = fn.hostActivation.dispatchSingleTypedI32FixedPortal
			case syncHostTypedI32x2:
				fn.hostFixed = fn.hostActivation.dispatchSingleTypedI32x2FixedPortal
			}
		}
	} else if !reuse {
		if err := in.jm.RebindTrapCell(in.trap); err != nil {
			return err
		}
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
	}
	if hasHostInvocationBindings() {
		restoreInvocationContext := bindHostInvocationParent(in, nil)
		defer restoreInvocationContext()
	}
	if atomicBlockingWaitRuntimeBuilt && in.c.usesAtomicWaitHelpers() {
		//lint:ignore SA1012 nil selects the helper's background context only when atomic waits are present.
		stopWaitContext := in.publishAtomicWaitContext(nil)
		defer stopWaitContext()
	}
	// Unlike a caller-owned session, each ordinary Invoke has a new invocation
	// identity. Foreign host dispatch must not reuse the previous call's cache.
	if fn.hostActivation.invocation.id != 0 {
		fn.hostActivation.invocation = hostInvocationContext{}
	}
	return in.callPreparedHostSyncAdmitted(fn.hostPrepared, fn.hostFixed, &fn.hostActivation)
}

func (fn *WasmFunc) invokeScalar(args []uint64) ([]uint64, error) {
	in := fn.in
	if fn.isolatedFast && !in.syncMode && !fn.gcMaintenance && fn.tryBeginFastInvocation() {
		// The fast gate already excludes ownership revocation and concurrent
		// entries. The asynchronous isolated wrapper cannot enter the host.
		out, err := fn.invokeScalarAdmittedState(args, true)
		in.endFastInvocation(in.pluginState.Load())
		return out, err
	}
	return fn.invokeScalarSlow(args)
}

// Keep the panic-safe cleanup closures off the isolated entry path.
//
//go:noinline
func (fn *WasmFunc) invokeScalarSlow(args []uint64) (result []uint64, resultErr error) {
	in := fn.in
	var admissionErr error
	if fn.isolatedFast {
		admissionErr = in.beginDirectInvocation()
	} else {
		admissionErr = in.beginInvocation()
	}
	if admissionErr != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", admissionErr)
	}
	if fn.isolatedFast {
		if fn.tryDirectGate() {
			out, err := fn.invokeScalarAdmitted(args)
			fn.directGate.Unlock()
			in.endDirectInvocation()
			return out, err
		}
	}
	if !nativeCollectorBuilt || !in.hasNativeCollector() && in.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain) == 0 {
		var preparedLease preparedScalarNoGCLease
		leaseOwned := false
		var cleanup resourceCleanup
		cleanup.run(func() {
			preparedLease = in.lockPreparedScalarNoGC()
			leaseOwned = true
			result, resultErr = fn.invokeScalarAdmitted(args)
		})
		if leaseOwned {
			cleanup.run(preparedLease.unlock)
		}
		cleanup.run(in.endInvocation)
		cleanup.finish()
		return result, resultErr
	}
	if preparedScalarInPlaceLease {
		var preparedLease preparedInvocationLease
		leaseOwned := false
		var cleanup resourceCleanup
		cleanup.run(func() {
			in.lockPreparedScalarInvocationInto(&preparedLease)
			leaseOwned = true
			result, resultErr = fn.invokeScalarAdmitted(args)
		})
		if leaseOwned {
			cleanup.run(preparedLease.unlockScalarInvocation)
		}
		cleanup.run(in.endInvocation)
		cleanup.finish()
		return result, resultErr
	}
	var preparedLease preparedInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		preparedLease = in.lockPreparedInvocation()
		leaseOwned = true
		result, resultErr = fn.invokeScalarAdmitted(args)
	})
	if leaseOwned {
		cleanup.run(preparedLease.unlock)
	}
	cleanup.run(in.endInvocation)
	cleanup.finish()
	return result, resultErr
}

func (fn *WasmFunc) invokeScalarAdmitted(args []uint64) ([]uint64, error) {
	return fn.invokeScalarAdmittedState(args, false)
}

func (fn *WasmFunc) invokeScalarAdmittedState(args []uint64, reserved bool) ([]uint64, error) {
	in := fn.in
	if !reserved && (fn.gcMaintenance || !in.preparedFastStateValid()) {
		return fn.invokeGeneralAdmitted(args)
	}
	if len(args) <= 4 {
		switch len(args) {
		case 4:
			putPreparedScalarArg(in.serArgs[24:], args[3], fn.scalarWideMask&8 != 0)
			fallthrough
		case 3:
			putPreparedScalarArg(in.serArgs[16:], args[2], fn.scalarWideMask&4 != 0)
			fallthrough
		case 2:
			putPreparedScalarArg(in.serArgs[8:], args[1], fn.scalarWideMask&2 != 0)
			fallthrough
		case 1:
			putPreparedScalarArg(in.serArgs, args[0], fn.scalarWideMask&1 != 0)
		}
	} else {
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, fn.paramWide, fn.paramWidthClass)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if syncHostControlForImage && in.syncMode {
		var err error
		if fn.isolatedFast {
			// The current fast gate may belong to Invoke or a reserved session.
			reserved := fn.directGate != nil && fn.directGate.state.Load()&(invocationGateHeld|invocationGateFast) == invocationGateHeld|invocationGateFast
			err = in.callPreparedIsolated(fn.entry, in.trap, reserved, true)
		} else if fn.boundedNumericHost && in.usesIndependentExecution() && !in.guestStorageBorrowed() {
			if in.syncHosts[0].privateBoundedTypedHost() {
				err = in.callCachedBoundedTypedHost(fn.entry)
			} else {
				err = in.callCachedBoundedHostView(fn.entry)
			}
		} else if !in.hasNativeCollector() && in.usesIndependentExecution() && (in.hasSingleDirectTypedScalarHost() || in.hasSingleExpandedTypedScalarHost()) {
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
			err = in.callPreparedIsolated(fn.entry, in.trap, reserved, fn.boundedWrapper || fn.boundedBulkArguments(args))
		} else if fn.privateFast {
			err = in.callPreparedPrivate(fn.entry, in.trap)
		} else {
			prepared := directPreparedCallEnabled && preparedCallEnabled && in.ownsMem
			err = in.callNativeAsync(fn.entry, prepared)
		}
		if err != nil {
			return nil, err
		}
		if hostFunctionImportsBuilt && len(in.hostLog) != 0 {
			if err := in.replayHostLog(); err != nil {
				return nil, err
			}
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	return out, nil
}

func putPreparedScalarArg(dst []byte, bits uint64, wide bool) {
	if !wide {
		bits = uint64(uint32(bits))
	}
	binary.LittleEndian.PutUint64(dst, bits)
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
		copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, fn.paramWide, fn.paramWidthClass)
	}
	if err := in.callNativeSyncAdmitted(fn.entry, in.trap, nil, prepared, fixed, activation, nil); err != nil {
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	return out, nil
}

// Resolve bounded numeric metadata with the handle. Invocation rechecks mutable
// ownership before selecting the private driver or retaining the general path.
func (in *Instance) preparedBoundedNumericEligible() bool {
	if codeProfileEnabled || in.rt != nil || !in.boundedHostSegments() || !in.singleTypedScalarHostEligible() ||
		!in.usesIndependentExecution() || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() ||
		len(in.hostLog) != 0 || in.guestStorageBorrowed() || in.refStore != nil && !in.refStore.private {
		return false
	}
	b := &in.syncHosts[0]
	if b.sig == nil || b.callbackGate() != nil {
		return false
	}
	if b.privateBoundedTypedHost() {
		return true
	}
	_, caller := b.fn.(CallerHostCallFunc)
	return b.scalarKind == syncHostScalar && (b.hostCall || caller) &&
		len(b.sig.Params) <= wruntime.MaxHostArity && len(b.sig.Results) <= wruntime.MaxHostArity
}

// Like the public warm cache, resolved private numeric entry takes ordinary
// lifecycle and invocation ownership. A session or revocation takes fallback.
func (fn *WasmFunc) tryInvokeBoundedNumeric(args []uint64) (out []uint64, err error, admitted bool) {
	in := fn.in
	state := in.pluginState.Load()
	if !in.tryBeginOrdinaryInvocation(state) {
		return nil, nil, false
	}
	privateRefStore := in.refStore == nil || in.refStore.private
	if !privateRefStore || in.guestStorageBorrowed() || !in.preparedFastStateValid() || !in.usesIndependentExecution() ||
		in.hasNativeCollector() || in.threadedMemoryZero || in.table != nil || in.importsFuncrefStorage() || len(in.hostLog) != 0 {
		in.endOrdinaryInvocation(state)
		return nil, nil, false
	}
	state.invocationID = newInvocationID()
	admitted = true
	var locked executionLease
	nativeOwned := false
	defer func() {
		defer func() { state.invocationID = 0; in.endOrdinaryInvocation(state) }()
		if nativeOwned {
			defer in.unlockNativeEntry(locked)
		}
		if r := recover(); r != nil {
			setNativeSyncPanicError(r, &err)
		}
		if err != nil {
			err = in.decorateTrap(err)
		}
	}()
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, fn.paramWide, fn.paramWidthClass)
	var reuse bool
	locked, reuse, err = in.beginCachedBoundedViewEntry()
	if err != nil {
		return nil, err, true
	}
	nativeOwned = true
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	activation := boundedViewHostActivation{
		boundedTypedHostActivation: boundedTypedHostActivation{
			root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: locked.local,
		},
	}
	binding := &in.syncHosts[0]
	rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
	if binding.privateBoundedTypedHost() {
		if goruntime.GOARCH == "arm64" || in.eng.PreparedScalarHost() != nil {
			fixed := wruntime.FixedScalarHostContextCall(boundedTypedHostDispatchI32)
			fallback := wruntime.FixedScalarHostCall(activation.dispatchI32)
			switch binding.scalarKind {
			case syncHostTypedI32x2:
				fixed = boundedTypedHostDispatchI32x2
				fallback = activation.dispatchI32x2
			case syncHostTypedI64:
				fixed = boundedTypedHostDispatchI64
				fallback = activation.dispatchI64
			}
			if prepared := in.eng.PreparedScalarHost(); prepared != nil {
				if locked.privateContext() {
					switch binding.scalarKind {
					case syncHostTypedI32:
						fixed = detachedNumericDispatchI32
					case syncHostTypedI32x2:
						fixed = detachedNumericDispatchI32x2
					case syncHostTypedI64:
						fixed = detachedNumericDispatchI64
					}
				}
				if prepared.IntegerGuestContext() {
					if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && directIntegerI32Enabled && locked.privateContext() && binding.scalarKind == syncHostTypedI32 {
						err = prepared.CallIntegerI32(fn.entry, locked.memoryBase(in), (func(int32) int32)(binding.typedI32))
					} else {
						err = prepared.CallInteger(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
					}
				} else {
					err = prepared.Call(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
				}
			} else {
				err = in.eng.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation.boundedTypedHostActivation), fixed, fallback)
			}
		} else {
			fixed := wruntime.FixedScalarHostCall(activation.dispatchI32)
			switch binding.scalarKind {
			case syncHostTypedI32x2:
				fixed = activation.dispatchI32x2
			case syncHostTypedI64:
				fixed = activation.dispatchI64
			}
			err = in.eng.CallWithHostBaseScalarBoundedLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fixed)
		}
	} else {
		fixed := wruntime.FixedHostContextCallView(boundedHostDispatchHostCallView)
		fallback := wruntime.FixedHostCallView(activation.dispatchHostCallView)
		_, caller := binding.fn.(CallerHostCallFunc)
		if caller {
			fixed = boundedHostDispatchCallerView
			fallback = activation.dispatchCallerView
		}
		// Both native architectures retain the method adapter for uncached Caller views.
		if prepared := in.eng.PreparedScalarHost(); prepared != nil {
			if locked.privateContext() {
				fixed = detachedNumericDispatchHostCall
				if caller {
					fixed = detachedNumericDispatchCaller
				}
			}
			if prepared.IntegerGuestContext() {
				err = prepared.CallIntegerView(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			} else {
				err = prepared.CallView(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			}
		} else if caller && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") {
			err = in.eng.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fallback)
		} else {
			err = in.eng.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation), fixed, fallback)
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err, true
	}
	out = in.resultVals[:fn.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	return out, nil, true
}

// Session entry already owns lifecycle and invocation reservations. Native
// ownership remains per-call so callbacks may publish resources or reenter.
func (fn *WasmFunc) invokeBoundedNumericAdmitted(args []uint64) (out []uint64, err error) {
	in := fn.in
	copyPublicScalarSlotsByClass(nativeUint64Slots(in.serArgs), args, fn.paramWide, fn.paramWidthClass)
	locked, reuse, err := in.beginCachedBoundedViewEntry()
	if err != nil {
		return nil, err
	}
	defer func() {
		defer in.unlockNativeEntry(locked)
		if r := recover(); r != nil {
			setNativeSyncPanicError(r, &err)
		}
		if err != nil {
			err = in.decorateTrap(err)
		}
	}()
	state := in.ensurePluginState()
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	activation := boundedViewHostActivation{
		boundedTypedHostActivation: boundedTypedHostActivation{
			root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: locked.local,
		},
	}
	binding := &in.syncHosts[0]
	rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
	if binding.privateBoundedTypedHost() {
		if goruntime.GOARCH == "arm64" || in.eng.PreparedScalarHost() != nil {
			fixed := wruntime.FixedScalarHostContextCall(boundedTypedHostDispatchI32)
			fallback := wruntime.FixedScalarHostCall(activation.dispatchI32)
			switch binding.scalarKind {
			case syncHostTypedI32x2:
				fixed = boundedTypedHostDispatchI32x2
				fallback = activation.dispatchI32x2
			case syncHostTypedI64:
				fixed = boundedTypedHostDispatchI64
				fallback = activation.dispatchI64
			}
			if prepared := in.eng.PreparedScalarHost(); prepared != nil {
				if locked.privateContext() {
					switch binding.scalarKind {
					case syncHostTypedI32:
						fixed = detachedNumericDispatchI32
					case syncHostTypedI32x2:
						fixed = detachedNumericDispatchI32x2
					case syncHostTypedI64:
						fixed = detachedNumericDispatchI64
					}
				}
				if prepared.IntegerGuestContext() {
					if (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") && directIntegerI32Enabled && locked.privateContext() && binding.scalarKind == syncHostTypedI32 {
						err = prepared.CallIntegerI32(fn.entry, locked.memoryBase(in), (func(int32) int32)(binding.typedI32))
					} else {
						err = prepared.CallInteger(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
					}
				} else {
					err = prepared.Call(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation.boundedTypedHostActivation), fixed)
				}
			} else {
				err = in.eng.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation.boundedTypedHostActivation), fixed, fallback)
			}
		} else {
			fixed := wruntime.FixedScalarHostCall(activation.dispatchI32)
			switch binding.scalarKind {
			case syncHostTypedI32x2:
				fixed = activation.dispatchI32x2
			case syncHostTypedI64:
				fixed = activation.dispatchI64
			}
			err = in.eng.CallWithHostBaseScalarBoundedLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fixed)
		}
	} else {
		fixed := wruntime.FixedHostContextCallView(boundedHostDispatchHostCallView)
		fallback := wruntime.FixedHostCallView(activation.dispatchHostCallView)
		_, caller := binding.fn.(CallerHostCallFunc)
		if caller {
			fixed = boundedHostDispatchCallerView
			fallback = activation.dispatchCallerView
		}
		// Both native architectures retain the method adapter for uncached Caller views.
		if prepared := in.eng.PreparedScalarHost(); prepared != nil {
			if locked.privateContext() {
				fixed = detachedNumericDispatchHostCall
				if caller {
					fixed = detachedNumericDispatchCaller
				}
			}
			if prepared.IntegerGuestContext() {
				err = prepared.CallIntegerView(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			} else {
				err = prepared.CallView(fn.entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
			}
		} else if caller && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") {
			err = in.eng.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fallback)
		} else {
			err = in.eng.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), fn.entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation), fixed, fallback)
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	if err != nil {
		return nil, err
	}
	out = in.resultVals[:fn.resultSlots]
	copyPublicScalarSlotsByClass(out, nativeUint64Slots(in.results), fn.resultWide, fn.resultWidthClass)
	return out, nil
}
