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
	if h, ok := resolveHostCaller(caller); ok {
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
// ABI. It binds identically
// under standard Go and TinyGo — with no reflection anywhere on the path.
type HostFunc func(m HostModule, params, results []uint64)

// HostCall is a borrowed, logical view of one synchronous Wasm-to-Go call.
// Values are indexed by WebAssembly parameter/result position, not raw ABI
// slot.
// HostCall and values obtained from it are valid only until the callback
// returns and must not be retained.
type HostCall struct {
	params  []uint64
	results []uint64
	sig     *FuncSig
	exact   *DefinedTypeDescriptor
}

// HostCallFunc is the universal portable host callback for scalar and reference
// values. It supports arbitrary parameter and result arity without reflection
// or callback-time allocation. V128 host boundaries are intentionally
// unsupported. Ordinary Go functions recognized by Func and Imports use more
// specialized direct lanes when available.
type HostCallFunc func(HostCall)

// CallerHostCallFunc is the universal form when a callback also needs memory,
// reference-store access, invocation context, or synchronous re-entry authority.
type CallerHostCallFunc func(Caller, HostCall)

func (c HostCall) ParamCount() int {
	if c.sig == nil {
		return 0
	}
	return len(c.sig.Params)
}

func (c HostCall) ResultCount() int {
	if c.sig == nil {
		return 0
	}
	return len(c.sig.Results)
}

// ParamSlots returns the borrowed raw ABI slots for this call. The slice is
// valid only until the callback returns and must not be retained.
func (c HostCall) ParamSlots() []uint64 { return c.params }

// ResultSlots returns the borrowed writable raw ABI slots for this call. The
// slice is valid only until the callback returns and must not be retained.
func (c HostCall) ResultSlots() []uint64 { return c.results }

func (c HostCall) ParamType(i int) ValueTypeDescriptor {
	if c.exact != nil && uint(i) < uint(len(c.exact.Params)) {
		return c.exact.Params[i]
	}
	t, _ := valueTypeDescriptorFromValType(c.paramType(i))
	return t
}

func (c HostCall) ResultType(i int) ValueTypeDescriptor {
	if c.exact != nil && uint(i) < uint(len(c.exact.Results)) {
		return c.exact.Results[i]
	}
	t, _ := valueTypeDescriptorFromValType(c.resultType(i))
	return t
}

func (c HostCall) I32(i int) int32       { return AsI32(c.paramSlot(i, ValI32)) }
func (c HostCall) I64(i int) int64       { return AsI64(c.paramSlot(i, ValI64)) }
func (c HostCall) F32(i int) float32     { return AsF32(c.paramSlot(i, ValF32)) }
func (c HostCall) F64(i int) float64     { return AsF64(c.paramSlot(i, ValF64)) }
func (c HostCall) FuncRef(i int) FuncRef { return FuncRef{token: c.paramSlot(i, ValFuncRef)} }
func (c HostCall) ExternRef(i int) ExternRef {
	return ExternRef{token: c.paramSlot(i, ValExternRef)}
}
func (c HostCall) ExnRef(i int) ExnRef { return ExnRef{token: c.paramSlot(i, ValExnRef)} }
func (c HostCall) GCRef(i int) GCRef   { return GCRef{token: c.paramSlot(i, ValAnyRef)} }
func (c HostCall) I31Ref(i int) I31Ref { return I31Ref{bits: uint32(c.paramSlot(i, ValI31Ref))} }

func (c HostCall) SetI32(i int, v int32)       { c.setResultSlot(i, ValI32, I32(v)) }
func (c HostCall) SetI64(i int, v int64)       { c.setResultSlot(i, ValI64, I64(v)) }
func (c HostCall) SetF32(i int, v float32)     { c.setResultSlot(i, ValF32, F32(v)) }
func (c HostCall) SetF64(i int, v float64)     { c.setResultSlot(i, ValF64, F64(v)) }
func (c HostCall) SetFuncRef(i int, v FuncRef) { c.setResultSlot(i, ValFuncRef, v.token) }
func (c HostCall) SetExternRef(i int, v ExternRef) {
	c.setResultSlot(i, ValExternRef, v.token)
}
func (c HostCall) SetExnRef(i int, v ExnRef) { c.setResultSlot(i, ValExnRef, v.token) }
func (c HostCall) SetGCRef(i int, v GCRef)   { c.setResultSlot(i, ValAnyRef, v.token) }
func (c HostCall) SetI31Ref(i int, v I31Ref) { c.setResultSlot(i, ValI31Ref, uint64(v.bits)) }

// RawParam and SetRawResult are the complete, future-proof escape hatch for
// value types added after this release.
func (c HostCall) RawParam(i int) (lo, hi uint64) {
	typ := c.paramType(i)
	slot := hostCallSlot(c.sig.Params, i)
	lo = c.params[slot]
	if typ == ValV128 {
		hi = c.params[slot+1]
	}
	return
}

func (c HostCall) SetRawResult(i int, lo, hi uint64) {
	typ := c.resultType(i)
	slot := hostCallSlot(c.sig.Results, i)
	c.results[slot] = lo
	if typ == ValV128 {
		c.results[slot+1] = hi
	}
}

func (c HostCall) paramType(i int) ValType {
	if c.sig == nil || uint(i) >= uint(len(c.sig.Params)) {
		panic(fmt.Sprintf("wago: host parameter index %d out of range", i))
	}
	return c.sig.Params[i]
}

func (c HostCall) resultType(i int) ValType {
	if c.sig == nil || uint(i) >= uint(len(c.sig.Results)) {
		panic(fmt.Sprintf("wago: host result index %d out of range", i))
	}
	return c.sig.Results[i]
}

func hostCallSlot(types []ValType, index int) int {
	if uint(index) >= uint(len(types)) {
		panic(fmt.Sprintf("wago: host value index %d out of range", index))
	}
	slot := index
	for i := 0; i < index; i++ {
		if types[i] == ValV128 {
			slot++
		}
	}
	return slot
}

func (c HostCall) paramSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Params) != 0 {
		got := c.sig.Params[0]
		if got != want {
			panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.paramType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host parameter %d is %s, not %s", i, got, want))
	}
	return hostCallSlot(c.sig.Params, i)
}

func (c HostCall) resultSlotIndex(i int, want ValType) int {
	if i == 0 && c.sig != nil && len(c.sig.Results) != 0 {
		got := c.sig.Results[0]
		if got != want {
			panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
		}
		return 0
	}
	got := c.resultType(i)
	if got != want {
		panic(fmt.Sprintf("wago: host result %d is %s, not %s", i, got, want))
	}
	return hostCallSlot(c.sig.Results, i)
}

func (c HostCall) paramSlot(i int, want ValType) uint64 {
	return c.params[c.paramSlotIndex(i, want)]
}

func (c HostCall) setResultSlot(i int, want ValType, value uint64) {
	c.results[c.resultSlotIndex(i, want)] = value
}

// Caller is an immutable, callback-scoped capability for a synchronous host
// import. Its zero value has no authority. Copying or retaining a Caller does
// not extend its lifetime. Its methods have the same checks as HostModule.
// Caller also implements HostModule and its optional capability interfaces.
type Caller struct {
	instanceHostModule
}

// CallerHostFunc is the concrete-value alternative to HostFunc for synchronous
// imports. Direct dispatch does not box Caller into an interface. Parameters
// and results use HostFunc's slot ABI and must not be used after the callback
// returns. Host code may allocate; Wago does not pool or renew caller tokens.
// Legacy asynchronous host-call logging does not support CallerHostFunc.
type CallerHostFunc func(caller Caller, params, results []uint64)

// NoArgsHostFunc is a capability-free synchronous host import specialized for
// the Wasm signature () -> ().
// Deprecated: pass func() directly to Func or Imports.
type NoArgsHostFunc func()

// I32HostFunc is a capability-free synchronous host import specialized for the
// Wasm signature (i32) -> (). Unlike I32HostEvent, it runs before the guest's
// next instruction.
// Deprecated: pass func(int32) directly to Func or Imports.
type I32HostFunc func(int32)

// I32ToI32HostFunc is a capability-free synchronous host import specialized
// for the Wasm signature (i32) -> i32. Its signature is checked once when the
// instance is created. Calls do not construct a Caller or expose slot slices.
// The callback still runs on an ordinary Go stack and may allocate or panic.
// It carries no supported synchronous re-entry authority; use CallerHostFunc
// when callback-scoped re-entry is required. Directly invoking a captured
// Instance remains subject to ordinary instance serialization and is not a
// substitute for Caller-authorized re-entry.
// Deprecated: pass func(int32) int32 directly to Func or Imports.
type I32ToI32HostFunc func(int32) int32

// I32I32ToI32HostFunc is a capability-free synchronous host import specialized
// for the Wasm signature (i32, i32) -> i32. It has the same execution and
// lifetime and re-entry rules as I32ToI32HostFunc.
// Deprecated: pass func(int32, int32) int32 directly to Func or Imports.
type I32I32ToI32HostFunc func(int32, int32) int32

// I32I32HostFunc is a capability-free synchronous host import specialized for
// the Wasm signature (i32, i32) -> ().
// Deprecated: pass func(int32, int32) directly to Func or Imports.
type I32I32HostFunc func(int32, int32)

// I32ToI32I32HostFunc is a capability-free synchronous host import specialized
// for the Wasm signature (i32) -> (i32, i32).
// Deprecated: pass func(int32) (int32, int32) directly to Func or Imports.
type I32ToI32I32HostFunc func(int32) (int32, int32)

// I32I32ToI32I32HostFunc is a capability-free synchronous host import
// specialized for the Wasm signature (i32, i32) -> (i32, i32).
// Deprecated: pass func(int32, int32) (int32, int32) directly to Func or Imports.
type I32I32ToI32I32HostFunc func(int32, int32) (int32, int32)

// MaxDeferredHostEventsPerInvocation bounds an instance's deferred event log.
// Exceeding it traps the invocation and discards the whole event transaction.
const MaxDeferredHostEventsPerInvocation = runtime.HostCallLogEntries

// I32HostEvent is a deferred, capability-free host import specialized for the
// Wasm signature (i32) -> (). Native code appends each value to the instance's
// event log and continues without crossing onto the Go stack. Wago delivers the
// events in call order after the native invocation returns.
//
// The callback therefore cannot affect the currently running Wasm invocation.
// It runs on an ordinary Go stack and may allocate, block, or panic. Use a
// synchronous host function when the guest must observe the callback before its
// next instruction. At most MaxDeferredHostEventsPerInvocation events may be
// emitted by one invocation; overflow traps and delivers none of them.
type I32HostEvent func(int32)

// Only runtime-issued representations carry authority. The snapshot is copied,
// never replaced with the scope's current generation, including on re-entry.
func resolveHostCaller(module HostModule) (instanceHostModule, bool) {
	switch h := module.(type) {
	case instanceHostModule:
		return h, true
	case Caller:
		return h.instanceHostModule, true
	default:
		return instanceHostModule{}, false
	}
}

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
	h, ok := resolveHostCaller(caller)
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
	h, ok := resolveHostCaller(caller)
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
	h, ok := resolveHostCaller(caller)
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
	active   atomic.Uint64
	sequence atomic.Uint64 // never restored when an outer callback resumes
	state    atomic.Pointer[hostCallState]
}

func (s *hostCallScope) nextGeneration() uint64 {
	for {
		previous := s.sequence.Load()
		if previous == ^uint64(0) {
			panic(invalidHostReference{err: fmt.Errorf("host callback generation exhausted")})
		}
		if s.sequence.CompareAndSwap(previous, previous+1) {
			return previous + 1
		}
	}
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
	hostScope            hostCallScope
	activations          instanceActivations
	nativeContextVersion atomic.Uint64
	invokeMu             sync.Mutex // serializes unrelated public calls across parked host callbacks
	nativeExecutionMu    sync.Mutex // serializes native entry for an independent instance
	invocationID         invocationID
	close                atomic.Pointer[instanceCloseState]
	gcConfig             *GCConfig
	origin               InstantiateOrigin
	gcGlobalRootCount    uint32
	guestStorageBorrow   atomic.Uint32
	gcPublic             atomic.Pointer[gcPublicState]
	gcArrayElements      atomic.Pointer[gcArrayElementState]
	gcRefTestTable       atomic.Pointer[gcRefTestTableState]
	gcGlobalRoots        []gcGlobalRootMapping
	tagIdentityBase      uintptr      // arena-owned bounded native u64 directory for staged EH
	tagExports           map[int]*Tag // lazy stable identity handles for exported local tags
}

type instanceCloseState struct {
	done            chan struct{}
	quiesced        chan struct{}
	quiescedOnce    sync.Once
	prepared        atomic.Bool // publishes hook data and completion of all BeforeClose work
	result          error
	interruptStop   func()
	hooks           *hookRegistry
	event           *InstanceCloseEvent
	terminalStarted atomic.Bool
	terminalDone    chan struct{} // publishes terminalResult, independently of quiescence
	terminalResult  error
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
	generation := s.nextGeneration()
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

// instanceHostModule is an immutable capability snapshot. Never pool or mutate
// an interface-boxed value: a retained callback must not gain a later generation.
// Mutable reference-result cleanup stays in dispatch-owned ephemeralGCResults;
// every operation checks scope validity before accessing that temporary state.
type instanceHostModule struct {
	in                 *Instance
	scope              *hostCallScope
	generation         uint64
	parentGeneration   uint64
	invocationID       invocationID
	reservation        *pluginOperationReservation
	exact              *DefinedTypeDescriptor // immutable compiled signature, not per-call slice headers
	ephemeralGCResults *gcHostTempTokens
}

func (h instanceHostModule) exactSignature() (params, results []ValueTypeDescriptor) {
	if h.exact != nil {
		return h.exact.Params, h.exact.Results
	}
	return nil, nil
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

// bindHostImport resolves the legacy callback representation without reflection.
// Concrete synchronous callbacks use bindSyncHostImport instead of an adapter
// through HostModule, which would reintroduce per-callback interface boxing.
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
	fn           any
	typedI32     I32ToI32HostFunc
	typedI32x2   I32I32ToI32HostFunc
	gate         *pluginCallGate
	exact        *DefinedTypeDescriptor
	importIdx    uint32
	scalarKind   uint8
	hostCall     bool
	hostCallView bool
	typedNone    NoArgsHostFunc
	typedI32V    I32HostFunc
	typedI32x2V  I32I32HostFunc
	typedI32R2   I32ToI32I32HostFunc
	typedI32x2R2 I32I32ToI32I32HostFunc
	sig          *FuncSig
}

const (
	syncHostNonScalar uint8 = iota
	syncHostScalar
	syncHostTypedI32
	syncHostTypedI32x2
	syncHostTypedNone
	syncHostTypedI32V
	syncHostTypedI32x2V
	syncHostTypedI32R2
	syncHostTypedI32x2R2
	syncHostTypedI64
	syncHostTypedI64x2
	syncHostTypedF32
	syncHostTypedF32x2
	syncHostTypedF64
	syncHostTypedF64x2
)

type gatedI32ToI32HostFunc struct {
	fn   I32ToI32HostFunc
	gate *pluginCallGate
}

type gatedNoArgsHostFunc struct {
	fn   NoArgsHostFunc
	gate *pluginCallGate
}

type gatedI32HostFunc struct {
	fn   I32HostFunc
	gate *pluginCallGate
}

type gatedI32I32HostFunc struct {
	fn   I32I32HostFunc
	gate *pluginCallGate
}

type gatedI32ToI32I32HostFunc struct {
	fn   I32ToI32I32HostFunc
	gate *pluginCallGate
}

type gatedI32I32ToI32I32HostFunc struct {
	fn   I32I32ToI32I32HostFunc
	gate *pluginCallGate
}

type gatedI32I32ToI32HostFunc struct {
	fn   I32I32ToI32HostFunc
	gate *pluginCallGate
}

type gatedI32HostEvent struct {
	fn   I32HostEvent
	gate *pluginCallGate
}

type gatedHostCallFunc struct {
	fn   any
	gate *pluginCallGate
}

type gatedOrdinaryHostFunc struct {
	fn   any
	gate *pluginCallGate
}

func isHostCallback(value any) bool {
	switch value.(type) {
	case HostFunc, func(HostModule, []uint64, []uint64),
		CallerHostFunc, func(Caller, []uint64, []uint64),
		HostCallFunc, func(HostCall), CallerHostCallFunc, func(Caller, HostCall), gatedHostCallFunc,
		NoArgsHostFunc, func(), I32HostFunc, func(int32),
		I32ToI32HostFunc, func(int32) int32,
		I32I32HostFunc, func(int32, int32),
		I32I32ToI32HostFunc, func(int32, int32) int32,
		I32ToI32I32HostFunc, func(int32) (int32, int32),
		I32I32ToI32I32HostFunc, func(int32, int32) (int32, int32),
		func(int64) int64, func(int64, int64) int64,
		func(float32) float32, func(float32, float32) float32,
		func(float64) float64, func(float64, float64) float64,
		func(FuncRef) FuncRef, func(ExternRef) ExternRef,
		func(ExnRef) ExnRef, func(GCRef) GCRef, func(I31Ref) I31Ref,
		gatedNoArgsHostFunc, gatedI32HostFunc, gatedI32ToI32HostFunc,
		gatedI32I32HostFunc, gatedI32I32ToI32HostFunc,
		gatedI32ToI32I32HostFunc, gatedI32I32ToI32I32HostFunc,
		gatedOrdinaryHostFunc, I32HostEvent, gatedI32HostEvent, *HostFuncRef:
		return true
	default:
		return false
	}
}

func inferredHostFuncSignature(value any) (params, results []ValType, ok bool) {
	switch value.(type) {
	case NoArgsHostFunc, func():
		return nil, nil, true
	case I32HostFunc, func(int32):
		return []ValType{ValI32}, nil, true
	case I32ToI32HostFunc, func(int32) int32:
		return []ValType{ValI32}, []ValType{ValI32}, true
	case I32I32HostFunc, func(int32, int32):
		return []ValType{ValI32, ValI32}, nil, true
	case I32I32ToI32HostFunc, func(int32, int32) int32:
		return []ValType{ValI32, ValI32}, []ValType{ValI32}, true
	case I32ToI32I32HostFunc, func(int32) (int32, int32):
		return []ValType{ValI32}, []ValType{ValI32, ValI32}, true
	case I32I32ToI32I32HostFunc, func(int32, int32) (int32, int32):
		return []ValType{ValI32, ValI32}, []ValType{ValI32, ValI32}, true
	case func(int64) int64:
		return []ValType{ValI64}, []ValType{ValI64}, true
	case func(int64, int64) int64:
		return []ValType{ValI64, ValI64}, []ValType{ValI64}, true
	case func(float32) float32:
		return []ValType{ValF32}, []ValType{ValF32}, true
	case func(float32, float32) float32:
		return []ValType{ValF32, ValF32}, []ValType{ValF32}, true
	case func(float64) float64:
		return []ValType{ValF64}, []ValType{ValF64}, true
	case func(float64, float64) float64:
		return []ValType{ValF64, ValF64}, []ValType{ValF64}, true
	case func(FuncRef) FuncRef:
		return []ValType{ValFuncRef}, []ValType{ValFuncRef}, true
	case func(ExternRef) ExternRef:
		return []ValType{ValExternRef}, []ValType{ValExternRef}, true
	case func(ExnRef) ExnRef:
		return []ValType{ValExnRef}, []ValType{ValExnRef}, true
	case func(GCRef) GCRef:
		return []ValType{ValAnyRef}, []ValType{ValAnyRef}, true
	case func(I31Ref) I31Ref:
		return []ValType{ValI31Ref}, []ValType{ValI31Ref}, true
	default:
		return nil, nil, false
	}
}

func gateHostImport(value any, gate *pluginCallGate) (any, error) {
	switch fn := value.(type) {
	case HostFunc:
		if fn == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return gate.wrap(fn), nil
	case func(HostModule, []uint64, []uint64):
		if fn == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return gate.wrap(HostFunc(fn)), nil
	case CallerHostFunc:
		if fn == nil {
			return nil, fmt.Errorf("caller host function is nil")
		}
		return gate.wrapCaller(fn), nil
	case func(Caller, []uint64, []uint64):
		if fn == nil {
			return nil, fmt.Errorf("caller host function is nil")
		}
		return gate.wrapCaller(CallerHostFunc(fn)), nil
	case HostCallFunc:
		if fn == nil {
			return nil, fmt.Errorf("host call function is nil")
		}
		return gatedHostCallFunc{fn: fn, gate: gate}, nil
	case func(HostCall):
		if fn == nil {
			return nil, fmt.Errorf("host call function is nil")
		}
		return gatedHostCallFunc{fn: HostCallFunc(fn), gate: gate}, nil
	case CallerHostCallFunc:
		if fn == nil {
			return nil, fmt.Errorf("caller host call function is nil")
		}
		return gatedHostCallFunc{fn: fn, gate: gate}, nil
	case func(Caller, HostCall):
		if fn == nil {
			return nil, fmt.Errorf("caller host call function is nil")
		}
		return gatedHostCallFunc{fn: CallerHostCallFunc(fn), gate: gate}, nil
	case NoArgsHostFunc:
		return gatedNoArgsHostFunc{fn: fn, gate: gate}, nil
	case func():
		return gatedNoArgsHostFunc{fn: NoArgsHostFunc(fn), gate: gate}, nil
	case I32HostFunc:
		return gatedI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32):
		return gatedI32HostFunc{fn: I32HostFunc(fn), gate: gate}, nil
	case I32ToI32HostFunc:
		return gatedI32ToI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32) int32:
		return gatedI32ToI32HostFunc{fn: I32ToI32HostFunc(fn), gate: gate}, nil
	case I32I32HostFunc:
		return gatedI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32):
		return gatedI32I32HostFunc{fn: I32I32HostFunc(fn), gate: gate}, nil
	case I32I32ToI32HostFunc:
		return gatedI32I32ToI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32) int32:
		return gatedI32I32ToI32HostFunc{fn: I32I32ToI32HostFunc(fn), gate: gate}, nil
	case I32ToI32I32HostFunc:
		return gatedI32ToI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32) (int32, int32):
		return gatedI32ToI32I32HostFunc{fn: I32ToI32I32HostFunc(fn), gate: gate}, nil
	case I32I32ToI32I32HostFunc:
		return gatedI32I32ToI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32) (int32, int32):
		return gatedI32I32ToI32I32HostFunc{fn: I32I32ToI32I32HostFunc(fn), gate: gate}, nil
	case func(int64) int64, func(int64, int64) int64,
		func(float32) float32, func(float32, float32) float32,
		func(float64) float64, func(float64, float64) float64,
		func(FuncRef) FuncRef, func(ExternRef) ExternRef,
		func(ExnRef) ExnRef, func(GCRef) GCRef, func(I31Ref) I31Ref:
		return gatedOrdinaryHostFunc{fn: value, gate: gate}, nil
	default:
		return nil, fmt.Errorf("unsupported host function %T; use an ordinary supported function or func(wago.HostCall)", value)
	}
}

type asyncHostBinding struct {
	eventI32 I32HostEvent
	gate     *pluginCallGate
}

type hostEventBindings struct {
	byImport []asyncHostBinding
}

func bindI32HostEvent(value any, sig FuncSig) (asyncHostBinding, error) {
	var binding asyncHostBinding
	switch fn := value.(type) {
	case I32HostEvent:
		binding.eventI32 = fn
	case gatedI32HostEvent:
		binding.eventI32, binding.gate = fn.fn, fn.gate
	default:
		return binding, fmt.Errorf("deferred host event must be a wago.I32HostEvent; got %T", value)
	}
	if binding.eventI32 == nil {
		return asyncHostBinding{}, fmt.Errorf("deferred host event is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 0 {
		return asyncHostBinding{}, fmt.Errorf("deferred host event requires signature (i32) -> ()")
	}
	return binding, nil
}

func (c *Compiled) buildHostEvents(imports Imports) (*hostEventBindings, error) {
	events := &hostEventBindings{byImport: make([]asyncHostBinding, len(c.Imports))}
	for i, key := range c.Imports {
		if _, cross := imports[key].(*InstanceExport); cross {
			continue
		}
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		binding, err := bindI32HostEvent(imports[key], c.importFuncSigs[i])
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		events.byImport[i] = binding
	}
	return events, nil
}

func (b *syncHostBinding) callable() bool {
	return b.fn != nil || b.scalarKind >= syncHostTypedI32
}

// call is the common Go-level entry for a bound synchronous host function. It
// owns plugin admission unless the caller carries an operation reservation for
// this exact gate. Native guest dispatch uses representation-specific unchecked
// helpers after performing the same admission around argument/result translation.
func (b *syncHostBinding) call(caller instanceHostModule, args, results []uint64) {
	if b.gate != nil && (caller.reservation == nil || !caller.reservation.allows(b.gate)) {
		if err := b.gate.enter(); err != nil {
			panic(HostTrap{Err: err})
		}
		defer b.gate.release()
	}
	b.callUnchecked(caller, args, results)
}

func (b *syncHostBinding) callUnchecked(caller instanceHostModule, args, results []uint64) {
	if b.fn != nil {
		b.callBoundUnchecked(caller, args, results)
		return
	}
	switch b.scalarKind {
	case syncHostTypedI32:
		results[0] = I32(b.typedI32(AsI32(args[0])))
	case syncHostTypedI32x2:
		results[0] = I32(b.typedI32x2(AsI32(args[0]), AsI32(args[1])))
	case syncHostTypedNone:
		b.typedNone()
	case syncHostTypedI32V:
		b.typedI32V(AsI32(args[0]))
	case syncHostTypedI32x2V:
		b.typedI32x2V(AsI32(args[0]), AsI32(args[1]))
	case syncHostTypedI32R2:
		r0, r1 := b.typedI32R2(AsI32(args[0]))
		results[0], results[1] = I32(r0), I32(r1)
	case syncHostTypedI32x2R2:
		r0, r1 := b.typedI32x2R2(AsI32(args[0]), AsI32(args[1]))
		results[0], results[1] = I32(r0), I32(r1)
	default:
		panic(fmt.Sprintf("wago: invalid bound host function %T", b.fn))
	}
}

func (b *syncHostBinding) callBoundUnchecked(caller instanceHostModule, args, results []uint64) {
	switch fn := b.fn.(type) {
	case CallerHostFunc:
		fn(Caller{instanceHostModule: caller}, args, results)
	case HostCallFunc:
		fn(HostCall{
			params: args, results: results, sig: b.sig, exact: b.exact,
		})
	case CallerHostCallFunc:
		fn(Caller{instanceHostModule: caller}, HostCall{
			params: args, results: results, sig: b.sig, exact: b.exact,
		})
	case func(int64) int64:
		results[0] = I64(fn(AsI64(args[0])))
	case func(int64, int64) int64:
		results[0] = I64(fn(AsI64(args[0]), AsI64(args[1])))
	case func(float32) float32:
		results[0] = F32(fn(AsF32(args[0])))
	case func(float32, float32) float32:
		results[0] = F32(fn(AsF32(args[0]), AsF32(args[1])))
	case func(float64) float64:
		results[0] = F64(fn(AsF64(args[0])))
	case func(float64, float64) float64:
		results[0] = F64(fn(AsF64(args[0]), AsF64(args[1])))
	case func(FuncRef) FuncRef:
		results[0] = fn(FuncRef{token: args[0]}).token
	case func(ExternRef) ExternRef:
		results[0] = fn(ExternRef{token: args[0]}).token
	case func(ExnRef) ExnRef:
		results[0] = fn(ExnRef{token: args[0]}).token
	case func(GCRef) GCRef:
		results[0] = fn(GCRef{token: args[0]}).token
	case func(I31Ref) I31Ref:
		results[0] = uint64(fn(I31Ref{bits: uint32(args[0])}).bits)
	case HostFunc:
		fn(caller, args, results)
	default:
		panic(fmt.Sprintf("wago: invalid bound host function %T", b.fn))
	}
}

func bindSyncHostImport(value any, sig FuncSig) (syncHostBinding, error) {
	if hasValType(sig.Params, ValV128) || hasValType(sig.Results, ValV128) {
		return syncHostBinding{}, fmt.Errorf("v128 host callbacks are not supported")
	}
	switch fn := value.(type) {
	case I32HostEvent, gatedI32HostEvent:
		return syncHostBinding{}, fmt.Errorf("deferred host event cannot be used by a module that requires synchronous host control")
	case CallerHostFunc:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("concrete host function is nil")
		}
		return syncHostBinding{fn: fn, sig: &sig}, nil
	case func(Caller, []uint64, []uint64):
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("concrete host function is nil")
		}
		return syncHostBinding{fn: CallerHostFunc(fn)}, nil
	case HostCallFunc:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("host call function is nil")
		}
		return syncHostBinding{fn: fn, sig: &sig, hostCall: true}, nil
	case func(HostCall):
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("host call function is nil")
		}
		return syncHostBinding{fn: HostCallFunc(fn), sig: &sig, hostCall: true}, nil
	case CallerHostCallFunc:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("caller host call function is nil")
		}
		return syncHostBinding{fn: fn, sig: &sig}, nil
	case func(Caller, HostCall):
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("caller host call function is nil")
		}
		return syncHostBinding{fn: CallerHostCallFunc(fn), sig: &sig}, nil
	case NoArgsHostFunc:
		return bindNoArgsHostFunc(fn, sig)
	case func():
		return bindNoArgsHostFunc(NoArgsHostFunc(fn), sig)
	case I32HostFunc:
		return bindI32HostFunc(fn, sig)
	case func(int32):
		return bindI32HostFunc(I32HostFunc(fn), sig)
	case I32ToI32HostFunc:
		return bindI32ToI32HostFunc(fn, sig)
	case func(int32) int32:
		return bindI32ToI32HostFunc(I32ToI32HostFunc(fn), sig)
	case I32I32ToI32HostFunc:
		return bindI32I32ToI32HostFunc(fn, sig)
	case func(int32, int32) int32:
		return bindI32I32ToI32HostFunc(I32I32ToI32HostFunc(fn), sig)
	case I32I32HostFunc:
		return bindI32I32HostFunc(fn, sig)
	case func(int32, int32):
		return bindI32I32HostFunc(I32I32HostFunc(fn), sig)
	case I32ToI32I32HostFunc:
		return bindI32ToI32I32HostFunc(fn, sig)
	case func(int32) (int32, int32):
		return bindI32ToI32I32HostFunc(I32ToI32I32HostFunc(fn), sig)
	case I32I32ToI32I32HostFunc:
		return bindI32I32ToI32I32HostFunc(fn, sig)
	case func(int32, int32) (int32, int32):
		return bindI32I32ToI32I32HostFunc(I32I32ToI32I32HostFunc(fn), sig)
	case func(int64) int64:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValI64, 1, syncHostBinding{fn: fn, scalarKind: syncHostTypedI64})
	case func(int64, int64) int64:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValI64, 2, syncHostBinding{fn: fn, scalarKind: syncHostTypedI64x2})
	case func(float32) float32:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValF32, 1, syncHostBinding{fn: fn, scalarKind: syncHostTypedF32})
	case func(float32, float32) float32:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValF32, 2, syncHostBinding{fn: fn, scalarKind: syncHostTypedF32x2})
	case func(float64) float64:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValF64, 1, syncHostBinding{fn: fn, scalarKind: syncHostTypedF64})
	case func(float64, float64) float64:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValF64, 2, syncHostBinding{fn: fn, scalarKind: syncHostTypedF64x2})
	case func(FuncRef) FuncRef:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValFuncRef, 1, syncHostBinding{fn: fn})
	case func(ExternRef) ExternRef:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValExternRef, 1, syncHostBinding{fn: fn})
	case func(ExnRef) ExnRef:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValExnRef, 1, syncHostBinding{fn: fn})
	case func(GCRef) GCRef:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValAnyRef, 1, syncHostBinding{fn: fn})
	case func(I31Ref) I31Ref:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValI31Ref, 1, syncHostBinding{fn: fn})
	case gatedI32ToI32HostFunc:
		binding, err := bindI32ToI32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedNoArgsHostFunc:
		binding, err := bindNoArgsHostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedI32HostFunc:
		binding, err := bindI32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedI32I32HostFunc:
		binding, err := bindI32I32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedI32ToI32I32HostFunc:
		binding, err := bindI32ToI32I32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedI32I32ToI32I32HostFunc:
		binding, err := bindI32I32ToI32I32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedI32I32ToI32HostFunc:
		binding, err := bindI32I32ToI32HostFunc(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedHostCallFunc:
		binding, err := bindSyncHostImport(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case gatedOrdinaryHostFunc:
		binding, err := bindSyncHostImport(fn.fn, sig)
		binding.gate = fn.gate
		return binding, err
	case func(HostModule, []uint64, []uint64):
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("host function is nil")
		}
		return syncHostBinding{fn: HostFunc(fn)}, nil
	}
	fn, err := bindHostImport(value, sig)
	return syncHostBinding{fn: fn}, err
}

func bindSimpleTypedHostFunc(sig FuncSig, typ ValType, params int, binding syncHostBinding) (syncHostBinding, error) {
	if len(sig.Params) != params || len(sig.Results) != 1 || sig.Results[0] != typ {
		return syncHostBinding{}, fmt.Errorf("typed host function requires %d %s parameter(s) and one %s result", params, typ, typ)
	}
	for _, param := range sig.Params {
		if param != typ {
			return syncHostBinding{}, fmt.Errorf("typed host function requires %d %s parameter(s) and one %s result", params, typ, typ)
		}
	}
	return binding, nil
}

func bindNoArgsHostFunc(fn NoArgsHostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 0 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature () -> ()")
	}
	return syncHostBinding{typedNone: fn, scalarKind: syncHostTypedNone}, nil
}

func bindI32HostFunc(fn I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> ()")
	}
	return syncHostBinding{typedI32V: fn, scalarKind: syncHostTypedI32V}, nil
}

func bindI32ToI32HostFunc(fn I32ToI32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> i32")
	}
	return syncHostBinding{typedI32: fn, scalarKind: syncHostTypedI32}, nil
}

func bindI32I32ToI32HostFunc(fn I32I32ToI32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32, i32) -> i32")
	}
	return syncHostBinding{typedI32x2: fn, scalarKind: syncHostTypedI32x2}, nil
}

func bindI32I32HostFunc(fn I32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32, i32) -> ()")
	}
	return syncHostBinding{typedI32x2V: fn, scalarKind: syncHostTypedI32x2V}, nil
}

func bindI32ToI32I32HostFunc(fn I32ToI32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 2 || sig.Results[0] != ValI32 || sig.Results[1] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> (i32, i32)")
	}
	return syncHostBinding{typedI32R2: fn, scalarKind: syncHostTypedI32R2}, nil
}

func bindI32I32ToI32I32HostFunc(fn I32I32ToI32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 2 || sig.Results[0] != ValI32 || sig.Results[1] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32, i32) -> (i32, i32)")
	}
	return syncHostBinding{typedI32x2R2: fn, scalarKind: syncHostTypedI32x2R2}, nil
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
		paramSlots, err := valTypesSlots(sig.Params)
		if err != nil {
			return nil, fmt.Errorf("import %q params: %w", key, err)
		}
		resultSlots, err := valTypesSlots(sig.Results)
		if err != nil {
			return nil, fmt.Errorf("import %q results: %w", key, err)
		}
		binding, err := bindSyncHostImport(imports[key], sig)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", key, err)
		}
		binding.importIdx = uint32(i)
		binding.sig = &c.importFuncSigs[i]
		binding.hostCallView = binding.hostCall && paramSlots <= runtime.MaxHostArity &&
			resultSlots <= runtime.MaxHostArity && paramSlots+resultSlots >= directHostCallViewSlots
		if binding.scalarKind == syncHostNonScalar {
			binding.scalarKind = syncHostScalar
		}
		for _, typ := range sig.Params {
			if isReferenceValType(typ) {
				binding.scalarKind = syncHostNonScalar
				break
			}
		}
		if binding.scalarKind != syncHostNonScalar {
			for _, typ := range sig.Results {
				if isReferenceValType(typ) {
					binding.scalarKind = syncHostNonScalar
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

func dispatchSyncHostScalar(in *Instance, scope *hostCallScope, binding *syncHostBinding, args, results []uint64, invocation hostInvocationContext) {
	enteredGate := false
	if binding.gate != nil && (invocation.reservation == nil || !invocation.reservation.allows(binding.gate)) {
		if err := binding.gate.enter(); err != nil {
			panic(HostTrap{Err: err})
		}
		enteredGate = true
	}
	if enteredGate {
		defer binding.gate.release()
	}
	if binding.typedI32 != nil {
		results[0] = I32(binding.typedI32(AsI32(args[0])))
		return
	}
	if binding.typedNone != nil {
		binding.typedNone()
		return
	}
	if binding.typedI32V != nil {
		binding.typedI32V(AsI32(args[0]))
		return
	}
	if binding.typedI32x2V != nil {
		binding.typedI32x2V(AsI32(args[0]), AsI32(args[1]))
		return
	}
	if binding.typedI32R2 != nil {
		r0, r1 := binding.typedI32R2(AsI32(args[0]))
		results[0], results[1] = I32(r0), I32(r1)
		return
	}
	if binding.typedI32x2R2 != nil {
		r0, r1 := binding.typedI32x2R2(AsI32(args[0]), AsI32(args[1]))
		results[0], results[1] = I32(r0), I32(r1)
		return
	}
	if binding.typedI32x2 != nil {
		results[0] = I32(binding.typedI32x2(AsI32(args[0]), AsI32(args[1])))
		return
	}
	if binding.hostCall {
		binding.fn.(HostCallFunc)(HostCall{
			params: args, results: results, sig: binding.sig, exact: binding.exact,
		})
		return
	}
	caller := scope.beginReservedWithID(in, invocation.id, invocation.reservation)
	caller.exact = binding.exact
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	binding.callBoundUnchecked(caller, args, results)
}

func dispatchSyncHostReference(in *Instance, scope *hostCallScope, ctrl uintptr, importIdx uint32, binding *syncHostBinding, sig FuncSig, exact *DefinedTypeDescriptor, exactTypes []DefinedTypeDescriptor, exactTypesPtr *[]DefinedTypeDescriptor, args, results []uint64, invocation hostInvocationContext) {
	if binding.gate != nil && (invocation.reservation == nil || !invocation.reservation.allows(binding.gate)) {
		dispatchSyncHostReferenceGated(in, scope, ctrl, importIdx, binding, sig, exact, exactTypes, exactTypesPtr, args, results, invocation)
		return
	}
	if _, ok := binding.fn.(HostCallFunc); ok && !hasReferenceValType(sig.Params) && !hasReferenceValType(sig.Results) {
		binding.callBoundUnchecked(instanceHostModule{}, args, results)
		return
	}
	var exactParams, exactResults []ValueTypeDescriptor
	if exact != nil {
		exactParams, exactResults = exact.Params, exact.Results
	}
	var gcTemps gcHostTempTokens
	if err := in.translateHostReferenceArgs(args, sig.Params, exactParams, exactTypes, &gcTemps); err != nil {
		gcTemps.release(in)
		panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
	}
	defer gcTemps.release(in)
	caller := scope.beginReservedWithID(in, invocation.id, invocation.reservation)
	caller.exact = exact
	var gcResultTemps gcHostTempTokens
	gcResultTemps.exactTypes = exactTypesPtr
	caller.ephemeralGCResults = &gcResultTemps
	defer gcResultTemps.release(in)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	binding.callBoundUnchecked(caller, args, results)
	if err := in.translateHostReferenceResults(ctrl, results, sig.Results, exactResults, exactTypes); err != nil {
		panic(invalidHostReference{err: fmt.Errorf("host import %d: %w", importIdx, err)})
	}
}

func dispatchSyncHostReferenceGated(in *Instance, scope *hostCallScope, ctrl uintptr, importIdx uint32, binding *syncHostBinding, sig FuncSig, exact *DefinedTypeDescriptor, exactTypes []DefinedTypeDescriptor, exactTypesPtr *[]DefinedTypeDescriptor, args, results []uint64, invocation hostInvocationContext) {
	if err := binding.gate.enter(); err != nil {
		panic(HostTrap{Err: err})
	}
	defer binding.gate.release()
	ungated := *binding
	ungated.gate = nil
	dispatchSyncHostReference(in, scope, ctrl, importIdx, &ungated, sig, exact, exactTypes, exactTypesPtr, args, results, invocation)
}

// newHostDispatch builds the runtime callback the CallWithHost loop invokes: it
// maps the wasm import index to the bound HostFunc and runs it with a HostModule
// bound to this instance. It is constructed once at instantiation so hot Invoke
// paths do not allocate a fresh closure per call.
func (in *Instance) newHostDispatch() resolvedHostCall {
	// Atomic publication happens once; the sidecar is never replaced. Cache
	// only its address, not authority: each callback still gets a fresh atomic
	// generation and the runtime-resolved invocation identity passed below.
	scope := &in.ensurePluginState().hostScope
	dispatch := func(ctrl uintptr, importIdx uint32, args, results []uint64, invocation hostInvocationContext) {
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
			var signature *DefinedTypeDescriptor
			var exactTypes []DefinedTypeDescriptor
			var exactTypesPtr *[]DefinedTypeDescriptor
			if exact != nil {
				sig = exact.sig
				// Dispatch bindings are created only after exact signature validation.
				signature = &exact.types[sig.TypeIndex]
				exactTypes = exact.types
				exactTypesPtr = &exact.types
			}
			dispatchSyncHostReference(in, scope, ctrl, importIdx, &syncHostBinding{fn: fn}, sig, signature, exactTypes, exactTypesPtr, args, results, invocation)
			return
		}
		if int(importIdx) >= len(in.syncHosts) || !in.syncHosts[importIdx].callable() {
			panic(missingHostFunc{importIdx: importIdx})
		}
		binding := &in.syncHosts[importIdx]
		if binding.scalarKind != syncHostNonScalar {
			dispatchSyncHostScalar(in, scope, binding, args, results, invocation)
		} else {
			dispatchSyncHostReference(in, scope, ctrl, importIdx, binding, in.c.importFuncSigs[importIdx], binding.exact, in.c.Types, &in.c.Types, args, results, invocation)
		}
	}
	if len(in.syncHosts) == 1 {
		binding := &in.syncHosts[0]
		if binding.hostCall && binding.gate == nil && binding.scalarKind != syncHostNonScalar {
			fn := binding.fn.(HostCallFunc)
			sig, exact := binding.sig, binding.exact
			return func(ctrl uintptr, importIdx uint32, args, results []uint64, invocation hostInvocationContext) {
				if importIdx == 0 {
					fn(HostCall{params: args, results: results, sig: sig, exact: exact})
					return
				}
				dispatch(ctrl, importIdx, args, results, invocation)
			}
		}
	}
	return dispatch
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
		case ValExnRef:
			if values[slot] != 0 {
				return fmt.Errorf("non-null exception reference argument %d cannot cross the host boundary", i)
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
		case ValExnRef:
			if values[slot] != 0 {
				return fmt.Errorf("non-null exception reference result %d cannot cross the host boundary", i)
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
			if ex, ok := r.(*HostExit); ok && ex != nil {
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
	// Resolve stable root context lazily in this activation, not on the instance.
	// Each nested native entry receives a separate snapshot and fresh callbacks.
	activation := hostLoopActivation{
		root:                        in,
		ctrl:                        offHeapSlicePtr(in.ctrl),
		state:                       in.ensurePluginState(),
		parkedNativeContextReusable: in.gc == nil && !in.c.threadedMemory0(),
	}
	if in.hasSingleDirectTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalar(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchSingleTypedScalarPortal)
	} else if in.hasSingleExpandedTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalarExpanded(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchSingleTypedScalarExpandedPortal)
	} else if in.hasExpandedTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalarExpanded(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchTypedScalarExpandedPortal)
	} else if in.hasDirectTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalar(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchTypedScalarPortal)
	} else if in.hasSingleHostCallPortal() {
		err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatchSingleHostCall)
	} else if in.hasSingleHostCallViewPortal() {
		err = in.eng.CallWithHostBaseView(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchSingleHostCallView)
	} else {
		err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}
