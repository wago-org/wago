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
		Arguments: arena.Alloc(rv64NativeImportTransportBytes), Results: arena.Alloc(rv64NativeImportTransportBytes),
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
// must therefore contribute to the root buffers before arena allocation. Tables
// can reach signatures with no local definition, so their frozen type directory
// contributes too, including currently unused indirect signatures.
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
	if rv64GCArrayInitElem(c) || rv64GCArrayInitData(c) {
		// The generated helper bridge stages array, destination, source, length,
		// type, and element indexes in the ordinary wrapper argument arena. This
		// internal six-slot call can exceed every exported function signature;
		// retain its complete capacity before allocating adjacent result/trap
		// buffers so helper staging cannot cross an arena boundary.
		params = max(params, 6)
	}
	if rv64GCStructNull(c) || rv64GCStructPacked(c) || rv64GCStructBasic(c) {
		// struct.set stages the reference, value, exact type, and field index.
		params = max(params, 4)
	}
	if rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefTestAbstract(c) {
		// ref.test stages the candidate word, signed heap type, nullability, and
		// exactness. Preserve all four cells even when every exported function is
		// parameterless so the helper cannot cross into results/trap storage.
		params = max(params, 4)
	}
	if rv64GCRefCastConcrete(c) {
		// Concrete ref.cast uses the same four arguments and returns one reference.
		params = max(params, 4)
		results = max(results, 1)
	}
	if rv64GCExtern(c) {
		// table.set stages the zero-normalized index, full-width reference, and
		// exact table index. Conversion helpers use one slot.
		params = max(params, 3)
	}
	if rv64GCRefCastAbstract(c) {
		// ref.cast stages the reference, heap code, nullability, and exactness.
		params = max(params, 4)
		results = max(results, 1)
	}
	if rv64GCArrayFill(c) {
		// The packed fill bridge adds the exact type index after the array,
		// destination, value, and length operands.
		params = max(params, 5)
	}
	if rv64GCArrayCopy(c) {
		// The copy bridge carries both arrays, both offsets, length, and both
		// exact type indexes.
		params = max(params, 7)
	}
	if c.tableCount() != 0 || c.nativeReferenceCallABI {
		// The exact allocating gc/ref_eq checkpoint has an eqref table but no
		// indirect calls. The exact array.init_elem checkpoint pins its sole
		// indirect signature to type 0, which has no parameters or results. Their
		// complete retained classifiers pin the non-function type graph; only the
		// local function signatures already counted above contribute to transport
		// capacity.
		if rv64OfficialGCRefEq(c) || rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefCastConcrete(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCArrayInitElem(c) {
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
		for i := range c.Types {
			typ := &c.Types[i]
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
		if slots > 256 {
			return 0, fmt.Errorf("RV64 indirect signature physical slot bound")
		}
	}
	return slots, nil
}

// Callback authority and ownership of the producer's public gate differ for a
// foreign activation. Only its actual gate owner may swap parked public scratch.
type railshotInvocationState struct {
	nativeImports        *railshotNativeImportConstruction
	tailDispatcher       *railshot.NativeTailDispatcher
	tailPolicy           rv64TailAllocationPolicy
	tailGenerationBudget rv64NativeTailGenerationBudget
	activeTailGeneration *rv64NativeTailGeneration
	definedTailOwner     *railshot.NativeTailDefinedOwner
	tailTargets          []byte // immutable instance-arena storage; retained with its descriptor owner
	gateOwner            invocationID
	longImportKeysOnce   sync.Once
	longImportKeys       []string
	tableTargets         [rv64MaxTableEntryDepth]*rv64NativeTableTargets
	argumentOwners       [rv64MaxTableEntryDepth]*rv64InvocationArgumentOwners
	tableDepth           int
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
func railshotCallbackHostScope(in *Instance, ctrl uintptr, ordinary *hostCallScope) *hostCallScope {
	if owner, registered := hostControlInstances.Load(ctrl); !registered || owner != in {
		return new(hostCallScope)
	}
	return ordinary
}
