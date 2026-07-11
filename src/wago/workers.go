package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// WorkerID identifies one plugin worker within a Runtime. IDs are nonzero,
// monotonically allocated, and never reused.
type WorkerID uint64

const (
	// DefaultWorkerQueueCapacity is used when WorkerOptions.QueueCapacity is zero.
	DefaultWorkerQueueCapacity uint32 = 64
	// DefaultWorkerMaxPayloadBytes is used when WorkerOptions.MaxPayloadBytes is zero.
	DefaultWorkerMaxPayloadBytes uint32 = 64 << 10
	// DefaultWorkerMaxQueueBytes bounds total queued payload bytes by default.
	DefaultWorkerMaxQueueBytes uint32 = 1 << 20
	// MaxWorkerQueueCapacity bounds the preallocated queue header array.
	MaxWorkerQueueCapacity uint32 = 1 << 16
	// MaxWorkerPayloadBytes bounds one copied payload even under explicit options.
	MaxWorkerPayloadBytes uint32 = 16 << 20
	// MaxWorkerQueueBytes bounds aggregate copied payload bytes per worker.
	MaxWorkerQueueBytes uint32 = 64 << 20
	// DefaultMaxLiveWorkers bounds worker instances, goroutines, and foreign stacks.
	DefaultMaxLiveWorkers uint32 = 64
	// DefaultMaxWorkerQueueBytes bounds aggregate configured queue bytes runtime-wide.
	DefaultMaxWorkerQueueBytes uint64 = 64 << 20
)

type workerError string

func (e workerError) Error() string { return string(e) }

const (
	ErrWorkersInactive      = workerError("worker service is not active")
	ErrInvalidWorkerOptions = workerError("invalid worker options")
	ErrInvalidWorkerCaller  = workerError("worker operation requires a current plugin host caller")
	ErrWorkerImportLifetime = workerError("worker cannot safely inherit a borrowed import")
	ErrWorkerQuotaExceeded  = workerError("worker runtime quota exceeded")
	ErrWorkerNotFound       = workerError("worker not found")
	ErrWorkerStopping       = workerError("worker is stopping")
	ErrWorkerQueueFull      = workerError("worker queue is full")
	ErrWorkerDispatchActive = workerError("worker message dispatch is already active")
	ErrPayloadTooLarge      = workerError("worker payload is too large")
	ErrWorkerIDExhausted    = workerError("worker ID space exhausted")
	ErrInvalidWorkerLink    = workerError("invalid worker link")
	ErrWorkerKilled         = workerError("worker killed")
	ErrWorkerParentClosed   = workerError("worker parent closed")
	ErrWorkerRuntimeClosed  = workerError("worker runtime closed")
)

// WorkerOptions bounds one worker's message count, individual payload size, and
// aggregate queued payload bytes. Zero fields select bounded defaults.
type WorkerOptions struct {
	QueueCapacity   uint32
	MaxPayloadBytes uint32
	MaxQueueBytes   uint32
}

// WorkerLimits bounds aggregate worker resources for one Runtime. Zero fields
// select bounded defaults.
type WorkerLimits struct {
	MaxLiveWorkers uint32
	MaxQueueBytes  uint64
}

// MessageContext is delivered to plugin OnMessage handlers by DispatchNext.
// Caller is the suspended worker's HostModule and is valid only during the
// handler; it provides memory access and can be passed to Spawn or Link without
// exposing a re-entrant *Instance call surface. Payload is the copy made by Send
// and may be retained.
type MessageContext struct {
	WorkerID WorkerID
	Tag      uint64
	Payload  []byte
	Caller   HostModule
}

// WorkerExitKind classifies a worker's terminal outcome without imposing actor
// signal, monitoring, restart, or supervision semantics.
type WorkerExitKind uint8

const (
	WorkerReturned WorkerExitKind = iota + 1
	WorkerFailed
	WorkerKilled
)

// WorkerExitContext is emitted exactly once to the owning plugin after a worker
// has terminated and its instance has been disposed.
type WorkerExitContext struct {
	WorkerID WorkerID
	Kind     WorkerExitKind
	Err      error
}

// Workers is an extension-scoped handle to the runtime's neutral worker
// primitives. Registry.Workers returns a pending handle during Register; it is
// activated only if Runtime.Use commits successfully.
type Workers struct {
	mu        sync.RWMutex
	runtime   *workerRuntime
	onMessage []func(*MessageContext) error
	onExit    []func(*WorkerExitContext)
}

// NewWorkers requests a pending worker service through the generic plugin
// activation path. The concrete worker kernel is linked only when a plugin calls
// this constructor; the runtime loader itself has no worker-specific branch.
func NewWorkers(reg *Registry) (*Workers, error) {
	if reg == nil {
		return nil, fmt.Errorf("wago: nil plugin registry")
	}
	if err := reg.authorize(PluginManagedInstances); err != nil {
		return nil, err
	}
	service := &Workers{}
	reg.activate = append(reg.activate, func(rt *Runtime) {
		if rt.workers == nil {
			rt.workers = newWorkerRuntime(rt)
			rt.hooks.internalClose = append(rt.hooks.internalClose, rt.workers.close)
			rt.hooks.internalBeforeClose = append(rt.hooks.internalBeforeClose, rt.workers.parentClosing)
		}
		rt.workersActive.Store(true)
		service.activate(rt.workers)
	})
	return service, nil
}

// OnMessage registers handlers invoked by DispatchNext on the worker goroutine.
// A returned error fails the worker. Registrations are snapshotted by Spawn, so
// extensions should normally add them during Register. Handlers must not call
// Runtime.Close synchronously from the worker goroutine.
func (w *Workers) OnMessage(fns ...func(*MessageContext) error) {
	w.mu.Lock()
	w.onMessage = append(w.onMessage, fns...)
	w.mu.Unlock()
}

// OnExit registers terminal observers for workers owned by this extension.
// Registrations are snapshotted by Spawn. Runtime shutdown waits for exit
// handlers; they therefore must not re-enter Runtime.Close. Observer panics are
// isolated so later observers and shutdown still run, then reported by
// Runtime.Close as an aggregated error.
func (w *Workers) OnExit(fns ...func(*WorkerExitContext)) {
	w.mu.Lock()
	w.onExit = append(w.onExit, fns...)
	w.mu.Unlock()
}

// Spawn creates a fresh instance of the caller's compiled module and starts its
// table callback on a dedicated goroutine. The callback entry must be non-null
// and have the exact signature () -> (). Borrowed memory, table, global-object,
// and cross-instance function imports are rejected until core can retain their
// owners safely for an unlinked worker's lifetime.
func (w *Workers) Spawn(caller HostModule, tableIndex uint32, opts WorkerOptions) (WorkerID, error) {
	rt, err := w.activeRuntime()
	if err != nil {
		return 0, err
	}
	parent, err := workerCaller(caller)
	if err != nil {
		return 0, err
	}
	return rt.spawn(w, parent, tableIndex, opts)
}

// Send nonblockingly enqueues a copied payload for a worker owned by this
// extension.
func (w *Workers) Send(id WorkerID, tag uint64, payload []byte) error {
	rt, err := w.activeRuntime()
	if err != nil {
		return err
	}
	return rt.send(w, id, tag, payload)
}

// Current returns the ID of the current worker for a plugin host-call caller.
// A direct, non-worker instance returns ErrWorkerNotFound.
func (w *Workers) Current(caller HostModule) (WorkerID, error) {
	rt, err := w.activeRuntime()
	if err != nil {
		return 0, err
	}
	in, err := workerCaller(caller)
	if err != nil {
		return 0, err
	}
	return rt.current(w, in)
}

// DispatchNext waits for and dispatches the next message for the current worker.
// Plugins call it synchronously from one of their host imports; OnMessage then
// runs on this same worker goroutine before DispatchNext returns to guest code.
// Retained or asynchronously continued callers are rejected once that host call
// returns.
func (w *Workers) DispatchNext(caller HostModule) error {
	rt, err := w.activeRuntime()
	if err != nil {
		return err
	}
	h, ok := caller.(instanceHostModule)
	if !ok || !h.valid() {
		return ErrInvalidWorkerCaller
	}
	return rt.dispatchNext(w, h)
}

// Link establishes a secure lifetime link from caller to a child created by
// that exact instance. It does not implement signals or supervision policy.
func (w *Workers) Link(caller HostModule, child WorkerID) error {
	rt, err := w.activeRuntime()
	if err != nil {
		return err
	}
	parent, err := workerCaller(caller)
	if err != nil {
		return err
	}
	return rt.link(w, parent, child)
}

// Kill requests cooperative termination. It is idempotent while the worker is
// stopping; an already-running native callback cannot be preempted until engine
// interruption support is implemented.
func (w *Workers) Kill(id WorkerID) error {
	rt, err := w.activeRuntime()
	if err != nil {
		return err
	}
	return rt.kill(w, id, ErrWorkerKilled)
}

func (w *Workers) activeRuntime() (*workerRuntime, error) {
	if w == nil {
		return nil, ErrWorkersInactive
	}
	w.mu.RLock()
	rt := w.runtime
	w.mu.RUnlock()
	if rt == nil {
		return nil, ErrWorkersInactive
	}
	return rt, nil
}

func (w *Workers) activate(rt *workerRuntime) {
	w.mu.Lock()
	w.runtime = rt
	w.mu.Unlock()
}

func (w *Workers) messageHandlers() []func(*MessageContext) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]func(*MessageContext) error(nil), w.onMessage...)
}

func (w *Workers) exitHandlers() []func(*WorkerExitContext) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]func(*WorkerExitContext){}, w.onExit...)
}

func workerCaller(caller HostModule) (*Instance, error) {
	h, ok := caller.(instanceHostModule)
	if !ok || !h.valid() {
		return nil, ErrInvalidWorkerCaller
	}
	return h.in, nil
}

type workerMessage struct {
	tag     uint64
	payload []byte
}

type worker struct {
	id              WorkerID
	service         *Workers
	manager         *workerRuntime
	creator         *Instance
	instance        *Instance
	tableIndex      uint32
	messageHandlers []func(*MessageContext) error
	exitHandlers    []func(*WorkerExitContext)
	messageScope    hostCallScope
	hostWaiter      hostCallWaiter

	mu              sync.Mutex
	queue           []workerMessage
	head            int
	length          int
	maxPayloadBytes uint32
	maxQueueBytes   uint32
	queueBytes      uint32
	dispatching     bool
	started         bool
	stopping        bool
	terminal        bool
	stopErr         error
	failed          error
	wake            chan struct{}
	disposed        chan struct{}
	done            chan struct{}
}

type workerRuntime struct {
	rt *Runtime

	mu         sync.Mutex
	closed     bool
	next       WorkerID
	workers    map[WorkerID]*worker
	byInstance map[*Instance]*worker
	linked     map[*Instance]map[WorkerID]*worker
	limits     WorkerLimits
	live       uint32
	queueBytes uint64
	exitPanics []error
	launch     func(*worker, uintptr)

	dispatchMu   sync.Mutex
	dispatchCode *Compiled
	dispatchBase uintptr
}

func newWorkerRuntime(rt *Runtime) *workerRuntime {
	return &workerRuntime{
		rt:         rt,
		next:       1,
		workers:    map[WorkerID]*worker{},
		byInstance: map[*Instance]*worker{},
		linked:     map[*Instance]map[WorkerID]*worker{},
		limits:     rt.workerLimits,
		launch:     func(wr *worker, dispatchBase uintptr) { go wr.run(dispatchBase) },
	}
}

func normalizeWorkerLimits(limits WorkerLimits) WorkerLimits {
	if limits.MaxLiveWorkers == 0 {
		limits.MaxLiveWorkers = DefaultMaxLiveWorkers
	}
	if limits.MaxQueueBytes == 0 {
		limits.MaxQueueBytes = DefaultMaxWorkerQueueBytes
	}
	return limits
}

func normalizeWorkerOptions(opts WorkerOptions) (WorkerOptions, error) {
	if opts.QueueCapacity == 0 {
		opts.QueueCapacity = DefaultWorkerQueueCapacity
	}
	if opts.MaxPayloadBytes == 0 {
		opts.MaxPayloadBytes = DefaultWorkerMaxPayloadBytes
	}
	if opts.MaxQueueBytes == 0 {
		opts.MaxQueueBytes = DefaultWorkerMaxQueueBytes
	}
	if opts.QueueCapacity > MaxWorkerQueueCapacity {
		return WorkerOptions{}, fmt.Errorf("worker queue capacity %d exceeds maximum %d: %w", opts.QueueCapacity, MaxWorkerQueueCapacity, ErrInvalidWorkerOptions)
	}
	if opts.MaxPayloadBytes > MaxWorkerPayloadBytes {
		return WorkerOptions{}, fmt.Errorf("worker maximum payload %d exceeds maximum %d: %w", opts.MaxPayloadBytes, MaxWorkerPayloadBytes, ErrInvalidWorkerOptions)
	}
	if opts.MaxQueueBytes > MaxWorkerQueueBytes {
		return WorkerOptions{}, fmt.Errorf("worker maximum queued bytes %d exceeds maximum %d: %w", opts.MaxQueueBytes, MaxWorkerQueueBytes, ErrInvalidWorkerOptions)
	}
	if opts.MaxQueueBytes < opts.MaxPayloadBytes {
		return WorkerOptions{}, fmt.Errorf("worker maximum queued bytes %d is less than maximum payload %d: %w", opts.MaxQueueBytes, opts.MaxPayloadBytes, ErrInvalidWorkerOptions)
	}
	return opts, nil
}

func workerCallerClosed(in *Instance) bool {
	if in == nil {
		return true
	}
	in.lifeMu.Lock()
	closed := in.closed
	in.lifeMu.Unlock()
	return closed
}

func (rt *workerRuntime) spawn(service *Workers, parent *Instance, tableIndex uint32, opts WorkerOptions) (WorkerID, error) {
	if parent == nil || parent.rt != rt.rt || workerCallerClosed(parent) {
		return 0, ErrInvalidWorkerCaller
	}
	opts, err := normalizeWorkerOptions(opts)
	if err != nil {
		return 0, err
	}
	imports, err := workerImports(parent)
	if err != nil {
		return 0, err
	}
	if err := rt.reserve(parent, opts.MaxQueueBytes); err != nil {
		return 0, err
	}
	reserved := true
	defer func() {
		if reserved {
			rt.release(opts.MaxQueueBytes)
		}
	}()

	dispatchBase, err := rt.ensureDispatcher()
	if err != nil {
		return 0, err
	}
	mod := rt.rt.buildModule(parent.c)
	var gc GCConfig
	state := parent.pluginState.Load()
	hasGC := state != nil && state.gcConfig != nil
	if hasGC {
		gc = *state.gcConfig
	}
	child, err := rt.rt.instantiateWithHooksOrigin(mod, imports, gc, hasGC, InstantiateWorker)
	if err != nil {
		return 0, err
	}
	if err := validateWorkerCallback(child, tableIndex); err != nil {
		_ = child.Close()
		return 0, err
	}

	wr := &worker{
		service: service, manager: rt, creator: parent, instance: child, tableIndex: tableIndex,
		messageHandlers: service.messageHandlers(), exitHandlers: service.exitHandlers(),
		queue: make([]workerMessage, int(opts.QueueCapacity)), maxPayloadBytes: opts.MaxPayloadBytes, maxQueueBytes: opts.MaxQueueBytes,
		wake: make(chan struct{}, 1), disposed: make(chan struct{}), done: make(chan struct{}),
	}

	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		_ = child.Close()
		return 0, ErrWorkerRuntimeClosed
	}
	if workerCallerClosed(parent) {
		rt.mu.Unlock()
		_ = child.Close()
		return 0, ErrInvalidWorkerCaller
	}
	if rt.next == 0 {
		rt.mu.Unlock()
		_ = child.Close()
		return 0, ErrWorkerIDExhausted
	}
	wr.id = rt.next
	if rt.next == WorkerID(^uint64(0)) {
		rt.next = 0
	} else {
		rt.next++
	}
	rt.workers[wr.id] = wr
	rt.byInstance[child] = wr
	reserved = false // finish now owns the reservation.
	rt.mu.Unlock()

	rt.launch(wr, dispatchBase)
	return wr.id, nil
}

// workerImports copies only imports declared by the module and rejects borrowed
// runtime objects whose owner may close or unmap them while an unlinked worker
// remains alive. Host functions and by-value globals have no such hidden native
// lifetime; all other imported runtime resources require an explicit future
// retention mechanism before workers may inherit them.
func workerImports(parent *Instance) (Imports, error) {
	imports := make(Imports, len(parent.c.Imports)+len(parent.c.GlobalImports)+2)
	copyImport := func(key string) error {
		v, ok := parent.imports[key]
		if !ok {
			return fmt.Errorf("worker import %q is missing", key)
		}
		switch x := v.(type) {
		case HostFunc:
			imports[key] = x
		case GlobalImport:
			if x.Global != nil {
				return fmt.Errorf("worker import %q uses a borrowed global: %w", key, ErrWorkerImportLifetime)
			}
			imports[key] = x
		default:
			return fmt.Errorf("worker import %q has type %T: %w", key, v, ErrWorkerImportLifetime)
		}
		return nil
	}
	for _, key := range parent.c.Imports {
		if err := copyImport(key); err != nil {
			return nil, err
		}
	}
	for _, imp := range parent.c.GlobalImports {
		if err := copyImport(imp.Module + "." + imp.Name); err != nil {
			return nil, err
		}
	}
	if parent.c.memoryImport != "" {
		if err := copyImport(parent.c.memoryImport); err != nil {
			return nil, err
		}
	}
	if parent.c.tableImport != "" {
		if err := copyImport(parent.c.tableImport); err != nil {
			return nil, err
		}
	}
	return imports, nil
}

func (rt *workerRuntime) reserve(parent *Instance, queueBytes uint32) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrWorkerRuntimeClosed
	}
	if workerCallerClosed(parent) {
		return ErrInvalidWorkerCaller
	}
	if rt.live >= rt.limits.MaxLiveWorkers || rt.queueBytes > rt.limits.MaxQueueBytes || uint64(queueBytes) > rt.limits.MaxQueueBytes-rt.queueBytes {
		return ErrWorkerQuotaExceeded
	}
	rt.live++
	rt.queueBytes += uint64(queueBytes)
	return nil
}

func (rt *workerRuntime) release(queueBytes uint32) {
	rt.mu.Lock()
	rt.live--
	rt.queueBytes -= uint64(queueBytes)
	rt.mu.Unlock()
}

func validateWorkerCallback(in *Instance, tableIndex uint32) error {
	tableDesc := in.tableDescriptor(0)
	if len(tableDesc) < 8 {
		return fmt.Errorf("worker callback: instance has no table")
	}
	size := binary.LittleEndian.Uint32(tableDesc)
	if tableIndex >= size {
		return fmt.Errorf("worker callback table index %d out of bounds (size %d)", tableIndex, size)
	}
	off := 8 + int(tableIndex)*coreruntime.TableEntryBytes
	if off < 8 || off+coreruntime.TableEntryBytes > len(tableDesc) {
		return fmt.Errorf("worker callback table descriptor is truncated")
	}
	entry := tableDesc[off : off+coreruntime.TableEntryBytes]
	if binary.LittleEndian.Uint64(entry) == 0 {
		return fmt.Errorf("worker callback table index %d is null", tableIndex)
	}
	if got, want := binary.LittleEndian.Uint32(entry[8:]), workerVoidTypeID(); got != want {
		return fmt.Errorf("worker callback table index %d has signature id %d, want () -> () (%d)", tableIndex, got, want)
	}
	return nil
}

var workerVoidFuncType = wasm.CompType{Kind: wasm.CompFunc}

func workerVoidTypeID() uint32 { return wasm.StructuralFuncTypeID(&workerVoidFuncType) }

func (rt *workerRuntime) send(service *Workers, id WorkerID, tag uint64, payload []byte) error {
	wr, err := rt.ownedWorker(service, id)
	if err != nil {
		return err
	}
	return wr.enqueue(tag, payload)
}

func (rt *workerRuntime) current(service *Workers, in *Instance) (WorkerID, error) {
	rt.mu.Lock()
	wr := rt.byInstance[in]
	rt.mu.Unlock()
	if wr == nil || wr.service != service {
		return 0, ErrWorkerNotFound
	}
	return wr.id, nil
}

func (rt *workerRuntime) dispatchNext(service *Workers, caller instanceHostModule) error {
	rt.mu.Lock()
	wr := rt.byInstance[caller.in]
	if wr == nil || wr.service != service {
		rt.mu.Unlock()
		return ErrWorkerNotFound
	}
	rt.mu.Unlock()
	return wr.dispatchNext(caller)
}

func (rt *workerRuntime) link(service *Workers, parent *Instance, childID WorkerID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrWorkerRuntimeClosed
	}
	wr := rt.workers[childID]
	if wr == nil || wr.service != service {
		return ErrWorkerNotFound
	}
	if workerCallerClosed(parent) || wr.creator != parent || wr.instance == parent {
		return ErrInvalidWorkerLink
	}
	wr.mu.Lock()
	stopping := wr.stopping || wr.terminal
	wr.mu.Unlock()
	if stopping {
		return ErrWorkerStopping
	}
	children := rt.linked[parent]
	if children == nil {
		children = map[WorkerID]*worker{}
		rt.linked[parent] = children
	}
	children[childID] = wr
	return nil
}

func (rt *workerRuntime) kill(service *Workers, id WorkerID, cause error) error {
	wr, err := rt.ownedWorker(service, id)
	if err != nil {
		return err
	}
	if !wr.requestStop(cause) {
		return ErrWorkerNotFound
	}
	return nil
}

func (rt *workerRuntime) ownedWorker(service *Workers, id WorkerID) (*worker, error) {
	if id == 0 {
		return nil, ErrWorkerNotFound
	}
	rt.mu.Lock()
	wr := rt.workers[id]
	closed := rt.closed
	rt.mu.Unlock()
	if wr == nil || wr.service != service {
		return nil, ErrWorkerNotFound
	}
	if closed {
		return nil, ErrWorkerRuntimeClosed
	}
	return wr, nil
}

func (wr *worker) enqueue(tag uint64, payload []byte) error {
	if uint64(len(payload)) > uint64(wr.maxPayloadBytes) {
		return ErrPayloadTooLarge
	}
	// Copy before taking the worker lock so a large payload cannot delay Kill,
	// shutdown, dequeue, or another sender that already owns its payload.
	copyPayload := append([]byte(nil), payload...)

	wr.mu.Lock()
	defer wr.mu.Unlock()
	if wr.stopping || wr.terminal {
		return ErrWorkerStopping
	}
	if wr.length == len(wr.queue) || uint64(wr.queueBytes)+uint64(len(copyPayload)) > uint64(wr.maxQueueBytes) {
		return ErrWorkerQueueFull
	}
	idx := (wr.head + wr.length) % len(wr.queue)
	wr.queue[idx] = workerMessage{tag: tag, payload: copyPayload}
	wr.length++
	wr.queueBytes += uint32(len(copyPayload))
	wr.signal()
	return nil
}

func (wr *worker) dispatchNext(caller instanceHostModule) error {
	wr.mu.Lock()
	if wr.dispatching {
		wr.mu.Unlock()
		return ErrWorkerDispatchActive
	}
	wr.dispatching = true
	wr.mu.Unlock()
	defer func() {
		wr.mu.Lock()
		wr.dispatching = false
		wr.mu.Unlock()
	}()

	for {
		// A retained caller may have been valid when an asynchronous call began
		// but expired while it waited. Recheck before touching worker state or
		// invoking handlers so dispatch can never continue after host return.
		if !caller.valid() {
			return ErrInvalidWorkerCaller
		}
		wr.mu.Lock()
		if wr.stopping || wr.terminal {
			err := wr.stopErr
			if err == nil {
				err = ErrWorkerStopping
			}
			wr.mu.Unlock()
			panic(err) // cooperative stop: unwind the suspended worker callback
		}
		if wr.length != 0 {
			msg := wr.queue[wr.head]
			wr.queue[wr.head] = workerMessage{}
			wr.head = (wr.head + 1) % len(wr.queue)
			wr.length--
			wr.queueBytes -= uint32(len(msg.payload))
			wr.mu.Unlock()

			caller := wr.messageScope.begin(wr.instance)
			ctx := &MessageContext{WorkerID: wr.id, Tag: msg.tag, Payload: msg.payload, Caller: caller}
			handlerErr := func() error {
				defer wr.messageScope.end(caller.generation)
				for _, fn := range wr.messageHandlers {
					if fn == nil {
						continue
					}
					if err := fn(ctx); err != nil {
						return err
					}
				}
				return nil
			}()
			if handlerErr != nil {
				wr.markFailed(handlerErr)
				panic(handlerErr) // abort the suspended callback; run reports the original failure
			}
			wr.mu.Lock()
			if wr.stopping {
				err := wr.stopErr
				wr.mu.Unlock()
				panic(err) // a stop accepted during OnMessage wins before guest resumes
			}
			wr.mu.Unlock()
			return nil
		}
		wr.mu.Unlock()
		wr.hostWaiter.wake = wr.wake
		if !caller.registerWait(&wr.hostWaiter) {
			return ErrInvalidWorkerCaller
		}
		<-wr.wake
		caller.unregisterWait(&wr.hostWaiter)
	}
}

func (wr *worker) requestStop(cause error) bool {
	wr.mu.Lock()
	if wr.terminal {
		wr.mu.Unlock()
		return false
	}
	if !wr.stopping {
		wr.stopping = true
		wr.stopErr = cause
		for i := range wr.queue {
			wr.queue[i] = workerMessage{}
		}
		wr.head, wr.length, wr.queueBytes = 0, 0, 0
	}
	wr.mu.Unlock()
	wr.signal()
	return true
}

func (wr *worker) markFailed(err error) {
	wr.mu.Lock()
	if wr.failed == nil && !wr.stopping {
		wr.failed = err
		wr.stopping = true
		wr.stopErr = err
		for i := range wr.queue {
			wr.queue[i] = workerMessage{}
		}
		wr.head, wr.length, wr.queueBytes = 0, 0, 0
	}
	wr.mu.Unlock()
	wr.signal()
}

func (wr *worker) signal() {
	select {
	case wr.wake <- struct{}{}:
	default:
	}
}

func (wr *worker) run(dispatchBase uintptr) {
	kind := WorkerReturned
	var runErr error
	wr.mu.Lock()
	if wr.stopping {
		kind, runErr = WorkerKilled, wr.stopErr
		wr.terminal = true
		wr.mu.Unlock()
		wr.finish(kind, runErr)
		return
	}
	// This lock-protected transition is the startup linearization point: a stop
	// accepted before it skips native entry; a later stop is cooperative.
	wr.started = true
	wr.mu.Unlock()

	func() {
		defer func() {
			if r := recover(); r != nil {
				kind = WorkerFailed
				if err, ok := r.(error); ok {
					runErr = fmt.Errorf("worker panic: %w", err)
				} else {
					runErr = fmt.Errorf("worker panic: %v", r)
				}
			}
		}()
		runErr = wr.instance.invokeWorkerCallback(dispatchBase, wr.tableIndex)
		if runErr != nil {
			var exit *ExitError
			if errors.As(runErr, &exit) && exit.Code == 0 {
				kind, runErr = WorkerReturned, nil
			} else {
				kind = WorkerFailed
			}
		}
	}()

	wr.mu.Lock()
	switch {
	case wr.failed != nil:
		kind, runErr = WorkerFailed, wr.failed
	case wr.stopping:
		kind, runErr = WorkerKilled, wr.stopErr
	}
	wr.terminal = true
	for i := range wr.queue {
		wr.queue[i] = workerMessage{}
	}
	wr.head, wr.length, wr.queueBytes = 0, 0, 0
	wr.mu.Unlock()
	wr.finish(kind, runErr)
}

func (wr *worker) finish(kind WorkerExitKind, err error) {
	defer close(wr.done)
	rt := wr.manager
	rt.mu.Lock()
	delete(rt.workers, wr.id)
	delete(rt.byInstance, wr.instance)
	if children := rt.linked[wr.creator]; children != nil {
		delete(children, wr.id)
		if len(children) == 0 {
			delete(rt.linked, wr.creator)
		}
	}
	rt.mu.Unlock()

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				rt.reportExitPanic(wr.id, -1, recovered)
			}
			close(wr.disposed)
		}()
		_ = wr.instance.Close()
	}()

	ctx := &WorkerExitContext{WorkerID: wr.id, Kind: kind, Err: err}
	for i, fn := range wr.exitHandlers {
		if fn == nil {
			continue
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					rt.reportExitPanic(wr.id, i, recovered)
				}
			}()
			fn(ctx)
		}()
	}

	// Hold aggregate quota through disposal and all exit observers so a
	// concurrent Spawn cannot exceed the configured live-resource ceiling while
	// this worker's goroutine or instance is still finalizing.
	rt.mu.Lock()
	rt.live--
	rt.queueBytes -= uint64(wr.maxQueueBytes)
	rt.mu.Unlock()
}

func (rt *workerRuntime) reportExitPanic(id WorkerID, observer int, recovered any) {
	where := "instance close callback"
	if observer >= 0 {
		where = fmt.Sprintf("exit observer %d", observer)
	}
	rt.mu.Lock()
	rt.exitPanics = append(rt.exitPanics, fmt.Errorf("worker %d %s panic: %v", id, where, recovered))
	rt.mu.Unlock()
}

func (rt *workerRuntime) parentClosing(parent *Instance) {
	rt.mu.Lock()
	children := rt.linked[parent]
	delete(rt.linked, parent)
	list := make([]*worker, 0, len(children))
	for _, wr := range children {
		list = append(list, wr)
	}
	rt.mu.Unlock()
	for _, wr := range list {
		wr.requestStop(ErrWorkerParentClosed)
	}
	for _, wr := range list {
		<-wr.disposed
	}
}

func (rt *workerRuntime) close() error {
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return nil
	}
	rt.closed = true
	list := make([]*worker, 0, len(rt.workers))
	for _, wr := range rt.workers {
		list = append(list, wr)
	}
	rt.mu.Unlock()
	for _, wr := range list {
		wr.requestStop(ErrWorkerRuntimeClosed)
	}
	for _, wr := range list {
		<-wr.done
	}

	rt.dispatchMu.Lock()
	var err error
	if rt.dispatchCode != nil {
		err = rt.dispatchCode.Close()
		rt.dispatchCode.releaseCode()
		rt.dispatchCode = nil
		rt.dispatchBase = 0
	}
	rt.dispatchMu.Unlock()

	rt.mu.Lock()
	panicErrs := append([]error(nil), rt.exitPanics...)
	rt.exitPanics = nil
	rt.mu.Unlock()
	if err != nil {
		panicErrs = append([]error{err}, panicErrs...)
	}
	return errors.Join(panicErrs...)
}

func (rt *workerRuntime) ensureDispatcher() (uintptr, error) {
	rt.dispatchMu.Lock()
	defer rt.dispatchMu.Unlock()
	rt.mu.Lock()
	closed := rt.closed
	rt.mu.Unlock()
	if closed {
		return 0, ErrWorkerRuntimeClosed
	}
	if rt.dispatchBase != 0 {
		return rt.dispatchBase, nil
	}
	c, err := Compile(rt.rt.cfg, workerDispatcherWasm)
	if err != nil {
		return 0, fmt.Errorf("compile worker call_indirect dispatcher: %w", err)
	}
	base, err := c.acquireCode()
	if err != nil {
		_ = c.Close()
		return 0, fmt.Errorf("map worker call_indirect dispatcher: %w", err)
	}
	if len(c.Entry) != 1 {
		c.releaseCode()
		_ = c.Close()
		return 0, fmt.Errorf("worker call_indirect dispatcher has %d entries, want 1", len(c.Entry))
	}
	rt.mu.Lock()
	closed = rt.closed
	rt.mu.Unlock()
	if closed {
		c.releaseCode()
		_ = c.Close()
		return 0, ErrWorkerRuntimeClosed
	}
	rt.dispatchCode, rt.dispatchBase = c, base+uintptr(c.Entry[0])
	return rt.dispatchBase, nil
}

// workerDispatcherWasm is:
//
//	(module
//	  (type (func))
//	  (type (func (param i32)))
//	  (table 0 funcref)
//	  (func (type 1) (param i32)
//	    local.get 0
//	    call_indirect (type 0)))
//
// Its code is entered with a worker instance's basedata, so call_indirect reads
// that instance's table and preserves all ordinary table dispatch semantics.
var workerDispatcherWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x08, 0x02, 0x60, 0x00, 0x00, 0x60, 0x01, 0x7f, 0x00,
	0x03, 0x02, 0x01, 0x01,
	0x04, 0x04, 0x01, 0x70, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x11, 0x00, 0x00, 0x0b,
}

func (in *Instance) invokeWorkerCallback(dispatchBase uintptr, tableIndex uint32) error {
	if len(in.serArgs) < 8 {
		return fmt.Errorf("worker callback argument buffer is unavailable")
	}
	binary.LittleEndian.PutUint64(in.serArgs, uint64(tableIndex))
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := dispatchBase
	if in.syncMode {
		return in.callNativeSync(entry)
	}
	if err := callNative(in.c, in.eng, in.jm, in.nativeControlShared, entry, in.serArgs, in.trap, in.results); err != nil {
		return err
	}
	return in.replayHostLog()
}
