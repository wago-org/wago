package wago

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type workerTestExt struct {
	id             string
	workers        *Workers
	onMessage      func(*MessageContext) error
	onExit         func(*WorkerExitContext)
	before         func(*InstantiateContext) error
	afterClose     func(*InstanceContext)
	onRuntimeClose func(*RuntimeContext)
	hostNext       HostFunc
	noImport       bool
}

func (e *workerTestExt) Info() ExtensionInfo {
	id := e.id
	if id == "" {
		id = "worker-test"
	}
	return ExtensionInfo{ID: id, Version: "1.0.0", Stability: Experimental}
}

func (e *workerTestExt) Register(r *Registry) error {
	e.workers = r.Workers()
	if e.onMessage != nil {
		e.workers.OnMessage(e.onMessage)
	}
	if e.onExit != nil {
		e.workers.OnExit(e.onExit)
	}
	if e.before != nil {
		r.Hooks().BeforeInstantiate(e.before)
	}
	if e.afterClose != nil {
		r.Hooks().AfterClose(e.afterClose)
	}
	if e.onRuntimeClose != nil {
		r.Hooks().OnRuntimeClose(e.onRuntimeClose)
	}
	if !e.noImport {
		next := e.hostNext
		if next == nil {
			next = func(m HostModule, _, _ []uint64) { _ = e.workers.DispatchNext(m) }
		}
		r.ImportModule("worker_test").Func("next", next)
	}
	return nil
}

func workerModule(callbackBody []byte, callbackParams []wasm.ValType, tableEntry *uint32) []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(callbackParams, nil),
	}
	imp := append(wasmtest.Name("worker_test"), wasmtest.Name("next")...)
	imp = append(imp, 0x00, 0x00) // func import, type 0
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
	}
	if tableEntry != nil {
		elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01}
		elem = append(elem, wasmtest.ULEB(*tableEntry)...)
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(elem)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callbackBody))))
	return wasmtest.Module(sections...)
}

func workerCrossInstanceProviderModule() []byte {
	entry := uint32(0)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x00, 0x41, 0x00, 0x0b, 0x01}, wasmtest.ULEB(entry)...))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, // i32.const 0
			0x41, 0x01, // i32.const 1
			0x36, 0x02, 0x00, // i32.store align=2 offset=0
			0x0b,
		}))),
	)
}

func workerCrossInstanceConsumerModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	imp = append(imp, 0x01, 0x70, 0x00, 0x01) // table import, funcref, min 1
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imp)))
}

func workerReceiveNModule(n int) []byte {
	entry := uint32(1) // one imported function precedes the local callback
	body := make([]byte, 0, n*2+1)
	for i := 0; i < n; i++ {
		body = append(body, 0x10, 0x00) // call imported next
	}
	body = append(body, 0x0b)
	return workerModule(body, nil, &entry)
}

func workerReceiveModule() []byte { return workerReceiveNModule(1) }

type rejectedWorkerExt struct{ workers *Workers }

func (e *rejectedWorkerExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "rejected-worker", Version: "1.0.0", Stability: Experimental}
}

func (e *rejectedWorkerExt) Register(r *Registry) error {
	e.workers = r.Workers()
	e.workers.OnExit(func(*WorkerExitContext) {})
	return errors.New("reject worker extension")
}

func waitWorkerExit(t *testing.T, ch <-chan WorkerExitContext) WorkerExitContext {
	t.Helper()
	select {
	case ex := <-ch:
		return ex
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker exit")
		return WorkerExitContext{}
	}
}

func TestWorkerVoidTypeIDMatchesCompiler(t *testing.T) {
	want := wasm.StructuralFuncTypeID(&wasm.CompType{Kind: wasm.CompFunc})
	if got := workerVoidTypeID(); got != want {
		t.Fatalf("worker void type ID = %d, compiler = %d", got, want)
	}
}

func TestWorkersSpawnSendCopiesPayloadAndExits(t *testing.T) {
	exits := make(chan WorkerExitContext, 2)
	messages := make(chan MessageContext, 2)
	ext := &workerTestExt{}
	ext.onMessage = func(ctx *MessageContext) error {
		if ctx.Caller == nil {
			return errors.New("message caller is nil")
		}
		current, err := ext.workers.Current(ctx.Caller)
		if err != nil || current != ctx.WorkerID {
			return fmt.Errorf("Current = (%d, %v), want %d", current, err, ctx.WorkerID)
		}
		messages <- MessageContext{WorkerID: ctx.WorkerID, Tag: ctx.Tag, Payload: append([]byte(nil), ctx.Payload...)}
		return nil
	}
	ext.onExit = func(ctx *WorkerExitContext) { exits <- *ctx }
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate parent: %v", err)
	}
	defer parent.Close()
	if id, err := ext.workers.Current(instanceHostModule{in: parent}); id != 0 || !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("Current(direct parent) = (%d, %v), want ErrWorkerNotFound", id, err)
	}

	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{QueueCapacity: 1, MaxPayloadBytes: 16})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if id == 0 {
		t.Fatal("Spawn returned zero WorkerID")
	}
	payload := []byte("abc")
	if err := ext.workers.Send(id, 42, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	copy(payload, "zzz")

	select {
	case msg := <-messages:
		if msg.WorkerID != id || msg.Tag != 42 || string(msg.Payload) != "abc" {
			t.Fatalf("message = id %d tag %d payload %q", msg.WorkerID, msg.Tag, msg.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}
	ex := waitWorkerExit(t, exits)
	if ex.WorkerID != id || ex.Kind != WorkerReturned || ex.Err != nil {
		t.Fatalf("exit = %+v, want returned", ex)
	}
	if err := ext.workers.Send(id, 0, nil); !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("Send after exit = %v, want ErrWorkerNotFound", err)
	}
}

func TestWorkersDeliverFIFO(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	tags := make(chan uint64, 2)
	ext := &workerTestExt{
		onMessage: func(ctx *MessageContext) error { tags <- ctx.Tag; return nil },
		onExit:    func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveNModule(2))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{QueueCapacity: 2})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Send(id, 10, nil); err != nil {
		t.Fatalf("Send first: %v", err)
	}
	if err := ext.workers.Send(id, 20, nil); err != nil {
		t.Fatalf("Send second: %v", err)
	}
	got := make([]uint64, 0, 2)
	for len(got) < 2 {
		select {
		case tag := <-tags:
			got = append(got, tag)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for FIFO delivery")
		}
	}
	if got[0] != 10 || got[1] != 20 {
		t.Fatalf("delivery order = %v; want [10 20]", got)
	}
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersUnlinkedChildOutlivesCreator(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	messages := make(chan struct{}, 1)
	ext := &workerTestExt{
		onMessage: func(*MessageContext) error { messages <- struct{}{}; return nil },
		onExit:    func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	if err := ext.workers.Send(id, 1, nil); err != nil {
		t.Fatalf("Send after unlinked parent Close: %v", err)
	}
	select {
	case <-messages:
	case <-time.After(2 * time.Second):
		t.Fatal("unlinked child did not receive after creator close")
	}
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersKillAndLinkAuthorization(t *testing.T) {
	exits := make(chan WorkerExitContext, 2)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate parent: %v", err)
	}
	other, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate other: %v", err)
	}
	defer other.Close()

	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Link(instanceHostModule{in: other}, id); !errors.Is(err, ErrInvalidWorkerLink) {
		t.Fatalf("foreign Link = %v, want ErrInvalidWorkerLink", err)
	}
	if err := ext.workers.Link(instanceHostModule{in: parent}, id); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.WorkerID != id || ex.Kind != WorkerKilled || !errors.Is(ex.Err, ErrWorkerParentClosed) {
		t.Fatalf("exit = %+v, want parent-closed kill", ex)
	}
}

func TestWorkersConcurrentSpawnAllocatesUniqueIDs(t *testing.T) {
	const count = 8
	exits := make(chan WorkerExitContext, count)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()

	ids := make(chan WorkerID, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Spawn: %v", err)
	}
	seen := map[WorkerID]bool{}
	for id := range ids {
		if id == 0 || seen[id] {
			t.Fatalf("duplicate or zero WorkerID %d", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("spawned %d IDs, want %d", len(seen), count)
	}
	for id := range seen {
		if err := ext.workers.Kill(id); err != nil {
			t.Fatalf("Kill(%d): %v", id, err)
		}
	}
	for i := 0; i < count; i++ {
		if ex := waitWorkerExit(t, exits); ex.Kind != WorkerKilled {
			t.Fatalf("exit = %+v, want killed", ex)
		}
	}
}

func TestWorkersKillDuringMessageHandlerWinsBeforeGuestResume(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		onMessage: func(*MessageContext) error {
			close(entered)
			<-release
			return nil
		},
		onExit: func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Send(id, 0, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("message handler did not start")
	}
	if err := ext.workers.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	close(release)
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerKilled || !errors.Is(ex.Err, ErrWorkerKilled) {
		t.Fatalf("exit = %+v, want killed", ex)
	}
}

func TestWorkersKillIsCooperativeAndIdempotent(t *testing.T) {
	exits := make(chan WorkerExitContext, 2)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := ext.workers.Kill(id); err != nil && !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("second Kill: %v", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerKilled || !errors.Is(ex.Err, ErrWorkerKilled) {
		t.Fatalf("exit = %+v, want killed", ex)
	}
	id2, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("second Spawn: %v", err)
	}
	if id2 <= id {
		t.Fatalf("second WorkerID = %d, want greater than %d", id2, id)
	}
	if err := ext.workers.Kill(id2); err != nil {
		t.Fatalf("second worker Kill: %v", err)
	}
	_ = waitWorkerExit(t, exits)
}

func TestWorkersRejectInvalidCallbacks(t *testing.T) {
	ext := &workerTestExt{}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}

	entry := uint32(1)
	cases := []struct {
		name string
		wasm []byte
		idx  uint32
	}{
		{name: "null", wasm: workerModule([]byte{0x0b}, nil, nil), idx: 0},
		{name: "out of bounds", wasm: workerModule([]byte{0x0b}, nil, &entry), idx: 1},
		{name: "wrong signature", wasm: workerModule([]byte{0x0b}, []wasm.ValType{wasm.I32}, &entry), idx: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, err := rt.Compile(tc.wasm)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			parent, err := rt.Instantiate(nil, mod)
			if err != nil {
				t.Fatalf("Instantiate: %v", err)
			}
			defer parent.Close()
			if id, err := ext.workers.Spawn(instanceHostModule{in: parent}, tc.idx, WorkerOptions{}); err == nil || id != 0 {
				t.Fatalf("Spawn = (%d, %v), want callback validation error", id, err)
			}
		})
	}
}

func TestWorkersBoundedQueueAndPayloadLimit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		onMessage: func(*MessageContext) error {
			close(entered)
			<-release
			return nil
		},
		onExit: func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	if id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{QueueCapacity: MaxWorkerQueueCapacity + 1}); id != 0 || !errors.Is(err, ErrInvalidWorkerOptions) {
		t.Fatalf("Spawn with oversized queue = (%d, %v), want ErrInvalidWorkerOptions", id, err)
	}
	if id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{MaxPayloadBytes: 4, MaxQueueBytes: 3}); id != 0 || !errors.Is(err, ErrInvalidWorkerOptions) {
		t.Fatalf("Spawn with payload larger than byte queue = (%d, %v), want ErrInvalidWorkerOptions", id, err)
	}
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{QueueCapacity: 2, MaxPayloadBytes: 3, MaxQueueBytes: 3})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Send(id, 1, []byte("one")); err != nil {
		t.Fatalf("Send first: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("message handler did not start")
	}
	if err := ext.workers.Send(id, 2, []byte("two")); err != nil {
		t.Fatalf("Send queued: %v", err)
	}
	if err := ext.workers.Send(id, 3, []byte("tri")); !errors.Is(err, ErrWorkerQueueFull) {
		t.Fatalf("Send full = %v, want ErrWorkerQueueFull", err)
	}
	if err := ext.workers.Send(id, 4, []byte("four")); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Send oversized = %v, want ErrPayloadTooLarge", err)
	}
	close(release)
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersRejectRecursiveDispatch(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	nested := make(chan error, 1)
	ext := &workerTestExt{}
	ext.onMessage = func(ctx *MessageContext) error {
		nested <- ext.workers.DispatchNext(ctx.Caller)
		return nil
	}
	ext.onExit = func(ctx *WorkerExitContext) { exits <- *ctx }
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Send(id, 0, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case err := <-nested:
		if !errors.Is(err, ErrWorkerDispatchActive) {
			t.Fatalf("recursive DispatchNext = %v, want ErrWorkerDispatchActive", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recursive dispatch result")
	}
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersOnMessageErrorFailsWorker(t *testing.T) {
	boom := errors.New("message failed")
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		onMessage: func(*MessageContext) error { return boom },
		onExit:    func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Send(id, 0, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerFailed || !errors.Is(ex.Err, boom) {
		t.Fatalf("exit = %+v, want message failure", ex)
	}
}

func TestWorkersRuntimeCloseStopsWorkers(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	exitObserved := false
	ext := &workerTestExt{
		onExit: func(ctx *WorkerExitContext) {
			exitObserved = true
			exits <- *ctx
		},
		onRuntimeClose: func(*RuntimeContext) {
			if !exitObserved {
				panic("runtime close hook ran before worker exit handler")
			}
		},
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	dispatcher := rt.workers.dispatchCode
	if dispatcher == nil {
		t.Fatal("worker dispatcher was not initialized")
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.WorkerID != id || ex.Kind != WorkerKilled || !errors.Is(ex.Err, ErrWorkerRuntimeClosed) {
		t.Fatalf("exit = %+v, want runtime-closed kill", ex)
	}
	if err := ext.workers.Send(id, 0, nil); !errors.Is(err, ErrWorkerNotFound) && !errors.Is(err, ErrWorkerRuntimeClosed) {
		t.Fatalf("Send after Runtime.Close = %v", err)
	}
	dispatcher.codeCache.mu.Lock()
	mapped, closed := dispatcher.codeCache.mem != nil, dispatcher.codeCache.closed
	dispatcher.codeCache.mu.Unlock()
	if mapped || !closed {
		t.Fatalf("dispatcher after Runtime.Close: mapped=%v closed=%v", mapped, closed)
	}
	_ = parent.Close()
}

func TestWorkersInvokeCrossInstanceTableCallback(t *testing.T) {
	ext := &workerTestExt{}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	providerCompiled, err := Compile(workerCrossInstanceProviderModule())
	if err != nil {
		t.Fatalf("Compile provider: %v", err)
	}
	provider, err := Instantiate(providerCompiled)
	if err != nil {
		t.Fatalf("Instantiate provider: %v", err)
	}
	defer provider.Close()
	table, err := provider.ExportedTable("table")
	if err != nil {
		t.Fatalf("ExportedTable: %v", err)
	}
	consumerMod, err := rt.Compile(workerCrossInstanceConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(nil, consumerMod, WithImports(Imports{"env.table": table}))
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	exits := make(chan WorkerExitContext, 1)
	ext.workers.OnExit(func(ctx *WorkerExitContext) { exits <- *ctx })
	if _, err := ext.workers.Spawn(instanceHostModule{in: consumer}, 0, WorkerOptions{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
	if got := provider.Memory().Bytes(); len(got) < 4 || got[0] != 1 {
		t.Fatalf("provider memory after cross-instance callback = %v", got[:min(len(got), 4)])
	}
}

func TestWorkersInvokeHostFuncrefCallback(t *testing.T) {
	called := make(chan struct{}, 1)
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		hostNext: func(HostModule, []uint64, []uint64) { called <- struct{}{} },
		onExit:   func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	entry := uint32(0) // imported worker_test.next itself is the table callback
	mod, err := rt.Compile(workerModule([]byte{0x0b}, nil, &entry))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	if _, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("host funcref callback was not invoked")
	}
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersHostExitZeroIsNormalReturn(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		hostNext: func(HostModule, []uint64, []uint64) { panic(HostExit{Code: 0}) },
		onExit:   func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	if _, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerReturned || ex.Err != nil {
		t.Fatalf("exit = %+v, want normal return", ex)
	}
}

func TestWorkersTrapAndExtensionOwnership(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	owner := &workerTestExt{id: "worker-owner", onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	foreign := &workerTestExt{id: "worker-foreign", noImport: true}
	rt := NewRuntime()
	if err := rt.Use(owner); err != nil {
		t.Fatalf("Use owner: %v", err)
	}
	if err := rt.Use(foreign); err != nil {
		t.Fatalf("Use foreign: %v", err)
	}
	entry := uint32(1)
	mod, err := rt.Compile(workerModule([]byte{0x00, 0x0b}, nil, &entry)) // unreachable
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer parent.Close()
	id, err := owner.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := foreign.workers.Send(id, 0, nil); !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("foreign Send = %v, want ErrWorkerNotFound", err)
	}
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerFailed || ex.Err == nil {
		t.Fatalf("exit = %+v, want failed trap", ex)
	}
}

func BenchmarkWorkersSend64B(b *testing.B) {
	service := &Workers{}
	wr := &worker{id: 1, service: service, queue: make([]workerMessage, 1), maxPayloadBytes: 64, maxQueueBytes: 64, wake: make(chan struct{}, 1)}
	rt := &workerRuntime{workers: map[WorkerID]*worker{1: wr}}
	payload := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rt.send(service, 1, 1, payload); err != nil {
			b.Fatal(err)
		}
		wr.mu.Lock()
		wr.queue[0] = workerMessage{}
		wr.head, wr.length, wr.queueBytes = 0, 0, 0
		wr.mu.Unlock()
		select {
		case <-wr.wake:
		default:
		}
	}
}

func TestWorkersLifecycleOriginAndTransactionalActivation(t *testing.T) {
	var mu sync.Mutex
	var instOrigins, closeOrigins []InstantiateOrigin
	ext := &workerTestExt{
		before: func(ctx *InstantiateContext) error {
			mu.Lock()
			instOrigins = append(instOrigins, ctx.Origin)
			mu.Unlock()
			return nil
		},
		afterClose: func(ctx *InstanceContext) {
			mu.Lock()
			closeOrigins = append(closeOrigins, ctx.Origin)
			mu.Unlock()
		},
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := ext.workers.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	for {
		if err := ext.workers.Send(id, 0, nil); errors.Is(err, ErrWorkerNotFound) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(instOrigins) != 2 || instOrigins[0] != InstantiateDirect || instOrigins[1] != InstantiateWorker {
		t.Fatalf("instantiate origins = %v", instOrigins)
	}
	if len(closeOrigins) != 2 || closeOrigins[0] != InstantiateWorker || closeOrigins[1] != InstantiateDirect {
		t.Fatalf("close origins = %v", closeOrigins)
	}

	pending := (&Registry{info: ExtensionInfo{ID: "pending"}, hooks: &HookRegistry{}}).Workers()
	if _, err := pending.Spawn(nil, 0, WorkerOptions{}); !errors.Is(err, ErrWorkersInactive) {
		t.Fatalf("pending Spawn = %v, want ErrWorkersInactive", err)
	}
	rejected := &rejectedWorkerExt{}
	if err := rt.Use(rejected); err == nil {
		t.Fatal("Use rejected extension succeeded")
	}
	if err := rejected.workers.Kill(1); !errors.Is(err, ErrWorkersInactive) {
		t.Fatalf("rejected worker handle Kill = %v, want ErrWorkersInactive", err)
	}
}
