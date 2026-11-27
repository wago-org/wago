// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"

	"github.com/wago-org/wago/internal/buildfeatures"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64NativeImportTransportBytes = railshot.MaxNativeCallSlots * 8

func rv64NativeCallSignatureValues(sig FuncSig, referenceCarriers, collectorProof bool) (int, error) {
	for _, list := range [][]ValType{sig.Params, sig.Results} {
		if len(list) > 256 {
			return 0, fmt.Errorf("%w: RV64 native owner signature count", railshot.ErrUnsupported)
		}
		cells := 0
		for _, typ := range list {
			switch typ {
			case ValI32, ValI64, ValF32, ValF64:
				cells++
			case ValV128:
				cells += 2
			case ValAnyRef, ValI31Ref, ValExnRef:
				if buildfeatures.TailCall == 0 || !collectorProof {
					return 0, fmt.Errorf("%w: RV64 native collector owner signature proof", railshot.ErrUnsupported)
				}
				cells++
			case ValFuncRef, ValExternRef:
				if !referenceCarriers {
					return 0, fmt.Errorf("%w: RV64 native owner signature type", railshot.ErrUnsupported)
				}
				cells++
			default:
				return 0, fmt.Errorf("%w: RV64 native owner signature type", railshot.ErrUnsupported)
			}
		}
		if cells > railshot.MaxNativeCallSlots {
			return 0, fmt.Errorf("%w: RV64 native owner physical signature bound", railshot.ErrUnsupported)
		}
	}
	return len(sig.Params) + len(sig.Results), nil
}

// Native imported starts enter the constructor's already published owned bridge,
// with the same invocation identity, cancellation and stack used by local starts.
// No producer public invocation is started: its physical attachment is the lease.
func railshotInvokeNativeImportedStart(in *Instance, index int, owned map[uint32]uint64, ctx context.Context) (bool, error) {
	entry := uintptr(owned[uint32(index)])
	base := offHeapSlicePtr(in.thunkMem)
	if entry == 0 || entry&15 != 0 || entry < base || entry-base >= uintptr(len(in.thunkMem)) {
		return true, fmt.Errorf("RV64 native imported start requires its owned framed bridge")
	}
	if err := in.beginInvocation(); err != nil {
		return true, err
	}
	defer in.endInvocation()
	state := in.lockInvocation(0)
	defer state.unlockInvocation()
	if err := in.invokeVoidEntry(ctx, entry, in.constructionReservationSnapshot()); err != nil {
		return true, err
	}
	return true, railshotCheckImportedStartCompletion(in, ctx)
}

func railshotCheckImportedStartCompletion(in *Instance, ctx context.Context) error {
	// A final native poll can precede Close/cancellation racing normal return.
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	// An admitted plugin operation retains its terminal observers through runtime
	// shutdown. A normal imported-start return must reach those observers before
	// Runtime.instantiateOrigin rejects publication of the closed instance.
	if in.rt != nil && in.rt.isClosed() && hasPluginReservation(in.constructionReservationSnapshot()) {
		return nil
	}
	if in.isLogicallyClosed() || in.rt != nil && in.rt.isClosed() {
		return fmt.Errorf("instance is closed")
	}
	return nil
}

func railshotNativeContextTrapABI() bool { return true }

// Each retained edge has full-width transport even when the public signature is
// narrow: the producer may make wider internal calls on the same transport.
// Admission bounds the edge count before this footprint is computed.
func railshotNativeImportArenaBytes(c *Compiled, imports resolvedImports) int {
	// Public native imports carry arguments/results in their checked stack
	// activation. Their certificate retains no unused arena transport buffers.
	return 0
}

func railshotRequiresPrivateOwnedHostThunk(c *Compiled) bool {
	return c != nil
}

func railshotNativeContextIdentity(jm *coreruntime.JobMemory) uintptr { return jm.RV64NativeContext() }
func railshotSetNativeContextIdentity(jm *coreruntime.JobMemory, identity uintptr) {
	jm.SetRV64NativeContext(identity)
}

func railshotAlignNativeImportBlob(blob []byte) []byte {
	// Every RV64 thunk consists of fixed four-byte instructions.
	for len(blob)&15 != 0 {
		blob = append(blob, 0x13, 0, 0, 0)
	}
	return blob
}

// This pre-publication check borrows the producer's public invocation lease so a
// concurrent Close cannot clear its native pointers while validation reads them.
// The later attachment repeats lifetime admission under the physical ownership
// lock. Signature and ABI validation rely on the producer's frozen metadata.
func railshotValidateNativeExport(c *Compiled, index int, export *InstanceExport) error {
	if export == nil || export.inst == nil {
		return fmt.Errorf("native instance export is nil")
	}
	producer := export.inst
	if err := producer.beginInvocation(); err != nil {
		return err
	}
	defer producer.endInvocation()
	if err := railshotValidateCompiled(producer.c); err != nil {
		return err
	}
	if producer.hostEvents != nil {
		return deferredHostEventCalleeError()
	}
	if producer.jm == nil || producer.base == 0 || producer.nativeContext == 0 || export.localIdx < 0 || export.localIdx >= len(producer.c.Entry) || export.localIdx >= len(producer.c.Funcs) {
		return fmt.Errorf("native instance export has no retained framed entry")
	}
	if wasmGCBuilt {
		consumerProduct := c.stagedGCTypeSubtypingProduct()
		if expectedProvider := consumerProduct.linkProviderProduct(); expectedProvider != 0 && producer.c.stagedGCTypeSubtypingProduct() != expectedProvider {
			return fmt.Errorf("native instance export signature mismatch: producer is outside the exact gc/type-subtyping link product")
		}
	}
	allowSubtype := rv64PrimitiveReferenceContract(c) && rv64PrimitiveReferenceContract(producer.c) ||
		(c.usesDynamicFuncRefTest() && rv64FreshRootedGC(c) && (rv64FreshRootedGC(producer.c) || rv64PrimitiveReferenceContract(producer.c))) ||
		rv64LegacyGCProductsForImage && rv64LegacyGCImportSubtype(c, producer.c)
	if index < 0 || index >= len(c.importFuncSigs) || !sigMatches(c.importFuncSigs[index], c.Types, export, allowSubtype) {
		return fmt.Errorf("native instance export signature mismatch")
	}
	return nil
}

// Called only after function attachment, which retains the producer through
// failure cleanup and through the importing instance's final physical release.
// A producer logically closed after attachment remains callable by that owner.
func railshotNativeImportCode(c *Compiled, export *InstanceExport, arena *coreruntime.Arena, callerHome, callerContext uintptr, index int, construction *railshotNativeImportConstruction) ([]byte, bool, error) {
	if export == nil || export.inst == nil || index < 0 || index >= len(c.importFuncSigs) {
		return nil, true, fmt.Errorf("invalid retained native import")
	}
	producer := export.inst
	if producer.jm == nil || producer.nativeContext == 0 || export.localIdx < 0 || export.localIdx >= len(producer.c.Entry) {
		return nil, true, fmt.Errorf("native import lost its attached producer")
	}
	sig := c.importFuncSigs[index]
	args, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, true, err
	}
	results, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, true, err
	}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		return nil, true, err
	}
	target := railshot.NativeImportTarget{
		Entry: producer.base + uintptr(producer.c.Entry[export.localIdx]),
		Home:  producer.jm.LinMemBase(), Context: producer.nativeContext,

		ArgumentSlots: args, ResultSlots: results, StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline,
	}
	if len(construction.records) != 0 {
		code, certificate, err := railshot.NativeImportThunkWithCertificate(target, callerHome, callerContext, uint32(index))
		if err == nil {
			construction.records[index] = rv64NativeImportRecord{producer: producer, local: export.localIdx, target: target, certificate: certificate}
		}
		return code, true, err
	}
	code, err := railshot.NativeImportThunk(target, callerHome, callerContext, uint32(index))
	return code, true, err
}

// Resolve only within the frozen physically attached function graph. A trap
// carries an opaque context identity; never dereference it or infer an owner
// from a shared memory home. Work and storage are bounded even for cyclic graphs.
// Consume once so a retained TrapError cannot acquire a new owner's name after
// context addresses are recycled. Root admission keeps the graph alive here.
func railshotTrapOwner(root *Instance, trap *coreruntime.TrapError) (*Instance, bool) {
	identity, present, pending := trap.TakeNativeOrigin()
	if !pending {
		return nil, false
	}
	if !present {
		return root, true
	}
	if identity == 0 {
		return nil, true
	}
	return railshotAttachedNativeOwner(root, identity), true
}

func railshotAttachedNativeOwner(root *Instance, identity uintptr) *Instance {
	if !foreignHostControlForImage {
		if root != nil && root.nativeContext == identity {
			return root
		}
		return nil
	}
	if owner := rv64ActiveTableTrapOwner(root, identity); owner != nil {
		return owner
	}
	const maxOwners, maxEdges = 256, 4096
	var owners [maxOwners + 1]*Instance
	owners[0] = root
	count, edges := 1, 0
	for cursor := 0; cursor < count; cursor++ {
		owner := owners[cursor]
		if owner == nil || owner.c == nil {
			continue
		}
		if owner.nativeContext == identity {
			return owner
		}
		for index := range owner.c.Imports {
			if edges == maxEdges {
				return nil
			}
			edges++
			export, native := rv64NativeFunctionAttachment(owner, index)
			if !native || export == nil || export.inst == nil {
				continue
			}
			target := export.inst
			if target.nativeContext == identity {
				return target
			}
			seen := false
			for i := 0; i < count; i++ {
				if owners[i] == target {
					seen = true
					break
				}
			}
			if !seen && count < len(owners) {
				owners[count] = target
				count++
			}
		}
	}
	return nil
}

// The registry names physically live host owners by opaque context identity.
// Attachments retain their registration through the last foreign activation.
var rv64NativeHostOwners = struct {
	sync.Mutex
	values map[uintptr]rv64NativeHostOwner
}{}

type rv64NativeHostOwner struct {
	owner   *Instance
	aliases int
}

func railshotRegisterNativeHostOwner(in *Instance) error {
	if in.nativeContext == 0 {
		return fmt.Errorf("RV64 host owner has no native context")
	}
	if !foreignHostControlForImage {
		return nil
	}
	rv64NativeHostOwners.Lock()
	defer rv64NativeHostOwners.Unlock()
	if rv64NativeHostOwners.values == nil {
		rv64NativeHostOwners.values = make(map[uintptr]rv64NativeHostOwner)
	}
	entry := rv64NativeHostOwners.values[in.nativeContext]
	if entry.owner != nil && entry.owner != in {
		return fmt.Errorf("RV64 host owner context already registered")
	}
	entry.owner = in
	entry.aliases++
	rv64NativeHostOwners.values[in.nativeContext] = entry
	return nil
}
func railshotUnregisterNativeHostOwner(in *Instance) {
	rv64NativeHostOwners.Lock()
	defer rv64NativeHostOwners.Unlock()
	entry := rv64NativeHostOwners.values[in.nativeContext]
	if entry.owner != in {
		return
	}
	entry.aliases--
	if entry.aliases == 0 {
		delete(rv64NativeHostOwners.values, in.nativeContext)
	} else {
		rv64NativeHostOwners.values[in.nativeContext] = entry
	}
}

// Core has already authenticated the full stack control frame before reading it.
// Its private bridge header names an opaque owner; no raw identity is dereferenced.
func railshotResolveNativeHostControl(root *Instance, ctrl uintptr) *Instance {
	if root == nil || root.eng == nil || ctrl&15 != 0 || ctrl < root.eng.StackLimit()+16 || ctrl > root.eng.StackTop() || root.eng.StackTop()-ctrl < railshot.HostFixedFrameBytes {
		return nil
	}
	view := root.eng.NativeStackRange(ctrl-16, railshot.HostFixedFrameBytes+16)
	if view == nil {
		return nil
	}
	identity := uintptr(binary.LittleEndian.Uint64(view))
	owner := root
	if foreignHostControlForImage {
		rv64NativeHostOwners.Lock()
		owner = rv64NativeHostOwners.values[identity].owner
		rv64NativeHostOwners.Unlock()
	}
	if owner == nil {
		return nil
	}
	frame := view[16:]
	home := uintptr(binary.LittleEndian.Uint64(frame[railshot.HostSavedHomeOffset:]))
	if owner.jm == nil || owner.jm.LinMemBase() != home || owner.nativeContext != identity || railshotNativeContextIdentity(owner.jm) != identity {
		return nil
	}
	return owner
}

// Public reexports use the relay's transport and engine. Entering the producer's
// public buffers here would race other relays or a producer Invoke while parked.
func railshotInvokeNativeReexport(in *Instance, name string, index int, args []uint64, contexts invocationContextSet) ([]uint64, bool, error) {
	if index < 0 || index >= len(in.c.importFuncSigs) || in.nativeContext == 0 {
		return nil, true, fmt.Errorf("RV64 native reexport has no retained import context")
	}
	sig := in.c.importFuncSigs[index]
	params, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, true, err
	}
	results, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, true, err
	}
	if len(args) != params {
		return nil, true, fmt.Errorf("%s expects %d arg slot(s), got %d", name, params, len(args))
	}
	if params > railshot.MaxNativeCallSlots || results > railshot.MaxNativeCallSlots || params > len(in.serArgs)/8 || results > len(in.results)/8 || results > len(in.resultVals) {
		return nil, true, fmt.Errorf("RV64 native reexport exceeds instance call buffers")
	}
	// Read the immutable per-instance snapshot, not the currently bound shared
	// memory home, which may still name a different instance between entries.
	ctx := in.ar.AllocatedRange(in.nativeContext, coreruntime.InstanceContextBytes)
	if len(ctx) != coreruntime.InstanceContextBytes {
		return nil, true, fmt.Errorf("RV64 native reexport context is outside its retained arena")
	}
	dispatch := uintptr(binary.LittleEndian.Uint64(ctx[coreruntime.InstanceContextImportDispatchOffset:]))
	if dispatch == 0 {
		return nil, true, fmt.Errorf("RV64 native reexport has no owned dispatch")
	}
	directory := in.ar.AllocatedRange(dispatch, in.c.NumImports*coreruntime.ImportDispatchEntryBytes)
	if len(directory) != in.c.NumImports*coreruntime.ImportDispatchEntryBytes {
		return nil, true, fmt.Errorf("RV64 native reexport dispatch is outside its retained arena")
	}
	row := directory[index*coreruntime.ImportDispatchEntryBytes:]
	entry := uintptr(binary.LittleEndian.Uint64(row[coreruntime.ImportDispatchCodePtrOffset:]))
	base := offHeapSlicePtr(in.thunkMem)
	if entry == 0 || entry&15 != 0 || entry < base || entry-base >= uintptr(len(in.thunkMem)) {
		return nil, true, fmt.Errorf("RV64 native reexport requires its owned framed bridge")
	}
	if err := in.collectGenericGCAtBoundary(); err != nil {
		return nil, true, err
	}
	if referenceSignaturesBuilt && hasReferenceValType(sig.Params) {
		owners, err := railshotBeginPublicFuncrefArguments(in)
		if err != nil {
			return nil, true, err
		}
		defer railshotReleasePublicFuncrefArguments(owners)
		exact, _, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, true, err
		}
		if err := in.marshalPublicReferenceArgs(name, args, sig.Params, exact, &owners); err != nil {
			return nil, true, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, sig.Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	if contexts.interrupt != nil {
		stop, err := in.startCancellationWatch(contexts.interrupt, in.trap)
		if err != nil {
			return nil, true, err
		}
		defer stop()
	}
	var executionErr error
	if syncHostControlForImage && in.syncMode {
		executionErr = in.callNativeSyncWithTrapContext(entry, in.trap, contexts.callback)
	} else {
		executionErr = in.callNativeAsync(entry, false)
		if hostFunctionImportsBuilt && executionErr == nil {
			executionErr = in.replayHostLog()
		}
	}
	if err := in.reconcileGCGlobalRootsAfter(executionErr); err != nil {
		return nil, true, err
	}
	out := in.resultVals[:results]
	slot := 0
	for _, typ := range sig.Results {
		width := 1
		if typ == ValV128 {
			width = 2
		}
		for half := 0; half < width; half++ {
			if isWideValType(typ) || typ == ValV128 {
				out[slot] = binary.LittleEndian.Uint64(in.results[slot*8:])
			} else {
				out[slot] = uint64(binary.LittleEndian.Uint32(in.results[slot*8:]))
			}
			slot++
		}
	}
	if referenceSignaturesBuilt && hasReferenceValType(sig.Results) {
		_, exact, err := exactFuncSignatureView(sig, in.c.Types)
		if err != nil {
			return nil, true, err
		}
		if err := in.translatePublicReferenceResultsMode(name, out, sig.Results, exact, true); err != nil {
			return nil, true, err
		}
	}
	runtime.KeepAlive(in)
	return out, true, nil
}

// Imported calls marshal into the caller's serialized transport before entering
// their owned bridge. Every imported signature, including unexported functions,
// must therefore contribute to the root buffers before arena allocation. Tables
// can reach signatures with no local definition, so their frozen type directory
// contributes too, including currently unused indirect signatures.
func railshotImportedCallSlots(c *Compiled, params, results int) (int, int, error) {
	if rv64StructuralMetadata(c) {
		// This legacy image declares no executable RV64 ABI. Common codec
		// validation has checked its stored signatures; native transport sizing
		// is only needed after execution admission succeeds.
		return params, results, nil
	}
	if c.NumImports < 0 || c.NumImports > railshot.MaxFunctionImports || len(c.importFuncSigs) != c.NumImports {
		return 0, 0, fmt.Errorf("RV64 imported call signature directory is invalid")
	}
	if c.usesDynamicFuncRefTest() {
		params = max(params, 3)
		results = max(results, 1)
	}
	if c.usesAtomicWaitHelpers() {
		// The atomic bridge stages operands in the instance's call buffers.
		// A function can wait or notify using constants and drop the result.
		params, results = max(params, 4), max(results, 1)
	}
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return 0, 0, fmt.Errorf("RV64 function export index out of range")
		}
	}
	for _, sig := range c.importFuncSigs {
		p, err := valTypesSlots(sig.Params)
		if err != nil || p > railshot.MaxNativeCallSlots {
			return 0, 0, fmt.Errorf("RV64 imported function parameter slot bound")
		}
		r, err := valTypesSlots(sig.Results)
		if err != nil || r > railshot.MaxNativeCallSlots {
			return 0, 0, fmt.Errorf("RV64 imported function result slot bound")
		}
		params, results = max(params, p), max(results, r)
	}
	if rv64LegacyGCProductsForImage {
		params, results = rv64LegacyGCHelperSlots(c, params, results)
	}
	if !tablesForImage && c.tableCount() != 0 || !rv64ReferenceCallsForImage && c.nativeReferenceCallABI {
		return 0, 0, fmt.Errorf("RV64 indirect call directory exceeds the standalone runtime profile")
	}
	if tablesForImage && c.tableCount() != 0 || rv64ReferenceCallsForImage && c.nativeReferenceCallABI {
		if rv64LegacyGCProductsForImage && rv64LegacyGCTableUsesLocalSignatures(c) {
			return params, results, nil
		}
		if len(c.Types) > railshot.MaxFunctions {
			return 0, 0, fmt.Errorf("RV64 indirect type directory bound")
		}
		// A function-free module cannot reach an indirect or reference call. Its
		// table may still be public state, so validate the exact frozen directory
		// while excluding its declarative signatures from transport capacities.
		if c.hasNoFunctionCode() {
			if _, err := rv64ValidateDeclarativeControlTypes(c); err != nil {
				return 0, 0, err
			}
			return params, results, nil
		}
		rootedGC := rv64FreshRootedGC(c)
		functionGraph := compiledFunctionOnlyTypeGraph(c) || rv64PrimitiveReferenceDeclarations(c)
		for i := range c.Types {
			typ := &c.Types[i]
			if rootedGC || functionGraph {
				// Generic rooted modules retain their complete GC type graph next to
				// the function signatures. Struct and array definitions are not
				// callable directory entries, while reference values in a function
				// signature still occupy one physical transport cell.
				if typ.Kind != CompositeTypeFunction {
					continue
				}
				p, err := rv64RootedSignatureSlots(typ.Params)
				if err != nil {
					return 0, 0, err
				}
				r, err := rv64RootedSignatureSlots(typ.Results)
				if err != nil {
					return 0, 0, err
				}
				params, results = max(params, p), max(results, r)
				continue
			}
			controlOnly, err := rv64DeclaredControlSignature(c, typ)
			if err != nil {
				return 0, 0, err
			}
			if controlOnly {
				continue
			}
			p, err := rv64IndirectSignatureSlots(typ.Params)
			if err != nil {
				return 0, 0, err
			}
			r, err := rv64IndirectSignatureSlots(typ.Results)
			if err != nil {
				return 0, 0, err
			}
			params, results = max(params, p), max(results, r)
		}
	}
	return params, results, nil
}

func rv64RootedSignatureSlots(values []ValueTypeDescriptor) (int, error) {
	if len(values) > railshot.MaxVectorFunctionValues {
		return 0, fmt.Errorf("RV64 rooted signature value bound")
	}
	slots := 0
	for _, value := range values {
		switch value.Kind {
		case ValueTypeI32, ValueTypeI64, ValueTypeF32, ValueTypeF64, ValueTypeReference:
			slots++
		case ValueTypeV128:
			slots += 2
		default:
			return 0, fmt.Errorf("RV64 rooted signature carrier kind")
		}
		if slots > railshot.MaxNativeCallSlots {
			return 0, fmt.Errorf("RV64 rooted signature physical slot bound")
		}
	}
	return slots, nil
}

// Count directly from immutable descriptors without allocating ABI slices.
func rv64IndirectSignatureSlots(values []ValueTypeDescriptor) (int, error) {
	slots := 0
	for _, value := range values {
		switch value.Kind {
		case ValueTypeI32, ValueTypeI64, ValueTypeF32, ValueTypeF64:
			slots++
		case ValueTypeV128:
			slots += 2
		default:
			return 0, fmt.Errorf("RV64 indirect signature requires numeric carriers")
		}
		if slots > railshot.MaxNativeCallSlots {
			return 0, fmt.Errorf("RV64 indirect signature physical slot bound")
		}
	}
	return slots, nil
}

// Callback authority and ownership of the producer's public gate differ for a
// foreign activation. Only its actual gate owner may swap parked public scratch.
type railshotInvocationState struct {
	nativeImports      *railshotNativeImportConstruction
	tail               rv64NativeTailInvocationStorage
	tailTargets        []byte // immutable instance-arena storage; retained with its descriptor owner
	gateOwner          invocationID
	longImportKeysOnce sync.Once
	longImportKeys     []string
	tableTargets       [rv64MaxTableEntryDepth]*rv64NativeTableTargets
	argumentOwners     [rv64MaxTableEntryDepth]*rv64InvocationArgumentOwners
	tableDepth         int
}

func railshotSetInvocationGateOwner(state *instancePluginState, id invocationID) {
	state.activations.mu.Lock()
	state.railshotInvocation.gateOwner = id
	state.activations.mu.Unlock()
}
func railshotCanReenter(in *Instance, id invocationID) bool {
	state := in.pluginState.Load()
	if state == nil {
		return false
	}
	state.activations.mu.Lock()
	defer state.activations.mu.Unlock()
	return id != 0 && state.railshotInvocation.gateOwner == id
}

// Independent roots can park in one producer simultaneously. Each foreign
// callback needs its own generation so expiry of one cannot revoke another.
func railshotCallbackHostScope(in *Instance, ctrl uintptr, ordinary *hostCallScope, parent context.Context) *hostCallScope {
	if in != nil && ctrl != 0 && ctrl == offHeapSlicePtr(in.ctrl) {
		return ordinary
	}
	return &hostCallScope{railshotHostScopeParent: railshotHostScopeParent{parent: parent, bound: true}}
}
