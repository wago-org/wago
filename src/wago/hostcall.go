// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"context"
	"fmt"
	"os"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// HostModule gives a synchronous host import access to the instance that called
// it. Caller implements this interface during a caller-aware callback.
type HostModule interface {
	// Memory returns the calling instance's linear memory as a mutable slice
	// (empty if the module declares no memory). Writes are visible to wasm; the
	// slice is valid only for the duration of the host call.
	Memory() []byte
}

// InvokeFromHost invokes an export while caller is an active synchronous host
// callback. The HostModule carries an unforgeable, callback-scoped invocation
// identity. If that call chain re-enters a parked Instance, Wago supplies an
// isolated native stack and call buffers for the nested activation. One
// invocation may have at most 32 active InvokeFromHost calls under standard Go
// or three under TinyGo. Exceeding that limit returns an error wrapping
// ErrPermissionDenied. Retained or unrelated HostModule values fail closed.
func (in *Instance) InvokeFromHost(ctx context.Context, caller HostModule, export string, args ...uint64) (results []uint64, err error) {
	var active *Instance
	var id invocationID
	var reservation pluginReservation
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
	profileCompleted := true
	if codeProfileEnabled && activeHostInvocationContext(active).profileSession() != nil {
		var span profileSpanToken
		ctx, span = profileReentryContext(active, in, ctx)
		profileCompleted = false
		defer finishProfileBoundary(span, &err, &profileCompleted)
	}
	contexts := invocationContextSetFor(ctx)
	if codeProfileEnabled && activeHostInvocationContext(active).profileSession() != nil {
		contexts.callback = ctx
	}
	depth, ok := acquireHostReentryDepth(id)
	if !ok {
		profileCompleted = true
		return nil, fmt.Errorf("wago: host re-entry depth %d exceeds limit %d: %w", depth+1, maxHostReentryDepth, ErrPermissionDenied)
	}
	defer releaseHostReentryDepth(id)
	results, err = in.invokeWithToken(export, args, contexts, id, false, false, reservation)
	err = contextInterruptError(ctx, err)
	profileCompleted = true
	return results, err
}

// hostReentryDepths retains its small map for allocation-free ordinary reuse.
// A burst above the retained-chain cap discards the backing map after all
// active chains unwind, so process-wide memory does not retain its high-water
// capacity.
var hostReentryDepths struct {
	sync.Mutex
	values        map[invocationID]uint8
	resetWhenIdle bool
}

func acquireHostReentryDepth(id invocationID) (uint8, bool) {
	hostReentryDepths.Lock()
	defer hostReentryDepths.Unlock()
	if hostReentryDepths.values == nil {
		hostReentryDepths.values = make(map[invocationID]uint8)
	}
	depth := hostReentryDepths.values[id]
	if depth >= maxHostReentryDepth {
		return depth, false
	}
	depth++
	hostReentryDepths.values[id] = depth
	if len(hostReentryDepths.values) > maxRetainedHostReentryChains {
		hostReentryDepths.resetWhenIdle = true
	}
	return depth, true
}

func releaseHostReentryDepth(id invocationID) {
	hostReentryDepths.Lock()
	defer hostReentryDepths.Unlock()
	depth := hostReentryDepths.values[id]
	if depth <= 1 {
		delete(hostReentryDepths.values, id)
		if hostReentryDepths.resetWhenIdle && len(hostReentryDepths.values) == 0 {
			hostReentryDepths.values = nil
			hostReentryDepths.resetWhenIdle = false
		}
	} else {
		hostReentryDepths.values[id] = depth - 1
	}
}

// Bound retained accounting separately from each build's active call depth.
const maxRetainedHostReentryChains = 64

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

// slotHostFunc is a host import in reflection-free slot (stack) form: it reads its
// wasm params from params (i32/f32 in the low 32 bits) and writes its results
// into results, with the calling instance's linear memory and externref store
// available through HostModule. A reference occupies one opaque uint64 slot; a
// v128 occupies two adjacent little-endian uint64 slots, matching Invoke's public
// ABI. It binds identically
// under standard Go and TinyGo — with no reflection anywhere on the path.
type slotHostFunc func(m HostModule, params, results []uint64)

// HostCall is a borrowed, logical view of one synchronous Wasm-to-Go call.
// Values are indexed by WebAssembly parameter/result position, not raw ABI
// slot. Production dispatch supplies complete slot slices from the compiled
// signature and keeps that frozen signature for the callback lifetime. Each
// value occupies at least one slot, so equal slot/type counts exclude v128.
// HostCall and values obtained from it are valid only until the callback
// returns and must not be retained.
type HostCall struct {
	_       [0]func() // Keep the borrowed-view value non-comparable.
	params  hostCallSlots
	results hostCallSlots
	sig     *FuncSig
	exact   *DefinedTypeDescriptor
}

// HostCallFunc is the universal portable host callback for scalar and reference
// values. It supports arbitrary parameter and result arity without reflection
// or callback-time allocation. V128 host boundaries are intentionally
// unsupported. Ordinary Go functions recognized by Func and Imports use more
// specialized direct lanes when available. HostCall, ParamSlots, ResultSlots,
// and any borrowed memory/reference views must not be retained after return.
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
func (c HostCall) ParamSlots() []uint64 { return c.params.slice() }

// ResultSlots returns the borrowed writable raw ABI slots for this call. The
// slice is valid only until the callback returns and must not be retained.
func (c HostCall) ResultSlots() []uint64 { return c.results.slice() }

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
	slot := i
	if c.params.len() != len(c.sig.Params) {
		slot = hostCallSlot(c.sig.Params, i)
	}
	lo = c.params.slice()[slot]
	if typ == ValV128 {
		hi = c.params.slice()[slot+1]
	}
	return
}

func (c HostCall) SetRawResult(i int, lo, hi uint64) {
	typ := c.resultType(i)
	slot := i
	if c.results.len() != len(c.sig.Results) {
		slot = hostCallSlot(c.sig.Results, i)
	}
	c.results.slice()[slot] = lo
	if typ == ValV128 {
		c.results.slice()[slot+1] = hi
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

func (c HostCall) paramSlot(i int, want ValType) uint64 {
	if i == 0 && c.sig != nil && len(c.sig.Params) != 0 && c.sig.Params[0] == want {
		return c.params.slice()[0]
	}
	return c.params.slice()[c.paramSlotIndex(i, want)]
}

func (c HostCall) setResultSlot(i int, want ValType, value uint64) {
	if i == 0 && c.sig != nil && len(c.sig.Results) != 0 && c.sig.Results[0] == want {
		c.results.slice()[0] = value
		return
	}
	c.results.slice()[c.resultSlotIndex(i, want)] = value
}

// Caller is an immutable, callback-scoped capability for a synchronous host
// import. Its zero value has no authority. Copying or retaining a Caller does
// not extend its lifetime. Its methods have the same checks as HostModule.
// Caller also implements HostModule and its optional capability interfaces.
type Caller struct {
	instanceHostModule
}

// callerSlotHostFunc is the concrete-value alternative to slotHostFunc for synchronous
// imports. Direct dispatch does not box Caller into an interface. Parameters
// and results use slotHostFunc's slot ABI and must not be used after the callback
// returns. Host code may allocate; Wago does not pool or renew caller tokens.
// Legacy asynchronous host-call logging does not support callerSlotHostFunc.
type callerSlotHostFunc func(caller Caller, params, results []uint64)

// noArgsHostFunc is the private specialized lane for func().
type noArgsHostFunc func()

// i32HostFunc is the private synchronous lane for func(int32).
type i32HostFunc func(int32)

// i32ToI32HostFunc is the private synchronous lane for func(int32) int32.
type i32ToI32HostFunc func(int32) int32

// i32I32ToI32HostFunc is the private lane for func(int32, int32) int32.
type i32I32ToI32HostFunc func(int32, int32) int32

// i32I32HostFunc is the private lane for func(int32, int32).
type i32I32HostFunc func(int32, int32)

// i32ToI32I32HostFunc is the private lane for func(int32) (int32, int32).
type i32ToI32I32HostFunc func(int32) (int32, int32)

// i32I32ToI32I32HostFunc is the private lane for
// func(int32, int32) (int32, int32).
type i32I32ToI32I32HostFunc func(int32, int32) (int32, int32)

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
// Resolve succeeds only while the slotHostFunc callback is active. Retaining the
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
	ctx, ok := h.scope.invocationContext(h.generation, h.scope.invocationParent(activeHostInvocationContext(h.in).parent))
	if !ok {
		return nil, fmt.Errorf("wago: invocation context requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return ctx, nil
}

// InvocationMayCancel reports whether the active call has a cancellation or
// deadline signal. A false result lets synchronous re-entry avoid creating a
// cancellation watcher. The caller is authorized with the same callback scope
// as InvocationContext. The signal belongs to this callback even during nested
// execution; no parent context or values are exposed.
func (r *CallerResolver) InvocationMayCancel(caller HostModule) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("wago: nil caller resolver: %w", ErrPermissionDenied)
	}
	rt := r.rt.Load()
	h, ok := resolveHostCaller(caller)
	if rt == nil || !ok || !h.valid() || h.in == nil || h.in.rt != rt || h.scope == nil {
		return false, fmt.Errorf("wago: invocation cancellation requires an active host call from the owning runtime: %w", ErrPermissionDenied)
	}
	return h.generation&hostCallMayCancel != 0, nil
}

// hostCallScope authorizes one synchronous use of an instanceHostModule.
type hostCallScope struct {
	railshotHostScopeParent
	active   atomic.Uint64
	sequence atomic.Uint64 // never restored when an outer callback resumes
	state    atomic.Pointer[hostCallState]
}

// The low generation bit stores the immutable cancellation signal. The remaining
// bits identify the callback without increasing the boxed caller token.
const hostCallMayCancel uint64 = 1
const maxHostCallSequence = ^uint64(0) >> 1

func invocationMayCancel(parent context.Context) bool {
	if parent == nil {
		return false
	}
	if parent.Done() != nil || parent.Err() != nil {
		return true
	}
	_, hasDeadline := parent.Deadline()
	return hasDeadline
}

func (s *hostCallScope) nextGeneration() uint64 {
	for {
		previous := s.sequence.Load()
		if previous >= maxHostCallSequence {
			panic(invalidHostReference{err: fmt.Errorf("host callback generation exhausted")})
		}
		if s.sequence.CompareAndSwap(previous, previous+1) {
			return (previous + 1) << 1
		}
	}
}

type hostCallWaiter struct {
	generation uint64
	wake       chan struct{}
}

// hostCallState is allocated when a plugin watches/resolves a caller or a
// cancelable foreign callback must retain its root parent independently. The context list is bounded by the already-
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
	//lint:ignore U1000 used only by wago_profile builds; the ordinary placeholder is empty
	profileState         profileInstanceState
	hostScope            hostCallScope
	activations          instanceActivations
	railshotInvocation   railshotInvocationState
	nativeContextVersion atomic.Uint64
	invokeMu             invocationGate // serializes unrelated public calls across parked host callbacks
	nativeExecutionMu    sync.Mutex     // serializes native entry for an independent instance
	nativeShareMu        sync.Mutex     // coordinates retained local leases with resource publication
	preparedHostGate     *preparedHostLeaseGate
	hostInvokeCache      []*WasmFunc // allocated only after an ordinary prepared host call
	invokeCacheExtra     *invokeCacheOverflow
	invokeCacheSlots     uint8 // zero uses the four-slot default
	invocationID         invocationID
	gcConfig             *GCConfig
	origin               InstantiateOrigin
	guestStorageBorrow   atomic.Uint32
	pluginPublicGCState
	stagedGCObjectState
	stagedGCGlobalRootState
	instanceExceptionTagState
	boundedViewVersion uint64                  // native lease protected; valid only for this observed context epoch
	boundedViewMemBase uintptr                 // changed backing invalidates cached view entry too
	multiHost          *boundedMultiHostLayout // immutable admitted import shapes, allocated only for multi-import modules
}

type instanceCloseState struct {
	doneMu        sync.Mutex
	done          chan struct{}
	completed     atomic.Bool
	quiesced      chan struct{}
	quiescedOnce  sync.Once
	prepared      atomic.Bool // publishes hook data and completion of all BeforeClose work
	result        error
	interruptStop func()
	pluginInstanceCloseState
	terminalStarted   atomic.Bool
	terminalComplete  atomic.Bool
	terminalPublished atomic.Bool
	terminalDone      chan struct{} // publishes terminalResult, independently of quiescence
	terminalCloseCollection
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

func (s *hostCallScope) beginReserved(in *Instance, reservation pluginReservation) instanceHostModule {
	return s.beginReservedWithID(in, in.currentInvocationID(), reservation, activeHostInvocationContext(in).parent)
}

func (s *hostCallScope) beginReservedWithID(in *Instance, id invocationID, reservation pluginReservation, parentContext context.Context) instanceHostModule {
	generation, parent := s.beginGeneration(parentContext)
	return instanceHostModule{in: in, scope: s, generation: generation, parentGeneration: parent, invocationID: id, reservation: reservation}
}

// beginGeneration keeps scope activation separate from the immutable capability
// snapshot, so direct dispatch need not return and copy the whole token.
func (s *hostCallScope) beginGeneration(parentContext context.Context) (generation, parent uint64) {
	parent = s.active.Load()
	mayCancel := invocationMayCancel(parentContext)
	generation = s.nextGeneration()
	if mayCancel {
		generation |= hostCallMayCancel
	}
	s.active.Store(generation)
	return generation, parent
}

func (s *hostCallScope) end(generation, parent uint64) {
	active := s.active.CompareAndSwap(generation, parent)
	if active {
		s.releaseInvocationParent()
	}
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
		candidate.invokeMu.state.shared = &in.invocationState.word
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

func (in *Instance) beginHostCallScopeReserved(reservation pluginReservation) instanceHostModule {
	return in.beginHostCallScopeReservedWithID(in.currentInvocationID(), reservation)
}

func (in *Instance) beginHostCallScopeReservedWithID(id invocationID, reservation pluginReservation) instanceHostModule {
	return in.ensurePluginState().hostScope.beginReservedWithID(in, id, reservation, activeHostInvocationContext(in).parent)
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
// that may be materialized as a non-null funcref. Ordinary HostFunc
// registrations stay callable but fail closed if their descriptor would cross
// a public funcref boundary.
type HostFuncRef struct {
	mu            sync.Mutex
	fn            *syncHostBinding
	store         *referenceStore
	sig           FuncSig
	source        *Instance
	descriptor    uint64
	dispatchIndex uint32
	importers     int
	tokenLive     bool
	closed        bool
	hostGCBridgeCapability
	binding *hostFuncRefBindingState
}

// hostFuncRefBindingState is allocated for exact reference dispatch. Its
// collector domain is present only on builds that support Wasm GC.
type hostFuncRefBindingState struct {
	hostGCBridgeBinding
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
func (rt *Runtime) NewHostFuncRef(fn any, sig FuncSig) (*HostFuncRef, error) {
	return rt.newHostFuncRef(fn, sig, false, false)
}

// NewGCHostFuncRef creates a Runtime-owned host function that may transfer
// collector references through its declared signature. GC-reference argument
// slots are presented to fn as temporary opaque GCRef tokens, and result slots
// accept null, immediate i31 values, or a live GCRef token from the exact bound
// collector domain. The first importer binds the owner to one canonical Runtime
// collector domain; incompatible or foreign-domain importers fail closed.
func (rt *Runtime) NewGCHostFuncRef(fn any, sig FuncSig) (*HostFuncRef, error) {
	if !wasmGCBuilt {
		return nil, fmt.Errorf("wago: GC host function references are disabled in this build")
	}
	if !funcSigHasGCRefs(sig) {
		return nil, fmt.Errorf("wago: GC host function signature has no collector references")
	}
	return rt.newHostFuncRef(fn, sig, true, false)
}

func (rt *Runtime) newHostFuncRef(callback any, sig FuncSig, gcCapable, allowLoading bool) (*HostFuncRef, error) {
	if rt == nil || rt.references() == nil {
		return nil, fmt.Errorf("wago: nil runtime")
	}
	operation, err := rt.beginOperation("NewHostFuncRef", allowLoading)
	if err != nil {
		return nil, err
	}
	defer operation.end()
	if callback == nil {
		return nil, fmt.Errorf("wago: host function is nil")
	}
	if _, err := valTypesSlots(sig.Params); err != nil {
		return nil, fmt.Errorf("wago: host function parameters: %w", err)
	}
	if _, err := valTypesSlots(sig.Results); err != nil {
		return nil, fmt.Errorf("wago: host function results: %w", err)
	}
	// The owner and callback binding share one private signature copy.
	sig.Params = append([]ValType(nil), sig.Params...)
	sig.Results = append([]ValType(nil), sig.Results...)
	binding, err := bindSyncHostImport(callback, sig)
	if err != nil {
		return nil, fmt.Errorf("wago: host function: %w", err)
	}
	owner := &HostFuncRef{
		fn:    &binding,
		store: rt.references(),
		sig:   sig,
	}
	binding.sig = &owner.sig
	owner.setGCBridgeCapability(gcCapable)
	dispatchIndex, err := rt.references().registerHostFuncRef(owner)
	if err != nil {
		return nil, err
	}
	owner.dispatchIndex = dispatchIndex
	return owner, nil
}

// Attached importers retain the owner until their native code is released.
// Pure typed numeric callbacks need no callback Caller or reference translation;
// keeping their immutable binding permits the ordinary per-import scalar portal.
// Materialized funcrefs still retain their owner and owned dispatch namespace.
func (h *HostFuncRef) directNumericBinding() (syncHostBinding, bool) {
	if h == nil {
		return syncHostBinding{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.allowsGCBridge() || h.fn == nil || h.fn.callbackGate() != nil || h.fn.scalarKind < syncHostTypedI32 ||
		hasReferenceValType(h.sig.Params) || hasReferenceValType(h.sig.Results) {
		return syncHostBinding{}, false
	}
	return *h.fn, true
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
	if h.binding == nil || h.binding.types == nil || !exactSignatureEquivalent(h.binding.params, h.binding.results, h.binding.types, params, results, c.Types) {
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
	boundCollector, boundDomain := h.binding.gcDomain()
	if !wasmGCBuilt || !h.allowsGCBridge() || collector == nil || boundCollector != collector || boundDomain == 0 || boundDomain != domainID {
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
	if exactNeeded && h.binding != nil && h.binding.types != nil && !exactSignatureEquivalent(h.binding.params, h.binding.results, h.binding.types, params, results, c.Types) {
		return fmt.Errorf("host funcref structural signature mismatch")
	}
	gcRefs := funcSigHasGCRefs(sig)
	if gcRefs {
		if !h.allowsGCBridge() {
			return fmt.Errorf("host funcref collector-reference signature requires Runtime.NewGCHostFuncRef")
		}
		if !wasmGCBuilt || collector == nil || domainID == 0 || c == nil || c.genericGCFrameRoots() == nil || !store.ownsGCCollector(collector) {
			return fmt.Errorf("GC host funcref requires an exact live Runtime collector domain and native root maps")
		}
		boundCollector, boundDomain := h.binding.gcDomain()
		if boundCollector != nil && (boundCollector != collector || boundDomain != domainID) {
			return fmt.Errorf("GC host funcref belongs to a different Runtime collector domain")
		}
	} else if h.allowsGCBridge() {
		return fmt.Errorf("GC host funcref requires a collector-reference signature")
	}
	if exactNeeded && (h.binding == nil || h.binding.types == nil) {
		if h.binding == nil {
			h.binding = &hostFuncRefBindingState{}
		}
		h.binding.params = params
		h.binding.results = results
		h.binding.types = c.Types
	}
	if wasmGCBuilt && gcRefs {
		if h.binding == nil {
			return fmt.Errorf("GC host funcref requires an exact structural signature")
		}
		if boundCollector, _ := h.binding.gcDomain(); boundCollector == nil {
			h.binding.bindGCCollector(collector, domainID)
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
	if h.closed || h.store != store || h.binding == nil || h.binding.types == nil || !exactSignatureEquivalent(h.binding.params, h.binding.results, h.binding.types, params, results, c.Types) {
		return 0, false, fmt.Errorf("host funcref structural signature mismatch")
	}
	if binding := h.binding.inlineDispatchBinding; binding != nil && h.binding.inlineDispatchKey == key {
		if binding.refs == ^uint32(0) {
			return 0, false, fmt.Errorf("host funcref dispatch binding has too many importers")
		}
		binding.refs++
		return binding.dispatchIndex, true, nil
	}
	if binding := h.binding.dispatchBindings[key]; binding != nil {
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
	if h.binding.inlineDispatchBinding == nil {
		h.binding.inlineDispatchKey = key
		h.binding.inlineDispatchBinding = binding
	} else {
		if h.binding.dispatchBindings == nil {
			h.binding.dispatchBindings = make(map[hostFuncRefBindingKey]*hostFuncRefDispatchBinding)
		}
		h.binding.dispatchBindings[key] = binding
	}
	return dispatchIndex, true, nil
}

func (h *HostFuncRef) dispatchBinding(c *Compiled, importIndex int) (*hostFuncRefDispatchBinding, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.binding == nil {
		return nil, false
	}
	key := hostFuncRefBindingKey{owner: h, compiled: c, importIndex: importIndex}
	if h.binding.inlineDispatchBinding != nil && h.binding.inlineDispatchKey == key {
		return h.binding.inlineDispatchBinding, true
	}
	binding := h.binding.dispatchBindings[key]
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
	if h.binding != nil {
		if binding := h.binding.inlineDispatchBinding; binding != nil && h.binding.inlineDispatchKey == key {
			if binding.refs > 1 {
				binding.refs--
			} else {
				h.binding.inlineDispatchKey = hostFuncRefBindingKey{}
				h.binding.inlineDispatchBinding = nil
				store.unregisterHostFuncRefBindingLocked(binding)
			}
		} else if binding := h.binding.dispatchBindings[key]; binding != nil {
			if binding.refs > 1 {
				binding.refs--
			} else {
				delete(h.binding.dispatchBindings, key)
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
	if h.importers == 0 && h.binding != nil {
		h.binding.hostGCBridgeBinding = hostGCBridgeBinding{}
		h.binding.params = nil
		h.binding.results = nil
		h.binding.types = nil
		h.binding.inlineDispatchKey = hostFuncRefBindingKey{}
		h.binding.inlineDispatchBinding = nil
		h.binding.dispatchBindings = nil
	}
	h.mu.Unlock()
}

func (h *HostFuncRef) canonicalDescriptor(source *Instance, descriptor uint64, sig FuncSig) (*Instance, uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.store == nil || source == nil || source.refStore != h.store || h.importers == 0 || !funcSigEqual(h.sig, sig) {
		return nil, 0, false
	}
	// Type inspection can select a candidate without issuing a token/root.
	// A retired unissued candidate must not poison a later live importer.
	retiredCandidate := h.source != nil && !h.tokenLive && (h.source.isLogicallyClosed() || !h.source.hasPhysicalResources())
	if h.source == nil || retiredCandidate {
		if !source.hasPhysicalResources() {
			return nil, 0, false
		}
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
// The callback owns its reference context and temporary result tokens.
// Operations check the generation before accessing the dispatch state.
type instanceHostModule struct {
	in               *Instance
	scope            *hostCallScope
	generation       uint64
	parentGeneration uint64
	invocationID     invocationID
	reservation      pluginReservation
	exact            *DefinedTypeDescriptor // immutable compiled signature, not per-call slice headers
	referenceCall    *hostReferenceCallState
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
	if !nativeCollectorFrameRootsBuilt {
		return h.in.collectGCWithJoint(false)
	}
	if h.in.ownsGCInvocation(h.invocationID) {
		return h.in.collectGC()
	}
	lease := h.in.lockManagedFunctionCollectionInvocation(h.invocationID)
	if nativeCollectorBuilt {
		defer lease.unlock()
	}
	return h.in.collectGCWithJoint(lease.isDynamic())
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

// bindHostImport resolves the private native slot representation without
// reflection. Public callbacks are normalized by bindSyncHostImport.
func bindHostImport(v any, sig FuncSig) (slotHostFunc, error) {
	if custom, ok := v.(nativeCustomInstructionBinding); ok {
		if err := custom.validate(sig); err != nil {
			return nil, err
		}
		return custom.fn, nil
	}
	switch f := v.(type) {
	case slotHostFunc:
		if f == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return f, nil
	case *HostFuncRef:
		if !referenceStoreForImage {
			return nil, fmt.Errorf("host funcref requires a reference store")
		}
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
		binding := f.fn
		return slotHostFunc(func(module HostModule, params, results []uint64) {
			caller, ok := resolveHostCaller(module)
			if !ok {
				panic(HostTrap{Err: fmt.Errorf("wago: invalid host function caller")})
			}
			binding.callUnchecked(caller, params, results)
		}), nil
	case nil:
		return nil, fmt.Errorf("no host function provided")
	default:
		return nil, fmt.Errorf("host import must be an internal slot callback or *wago.HostFuncRef; got %T", v)
	}
}

type syncHostBinding struct {
	fn         any
	typedI32   i32ToI32HostFunc
	typedI32x2 i32I32ToI32HostFunc
	typedI64   func(int64) int64
	pluginCallbackGate
	exact        *DefinedTypeDescriptor
	importIdx    uint32
	scalarKind   uint8
	hostCall     bool
	hostCallView bool
	typedNone    noArgsHostFunc
	typedI32V    i32HostFunc
	typedI32x2V  i32I32HostFunc
	typedI32R2   i32ToI32I32HostFunc
	typedI32x2R2 i32I32ToI32I32HostFunc
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

func isHostCallback(value any) bool {
	switch value.(type) {
	case slotHostFunc, callerSlotHostFunc,
		HostCallFunc, func(HostCall), CallerHostCallFunc, func(Caller, HostCall),
		noArgsHostFunc, func(), i32HostFunc, func(int32),
		i32ToI32HostFunc, func(int32) int32,
		i32I32HostFunc, func(int32, int32),
		i32I32ToI32HostFunc, func(int32, int32) int32,
		i32ToI32I32HostFunc, func(int32) (int32, int32),
		i32I32ToI32I32HostFunc, func(int32, int32) (int32, int32),
		func(int64) int64, func(int64, int64) int64,
		func(float32) float32, func(float32, float32) float32,
		func(float64) float64, func(float64, float64) float64,
		func(FuncRef) FuncRef, func(ExternRef) ExternRef,
		func(ExnRef) ExnRef, func(GCRef) GCRef, func(I31Ref) I31Ref,
		I32HostEvent, *HostFuncRef:
		return true
	default:
		return pluginRuntimeBuilt && isGatedHostCallback(value)
	}
}

// inspectHostFuncSignature classifies only the public callback forms accepted
// by Imports.HostFunc and HostImportRegistrar.HostFunc. Internal slot-ABI and
// gated callbacks deliberately remain outside this registration boundary.
func inspectHostFuncSignature(value any) (params, results []ValType, inferred, supported, nilCallback bool) {
	switch fn := value.(type) {
	case HostCallFunc:
		return nil, nil, false, true, fn == nil
	case func(HostCall):
		return nil, nil, false, true, fn == nil
	case CallerHostCallFunc:
		return nil, nil, false, true, fn == nil
	case func(Caller, HostCall):
		return nil, nil, false, true, fn == nil
	case noArgsHostFunc:
		return nil, nil, true, true, fn == nil
	case func():
		return nil, nil, true, true, fn == nil
	case i32HostFunc:
		return []ValType{ValI32}, nil, true, true, fn == nil
	case func(int32):
		return []ValType{ValI32}, nil, true, true, fn == nil
	case i32ToI32HostFunc:
		return []ValType{ValI32}, []ValType{ValI32}, true, true, fn == nil
	case func(int32) int32:
		return []ValType{ValI32}, []ValType{ValI32}, true, true, fn == nil
	case i32I32HostFunc:
		return []ValType{ValI32, ValI32}, nil, true, true, fn == nil
	case func(int32, int32):
		return []ValType{ValI32, ValI32}, nil, true, true, fn == nil
	case i32I32ToI32HostFunc:
		return []ValType{ValI32, ValI32}, []ValType{ValI32}, true, true, fn == nil
	case func(int32, int32) int32:
		return []ValType{ValI32, ValI32}, []ValType{ValI32}, true, true, fn == nil
	case i32ToI32I32HostFunc:
		return []ValType{ValI32}, []ValType{ValI32, ValI32}, true, true, fn == nil
	case func(int32) (int32, int32):
		return []ValType{ValI32}, []ValType{ValI32, ValI32}, true, true, fn == nil
	case i32I32ToI32I32HostFunc:
		return []ValType{ValI32, ValI32}, []ValType{ValI32, ValI32}, true, true, fn == nil
	case func(int32, int32) (int32, int32):
		return []ValType{ValI32, ValI32}, []ValType{ValI32, ValI32}, true, true, fn == nil
	case func(int64) int64:
		return []ValType{ValI64}, []ValType{ValI64}, true, true, fn == nil
	case func(int64, int64) int64:
		return []ValType{ValI64, ValI64}, []ValType{ValI64}, true, true, fn == nil
	case func(float32) float32:
		return []ValType{ValF32}, []ValType{ValF32}, true, true, fn == nil
	case func(float32, float32) float32:
		return []ValType{ValF32, ValF32}, []ValType{ValF32}, true, true, fn == nil
	case func(float64) float64:
		return []ValType{ValF64}, []ValType{ValF64}, true, true, fn == nil
	case func(float64, float64) float64:
		return []ValType{ValF64, ValF64}, []ValType{ValF64}, true, true, fn == nil
	case func(FuncRef) FuncRef:
		return []ValType{ValFuncRef}, []ValType{ValFuncRef}, true, true, fn == nil
	case func(ExternRef) ExternRef:
		return []ValType{ValExternRef}, []ValType{ValExternRef}, true, true, fn == nil
	case func(ExnRef) ExnRef:
		return []ValType{ValExnRef}, []ValType{ValExnRef}, true, true, fn == nil
	case func(GCRef) GCRef:
		return []ValType{ValAnyRef}, []ValType{ValAnyRef}, true, true, fn == nil
	case func(I31Ref) I31Ref:
		return []ValType{ValI31Ref}, []ValType{ValI31Ref}, true, true, fn == nil
	default:
		return nil, nil, false, false, false
	}
}

type asyncHostBinding struct {
	pluginCallbackGate
	eventI32 I32HostEvent
}

type hostEventBindings struct {
	byImport []asyncHostBinding
}

func isI32HostEvent(value any) bool {
	if _, ok := value.(I32HostEvent); ok {
		return true
	}
	if pluginRuntimeBuilt {
		_, ok := value.(gatedI32HostEvent)
		return ok
	}
	return false
}

func bindI32HostEvent(value any, sig FuncSig) (asyncHostBinding, error) {
	event, ok := value.(I32HostEvent)
	var gate *pluginCallGate
	if pluginRuntimeBuilt && !ok {
		if gated, gatedOK := value.(gatedI32HostEvent); gatedOK {
			event, gate, ok = gated.fn, gated.gate, true
		}
	}
	if !ok {
		return asyncHostBinding{}, fmt.Errorf("deferred host event must be a wago.I32HostEvent; got %T", value)
	}
	if event == nil {
		return asyncHostBinding{}, fmt.Errorf("deferred host event is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 0 {
		return asyncHostBinding{}, fmt.Errorf("deferred host event requires signature (i32) -> ()")
	}
	binding := asyncHostBinding{eventI32: event}
	if pluginRuntimeBuilt {
		binding.setCallbackGate(gate)
	}
	return binding, nil
}

func (c *Compiled) buildHostEvents(imports resolvedImports) (*hostEventBindings, error) {
	events := &hostEventBindings{byImport: make([]asyncHostBinding, len(c.Imports))}
	for i, key := range c.Imports {
		bindingKey := c.functionImportBindingKey(i)
		if _, cross := imports[bindingKey].(*InstanceExport); cross {
			continue
		}
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		binding, err := bindI32HostEvent(imports[bindingKey], c.importFuncSigs[i])
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
	if gate := b.callbackGate(); pluginRuntimeBuilt && gate != nil && !pluginReservationAllows(caller.reservation, gate) {
		if err := gate.enter(); err != nil {
			panic(HostTrap{Err: err})
		}
		defer gate.release()
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
	case callerSlotHostFunc:
		fn(Caller{instanceHostModule: caller}, args, results)
	case HostCallFunc:
		fn(HostCall{
			params: compactHostSlots(args), results: compactHostSlots(results), sig: b.sig, exact: b.exact,
		})
	case CallerHostCallFunc:
		fn(Caller{instanceHostModule: caller}, HostCall{
			params: compactHostSlots(args), results: compactHostSlots(results), sig: b.sig, exact: b.exact,
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
	case slotHostFunc:
		fn(caller, args, results)
	default:
		panic(fmt.Sprintf("wago: invalid bound host function %T", b.fn))
	}
}

func bindSyncHostImport(value any, sig FuncSig) (syncHostBinding, error) {
	if custom, ok := value.(nativeCustomInstructionBinding); ok {
		if err := custom.validate(sig); err != nil {
			return syncHostBinding{}, err
		}
		return syncHostBinding{fn: custom.fn, sig: &sig}, nil
	}
	if hasValType(sig.Params, ValV128) || hasValType(sig.Results, ValV128) {
		return syncHostBinding{}, fmt.Errorf("v128 host callbacks are not supported")
	}
	switch fn := value.(type) {
	case *HostFuncRef:
		if !referenceStoreForImage {
			return syncHostBinding{}, fmt.Errorf("host funcref requires a reference store")
		}
		if binding, ok := fn.directNumericBinding(); ok {
			if !funcSigEqual(fn.sig, sig) {
				return syncHostBinding{}, fmt.Errorf("host funcref signature mismatch")
			}
			return binding, nil
		}
	case I32HostEvent:
		return syncHostBinding{}, fmt.Errorf("deferred host event cannot be used by a module that requires synchronous host control")
	case callerSlotHostFunc:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("concrete host function is nil")
		}
		return syncHostBinding{fn: fn, sig: &sig}, nil
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
	case noArgsHostFunc:
		return bindNoArgsHostFunc(fn, sig)
	case func():
		return bindNoArgsHostFunc(noArgsHostFunc(fn), sig)
	case i32HostFunc:
		return bindI32HostFunc(fn, sig)
	case func(int32):
		return bindI32HostFunc(i32HostFunc(fn), sig)
	case i32ToI32HostFunc:
		return bindI32ToI32HostFunc(fn, sig)
	case func(int32) int32:
		return bindI32ToI32HostFunc(i32ToI32HostFunc(fn), sig)
	case i32I32ToI32HostFunc:
		return bindI32I32ToI32HostFunc(fn, sig)
	case func(int32, int32) int32:
		return bindI32I32ToI32HostFunc(i32I32ToI32HostFunc(fn), sig)
	case i32I32HostFunc:
		return bindI32I32HostFunc(fn, sig)
	case func(int32, int32):
		return bindI32I32HostFunc(i32I32HostFunc(fn), sig)
	case i32ToI32I32HostFunc:
		return bindI32ToI32I32HostFunc(fn, sig)
	case func(int32) (int32, int32):
		return bindI32ToI32I32HostFunc(i32ToI32I32HostFunc(fn), sig)
	case i32I32ToI32I32HostFunc:
		return bindI32I32ToI32I32HostFunc(fn, sig)
	case func(int32, int32) (int32, int32):
		return bindI32I32ToI32I32HostFunc(i32I32ToI32I32HostFunc(fn), sig)
	case func(int64) int64:
		if fn == nil {
			return syncHostBinding{}, fmt.Errorf("typed host function is nil")
		}
		return bindSimpleTypedHostFunc(sig, ValI64, 1, syncHostBinding{fn: fn, typedI64: fn, scalarKind: syncHostTypedI64})
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
	}
	if pluginRuntimeBuilt {
		if binding, handled, err := bindGatedSyncHostImport(value, sig); handled {
			return binding, err
		}
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

func bindNoArgsHostFunc(fn noArgsHostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 0 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature () -> ()")
	}
	return syncHostBinding{typedNone: fn, scalarKind: syncHostTypedNone}, nil
}

func bindI32HostFunc(fn i32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> ()")
	}
	return syncHostBinding{typedI32V: fn, scalarKind: syncHostTypedI32V}, nil
}

func bindI32ToI32HostFunc(fn i32ToI32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> i32")
	}
	return syncHostBinding{typedI32: fn, scalarKind: syncHostTypedI32}, nil
}

func bindI32I32ToI32HostFunc(fn i32I32ToI32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32, i32) -> i32")
	}
	return syncHostBinding{typedI32x2: fn, scalarKind: syncHostTypedI32x2}, nil
}

func bindI32I32HostFunc(fn i32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 0 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32, i32) -> ()")
	}
	return syncHostBinding{typedI32x2V: fn, scalarKind: syncHostTypedI32x2V}, nil
}

func bindI32ToI32I32HostFunc(fn i32ToI32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
	if fn == nil {
		return syncHostBinding{}, fmt.Errorf("typed host function is nil")
	}
	if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 2 || sig.Results[0] != ValI32 || sig.Results[1] != ValI32 {
		return syncHostBinding{}, fmt.Errorf("typed host function requires signature (i32) -> (i32, i32)")
	}
	return syncHostBinding{typedI32R2: fn, scalarKind: syncHostTypedI32R2}, nil
}

func bindI32I32ToI32I32HostFunc(fn i32I32ToI32I32HostFunc, sig FuncSig) (syncHostBinding, error) {
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
func (c *Compiled) buildSyncHosts(imports resolvedImports) ([]syncHostBinding, error) {
	hosts := make([]syncHostBinding, len(c.Imports))
	for i, key := range c.Imports {
		bindingKey := c.functionImportBindingKey(i)
		if i >= len(c.importFuncSigs) {
			return nil, fmt.Errorf("import %q: missing signature", key)
		}
		// A cross-instance binding is a native call, not a host function; skip it.
		if _, cross := imports[bindingKey].(*InstanceExport); cross {
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
		binding, err := bindSyncHostImport(imports[bindingKey], sig)
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

type boundHostFuncRefCall struct {
	owner           *HostFuncRef
	fn              *syncHostBinding
	sig             FuncSig
	params, results []ValueTypeDescriptor
	types           *[]DefinedTypeDescriptor
}

func (in *Instance) pluginGCImportSet() map[uint32]struct{} {
	if !pluginRuntimeBuilt || in == nil {
		return nil
	}
	if state := in.importState.Load(); state != nil {
		return state.gcImports()
	}
	return nil
}

func (in *Instance) pluginGCHostSignature(dispatch uint32) (FuncSig, bool) {
	if !wasmGCBuilt || in == nil || in.c == nil || dispatch&hostFuncRefDispatchBit != 0 || uint64(dispatch) >= uint64(len(in.c.Imports)) || uint64(dispatch) >= uint64(len(in.c.importFuncSigs)) || !funcSigHasGCRefs(in.c.importFuncSigs[dispatch]) {
		return FuncSig{}, false
	}
	if _, ok := in.pluginGCImportSet()[dispatch]; !ok {
		return FuncSig{}, false
	}
	return in.c.importFuncSigs[dispatch], true
}

func (in *Instance) boundHostFuncRef(dispatch uint32) (boundHostFuncRefCall, bool) {
	if !referenceStoreForImage || in == nil || in.refStore == nil || dispatch&hostFuncRefDispatchBit == 0 {
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

func dispatchSyncHostScalar(in *Instance, scope *hostCallScope, ctrl uintptr, binding *syncHostBinding, callerExact *DefinedTypeDescriptor, args, results []uint64, invocation hostInvocationContext) {
	if gate := binding.callbackGate(); pluginRuntimeBuilt && gate != nil && !pluginReservationAllows(invocation.reservation, gate) {
		if err := gate.enter(); err != nil {
			panic(HostTrap{Err: err})
		}
		defer gate.release()
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
			params: compactHostSlots(args), results: compactHostSlots(results), sig: binding.sig, exact: binding.exact,
		})
		return
	}
	if invocation.callbackScope != nil {
		scope = invocation.callbackScope
	} else {
		scope = railshotCallbackHostScope(in, ctrl, scope, invocation.parent)
	}
	caller := scope.beginReservedWithID(in, invocation.id, invocation.reservation, invocation.parent)
	caller.exact = callerExact
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	// Foreign roots use their own scope, not the producer's ambient control
	// context. Preserve a cancelable parent before lazy resolver access.
	if invocation.scope != nil && invocationMayCancel(invocation.parent) {
		scope.invocationContext(caller.generation, invocation.parent)
	}
	binding.callBoundUnchecked(caller, args, results)
}

// newHostDispatch builds the optional specialized callback used by injected
// paths. Ordinary calls dispatch through dispatchHostCall directly
// so a never-invoked instance does not allocate a closure or plugin sidecar.
func (in *Instance) newHostDispatch() resolvedHostCall {
	if len(in.syncHosts) == 1 {
		binding := &in.syncHosts[0]
		if binding.hostCall && binding.callbackGate() == nil && binding.scalarKind != syncHostNonScalar {
			fn := binding.fn.(HostCallFunc)
			sig, exact := binding.sig, binding.exact
			return func(ctrl uintptr, importIdx uint32, args, results []uint64, invocation hostInvocationContext) {
				if importIdx == 0 {
					fn(HostCall{params: compactHostSlots(args), results: compactHostSlots(results), sig: sig, exact: exact})
					return
				}
				in.dispatchHostCall(ctrl, importIdx, args, results, invocation)
			}
		}
	}
	return in.dispatchHostCall
}

func (in *Instance) dispatchHostCall(ctrl uintptr, importIdx uint32, args, results []uint64, invocation hostInvocationContext) {
	if atomicWaitRuntimeBuilt && importIdx&shared.AtomicWaitDispatchBit != 0 {
		if importIdx&(gcStructDispatchBit|hostFuncRefDispatchBit) != 0 {
			panic(atomicWaitHelperError{err: fmt.Errorf("invalid overlapping atomic helper dispatch index %#x", importIdx)})
		}
		in.dispatchAtomicWaitHelper(importIdx&^shared.AtomicWaitDispatchBit, args, results)
		return
	}
	if nativeCollectorBuilt && importIdx&gcStructDispatchBit != 0 {
		if importIdx&hostFuncRefDispatchBit != 0 {
			panic(gcHelperFailuref("invalid overlapping GC/host dispatch index %#x", importIdx))
		}
		helper, safepoint := shared.DecodeGCDispatch(importIdx &^ gcStructDispatchBit)
		in.dispatchGCHelperParked(ctrl, helper, safepoint, args, results)
		return
	}
	if !foreignHostControlForImage {
		panic(missingHostFunc{importIdx: importIdx})
	}
	scope := invocation.scope
	if scope == nil {
		scope = &in.ensurePluginState().hostScope
	}
	if importIdx&hostFuncRefDispatchBit != 0 {
		if !referenceStoreForImage {
			panic(missingHostFunc{importIdx: importIdx})
		}
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
		if !referenceSignaturesBuilt {
			var signature *DefinedTypeDescriptor
			if exact != nil {
				signature = &exact.types[exact.sig.TypeIndex]
			}
			dispatchSyncHostScalar(in, scope, ctrl, fn, signature, args, results, invocation)
			return
		}
		var signature *DefinedTypeDescriptor
		var exactTypes []DefinedTypeDescriptor
		var exactTypesPtr *[]DefinedTypeDescriptor
		if exact != nil {
			sig = exact.sig
			signature = &exact.types[sig.TypeIndex]
			exactTypes = exact.types
			exactTypesPtr = &exact.types
		}
		if fn.scalarKind >= syncHostTypedI32 && !hasReferenceValType(sig.Params) && !hasReferenceValType(sig.Results) {
			dispatchSyncHostScalar(in, scope, ctrl, fn, fn.exact, args, results, invocation)
		} else {
			dispatchSyncHostReference(in, scope, ctrl, importIdx, fn, sig, signature, exactTypes, exactTypesPtr, args, results, invocation)
		}
		return
	}
	if int(importIdx) >= len(in.syncHosts) || !in.syncHosts[importIdx].callable() {
		panic(missingHostFunc{importIdx: importIdx})
	}
	binding := &in.syncHosts[importIdx]
	if !referenceSignaturesBuilt || binding.scalarKind != syncHostNonScalar {
		dispatchSyncHostScalar(in, scope, ctrl, binding, binding.exact, args, results, invocation)
	} else {
		dispatchSyncHostReference(in, scope, ctrl, importIdx, binding, in.c.importFuncSigs[importIdx], binding.exact, in.c.Types, &in.c.Types, args, results, invocation)
	}
}

func (in *Instance) translateHostReferenceArgs(values []uint64, types []ValType, exact []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor, gcTemps *gcHostTempTokens) error {
	return in.translateHostReferenceArgsMode(values, types, exact, exactTypes, gcTemps, nil)
}

func (in *Instance) translateHostReferenceArgsMode(values []uint64, types []ValType, exact []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor, gcTemps *gcHostTempTokens, caller *instanceHostModule) error {
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
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapExtern && required.Ref.Heap.Abstract != AbstractHeapNoExtern {
				return fmt.Errorf("missing exact externref type for argument %d", i)
			}
			if values[slot] == 0 && !required.Ref.Nullable || values[slot] != 0 && required.Ref.Heap.Abstract == AbstractHeapNoExtern {
				return fmt.Errorf("externref argument %d does not match its exact structural type", i)
			}
			if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				return fmt.Errorf("invalid externref token for argument %d", i)
			}
		case ValAnyRef, ValI31Ref, ValExnRef:
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
			if !collectorObjectTokensBuilt {
				return fmt.Errorf("invalid GC reference argument %d", i)
			}
			var token uint64
			var err error
			if caller != nil {
				token, err = in.refStore.issueCallbackGCRef(*caller, ref, required)
			} else {
				token, err = in.refStore.issueGCRef(in, ref, required)
			}
			if err != nil {
				return fmt.Errorf("GC reference argument %d: %w", i, err)
			}
			gcTemps.add(token)
			values[slot] = token
		}
		slot++
	}
	return nil
}

func (in *Instance) translateHostReferenceResults(activation gcHostActivationToken, values []uint64, types []ValType, exact []ValueTypeDescriptor, exactTypes []DefinedTypeDescriptor) error {
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
			required, ok := exactReferenceType(exact, i, typ)
			if !ok || required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapExtern && required.Ref.Heap.Abstract != AbstractHeapNoExtern {
				return fmt.Errorf("missing exact externref type for result %d", i)
			}
			if values[slot] == 0 && !required.Ref.Nullable || values[slot] != 0 && required.Ref.Heap.Abstract == AbstractHeapNoExtern {
				return fmt.Errorf("externref result %d does not match its exact structural type", i)
			}
			if values[slot] != 0 && !in.validExternrefToken(values[slot]) {
				return fmt.Errorf("invalid externref token for result %d", i)
			}
		case ValAnyRef, ValI31Ref, ValExnRef:
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
			if !collectorObjectTokensBuilt {
				return fmt.Errorf("GC reference result %d: invalid GC host result token", i)
			}
			resolved, err := in.refStore.stageGCHostResult(in, activation, bits, required)
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
	// Native release may reconcile retained table roots and run physical-release
	// callbacks. Journal it after the admitted body so a later cleanup panic does
	// not replace an arbitrary host panic, while still completing the release.
	var cleanup resourceCleanup
	cleanup.run(func() {
		// ARM64 benefits from recovering in the unprepared driver itself. Keep
		// AMD64's existing boundary: paired measurements regressed its view calls.
		if goruntime.GOARCH == "arm64" {
			err = in.callNativeSyncUnpreparedAdmitted(entry, activeTrap, waitParent, locked.local)
		} else {
			err = in.callNativeSyncAdmitted(entry, activeTrap, waitParent, nil, nil, nil, locked.local)
		}
	})
	cleanup.run(func() { in.unlockNativeEntry(locked) })
	cleanup.finish()
	return err
}

// callNativeSyncAdmitted drives a synchronous host-call activation after native
// admission. A reserved session supplies its prebound call and activation.
func (in *Instance) callNativeSyncAdmitted(entry uintptr, activeTrap []byte, waitParent context.Context, prepared *runtime.PreparedHostScalarCall, preparedFixed runtime.FixedScalarHostCall, preparedActivation *hostLoopActivation, heldNativeMu *sync.Mutex) (err error) {
	if goruntime.GOARCH == "arm64" && prepared == nil {
		return in.callNativeSyncUnpreparedAdmitted(entry, activeTrap, waitParent, heldNativeMu)
	}
	defer func() { err = in.decorateTrap(err) }()
	returned := false
	defer recoverNativeSyncPanicCompletion(&err, &returned)
	if hostFunctionImportsBuilt && prepared != nil {
		if preparedActivation == nil {
			err = fmt.Errorf("wago: prepared host call has no activation")
			returned = true
			return err
		}
		err = in.callPreparedHostSync(prepared, preparedFixed, preparedActivation)
		goruntime.KeepAlive(in)
		goruntime.KeepAlive(in.c)
		returned = true
		return err
	}
	err = in.callNativeSyncUnpreparedAdmitted(entry, activeTrap, waitParent, heldNativeMu)
	returned = true
	return err
}

// recoverNativeSyncPanic must be deferred directly by the native-call entry so
// recover observes host panics on that goroutine's active stack.
func recoverNativeSyncPanic(err *error) {
	if r := recover(); r != nil {
		setNativeSyncPanicError(r, err)
	}
}

// A legacy panic(nil) makes recover return nil. The explicit normal-completion
// sentinel keeps that real panic distinct without changing its value.
func recoverNativeSyncPanicCompletion(err *error, returned *bool) {
	r := recover()
	if !*returned || r != nil {
		setNativeSyncPanicError(r, err)
	}
}

// This must also be deferred directly: recover cannot be delegated to a helper.
// Recovery precedes trap annotation, matching the separate general defers.
func (in *Instance) recoverBoundedNativeSyncPanic(err *error) {
	if r := recover(); r != nil {
		setNativeSyncPanicError(r, err)
	}
	if *err != nil {
		*err = in.decorateTrap(*err)
	}
}

func setNativeSyncPanicError(r any, err *error) {
	switch trap := r.(type) {
	case HostTrap:
		if trap.Err == nil {
			*err = fmt.Errorf("wago: host trapped without an error")
		} else {
			*err = trap.Err
		}
		return
	case *HostTrap:
		if trap == nil || trap.Err == nil {
			*err = fmt.Errorf("wago: host trapped without an error")
		} else {
			*err = trap.Err
		}
		return
	}
	if ex, ok := r.(HostExit); ok {
		*err = &ExitError{Code: ex.Code}
		return
	}
	if ex, ok := r.(*HostExit); ok && ex != nil {
		*err = &ExitError{Code: ex.Code}
		return
	}
	if missing, ok := r.(missingHostFunc); ok {
		*err = fmt.Errorf("missing host function for import index %d", missing.importIdx)
		return
	}
	if invalid, ok := r.(invalidHostReference); ok {
		*err = invalid.err
		return
	}
	if instruction, ok := r.(instructionTrap); ok {
		*err = instruction.err
		return
	}
	if nativeCollectorBuilt && setGCHelperPanicError(r, err) {
		return
	}
	if atomicWaitRuntimeBuilt && setAtomicWaitHelperPanicError(r, err) {
		return
	}
	panic(r)
}

// callPreparedHostSyncAdmitted keeps the ordinary resolved-host path out of the
// generic sync entry's unused unprepared-call dispatch and argument setup.
func (in *Instance) callPreparedHostSyncAdmitted(prepared *runtime.PreparedHostScalarCall, fixed runtime.FixedScalarHostCall, activation *hostLoopActivation) (err error) {
	defer func() { err = in.decorateTrap(err) }()
	returned := false
	defer recoverNativeSyncPanicCompletion(&err, &returned)
	err = in.callPreparedHostSync(prepared, fixed, activation)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	returned = true
	return err
}

func (in *Instance) callPreparedHostSync(prepared *runtime.PreparedHostScalarCall, fixed runtime.FixedScalarHostCall, activation *hostLoopActivation) (err error) {
	if codeProfileEnabled && in.boundaryProfile() != nil {
		span, restore := activation.beginProfileActivation()
		defer restore()
		completed := false
		defer finishProfileBoundary(span, &err, &completed)
		err = prepared.Call(activation.dispatch, profileScalarFallback)
		completed = true
		return err
	}
	if preparedHostFixedEnabled && fixed != nil && in.boundedFixedSharedHostPortal() && in.syncHosts[0].scalarKind == syncHostTypedI32 {
		portal := dispatchSingleTypedI32FixedPortalContext
		if os.Getenv("WAGO_FIXED_HOST_RESUME_PAYLOAD") != "0" {
			portal = dispatchSingleTypedI32FixedResumeContext
		}
		if in.c.fixedSharedHostInteger && integerNumericHostEnabled && (goruntime.GOARCH == "amd64" || armIntegerNumericEnabled) && os.Getenv("WAGO_FIXED_SHARED_INTEGER_CONTEXT") != "0" {
			return prepared.CallFixedIntegerContext(runtimebridge.GrantHostScalarCall(), unsafe.Pointer(activation), portal, fixed)
		}
		return prepared.CallFixedContext(runtimebridge.GrantHostScalarCall(), unsafe.Pointer(activation), portal, fixed)
	}
	if preparedHostFixedEnabled {
		if fixed == nil {
			fixed = activation.dispatchSingleTypedScalarFixedPortal
		}
		return prepared.CallFixed(activation.dispatch, activation.dispatchSingleTypedScalarPortal, fixed)
	}
	return prepared.Call(activation.dispatch, activation.dispatchSingleTypedScalarPortal)
}

// callCachedBoundedHostView is reached only after bounded numeric admission
// has excluded references, helpers, native imports and shared execution. It
// retains normal native ownership and host-context inheritance; callbacks can
// still publish resources, reenter through Caller, grow their Go stack or panic.
func (in *Instance) callCachedBoundedHostView(entry uintptr) (err error) {
	locked, reuse, err := in.beginCachedBoundedViewEntry()
	if err != nil {
		return err
	}
	defer in.unlockNativeEntry(locked)
	defer in.recoverBoundedNativeSyncPanic(&err)
	if hasHostInvocationBindings() {
		restore := bindHostInvocationParent(in, nil)
		defer restore()
	}
	state := in.ensurePluginState()
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	binding := &in.syncHosts[0]
	rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
	activation := boundedViewHostActivation{
		boundedTypedHostActivation: boundedTypedHostActivation{
			root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: locked.local,
		},
	}
	fixed := runtime.FixedHostContextCallView(boundedHostDispatchHostCallView)
	fallback := runtime.FixedHostCallView(activation.dispatchHostCallView)
	_, caller := binding.fn.(CallerHostCallFunc)
	if caller {
		fixed = boundedHostDispatchCallerView
		fallback = activation.dispatchCallerView
	}
	// AMD64 Caller round trips retain the measured faster method adapter.
	if prepared := in.eng.PreparedScalarHost(); prepared != nil {
		if locked.privateContext() {
			fixed = detachedNumericDispatchHostCall
			if caller {
				fixed = detachedNumericDispatchCaller
			}
		}
		if prepared.IntegerGuestContext() {
			err = prepared.CallIntegerView(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
		} else {
			err = prepared.CallView(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
		}
	} else if caller && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64") {
		err = in.eng.CallWithHostBaseFixedViewBoundedLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, fallback)
	} else {
		err = in.eng.CallWithHostBaseFixedViewBoundedContextLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation), fixed, fallback)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}

// callCachedBoundedTypedHost has the same warm admission and ownership contract
// as the view driver, with a compact typed scalar callback frame on AMD64.
func (in *Instance) callCachedBoundedTypedHost(entry uintptr) error {
	return in.callCachedBoundedTypedHostContext(entry, nil)
}

func (in *Instance) callCachedBoundedTypedHostContext(entry uintptr, waitParent context.Context) (err error) {
	locked, reuse, err := in.beginCachedBoundedViewEntry()
	if err != nil {
		return err
	}
	defer in.unlockNativeEntry(locked)
	defer in.recoverBoundedNativeSyncPanic(&err)
	if hostInvocationBindingsForImage && (waitParent != nil || hasHostInvocationBindings()) {
		restore := bindHostInvocationParent(in, waitParent)
		defer restore()
	}
	state := in.ensurePluginState()
	if !reuse {
		in.jm.SetStackFence(in.eng.StackLimit())
		in.jm.SetCustomCtx(offHeapSlicePtr(in.ctrl))
		state.boundedViewVersion = state.nativeContextVersion.Load()
		state.boundedViewMemBase = in.jm.LinMemBase()
	}
	activation := boundedTypedHostActivation{
		root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: locked.local,
	}
	binding := &in.syncHosts[0]
	rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
	fixed := runtime.FixedScalarHostContextCall(boundedTypedHostDispatchI32)
	fallback := runtime.FixedScalarHostCall(activation.dispatchI32)
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
			err = prepared.CallInteger(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
		} else {
			err = prepared.Call(entry, locked.memoryBase(in), unsafe.Pointer(&activation), fixed)
		}
	} else {
		err = in.eng.CallWithHostBaseScalarBoundedContextLive(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results, in.ctrl, rawSlots, unsafe.Pointer(&activation), fixed, fallback)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return err
}

func (in *Instance) callNativeSyncUnpreparedAdmitted(entry uintptr, activeTrap []byte, waitParent context.Context, heldNativeMu *sync.Mutex) (err error) {
	returned := false
	if goruntime.GOARCH == "arm64" {
		defer func() { err = in.decorateTrap(err) }()
		defer recoverNativeSyncPanicCompletion(&err, &returned)
	}
	// Avoid no-op context cleanup on ARM64; paired AMD64 measurements
	// favor its original unconditional setup.
	if hostInvocationBindingsForImage && (goruntime.GOARCH != "arm64" || waitParent != nil || hasHostInvocationBindings()) {
		restoreInvocationContext := bindHostInvocationParent(in, waitParent)
		defer restoreInvocationContext()
	}
	if atomicBlockingWaitRuntimeBuilt && (goruntime.GOARCH != "arm64" || in.needsAtomicWaitContext()) {
		stopWaitContext := in.publishAtomicWaitContext(waitParent)
		defer stopWaitContext()
	}
	// Native CallWithHostBase entries validate and bind activeTrap themselves.
	// Avoid repeating that binding on either supported native architecture.
	if goruntime.GOARCH != "amd64" && goruntime.GOARCH != "arm64" {
		if err := in.jm.RebindTrapCell(activeTrap); err != nil {
			return err
		}
	}
	in.jm.SetStackFence(in.eng.StackLimit())
	if len(in.ctrl) > 0 && len(in.ctrl) >= runtime.HostCtrlFrameBytes {
		in.jm.SetCustomCtx(uintptr(unsafe.Pointer(&in.ctrl[0])))
	}
	// Resolve stable root context lazily in this activation, not on the instance.
	// Each nested native entry receives a separate snapshot and fresh callbacks.
	activation := hostLoopActivation{
		root:                        in,
		ctrl:                        offHeapSlicePtr(in.ctrl),
		state:                       in.ensurePluginState(),
		entryNativeMu:               heldNativeMu,
		parkedNativeContextReusable: in.parkedHostControlReusable(),
	}
	if codeProfileEnabled && (in.boundaryProfile() != nil || activeHostInvocationContext(in).profileSession() != nil) {
		span, restore := activation.beginProfileActivation()
		defer restore()
		completed := false
		defer finishProfileBoundary(span, &err, &completed)
		err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch)
		completed = true
	} else if hostFunctionImportsBuilt && in.hasSingleDirectTypedScalarHost() {
		if in.boundedHostSegments() || in.boundedFixedSharedHostPortal() {
			rawSlots, _ := in.syncHosts[0].typedScalarSlots()
			fixed := runtime.FixedScalarHostCall(activation.dispatchSingleTypedI32FixedPortal)
			if in.syncHosts[0].scalarKind == syncHostTypedI32x2 {
				fixed = activation.dispatchSingleTypedI32x2FixedPortal
			}
			err = in.eng.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, fixed)
		} else {
			err = in.eng.CallWithHostBaseScalar(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchSingleTypedScalarPortal)
		}
	} else if hostFunctionImportsBuilt && in.hasSingleExpandedTypedScalarHost() {
		rawSlots, ok := in.syncHosts[0].typedScalarSlots()
		if !ok {
			panic("wago: invalid fixed scalar host signature")
		}
		fixed := runtime.FixedScalarHostCall(activation.dispatchSingleTypedScalarFixedPortal)
		switch in.syncHosts[0].scalarKind {
		case syncHostTypedNone:
			fixed = activation.dispatchSingleTypedNoneFixedPortal
		case syncHostTypedI32V:
			fixed = activation.dispatchSingleTypedI32VoidFixedPortal
		case syncHostTypedI32x2V:
			fixed = activation.dispatchSingleTypedI32x2VoidFixedPortal
		case syncHostTypedI32R2:
			fixed = activation.dispatchSingleTypedI32PairFixedPortal
		case syncHostTypedI32x2R2:
			fixed = activation.dispatchSingleTypedI32x2PairFixedPortal
		case syncHostTypedI64:
			fixed = activation.dispatchSingleTypedI64FixedPortal
		case syncHostTypedI64x2:
			fixed = activation.dispatchSingleTypedI64x2FixedPortal
		case syncHostTypedF32:
			fixed = activation.dispatchSingleTypedF32FixedPortal
		case syncHostTypedF32x2:
			fixed = activation.dispatchSingleTypedF32x2FixedPortal
		case syncHostTypedF64:
			fixed = activation.dispatchSingleTypedF64FixedPortal
		case syncHostTypedF64x2:
			fixed = activation.dispatchSingleTypedF64x2FixedPortal
		}
		if in.boundedHostSegments() {
			err = in.eng.CallWithHostBaseScalarBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, fixed)
		} else {
			err = in.eng.CallWithHostBaseScalarFixed(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, activation.dispatch, activation.dispatchTypedScalarExpandedPortal, fixed)
		}
	} else if hostFunctionImportsBuilt && in.hasBoundedMultiHostView() {
		err = in.eng.CallWithHostBaseMultiViewBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, in.ensurePluginState().multiHost.shapes, activation.dispatchBoundedMultiHostView)
	} else if hostFunctionImportsBuilt && in.hasExpandedTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalarExpanded(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchTypedScalarExpandedPortal)
	} else if hostFunctionImportsBuilt && in.hasDirectTypedScalarHost() {
		err = in.eng.CallWithHostBaseScalar(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchTypedScalarPortal)
	} else if hostFunctionImportsBuilt && in.hasSingleHostCallFixedViewPortal() {
		binding := &in.syncHosts[0]
		rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
		if in.boundedHostSegments() {
			err = in.eng.CallWithHostBaseFixedViewBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, activation.dispatchSingleHostCallFixedView)
		} else {
			err = in.eng.CallWithHostBaseFixedView(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, activation.dispatch, activation.dispatchSingleHostCallFixedView)
		}
	} else if hostFunctionImportsBuilt && in.hasSingleHostCallPortal() {
		err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatchSingleHostCall)
	} else if hostFunctionImportsBuilt && in.hasSingleHostCallViewPortal() {
		err = in.eng.CallWithHostBaseView(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch, activation.dispatchSingleHostCallView)
	} else if hostFunctionImportsBuilt && in.boundedHostSegments() && in.hasBoundedCallerHostView() {
		binding := &in.syncHosts[0]
		rawSlots := uint32(len(binding.sig.Params)) | uint32(len(binding.sig.Results))<<16
		err = in.eng.CallWithHostBaseFixedViewBounded(runtimebridge.GrantHostScalarCall(), entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, rawSlots, activation.dispatchBoundedHostView)
	} else {
		err = in.eng.CallWithHostBase(entry, in.serArgs, in.jm.LinMemBase(), activeTrap, in.results, in.ctrl, activation.dispatch)
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	returned = true
	return err
}
