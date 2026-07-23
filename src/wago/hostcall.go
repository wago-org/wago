package wago

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/wago-org/wago/src/core/runtime"
)

// HostModule gives a synchronous host import access to the instance that called
// it. It is passed as the optional leading parameter of a host function.
type HostModule interface {
	// Memory returns the calling instance's linear memory as a mutable slice
	// (empty if the module declares no memory). Writes are visible to wasm; the
	// slice is valid only for the duration of the host call.
	Memory() []byte
}

// ExternRefHostModule is the optional reference-store surface implemented by the
// HostModule value wago passes to callbacks. Keeping it separate preserves the
// minimal HostModule interface for existing mocks and wrappers.
type ExternRefHostModule interface {
	HostModule
	// NewExternRef registers an embedder object in the calling instance's
	// compatible reference store and returns a non-null opaque token.
	NewExternRef(any) (ExternRef, error)
	// ExternRefValue resolves a token from the calling instance's compatible
	// store. Forged, stale, and incompatible-store tokens return false.
	ExternRefValue(ExternRef) (any, bool)
}

// HostFunc is a host import in reflection-free slot (stack) form: it reads its
// wasm params from params (i32/f32 in the low 32 bits) and writes its results
// into results, with the calling instance's linear memory and externref store
// available through HostModule. A reference occupies one opaque uint64 slot; a
// v128 occupies two adjacent little-endian uint64 slots, matching Invoke's public
// ABI. It is the single host-import type — it binds identically
// under standard Go and TinyGo — with no reflection anywhere on the path.
type HostFunc func(m HostModule, params, results []uint64)

// CallerResolver resolves the exact Runtime-owned instance making an active
// synchronous host call. Its authority is identity-only: it cannot create,
// invoke, close, manage, pool, or otherwise control instances.
//
// Resolve succeeds only while the HostFunc callback is active. Retaining the
// HostModule and resolving it after the callback returns fails closed.
type CallerResolver struct {
	rt atomic.Pointer[Runtime]
}

func (r *CallerResolver) activate(rt *Runtime) {
	if r == nil || rt == nil {
		return
	}
	r.rt.Store(rt)
	rt.callerResolverActive.Store(true)
}

// Resolve returns the exact instance making caller's active synchronous host
// call. Forged, expired, cross-runtime, and low-level HostModule values are
// rejected.
func (r *CallerResolver) Resolve(caller HostModule) (*Instance, error) {
	if r == nil {
		return nil, fmt.Errorf("wago: nil caller resolver: %w", ErrPermissionDenied)
	}
	rt := r.rt.Load()
	h, ok := caller.(instanceHostModule)
	if rt == nil || !ok || !h.valid() || h.in == nil || h.in.rt != rt {
		return nil, fmt.Errorf("wago: caller identity requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return h.in, nil
}

// hostCallScope authorizes one synchronous use of an instanceHostModule.
type hostCallScope struct {
	next   uint64
	active atomic.Uint64
	waiter atomic.Pointer[hostCallWaiter]
}

type hostCallWaiter struct {
	generation uint64
	wake       chan struct{}
}

type instancePluginState struct {
	hostScope hostCallScope
	close     atomic.Pointer[instanceCloseState]
	gcConfig  *GCConfig
	origin    InstantiateOrigin
}

type instanceCloseState struct {
	done   chan struct{}
	result error
}

func (in *Instance) instantiateOrigin() InstantiateOrigin {
	if state := in.pluginState.Load(); state != nil {
		return state.origin
	}
	return InstantiateDirect
}

func (s *hostCallScope) begin(in *Instance) instanceHostModule {
	s.next++
	if s.next == 0 {
		s.next++
	}
	s.active.Store(s.next)
	return instanceHostModule{in: in, scope: s, generation: s.next}
}

func (s *hostCallScope) end(generation uint64) {
	if !s.active.CompareAndSwap(generation, 0) {
		return
	}
	if waiter := s.waiter.Load(); waiter != nil && waiter.generation == generation {
		select {
		case waiter.wake <- struct{}{}:
		default:
		}
	}
}

func (in *Instance) ensurePluginState() *instancePluginState {
	state := in.pluginState.Load()
	if state == nil {
		candidate := &instancePluginState{}
		if in.pluginState.CompareAndSwap(nil, candidate) {
			state = candidate
		} else {
			state = in.pluginState.Load()
		}
	}
	return state
}

func (in *Instance) beginHostCallScope() instanceHostModule {
	return in.ensurePluginState().hostScope.begin(in)
}

type staticHostModule struct{ in *Instance }

func (h staticHostModule) Memory() []byte { return h.in.mem() }
func (h staticHostModule) NewExternRef(value any) (ExternRef, error) {
	return h.in.NewExternRef(value)
}
func (h staticHostModule) ExternRefValue(ref ExternRef) (any, bool) {
	return h.in.ExternRefValue(ref)
}

// HostFuncRef is an explicit Runtime/store ownership handle for a host function
// that may be materialized as a non-null funcref. Ordinary HostFunc imports stay
// callable but fail closed if their descriptor would cross a public funcref
// boundary.
type HostFuncRef struct {
	mu            sync.Mutex
	fn            HostFunc
	store         *referenceStore
	sig           FuncSig
	source        *Instance
	descriptor    uint64
	dispatchIndex uint32
	importers     int
	tokenLive     bool
	closed        bool
}

// NewHostFuncRef creates an explicitly owned host function with one exact Wasm
// signature. The returned handle is suitable as an Imports value for matching
// function imports and must be closed after every importing instance.
func (rt *Runtime) NewHostFuncRef(fn HostFunc, sig FuncSig) (*HostFuncRef, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	if fn == nil {
		return nil, fmt.Errorf("wago: host function is nil")
	}
	if _, err := valTypesSlots(sig.Params); err != nil {
		return nil, fmt.Errorf("wago: host function parameters: %w", err)
	}
	if _, err := valTypesSlots(sig.Results); err != nil {
		return nil, fmt.Errorf("wago: host function results: %w", err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, fmt.Errorf("wago: NewHostFuncRef on a closed runtime")
	}
	owner := &HostFuncRef{
		fn:    fn,
		store: rt.refStore,
		sig: FuncSig{
			Params:  append([]ValType(nil), sig.Params...),
			Results: append([]ValType(nil), sig.Results...),
		},
	}
	dispatchIndex, err := rt.refStore.registerHostFuncRef(owner)
	if err != nil {
		return nil, err
	}
	owner.dispatchIndex = dispatchIndex
	return owner, nil
}

// Close releases this host-function ownership handle after its importers and
// issued token lifetime have ended.
func (h *HostFuncRef) Close() error {
	if h == nil {
		return nil
	}
	store := h.store
	if store == nil {
		return nil
	}
	var release []*funcrefTokenEntry
	store.mu.Lock()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		store.mu.Unlock()
		return nil
	}
	// A runtime-closed store with no live logical instances may finish closing the
	// host owner that anchors its last public token. The token keeps fn intact
	// until releaseEntries drops the producer root and physical teardown detaches
	// the importer. Every other retained-code path (for example an external table
	// root without a token) continues to reject Close while importers remain.
	closingLastTokenRoot := h.tokenLive && store.runtimeClosed && store.liveInstances == 0
	if h.importers != 0 && !closingLastTokenRoot {
		count := h.importers
		h.mu.Unlock()
		store.mu.Unlock()
		return fmt.Errorf("wago: host funcref has %d live importer(s); close consumers before the owner", count)
	}
	if h.tokenLive && !closingLastTokenRoot {
		h.mu.Unlock()
		store.mu.Unlock()
		return fmt.Errorf("wago: host funcref has a live funcref token; close its runtime instances before the owner")
	}
	h.closed = true
	if !h.tokenLive {
		h.fn = nil
	}
	if store.liveObjects > 0 {
		store.liveObjects--
	}
	if store.runtimeClosed && store.liveInstances == 0 && store.liveObjects == 0 {
		release = store.releaseEntriesLocked()
	}
	h.mu.Unlock()
	store.mu.Unlock()
	releaseFuncrefEntries(release)
	return nil
}

func funcSigEqual(a, b FuncSig) bool {
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	for i := range a.Results {
		if a.Results[i] != b.Results[i] {
			return false
		}
	}
	return true
}

func (h *HostFuncRef) validateImport(store *referenceStore, sig FuncSig) error {
	if h == nil {
		return fmt.Errorf("host funcref owner is invalid")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store == nil || h.fn == nil {
		return fmt.Errorf("host funcref owner is invalid")
	}
	if h.closed {
		return fmt.Errorf("host funcref owner is closed")
	}
	if store == nil || h.store != store {
		return fmt.Errorf("host funcref belongs to an incompatible reference store")
	}
	if !funcSigEqual(h.sig, sig) {
		return fmt.Errorf("host funcref signature mismatch")
	}
	return nil
}

func (h *HostFuncRef) attachImporter(store *referenceStore, sig FuncSig) error {
	if err := h.validateImport(store, sig); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("host funcref owner is closed")
	}
	h.importers++
	return nil
}

func (h *HostFuncRef) detachImporter() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.importers > 0 {
		h.importers--
	}
	h.mu.Unlock()
}

func (h *HostFuncRef) canonicalDescriptor(source *Instance, descriptor uint64, sig FuncSig) (*Instance, uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.store == nil || source == nil || source.refStore != h.store || h.importers == 0 || !funcSigEqual(h.sig, sig) {
		return nil, 0, false
	}
	if h.source == nil {
		h.source = source
		h.descriptor = descriptor
		return source, descriptor, true
	}
	if !h.source.hasPhysicalResources() || h.descriptor == 0 {
		return nil, 0, false
	}
	return h.source, h.descriptor, true
}

func (h *HostFuncRef) markTokenLive(source *Instance, descriptor uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.source != source || h.descriptor != descriptor {
		return false
	}
	h.tokenLive = true
	return true
}

func (h *HostFuncRef) tokenReleased(source *Instance, descriptor uint64) {
	h.mu.Lock()
	if h.source == source && h.descriptor == descriptor {
		h.tokenLive = false
		h.source = nil
		h.descriptor = 0
		if h.closed {
			h.fn = nil
		}
	}
	h.mu.Unlock()
}

// instanceHostModule is the HostModule handed to host functions during a call.
type instanceHostModule struct {
	in         *Instance
	scope      *hostCallScope
	generation uint64
}

func (h instanceHostModule) valid() bool {
	return h.in != nil && (h.scope == nil || h.generation != 0 && h.scope.active.Load() == h.generation)
}

func (h instanceHostModule) registerWait(waiter *hostCallWaiter) bool {
	if h.scope == nil {
		return true
	}
	if !h.valid() {
		return false
	}
	waiter.generation = h.generation
	h.scope.waiter.Store(waiter)
	if !h.valid() {
		h.scope.waiter.CompareAndSwap(waiter, nil)
		return false
	}
	return true
}

func (h instanceHostModule) unregisterWait(waiter *hostCallWaiter) {
	if h.scope != nil {
		h.scope.waiter.CompareAndSwap(waiter, nil)
	}
}

func (h instanceHostModule) Memory() []byte {
	if !h.valid() {
		return nil
	}
	return h.in.mem()
}
func (h instanceHostModule) NewExternRef(value any) (ExternRef, error) {
	if !h.valid() {
		return ExternRef{}, fmt.Errorf("wago: host module is no longer valid")
	}
	return h.in.NewExternRef(value)
}
func (h instanceHostModule) ExternRefValue(ref ExternRef) (any, bool) {
	if !h.valid() {
		return nil, false
	}
	return h.in.ExternRefValue(ref)
}

// bindHostImport normalizes an Imports value into a HostFunc for the synchronous
// host-call path. The only accepted host-function form is a HostFunc (the stack
// form); any other value is an error. There is no reflection: host imports bind
// identically under standard Go and TinyGo.
func bindHostImport(v any, sig FuncSig) (HostFunc, error) {
	switch f := v.(type) {
	case HostFunc:
		if f == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return f, nil
	case *HostFuncRef:
		if f == nil {
			return nil, fmt.Errorf("host funcref owner is nil")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.closed || f.fn == nil {
			return nil, fmt.Errorf("host funcref owner is closed")
		}
		if !funcSigEqual(f.sig, sig) {
			return nil, fmt.Errorf("host funcref signature mismatch")
		}
		return f.fn, nil
	case nil:
		return nil, fmt.Errorf("no host function provided")
	default:
		return nil, fmt.Errorf("host import must be a wago.HostFunc or *wago.HostFuncRef; got %T", v)
	}
}

// buildSyncHosts resolves every function import of a sync-mode module to a
// HostFunc, indexed by import function index. c.Imports lists the function
// imports in order; c.importFuncSigs holds their compile-time signatures.
func (c *Compiled) buildSyncHosts(imports Imports) ([]HostFunc, error) {
	hosts := make([]HostFunc, len(c.Imports))
	for i, key := range c.Imports {
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		// A cross-instance binding is a native call, not a host function; skip it.
		if _, cross := imports[key].(*InstanceExport); cross {
			continue
		}
		sig := c.importFuncSigs[i]
		paramSlots, err := valTypesSlots(sig.Params)
		if err != nil {
			return nil, fmt.Errorf("import %q params: %w", key, err)
		}
		resultSlots, err := valTypesSlots(sig.Results)
		if err != nil {
			return nil, fmt.Errorf("import %q results: %w", key, err)
		}
		if paramSlots > runtime.MaxHostArity || resultSlots > runtime.MaxHostArity {
			return nil, fmt.Errorf("import %q uses %d param slot(s), %d result slot(s); synchronous host imports support at most %d slots in each direction", key, paramSlots, resultSlots, runtime.MaxHostArity)
		}
		fn, err := bindHostImport(imports[key], sig)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		hosts[i] = fn
	}
	return hosts, nil
}

type missingHostFunc struct{ importIdx uint32 }
type invalidHostReference struct{ err error }

// newHostDispatch builds the runtime callback the CallWithHost loop invokes: it
// maps the wasm import index to the bound HostFunc and runs it with a HostModule
// bound to this instance. It is constructed once at instantiation so hot Invoke
// paths do not allocate a fresh closure per call.
func (in *Instance) newHostDispatch() runtime.HostCall {
	return func(_ uintptr, importIdx uint32, args, results []uint64) {
		var fn HostFunc
		var sig FuncSig
		if importIdx&hostFuncRefDispatchBit != 0 {
			owner := in.refStore.hostFuncRef(importIdx)
			if owner == nil {
				panic(missingHostFunc{importIdx: importIdx})
			}
			owner.mu.Lock()
			fn, sig = owner.fn, owner.sig
			owner.mu.Unlock()
			if fn == nil {
				panic(missingHostFunc{importIdx: importIdx})
			}
		} else {
			if int(importIdx) >= len(in.syncHosts) || in.syncHosts[importIdx] == nil {
				panic(missingHostFunc{importIdx: importIdx})
			}
			if int(importIdx) >= len(in.c.importFuncSigs) {
				panic(invalidHostReference{err: fmt.Errorf("host import %d has no signature", importIdx)})
			}
			fn = in.syncHosts[importIdx]
			sig = in.c.importFuncSigs[importIdx]
		}
		if err := in.translateHostReferenceArgs(args, sig.Params); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
		var mod HostModule
		if in.rt == nil || !in.rt.scopedHostCalls() {
			mod = staticHostModule{in: in}
		} else {
			caller := in.beginHostCallScope()
			defer caller.scope.end(caller.generation)
			mod = caller
		}
		fn(mod, args, results)
		if err := in.translateHostReferenceResults(results, sig.Results); err != nil {
			panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
		}
	}
}

func (in *Instance) translateHostReferenceArgs(values []uint64, types []ValType) error {
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			slot += 2
			continue
		}
		if slot >= len(values) {
			return fmt.Errorf("missing argument slot %d", slot)
		}
		switch typ {
		case ValFuncRef:
			if values[slot] != 0 {
				store, err := in.funcrefStoreForEgress()
				if err != nil {
					return fmt.Errorf("funcref argument %d: %w", i, err)
				}
				token, err := store.issue(in, values[slot])
				if err != nil {
					return fmt.Errorf("invalid funcref argument %d: %w", i, err)
				}
				values[slot] = token
			}
		case ValExternRef:
			if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				return fmt.Errorf("invalid externref token for argument %d", i)
			}
		}
		slot++
	}
	return nil
}

func (in *Instance) translateHostReferenceResults(values []uint64, types []ValType) error {
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			slot += 2
			continue
		}
		if slot >= len(values) {
			return fmt.Errorf("missing result slot %d", slot)
		}
		switch typ {
		case ValFuncRef:
			if values[slot] != 0 {
				if in.refStore == nil {
					return fmt.Errorf("invalid funcref token for result %d", i)
				}
				descriptor, ok := in.refStore.resolve(values[slot])
				if !ok {
					return fmt.Errorf("invalid funcref token for result %d", i)
				}
				values[slot] = descriptor
			}
		case ValExternRef:
			if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				return fmt.Errorf("invalid externref token for result %d", i)
			}
		}
		slot++
	}
	return nil
}

// HostExit, panicked by a host function, terminates the current Invoke and
// surfaces as an *ExitError. It lets a host import end
// execution without returning to wasm; the abandoned foreign-stack frames are
// reset on the engine's next entry.
type HostExit struct{ Code int32 }

// ExitError is returned by Invoke when a host function requested termination via
// panic(HostExit{...}). A zero code is a normal exit.
type ExitError struct{ Code int32 }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// callNativeSync runs a native entry that may make synchronous host calls,
// driving the re-entry loop with this instance's host dispatch. A host function
// may panic(HostExit{...}) to terminate; it is recovered here as an *ExitError.
func (in *Instance) callNativeSync(entry uintptr) (err error) {
	locked := in.beginNativeEntry()
	defer locked.unlockExecution()
	defer func() {
		if r := recover(); r != nil {
			if ex, ok := r.(HostExit); ok {
				err = &ExitError{Code: ex.Code}
				return
			}
			if missing, ok := r.(missingHostFunc); ok {
				err = fmt.Errorf("missing host function for import index %d", missing.importIdx)
				return
			}
			if invalid, ok := r.(invalidHostReference); ok {
				err = invalid.err
				return
			}
			panic(r)
		}
	}()
	in.jm.SetStackFence(in.eng.StackLimit())
	if in.hostCall == nil {
		in.hostCall = in.newHostDispatch()
	}
	err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, in.dispatchSynchronousHostCall)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}
