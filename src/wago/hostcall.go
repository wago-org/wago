package wago

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// HostModule gives a synchronous host import access to the instance that called
// it. It is passed as the optional leading parameter of a host function.
type HostModule interface {
	// Memory returns the calling instance's linear memory as a mutable slice
	// (empty if the module declares no memory). Writes are visible to wasm; the
	// slice is valid only for the duration of the host call.
	Memory() []byte
}

// InvokeFromHost invokes an export while caller is an active synchronous host
// callback. The HostModule carries an unforgeable, callback-scoped invocation
// identity. If that call chain re-enters a parked Instance, Wago supplies an
// isolated native stack and call buffers for the nested activation. Retained or
// unrelated HostModule values fail closed.
func (in *Instance) InvokeFromHost(ctx context.Context, caller HostModule, export string, args ...uint64) (results []uint64, err error) {
	var active *Instance
	var id invocationID
	var reservation *pluginOperationReservation
	switch h := caller.(type) {
	case instanceHostModule:
		if h.valid() {
			active = h.in
			id = h.invocationID
			reservation = h.reservation
		}
	}
	if active == nil || id == 0 {
		return nil, fmt.Errorf("wago: re-entry requires the active host caller: %w", ErrPermissionDenied)
	}
	if in == nil {
		return nil, fmt.Errorf("wago: re-entry target instance is nil")
	}
	if active.guestStorageBorrowed() {
		return nil, fmt.Errorf("wago: re-entry is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	contexts := invocationContextSetFor(ctx)
	results, err = in.invokeWithToken(export, args, contexts, id, false, false, reservation)
	return results, contextInterruptError(ctx, err)
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
	// ReleaseExternRef releases a token after it is no longer reachable by Wasm.
	ReleaseExternRef(ExternRef) bool
}

// GCHostModule is the optional exact-collection surface implemented by HostModule
// values from collector-backed instances. It is primarily useful for embedders
// that provide an explicit guest-visible collection hook.
type GCHostModule interface {
	HostModule
	CollectGC() error
}

// HostFunc is a host import in reflection-free slot (stack) form: it reads its
// wasm params from params (i32/f32 in the low 32 bits) and writes its results
// into results, with the calling instance's linear memory and externref store
// available through HostModule. A reference occupies one opaque uint64 slot; a
// v128 occupies two adjacent little-endian uint64 slots, matching Invoke's public
// ABI. It is the single host-import type — it binds identically
// under standard Go and TinyGo — with no reflection anywhere on the path.
type HostFunc func(m HostModule, params, results []uint64)

// CallerResolver resolves information about the exact Runtime-owned invocation
// making an active synchronous host call. Its authority is read-only: it cannot
// create, invoke, close, manage, pool, or otherwise control instances.
//
// Resolve succeeds only while the HostFunc callback is active. Retaining the
// HostModule and resolving it after the callback returns fails closed.
type CallerResolver struct {
	rt atomic.Pointer[Runtime]
}

// CallerInvoker is a revocable synchronous re-entry handle for the exact guest
// making an active host call. The HostModule token supplies both instance
// identity and callback lifetime; forged, retained, and cross-runtime tokens
// fail closed.
type CallerInvoker struct {
	rt atomic.Pointer[Runtime]
}

func (r *CallerInvoker) activate(rt *Runtime) {
	if r == nil || rt == nil {
		return
	}
	r.rt.Store(rt)
	rt.callerResolverActive.Store(true)
}

func (r *CallerInvoker) close() error {
	if r != nil {
		r.rt.Store(nil)
	}
	return nil
}

// Invoke synchronously invokes an export on the active calling guest. Nested
// execution uses Wago's isolated re-entry stack and inherits cancellation from
// ctx. The authority expires when the outer host callback returns.
func (r *CallerInvoker) Invoke(ctx context.Context, caller HostModule, export string, args ...uint64) ([]uint64, error) {
	if r == nil {
		return nil, fmt.Errorf("wago: nil caller invoker: %w", ErrPermissionDenied)
	}
	rt := r.rt.Load()
	h, ok := caller.(instanceHostModule)
	if rt == nil || !ok || !h.valid() || h.in == nil || h.in.rt != rt {
		return nil, fmt.Errorf("wago: caller invocation requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return h.in.InvokeFromHost(ctx, caller, export, args...)
}

func (r *CallerResolver) activate(rt *Runtime) {
	if r == nil || rt == nil {
		return
	}
	r.rt.Store(rt)
	rt.callerResolverActive.Store(true)
}

func (r *CallerResolver) close() error {
	if r != nil {
		r.rt.Store(nil)
	}
	return nil
}

// Resolve returns the exact instance making caller's active synchronous host
// call. Forged, expired, cross-runtime, and low-level HostModule values are
// rejected.
func (r *CallerResolver) Resolve(caller HostModule) (InstanceIdentity, error) {
	if r == nil {
		return InstanceIdentity{}, fmt.Errorf("wago: nil caller resolver: %w", ErrPermissionDenied)
	}
	rt := r.rt.Load()
	h, ok := caller.(instanceHostModule)
	if rt == nil || !ok || !h.valid() || h.in == nil || h.in.rt != rt {
		return InstanceIdentity{}, fmt.Errorf("wago: caller identity requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return InstanceIdentity{value: h.in}, nil
}

// InvocationContext returns a cancellation- and deadline-only context for
// caller's active synchronous host callback. The returned context is canceled
// when its parent invocation is canceled or when the callback returns. It never
// exposes values from the parent context. Repeated calls during one callback
// return the same context.
//
// Forged, expired, cross-runtime, and low-level HostModule values are rejected.
func (r *CallerResolver) InvocationContext(caller HostModule) (context.Context, error) {
	if r == nil {
		return nil, fmt.Errorf("wago: nil caller resolver: %w", ErrPermissionDenied)
	}
	rt := r.rt.Load()
	h, ok := caller.(instanceHostModule)
	if rt == nil || !ok || !h.valid() || h.in == nil || h.in.rt != rt || h.scope == nil {
		return nil, fmt.Errorf("wago: invocation context requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	ctx, ok := h.scope.invocationContext(h.generation, activeHostInvocationContext(h.in).parent)
	if !ok {
		return nil, fmt.Errorf("wago: invocation context requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return ctx, nil
}

// hostCallScope authorizes one synchronous use of an instanceHostModule.
type hostCallScope struct {
	active atomic.Uint64
	state  atomic.Pointer[hostCallState]
}

type hostCallWaiter struct {
	generation uint64
	wake       chan struct{}
}

// hostCallState is allocated only after a plugin asks to watch a caller or
// resolve an invocation context. The context list is bounded by the already-
// bounded synchronous re-entry depth, and its lock does not protect waiter so
// the two optional facilities cannot replace or block each other.
type hostCallState struct {
	waiter atomic.Pointer[hostCallWaiter]
	mu     sync.Mutex
	head   *callbackInvocationContext
}

type callbackInvocationContext struct {
	generation  uint64
	next        *callbackInvocationContext
	done        chan struct{}
	deadline    time.Time
	hasDeadline bool

	mu         sync.Mutex
	err        error
	stopParent func() bool
}

func newCallbackInvocationContext(generation uint64, parent context.Context) *callbackInvocationContext {
	c := &callbackInvocationContext{generation: generation, done: make(chan struct{})}
	if parent == nil {
		return c
	}
	c.deadline, c.hasDeadline = parent.Deadline()
	if err := parent.Err(); err != nil {
		c.finish(err)
		return c
	}
	if parent.Done() == nil {
		return c
	}
	stop := context.AfterFunc(parent, func() { c.finish(parent.Err()) })
	c.mu.Lock()
	if c.err == nil {
		c.stopParent = stop
		c.mu.Unlock()
	} else {
		c.mu.Unlock()
		stop()
	}
	return c
}

func (c *callbackInvocationContext) Deadline() (time.Time, bool) {
	return c.deadline, c.hasDeadline
}

func (c *callbackInvocationContext) Done() <-chan struct{} { return c.done }

func (c *callbackInvocationContext) Err() error {
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	return err
}

func (*callbackInvocationContext) Value(any) any { return nil }

func (c *callbackInvocationContext) finish(err error) {
	if err == nil {
		err = context.Canceled
	}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	stop := c.stopParent
	c.stopParent = nil
	close(c.done)
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (s *hostCallScope) ensureState() *hostCallState {
	state := s.state.Load()
	if state == nil {
		candidate := &hostCallState{}
		if s.state.CompareAndSwap(nil, candidate) {
			state = candidate
		} else {
			state = s.state.Load()
		}
	}
	return state
}

func (s *hostCallScope) invocationContext(generation uint64, parent context.Context) (context.Context, bool) {
	state := s.ensureState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if generation == 0 || s.active.Load() != generation {
		return nil, false
	}
	for current := state.head; current != nil; current = current.next {
		if current.generation == generation {
			return current, true
		}
	}
	current := newCallbackInvocationContext(generation, parent)
	current.next = state.head
	state.head = current
	return current, true
}

func (s *hostCallScope) expireInvocationContext(state *hostCallState, generation uint64) {
	state.mu.Lock()
	var expired *callbackInvocationContext
	for link := &state.head; *link != nil; link = &(*link).next {
		if (*link).generation == generation {
			expired = *link
			*link = expired.next
			expired.next = nil
			break
		}
	}
	state.mu.Unlock()
	if expired != nil {
		expired.finish(context.Canceled)
	}
}

type instancePluginState struct {
	hostScope          hostCallScope
	invokeMu           sync.Mutex // serializes unrelated public calls across parked host callbacks
	nativeExecutionMu  sync.Mutex // serializes native entry for an independent instance
	invocationID       invocationID
	close              atomic.Pointer[instanceCloseState]
	gcConfig           *GCConfig
	origin             InstantiateOrigin
	gcGlobalRootCount  uint32
	guestStorageBorrow atomic.Uint32
	gcPublic           atomic.Pointer[gcPublicState]
	gcArrayElements    atomic.Pointer[gcArrayElementState]
	gcRefTestTable     atomic.Pointer[gcRefTestTableState]
	gcGlobalRoots      []gcGlobalRootMapping
	tagIdentityBase    uintptr      // arena-owned bounded native u64 directory for staged EH
	tagExports         map[int]*Tag // lazy stable identity handles for exported local tags
}

type instanceCloseState struct {
	done           chan struct{}
	quiesced       chan struct{}
	quiescedOnce   sync.Once
	result         error
	interruptStop  func()
	hooks          *hookRegistry
	event          *InstanceCloseEvent
	terminalOnce   sync.Once
	terminalResult error
}

func (in *Instance) instantiateOrigin() InstantiateOrigin {
	if state := in.pluginState.Load(); state != nil {
		return state.origin
	}
	return InstantiateDirect
}

func (s *hostCallScope) begin(in *Instance) instanceHostModule {
	return s.beginReserved(in, currentInvocationReservation(in))
}

func (s *hostCallScope) beginReserved(in *Instance, reservation *pluginOperationReservation) instanceHostModule {
	return s.beginReservedWithID(in, in.currentInvocationID(), reservation)
}

func (s *hostCallScope) beginReservedWithID(in *Instance, id invocationID, reservation *pluginOperationReservation) instanceHostModule {
	parent := s.active.Load()
	generation := uint64(newInvocationID())
	s.active.Store(generation)
	return instanceHostModule{in: in, scope: s, generation: generation, parentGeneration: parent, invocationID: id, reservation: reservation}
}

func (s *hostCallScope) end(generation, parent uint64) {
	active := s.active.CompareAndSwap(generation, parent)
	state := s.state.Load()
	if state != nil {
		s.expireInvocationContext(state, generation)
	}
	if !active {
		return
	}
	if state != nil {
		if waiter := state.waiter.Load(); waiter != nil && waiter.generation == generation {
			select {
			case waiter.wake <- struct{}{}:
			default:
			}
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
	return in.beginHostCallScopeReserved(currentInvocationReservation(in))
}

func (in *Instance) beginHostCallScopeReserved(reservation *pluginOperationReservation) instanceHostModule {
	return in.beginHostCallScopeReservedWithID(in.currentInvocationID(), reservation)
}

func (in *Instance) beginHostCallScopeReservedWithID(id invocationID, reservation *pluginOperationReservation) instanceHostModule {
	return in.ensurePluginState().hostScope.beginReservedWithID(in, id, reservation)
}

func (in *Instance) currentInvocationID() invocationID {
	if state := in.pluginState.Load(); state != nil {
		return state.invocationID
	}
	return 0
}

type staticHostModule struct{ in *Instance }

func (h staticHostModule) Memory() []byte { return h.in.mem() }
func (h staticHostModule) CollectGC() error {
	if h.in == nil {
		return fmt.Errorf("wago: GC host module has no instance")
	}
	if h.in.guestStorageBorrowed() {
		return fmt.Errorf("wago: collection is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	return h.in.CollectGC()
}
func (h staticHostModule) NewExternRef(value any) (ExternRef, error) {
	return h.in.NewExternRef(value)
}
func (h staticHostModule) ExternRefValue(ref ExternRef) (any, bool) {
	return h.in.ExternRefValue(ref)
}
func (h staticHostModule) ReleaseExternRef(ref ExternRef) bool {
	return h.in.ReleaseExternRef(ref)
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
	gcCapable     bool
	gc            *hostFuncRefGCState // lazy exact binding state; collector fields are used only when gcCapable
}

type hostFuncRefGCState struct {
	collector             *gc.Collector
	domainID              uint64
	params                []ValueTypeDescriptor
	results               []ValueTypeDescriptor
	types                 []DefinedTypeDescriptor
	inlineDispatchKey     hostFuncRefBindingKey
	inlineDispatchBinding *hostFuncRefDispatchBinding
	dispatchBindings      map[hostFuncRefBindingKey]*hostFuncRefDispatchBinding
}

type hostFuncRefBindingKey struct {
	owner       *HostFuncRef
	compiled    *Compiled
	importIndex int
}

type hostFuncRefDispatchBinding struct {
	owner           *HostFuncRef
	sig             FuncSig
	params, results []ValueTypeDescriptor
	types           []DefinedTypeDescriptor
	dispatchIndex   uint32
	refs            uint32
}

// NewHostFuncRef creates an explicitly owned host function with one exact Wasm
// signature. The returned handle is suitable as an Imports value for matching
// function imports and must be closed after every importing instance.
func (rt *Runtime) NewHostFuncRef(fn HostFunc, sig FuncSig) (*HostFuncRef, error) {
	return rt.newHostFuncRef(fn, sig, false, false)
}

// NewGCHostFuncRef creates a Runtime-owned host function that may transfer
// collector references through its declared signature. GC-reference argument
// slots are presented to fn as temporary opaque GCRef tokens, and result slots
// accept null, immediate i31 values, or a live GCRef token from the exact bound
// collector domain. The first importer binds the owner to one canonical Runtime
// collector domain; incompatible or foreign-domain importers fail closed.
func (rt *Runtime) NewGCHostFuncRef(fn HostFunc, sig FuncSig) (*HostFuncRef, error) {
	if !funcSigHasGCRefs(sig) {
		return nil, fmt.Errorf("wago: GC host function signature has no collector references")
	}
	return rt.newHostFuncRef(fn, sig, true, false)
}

func (rt *Runtime) newHostFuncRef(fn HostFunc, sig FuncSig, gcCapable, allowLoading bool) (*HostFuncRef, error) {
	if rt == nil || rt.refStore == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	operation, err := rt.beginOperation("NewHostFuncRef", allowLoading)
	if err != nil {
		return nil, err
	}
	defer operation.end()
	if fn == nil {
		return nil, fmt.Errorf("wago: host function is nil")
	}
	if _, err := valTypesSlots(sig.Params); err != nil {
		return nil, fmt.Errorf("wago: host function parameters: %w", err)
	}
	if _, err := valTypesSlots(sig.Results); err != nil {
		return nil, fmt.Errorf("wago: host function results: %w", err)
	}
	owner := &HostFuncRef{
		fn:    fn,
		store: rt.refStore,
		sig: FuncSig{
			Params:       append([]ValType(nil), sig.Params...),
			Results:      append([]ValType(nil), sig.Results...),
			TypeIndex:    sig.TypeIndex,
			HasTypeIndex: sig.HasTypeIndex,
		},
	}
	owner.gcCapable = gcCapable
	dispatchIndex, err := rt.refStore.registerHostFuncRef(owner)
	if err != nil {
		return nil, err
	}
	owner.dispatchIndex = dispatchIndex
	return owner, nil
}

func funcSigHasGCRefs(sig FuncSig) bool {
	return hasValType(sig.Params, ValAnyRef) || hasValType(sig.Params, ValI31Ref) || hasValType(sig.Results, ValAnyRef) || hasValType(sig.Results, ValI31Ref)
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
	var release referenceTokenEntries
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
	releaseReferenceEntries(release)
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
	return h.validateImportLocked(store, sig)
}

func (h *HostFuncRef) validateImportLocked(store *referenceStore, sig FuncSig) error {
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

func hostFuncRefExactSignature(sig FuncSig, c *Compiled) (params, results []ValueTypeDescriptor, needed bool, err error) {
	if c == nil {
		return nil, nil, false, nil
	}
	params, results, err = exactFuncSignatureView(sig, c.Types)
	if err != nil {
		return nil, nil, false, err
	}
	return params, results, sig.HasTypeIndex, nil
}

func (h *HostFuncRef) validateExactBindingLocked(sig FuncSig, c *Compiled) error {
	params, results, needed, err := hostFuncRefExactSignature(sig, c)
	if err != nil {
		return fmt.Errorf("host funcref exact signature: %w", err)
	}
	if !needed {
		return nil
	}
	if h.gc == nil || h.gc.types == nil || !exactSignatureEquivalent(h.gc.params, h.gc.results, h.gc.types, params, results, c.Types) {
		return fmt.Errorf("host funcref structural signature mismatch")
	}
	return nil
}

func (h *HostFuncRef) validateAttachedImporter(store *referenceStore, sig FuncSig, collector *gc.Collector, domainID uint64, c *Compiled) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.validateImportLocked(store, sig); err != nil {
		return err
	}
	if err := h.validateExactBindingLocked(sig, c); err != nil {
		return err
	}
	if !funcSigHasGCRefs(sig) {
		return nil
	}
	if !h.gcCapable || h.gc == nil || collector == nil || h.gc.collector != collector || h.gc.domainID == 0 || h.gc.domainID != domainID {
		return fmt.Errorf("GC host funcref belongs to a different Runtime collector domain")
	}
	return nil
}

func (h *HostFuncRef) attachImporter(store *referenceStore, sig FuncSig, collector *gc.Collector, domainID uint64, c *Compiled) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.validateImportLocked(store, sig); err != nil {
		return err
	}
	params, results, exactNeeded, err := hostFuncRefExactSignature(sig, c)
	if err != nil {
		return fmt.Errorf("host funcref exact signature: %w", err)
	}
	if exactNeeded && h.gc != nil && h.gc.types != nil && !exactSignatureEquivalent(h.gc.params, h.gc.results, h.gc.types, params, results, c.Types) {
		return fmt.Errorf("host funcref structural signature mismatch")
	}
	gcRefs := funcSigHasGCRefs(sig)
	if gcRefs {
		if !h.gcCapable {
			return fmt.Errorf("host funcref collector-reference signature requires Runtime.NewGCHostFuncRef")
		}
		if collector == nil || domainID == 0 || c == nil || c.genericGCFrameRoots() == nil || !store.ownsGCCollector(collector) {
			return fmt.Errorf("GC host funcref requires an exact live Runtime collector domain and native root maps")
		}
		if h.gc != nil && h.gc.collector != nil && (h.gc.collector != collector || h.gc.domainID != domainID) {
			return fmt.Errorf("GC host funcref belongs to a different Runtime collector domain")
		}
	} else if h.gcCapable {
		return fmt.Errorf("GC host funcref requires a collector-reference signature")
	}
	if exactNeeded && (h.gc == nil || h.gc.types == nil) {
		if h.gc == nil {
			h.gc = &hostFuncRefGCState{}
		}
		h.gc.params = params
		h.gc.results = results
		h.gc.types = c.Types
	}
	if gcRefs {
		if h.gc == nil {
			return fmt.Errorf("GC host funcref requires an exact structural signature")
		}
		if h.gc.collector == nil {
			h.gc.collector = collector
			h.gc.domainID = domainID
		}
	}
	h.importers++
	return nil
}

func (h *HostFuncRef) acquireDispatchBinding(store *referenceStore, c *Compiled, importIndex int, sig FuncSig) (uint32, bool, error) {
	params, results, needed, err := hostFuncRefExactSignature(sig, c)
	if err != nil {
		return 0, false, fmt.Errorf("host funcref exact signature: %w", err)
	}
	if !needed {
		return h.dispatchIndex, false, nil
	}
	key := hostFuncRefBindingKey{owner: h, compiled: c, importIndex: importIndex}
	store.mu.Lock()
	defer store.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.store != store || h.gc == nil || h.gc.types == nil || !exactSignatureEquivalent(h.gc.params, h.gc.results, h.gc.types, params, results, c.Types) {
		return 0, false, fmt.Errorf("host funcref structural signature mismatch")
	}
	if binding := h.gc.inlineDispatchBinding; binding != nil && h.gc.inlineDispatchKey == key {
		if binding.refs == ^uint32(0) {
			return 0, false, fmt.Errorf("host funcref dispatch binding has too many importers")
		}
		binding.refs++
		return binding.dispatchIndex, true, nil
	}
	if binding := h.gc.dispatchBindings[key]; binding != nil {
		if binding.refs == ^uint32(0) {
			return 0, false, fmt.Errorf("host funcref dispatch binding has too many importers")
		}
		binding.refs++
		return binding.dispatchIndex, true, nil
	}
	binding := &hostFuncRefDispatchBinding{
		owner: h, sig: sig, params: params, results: results, types: c.Types, refs: 1,
	}
	dispatchIndex, err := store.registerHostFuncRefBindingLocked(binding)
	if err != nil {
		return 0, false, err
	}
	binding.dispatchIndex = dispatchIndex
	if h.gc.inlineDispatchBinding == nil {
		h.gc.inlineDispatchKey = key
		h.gc.inlineDispatchBinding = binding
	} else {
		if h.gc.dispatchBindings == nil {
			h.gc.dispatchBindings = make(map[hostFuncRefBindingKey]*hostFuncRefDispatchBinding)
		}
		h.gc.dispatchBindings[key] = binding
	}
	return dispatchIndex, true, nil
}

func (h *HostFuncRef) dispatchBinding(c *Compiled, importIndex int) (*hostFuncRefDispatchBinding, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gc == nil {
		return nil, false
	}
	key := hostFuncRefBindingKey{owner: h, compiled: c, importIndex: importIndex}
	if h.gc.inlineDispatchBinding != nil && h.gc.inlineDispatchKey == key {
		return h.gc.inlineDispatchBinding, true
	}
	binding := h.gc.dispatchBindings[key]
	return binding, binding != nil
}

func (h *HostFuncRef) releaseDispatchBinding(c *Compiled, importIndex int) {
	if h == nil || h.store == nil {
		return
	}
	store := h.store
	key := hostFuncRefBindingKey{owner: h, compiled: c, importIndex: importIndex}
	store.mu.Lock()
	h.mu.Lock()
	if h.gc != nil {
		if binding := h.gc.inlineDispatchBinding; binding != nil && h.gc.inlineDispatchKey == key {
			if binding.refs > 1 {
				binding.refs--
			} else {
				h.gc.inlineDispatchKey = hostFuncRefBindingKey{}
				h.gc.inlineDispatchBinding = nil
				store.unregisterHostFuncRefBindingLocked(binding)
			}
		} else if binding := h.gc.dispatchBindings[key]; binding != nil {
			if binding.refs > 1 {
				binding.refs--
			} else {
				delete(h.gc.dispatchBindings, key)
				store.unregisterHostFuncRefBindingLocked(binding)
			}
		}
	}
	h.mu.Unlock()
	store.mu.Unlock()
}

func exactSignatureEquivalent(aParams, aResults []ValueTypeDescriptor, aTypes []DefinedTypeDescriptor, bParams, bResults []ValueTypeDescriptor, bTypes []DefinedTypeDescriptor) bool {
	if len(aParams) != len(bParams) || len(aResults) != len(bResults) {
		return false
	}
	for i := range aParams {
		if !valueTypeEquivalent(aParams[i], aTypes, bParams[i], bTypes) {
			return false
		}
	}
	for i := range aResults {
		if !valueTypeEquivalent(aResults[i], aTypes, bResults[i], bTypes) {
			return false
		}
	}
	return true
}

func (h *HostFuncRef) detachImporter() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.importers > 0 {
		h.importers--
	}
	if h.importers == 0 && h.gc != nil {
		h.gc.collector = nil
		h.gc.domainID = 0
		h.gc.params = nil
		h.gc.results = nil
		h.gc.types = nil
		h.gc.inlineDispatchKey = hostFuncRefBindingKey{}
		h.gc.inlineDispatchBinding = nil
		h.gc.dispatchBindings = nil
	}
	h.mu.Unlock()
}

func (h *HostFuncRef) isGCBridge() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	ok := h.gcCapable && h.gc != nil && !h.closed && h.gc.collector != nil && h.gc.domainID != 0
	h.mu.Unlock()
	return ok
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
	in                 *Instance
	scope              *hostCallScope
	generation         uint64
	parentGeneration   uint64
	invocationID       invocationID
	reservation        *pluginOperationReservation
	exactParams        []ValueTypeDescriptor
	exactResults       []ValueTypeDescriptor
	ephemeralGCResults *gcHostTempTokens
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
	state := h.scope.ensureState()
	state.waiter.Store(waiter)
	if !h.valid() {
		state.waiter.CompareAndSwap(waiter, nil)
		return false
	}
	return true
}

func (h instanceHostModule) unregisterWait(waiter *hostCallWaiter) {
	if h.scope != nil {
		if state := h.scope.state.Load(); state != nil {
			state.waiter.CompareAndSwap(waiter, nil)
		}
	}
}

func (h instanceHostModule) Memory() []byte {
	if !h.valid() {
		return nil
	}
	return h.in.mem()
}

func (h instanceHostModule) CollectGC() error {
	if !h.valid() {
		return fmt.Errorf("wago: GC host module is outside its active callback: %w", ErrPermissionDenied)
	}
	if h.in.guestStorageBorrowed() {
		return fmt.Errorf("wago: collection is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	if h.in.ownsGCInvocation(h.invocationID) {
		return h.in.collectGC()
	}
	lease := h.in.lockGCInvocation(h.invocationID)
	defer lease.unlock()
	return h.in.collectGC()
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
func (h instanceHostModule) ReleaseExternRef(ref ExternRef) bool {
	if !h.valid() {
		return false
	}
	return h.in.ReleaseExternRef(ref)
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

type syncHostBinding struct {
	fn        HostFunc
	exact     *DefinedTypeDescriptor
	importIdx uint32
	scalar    bool
}

// buildSyncHosts resolves every function import of a sync-mode module to one
// immutable binding indexed by import function index. The exact descriptor
// pointer and scalar/reference dispatch class are computed once at instantiation.
func (c *Compiled) buildSyncHosts(imports Imports) ([]syncHostBinding, error) {
	hosts := make([]syncHostBinding, len(c.Imports))
	for i, key := range c.Imports {
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		// A cross-instance binding is a native call, not a host function; skip it.
		if _, cross := imports[key].(*InstanceExport); cross {
			continue
		}
		sig := c.importFuncSigs[i]
		if _, err := valTypesSlots(sig.Params); err != nil {
			return nil, fmt.Errorf("import %q params: %w", key, err)
		}
		if _, err := valTypesSlots(sig.Results); err != nil {
			return nil, fmt.Errorf("import %q results: %w", key, err)
		}
		fn, err := bindHostImport(imports[key], sig)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		binding := syncHostBinding{fn: fn, importIdx: uint32(i), scalar: true}
		for _, typ := range sig.Params {
			if isReferenceValType(typ) {
				binding.scalar = false
				break
			}
		}
		if binding.scalar {
			for _, typ := range sig.Results {
				if isReferenceValType(typ) {
					binding.scalar = false
					break
				}
			}
		}
		if _, _, err = exactFuncSignatureView(sig, c.Types); err != nil {
			return nil, fmt.Errorf("import %q exact signature: %w", key, err)
		}
		if sig.HasTypeIndex {
			binding.exact = &c.Types[sig.TypeIndex]
		}
		hosts[i] = binding
	}
	return hosts, nil
}

type missingHostFunc struct{ importIdx uint32 }
type invalidHostReference struct{ err error }

type gcHostTempTokens struct {
	count      uint32
	exactTypes *[]DefinedTypeDescriptor
	tokens     [gcPublicSlotLimit]uint64
	extra      []uint64
}

func (t *gcHostTempTokens) token(index uint32) uint64 {
	if index < gcPublicSlotLimit {
		return t.tokens[index]
	}
	return t.extra[index-gcPublicSlotLimit]
}

func (t *gcHostTempTokens) setToken(index uint32, token uint64) {
	if index < gcPublicSlotLimit {
		t.tokens[index] = token
		return
	}
	extra := index - gcPublicSlotLimit
	if int(extra) == len(t.extra) {
		t.extra = append(t.extra, token)
	} else {
		t.extra[extra] = token
	}
}

func (t *gcHostTempTokens) add(token uint64) error {
	if token == 0 {
		return nil
	}
	t.setToken(t.count, token)
	t.count++
	return nil
}

func (t *gcHostTempTokens) release(in *Instance) {
	if t == nil || in == nil || in.refStore == nil {
		return
	}
	for t.count != 0 {
		t.count--
		token := t.token(t.count)
		t.setToken(t.count, 0)
		if token != 0 {
			_ = in.refStore.releaseGCRef(in, token)
		}
	}
	if len(t.extra) != 0 {
		t.extra = t.extra[:0]
	}
}

type boundHostFuncRefCall struct {
	owner           *HostFuncRef
	fn              HostFunc
	sig             FuncSig
	params, results []ValueTypeDescriptor
	types           *[]DefinedTypeDescriptor
}

func (in *Instance) pluginGCImportSet() map[uint32]struct{} {
	if in == nil {
		return nil
	}
	return in.pluginGCImports
}

func (in *Instance) pluginGCHostSignature(dispatch uint32) (FuncSig, bool) {
	if in == nil || in.c == nil || dispatch&hostFuncRefDispatchBit != 0 || uint64(dispatch) >= uint64(len(in.c.Imports)) || uint64(dispatch) >= uint64(len(in.c.importFuncSigs)) || !funcSigHasGCRefs(in.c.importFuncSigs[dispatch]) {
		return FuncSig{}, false
	}
	if _, ok := in.pluginGCImports[dispatch]; !ok {
		return FuncSig{}, false
	}
	return in.c.importFuncSigs[dispatch], true
}

func (in *Instance) boundHostFuncRef(dispatch uint32) (boundHostFuncRefCall, bool) {
	if in == nil || in.refStore == nil || dispatch&hostFuncRefDispatchBit == 0 {
		return boundHostFuncRefCall{}, false
	}
	owner, exact := in.refStore.hostFuncRefDispatch(dispatch)
	if owner == nil {
		return boundHostFuncRefCall{}, false
	}
	owner.mu.Lock()
	binding := boundHostFuncRefCall{owner: owner, fn: owner.fn, sig: owner.sig}
	owner.mu.Unlock()
	if binding.fn == nil {
		return boundHostFuncRefCall{}, false
	}
	if exact != nil {
		binding.sig = exact.sig
		binding.params = exact.params
		binding.results = exact.results
		binding.types = &exact.types
	}
	return binding, true
}

func dispatchSyncHostScalar(in *Instance, ctrl uintptr, binding *syncHostBinding, args, results []uint64) {
	var exactParams, exactResults []ValueTypeDescriptor
	if binding.exact != nil {
		exactParams, exactResults = binding.exact.Params, binding.exact.Results
	}
	invocation := currentHostInvocationContext(ctrl, in)
	caller := in.beginHostCallScopeReservedWithID(invocation.id, invocation.reservation)
	caller.exactParams = exactParams
	caller.exactResults = exactResults
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	var mod HostModule = caller
	binding.fn(mod, args, results)
}

func dispatchSyncHostReference(in *Instance, ctrl uintptr, importIdx uint32, fn HostFunc, sig FuncSig, exactParams, exactResults []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor, exactTypesPtr *[]DefinedTypeDescriptor, args, results []uint64) {
	var gcTemps gcHostTempTokens
	if err := in.translateHostReferenceArgs(args, sig.Params, exactParams, exactTypes, &gcTemps); err != nil {
		gcTemps.release(in)
		panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
	}
	defer gcTemps.release(in)
	invocation := currentHostInvocationContext(ctrl, in)
	caller := in.beginHostCallScopeReservedWithID(invocation.id, invocation.reservation)
	caller.exactParams = exactParams
	caller.exactResults = exactResults
	var gcResultTemps gcHostTempTokens
	gcResultTemps.exactTypes = exactTypesPtr
	caller.ephemeralGCResults = &gcResultTemps
	defer gcResultTemps.release(in)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	var mod HostModule = caller
	fn(mod, args, results)
	if err := in.translateHostReferenceResults(ctrl, results, sig.Results, exactResults, exactTypes); err != nil {
		panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
	}
}

// newHostDispatch builds the runtime callback the CallWithHost loop invokes: it
// maps the wasm import index to the bound HostFunc and runs it with a HostModule
// bound to this instance. It is constructed once at instantiation so hot Invoke
// paths do not allocate a fresh closure per call.
func (in *Instance) newHostDispatch() runtime.HostCall {
	return func(ctrl uintptr, importIdx uint32, args, results []uint64) {
		if importIdx&shared.AtomicWaitDispatchBit != 0 {
			if importIdx&(gcStructDispatchBit|hostFuncRefDispatchBit) != 0 {
				panic(atomicWaitHelperError{err: fmt.Errorf("invalid overlapping atomic helper dispatch index %#x", importIdx)})
			}
			in.dispatchAtomicWaitHelper(importIdx&^shared.AtomicWaitDispatchBit, args, results)
			return
		}
		if importIdx&gcStructDispatchBit != 0 {
			if importIdx&hostFuncRefDispatchBit != 0 {
				panic(gcStructHelperError{err: fmt.Errorf("invalid overlapping GC/host dispatch index %#x", importIdx)})
			}
			helper, safepoint := shared.DecodeGCDispatch(importIdx &^ gcStructDispatchBit)
			in.dispatchGCHelperParked(ctrl, helper, safepoint, args, results)
			return
		}
		if importIdx&hostFuncRefDispatchBit != 0 {
			owner, exact := in.refStore.hostFuncRefDispatch(importIdx)
			if owner == nil {
				panic(missingHostFunc{importIdx: importIdx})
			}
			owner.mu.Lock()
			fn, sig := owner.fn, owner.sig
			owner.mu.Unlock()
			if fn == nil {
				panic(missingHostFunc{importIdx: importIdx})
			}
			var exactParams, exactResults []ValueTypeDescriptor
			var exactTypes []DefinedTypeDescriptor
			var exactTypesPtr *[]DefinedTypeDescriptor
			if exact != nil {
				sig = exact.sig
				exactParams = exact.params
				exactResults = exact.results
				exactTypes = exact.types
				exactTypesPtr = &exact.types
			}
			dispatchSyncHostReference(in, ctrl, importIdx, fn, sig, exactParams, exactResults, exactTypes, exactTypesPtr, args, results)
			return
		}
		if int(importIdx) >= len(in.syncHosts) || in.syncHosts[importIdx].fn == nil {
			panic(missingHostFunc{importIdx: importIdx})
		}
		binding := &in.syncHosts[importIdx]
		if binding.scalar {
			dispatchSyncHostScalar(in, ctrl, binding, args, results)
		} else {
			var exactParams, exactResults []ValueTypeDescriptor
			if binding.exact != nil {
				exactParams, exactResults = binding.exact.Params, binding.exact.Results
			}
			dispatchSyncHostReference(in, ctrl, importIdx, binding.fn, in.c.importFuncSigs[importIdx], exactParams, exactResults, in.c.Types, &in.c.Types, args, results)
		}
	}
}

func (in *Instance) translateHostReferenceArgs(values []uint64, types []ValType, exact []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor, gcTemps *gcHostTempTokens) error {
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
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("missing exact funcref type for argument %d", i)
			}
			if values[slot] == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("null funcref for non-null argument %d", i)
				}
			} else {
				store, err := in.funcrefStoreForEgress()
				if err != nil {
					return fmt.Errorf("funcref argument %d: %w", i, err)
				}
				actual, actualTypes, valid := store.descriptorFuncrefExactType(in, values[slot])
				if !valid {
					return fmt.Errorf("invalid funcref argument %d", i)
				}
				if !valueTypeSubtype(actual, actualTypes, required, exactTypes) {
					return fmt.Errorf("funcref argument %d does not match its exact structural type", i)
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
		case ValAnyRef, ValI31Ref:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("missing exact GC reference type for argument %d", i)
			}
			bits := values[slot]
			if bits == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("null GC reference for non-null argument %d", i)
				}
				break
			}
			ref := gc.Ref(uint32(bits))
			if uint64(ref) != bits || (!ref.IsObj() && !ref.IsI31()) || !in.gcRefMatchesValueType(ref, required) {
				return fmt.Errorf("invalid GC reference argument %d", i)
			}
			if ref.IsI31() {
				break
			}
			token, err := in.refStore.issueGCRef(in, ref, required)
			if err != nil {
				return fmt.Errorf("GC reference argument %d: %w", i, err)
			}
			if err := gcTemps.add(token); err != nil {
				_ = in.refStore.releaseGCRef(in, token)
				return err
			}
			values[slot] = token
		}
		slot++
	}
	return nil
}

func (in *Instance) translateHostReferenceResults(ctrl uintptr, values []uint64, types []ValType, exact []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor) error {
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
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("missing exact funcref type for result %d", i)
			}
			if values[slot] == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("null funcref for non-null result %d", i)
				}
			} else {
				if in.refStore == nil {
					return fmt.Errorf("invalid funcref token for result %d", i)
				}
				descriptor, ok := in.refStore.resolve(values[slot])
				if !ok {
					return fmt.Errorf("invalid funcref token for result %d", i)
				}
				actual, actualTypes, valid := in.refStore.tokenFuncrefExactType(values[slot])
				if !valid {
					return fmt.Errorf("invalid funcref token for result %d", i)
				}
				if !valueTypeSubtype(actual, actualTypes, required, exactTypes) {
					return fmt.Errorf("funcref result %d does not match its exact structural type", i)
				}
				values[slot] = descriptor
			}
		case ValExternRef:
			if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				return fmt.Errorf("invalid externref token for result %d", i)
			}
		case ValAnyRef, ValI31Ref:
			required, ok := exactReferenceType(exact, i, typ)
			if !ok {
				return fmt.Errorf("missing exact GC reference type for result %d", i)
			}
			bits := values[slot]
			if bits == 0 {
				if !required.Ref.Nullable {
					return fmt.Errorf("null GC reference for non-null result %d", i)
				}
				break
			}
			ref := gc.Ref(uint32(bits))
			if uint64(ref) == bits && ref.IsI31() {
				if !in.gcRefMatchesValueType(ref, required) {
					return fmt.Errorf("i31 result %d does not match its exact structural type", i)
				}
				break
			}
			if uint64(ref) == bits && ref.IsObj() {
				return fmt.Errorf("raw compact GC reference for result %d is not a host token", i)
			}
			resolved, err := in.refStore.stageGCHostResult(in, ctrl, bits, required)
			if err != nil {
				return fmt.Errorf("GC reference result %d: %w", i, err)
			}
			values[slot] = uint64(resolved)
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

// HostTrap aborts the current Wasm invocation with a host-provided error.
// Host functions should panic with HostTrap instead of panicking with an
// arbitrary value when they cannot represent failure in their Wasm signature.
// Wago recovers HostTrap at the native boundary and returns Err to the caller.
type HostTrap struct{ Err error }

// ExitError is returned by Invoke when a host function requested termination via
// panic(HostExit{...}). A zero code is a normal exit.
type ExitError struct{ Code int32 }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// callNativeSync runs a native entry that may make synchronous host calls,
// driving the re-entry loop with this instance's host dispatch. A host function
// may panic(HostExit{...}) to terminate; it is recovered here as an *ExitError.
func (in *Instance) callNativeSync(entry uintptr) error {
	return in.callNativeSyncWithTrap(entry, in.trap)
}

// callNativeSyncWithTrap is the host-capable form used when a Go-level
// re-export delegates execution while retaining the outer caller's trap cell.
func (in *Instance) callNativeSyncWithTrap(entry uintptr, activeTrap []byte) (err error) {
	return in.callNativeSyncWithTrapContext(entry, activeTrap, nil)
}

func (in *Instance) callNativeSyncWithTrapContext(entry uintptr, activeTrap []byte, waitParent context.Context) (err error) {
	locked, err := in.beginNativeEntry()
	if err != nil {
		return err
	}
	defer locked.unlockExecution()
	restoreInvocationContext := bindHostInvocationParent(in, waitParent)
	defer restoreInvocationContext()
	stopWaitContext := in.publishAtomicWaitContext(waitParent)
	defer stopWaitContext()
	defer func() { err = in.decorateTrap(err) }()
	defer func() {
		if r := recover(); r != nil {
			switch trap := r.(type) {
			case HostTrap:
				if trap.Err == nil {
					err = fmt.Errorf("wago: host trapped without an error")
				} else {
					err = trap.Err
				}
				return
			case *HostTrap:
				if trap == nil || trap.Err == nil {
					err = fmt.Errorf("wago: host trapped without an error")
				} else {
					err = trap.Err
				}
				return
			}
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
			if instruction, ok := r.(instructionTrap); ok {
				err = instruction.err
				return
			}
			if trap, ok := r.(gcStructHelperTrap); ok {
				err = &runtime.TrapError{Code: trap.code}
				return
			}
			if helper, ok := r.(gcStructHelperError); ok {
				err = fmt.Errorf("wago: WasmGC struct helper: %w", helper.err)
				return
			}
			if helper, ok := r.(atomicWaitHelperError); ok {
				if errors.Is(helper.err, errAtomicWaitInstanceClosed) {
					err = &runtime.TrapError{Code: runtime.TrapInterrupted}
				} else {
					err = helper.err
				}
				return
			}
			panic(r)
		}
	}()
	if err := in.jm.BindTrapCell(activeTrap); err != nil {
		return err
	}
	in.jm.SetStackFence(in.eng.StackLimit())
	if len(in.ctrl) >= runtime.HostCtrlFrameBytes {
		in.jm.SetCustomCtx(uintptr(unsafe.Pointer(&in.ctrl[0])))
	}
	if in.hostCall == nil {
		in.hostCall = in.newHostDispatch()
	}
	err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, in.dispatchSynchronousHostCall)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}
