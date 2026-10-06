//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64NativeImportTransportBytes = 256 * 8

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
	bytes := 0
	for i := range c.Imports {
		if _, native := imports[c.functionImportBindingKey(i)].(*InstanceExport); native {
			bytes += 2 * rv64NativeImportTransportBytes
		}
	}
	return bytes
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
	if index < 0 || index >= len(c.importFuncSigs) || !sigMatches(c.importFuncSigs[index], c.Types, export, false) {
		return fmt.Errorf("native instance export signature mismatch")
	}
	return nil
}

// Called only after function attachment, which retains the producer through
// failure cleanup and through the importing instance's final physical release.
// A producer logically closed after attachment remains callable by that owner.
func railshotNativeImportCode(c *Compiled, export *InstanceExport, arena *coreruntime.Arena, callerHome, callerContext uintptr, index int) ([]byte, bool, error) {
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
	code, err := railshot.NativeImportThunk(railshot.NativeImportTarget{
		Entry: producer.base + uintptr(producer.c.Entry[export.localIdx]),
		Home:  producer.jm.LinMemBase(), Context: producer.nativeContext,
		Arguments: arena.Alloc(rv64NativeImportTransportBytes), Results: arena.Alloc(rv64NativeImportTransportBytes),
		ArgumentSlots: args, ResultSlots: results, StackOwnedActivation: true, HostTrampoline: trampoline,
	}, callerHome, callerContext, uint32(index))
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
			export, native := owner.imports[owner.c.functionImportBindingKey(index)].(*InstanceExport)
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
}{values: make(map[uintptr]rv64NativeHostOwner)}

type rv64NativeHostOwner struct {
	owner   *Instance
	aliases int
}

func railshotRegisterNativeHostOwner(in *Instance) error {
	if in.nativeContext == 0 {
		return fmt.Errorf("RV64 host owner has no native context")
	}
	rv64NativeHostOwners.Lock()
	defer rv64NativeHostOwners.Unlock()
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
	identity := uintptr(binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(ctrl-16)), 8)))
	rv64NativeHostOwners.Lock()
	owner := rv64NativeHostOwners.values[identity].owner
	rv64NativeHostOwners.Unlock()
	if owner == nil {
		return nil
	}
	frame := unsafe.Slice((*byte)(offHeapPtr(ctrl)), railshot.HostFixedFrameBytes)
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
	if params > 256 || results > 256 || params > len(in.serArgs)/8 || results > len(in.results)/8 || results > len(in.resultVals) {
		return nil, true, fmt.Errorf("RV64 native reexport exceeds instance call buffers")
	}
	// Read the immutable per-instance snapshot, not the currently bound shared
	// memory home, which may still name a different instance between entries.
	ctx := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	dispatch := uintptr(binary.LittleEndian.Uint64(ctx[coreruntime.InstanceContextImportDispatchOffset:]))
	if dispatch == 0 {
		return nil, true, fmt.Errorf("RV64 native reexport has no owned dispatch")
	}
	row := unsafe.Slice((*byte)(offHeapPtr(dispatch)), in.c.NumImports*coreruntime.ImportDispatchEntryBytes)[index*coreruntime.ImportDispatchEntryBytes:]
	entry := uintptr(binary.LittleEndian.Uint64(row[coreruntime.ImportDispatchCodePtrOffset:]))
	base := offHeapSlicePtr(in.thunkMem)
	if entry == 0 || entry&15 != 0 || entry < base || entry-base >= uintptr(len(in.thunkMem)) {
		return nil, true, fmt.Errorf("RV64 native reexport requires its owned framed bridge")
	}
	marshalPublicScalarArgs(in.serArgs, args, sig.Params)
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
	if err := in.callNativeSyncWithTrapContext(entry, in.trap, contexts.callback); err != nil {
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
	runtime.KeepAlive(in)
	return out, true, nil
}

// Imported calls marshal into the caller's serialized transport before entering
// their owned bridge. Every imported signature, including unexported functions,
// must therefore contribute to the root buffers before arena allocation.
func railshotImportedCallSlots(c *Compiled, params, results int) (int, int, error) {
	if c.NumImports < 0 || c.NumImports > railshot.MaxHostFunctionImports || len(c.importFuncSigs) != c.NumImports {
		return 0, 0, fmt.Errorf("RV64 imported call signature directory is invalid")
	}
	for _, index := range c.Exports {
		if index < 0 || index >= c.NumImports+len(c.Funcs) {
			return 0, 0, fmt.Errorf("RV64 function export index out of range")
		}
	}
	for _, sig := range c.importFuncSigs {
		p, err := valTypesSlots(sig.Params)
		if err != nil || p > 256 {
			return 0, 0, fmt.Errorf("RV64 imported function parameter slot bound")
		}
		r, err := valTypesSlots(sig.Results)
		if err != nil || r > 256 {
			return 0, 0, fmt.Errorf("RV64 imported function result slot bound")
		}
		params, results = max(params, p), max(results, r)
	}
	return params, results, nil
}

// Callback authority and ownership of the producer's public gate differ for a
// foreign activation. Only its actual gate owner may swap parked public scratch.
type railshotInvocationState struct{ gateOwner invocationID }

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
func railshotCallbackHostScope(in *Instance, ctrl uintptr, ordinary *hostCallScope) *hostCallScope {
	if owner, registered := hostControlInstances.Load(ctrl); !registered || owner != in {
		return new(hostCallScope)
	}
	return ordinary
}
