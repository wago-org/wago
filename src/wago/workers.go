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
)

type workerError string

func (e workerError) Error() string { return string(e) }

const (
	ErrWorkersInactive      = workerError("worker service is not active")
	ErrInvalidWorkerOptions = workerError("invalid worker options")
	ErrInvalidWorkerCaller  = workerError("worker operation requires a current plugin host caller")
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
// handlers; they therefore must not re-enter Runtime.Close.
func (w *Workers) OnExit(fns ...func(*WorkerExitContext)) {
	w.mu.Lock()
	w.onExit = append(w.onExit, fns...)
	w.mu.Unlock()
}

// Spawn creates a fresh instance of the caller's compiled module and starts its
// table callback on a dedicated goroutine. The callback entry must be non-null
// and have the exact signature () -> ().
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
// Plugins call it from one of their host imports; OnMessage then runs on this
// same worker goroutine before DispatchNext returns to guest code.
func (w *Workers) DispatchNext(caller HostModule) error {
	rt, err := w.activeRuntime()
	if err != nil {
		return err
	}
	in, err := workerCaller(caller)
	if err != nil {
		return err
	}
	return rt.dispatchNext(w, in)
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
	if !ok || h.in == nil {
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

	mu              sync.Mutex
	queue           []workerMessage
	head            int
	length          int
	maxPayloadBytes uint32
	maxQueueBytes   uint32
	queueBytes      uint32
	dispatching     bool
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
	}
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

func (rt *workerRuntime) spawn(service *Workers, parent *Instance, tableIndex uint32, opts WorkerOptions) (WorkerID, error) {
	if parent.rt != rt.rt || parent.closed.Load() {
		return 0, ErrInvalidWorkerCaller
	}
	opts, err := normalizeWorkerOptions(opts)
	if err != nil {
		return 0, err
	}
	dispatchBase, err := rt.ensureDispatcher()
	if err != nil {
		return 0, err
	}

	imports := make(Imports, len(parent.imports))
	for k, v := range parent.imports {
		imports[k] = v
	}
	mod := rt.rt.buildModule(parent.c)
	child, err := rt.rt.instantiateWithHooksOrigin(mod, imports, parent.gcConfig, parent.hasGCConfig, InstantiateWorker)
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
	if parent.closed.Load() {
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
	rt.mu.Unlock()

	go wr.run(dispatchBase)
	return wr.id, nil
}

func validateWorkerCallback(in *Instance, tableIndex uint32) error {
	if in == nil || len(in.tableDesc) < 8 {
		return fmt.Errorf("worker callback: instance has no table")
	}
	size := binary.LittleEndian.Uint32(in.tableDesc)
	if tableIndex >= size {
		return fmt.Errorf("worker callback table index %d out of bounds (size %d)", tableIndex, size)
	}
	off := 8 + int(tableIndex)*coreruntime.TableEntryBytes
	if off < 8 || off+coreruntime.TableEntryBytes > len(in.tableDesc) {
		return fmt.Errorf("worker callback table descriptor is truncated")
	}
	entry := in.tableDesc[off : off+coreruntime.TableEntryBytes]
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

func (rt *workerRuntime) dispatchNext(service *Workers, in *Instance) error {
	rt.mu.Lock()
	wr := rt.byInstance[in]
	if wr == nil || wr.service != service {
		rt.mu.Unlock()
		return ErrWorkerNotFound
	}
	rt.mu.Unlock()
	return wr.dispatchNext()
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
	if parent == nil || parent.closed.Load() || wr.creator != parent || wr.instance == parent {
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
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if wr.stopping || wr.terminal {
		return ErrWorkerStopping
	}
	if uint64(len(payload)) > uint64(wr.maxPayloadBytes) {
		return ErrPayloadTooLarge
	}
	if wr.length == len(wr.queue) || uint64(wr.queueBytes)+uint64(len(payload)) > uint64(wr.maxQueueBytes) {
		return ErrWorkerQueueFull
	}
	copyPayload := append([]byte(nil), payload...)
	idx := (wr.head + wr.length) % len(wr.queue)
	wr.queue[idx] = workerMessage{tag: tag, payload: copyPayload}
	wr.length++
	wr.queueBytes += uint32(len(copyPayload))
	wr.signal()
	return nil
}

func (wr *worker) dispatchNext() error {
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

			ctx := &MessageContext{WorkerID: wr.id, Tag: msg.tag, Payload: msg.payload, Caller: instanceHostModule{in: wr.instance}}
			for _, fn := range wr.messageHandlers {
				if fn == nil {
					continue
				}
				if err := fn(ctx); err != nil {
					wr.markFailed(err)
					panic(err) // abort the suspended callback; run reports the original failure
				}
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
		<-wr.wake
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

	_ = wr.instance.Close()
	close(wr.disposed)
	ctx := &WorkerExitContext{WorkerID: wr.id, Kind: kind, Err: err}
	for _, fn := range wr.exitHandlers {
		if fn != nil {
			fn(ctx)
		}
	}
	close(wr.done)
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
		rt.dispatchCode.releaseCode()
		err = rt.dispatchCode.Close()
		rt.dispatchCode = nil
		rt.dispatchBase = 0
	}
	rt.dispatchMu.Unlock()
	return err
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
	if err := callNative(in.c, in.eng, in.jm, entry, in.serArgs, in.trap, in.results); err != nil {
		return err
	}
	return in.replayHostLog()
}
