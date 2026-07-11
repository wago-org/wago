package wago

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	var err error
	e.workers, err = NewWorkers(r)
	if err != nil {
		return err
	}
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
		wasmtest.Section(7, wasmtest.Vec(append(wasmtest.Name("table"), 0x01, 0x00))),
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
	e.workers, _ = NewWorkers(r)
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

func TestWorkersRejectBorrowedCrossInstanceTable(t *testing.T) {
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

	if id, err := ext.workers.Spawn(instanceHostModule{in: consumer}, 0, WorkerOptions{}); id != 0 || !errors.Is(err, ErrWorkerImportLifetime) {
		t.Fatalf("Spawn with borrowed table = (%d, %v), want ErrWorkerImportLifetime", id, err)
	}
}

func TestWorkerImportsRejectBorrowedResources(t *testing.T) {
	memory, err := NewMemory(1, 1)
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	defer memory.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer table.Close()
	global := NewGlobalI32(1, true)
	defer global.Close()

	cases := []struct {
		name     string
		key      string
		value    any
		compiled *Compiled
	}{
		{name: "memory", key: "env.memory", value: memory, compiled: &Compiled{memoryImport: "env.memory"}},
		{name: "table", key: "env.table", value: table, compiled: &Compiled{tableImport: "env.table"}},
		{name: "global object", key: "env.global", value: GlobalImport{Global: global}, compiled: &Compiled{GlobalImports: []GlobalImportDef{{Module: "env", Name: "global"}}}},
		{name: "cross-instance function", key: "env.fn", value: &InstanceExport{}, compiled: &Compiled{Imports: []string{"env.fn"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := &Instance{c: tc.compiled, imports: Imports{tc.key: tc.value}}
			if _, err := workerImports(parent); !errors.Is(err, ErrWorkerImportLifetime) {
				t.Fatalf("workerImports = %v, want ErrWorkerImportLifetime", err)
			}
		})
	}

	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	parent := &Instance{
		c: &Compiled{
			Imports:       []string{"env.fn"},
			GlobalImports: []GlobalImportDef{{Module: "env", Name: "value"}},
		},
		imports: Imports{
			"env.fn":    fn,
			"env.value": GlobalImport{Type: ValI32, Bits: I32(7)},
		},
	}
	imports, err := workerImports(parent)
	if err != nil {
		t.Fatalf("workerImports safe values: %v", err)
	}
	if len(imports) != 2 {
		t.Fatalf("safe worker imports = %v", imports)
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

func TestCallerCreatedBeforeWorkerActivationCannotAuthorize(t *testing.T) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var retained HostModule
	in, err := rt.Instantiate(nil, mod, WithImports(Imports{
		"env.f": HostFunc(func(caller HostModule, params, results []uint64) {
			retained = caller
			results[0] = params[0]
		}),
	}))
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g", I32(1)); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	ext := &workerTestExt{}
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if id, err := ext.workers.Spawn(retained, 0, WorkerOptions{}); id != 0 || !errors.Is(err, ErrInvalidWorkerCaller) {
		t.Fatalf("Spawn(pre-activation caller) = (%d, %v), want ErrInvalidWorkerCaller", id, err)
	}
}

func TestWorkersRejectRetainedHostCallerAndAsyncDispatch(t *testing.T) {
	callers := make(chan HostModule, 1)
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		hostNext: func(caller HostModule, _, _ []uint64) { callers <- caller },
		onExit:   func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	entry := uint32(0) // imported worker_test.next is the callback
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
	var retained HostModule
	select {
	case retained = <-callers:
	case <-time.After(2 * time.Second):
		t.Fatal("host call did not run")
	}
	_ = waitWorkerExit(t, exits) // proves the host dispatch returned and expired the caller
	if retained.Memory() != nil {
		t.Fatal("retained HostModule.Memory remained usable")
	}
	if id, err := ext.workers.Spawn(retained, 0, WorkerOptions{}); id != 0 || !errors.Is(err, ErrInvalidWorkerCaller) {
		t.Fatalf("Spawn(retained) = (%d, %v), want ErrInvalidWorkerCaller", id, err)
	}
	result := make(chan error, 1)
	go func() { result <- ext.workers.DispatchNext(retained) }()
	select {
	case err := <-result:
		if !errors.Is(err, ErrInvalidWorkerCaller) {
			t.Fatalf("async DispatchNext(retained) = %v, want ErrInvalidWorkerCaller", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("async DispatchNext retained caller blocked")
	}
}

func TestWorkersAsyncDispatchCannotContinueAfterHostReturn(t *testing.T) {
	started := make(chan struct{})
	resumed := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	exits := make(chan WorkerExitContext, 1)
	var calls atomic.Int32
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	ext.hostNext = func(caller HostModule, _, _ []uint64) {
		switch calls.Add(1) {
		case 1:
			go func() {
				close(started)
				result <- ext.workers.DispatchNext(caller)
			}()
		case 2:
			close(resumed)
			<-release
		}
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	entry := uint32(1)
	mod, err := rt.Compile(workerModule([]byte{0x10, 0x00, 0x10, 0x00, 0x0b}, nil, &entry))
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
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("asynchronous DispatchNext did not start")
	}
	select {
	case <-resumed: // guest returned from the first host call and entered the second
	case <-time.After(2 * time.Second):
		t.Fatal("guest did not resume after launching asynchronous DispatchNext")
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrInvalidWorkerCaller) {
			t.Fatalf("asynchronous DispatchNext = %v, want ErrInvalidWorkerCaller", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asynchronous DispatchNext handled a message or remained blocked")
	}
	close(release)
	if ex := waitWorkerExit(t, exits); ex.Kind != WorkerReturned {
		t.Fatalf("exit = %+v, want returned", ex)
	}
}

func TestWorkersMessageCallerExpiresAndAllowsNestedSpawnLink(t *testing.T) {
	exits := make(chan WorkerExitContext, 2)
	nested := make(chan error, 1)
	var retained HostModule
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	ext.onMessage = func(ctx *MessageContext) error {
		retained = ctx.Caller
		child, err := ext.workers.Spawn(ctx.Caller, 0, WorkerOptions{})
		if err == nil {
			err = ext.workers.Link(ctx.Caller, child)
		}
		nested <- err
		return nil
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
	if err := ext.workers.Send(id, 1, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case err := <-nested:
		if err != nil {
			t.Fatalf("nested Spawn/Link: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nested Spawn/Link did not complete")
	}
	for i := 0; i < 2; i++ {
		_ = waitWorkerExit(t, exits)
	}
	if retained == nil || retained.Memory() != nil {
		t.Fatal("MessageContext.Caller remained usable after handler")
	}
	if _, err := ext.workers.Current(retained); !errors.Is(err, ErrInvalidWorkerCaller) {
		t.Fatalf("Current(retained message caller) = %v, want ErrInvalidWorkerCaller", err)
	}
}

func TestWorkersExitObserverPanicsAreIsolatedAndReported(t *testing.T) {
	later := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	ext.workers.OnExit(
		func(*WorkerExitContext) { panic("observer boom") },
		func(ctx *WorkerExitContext) { later <- *ctx },
	)
	mod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	parent, err := rt.Instantiate(nil, mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if _, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := rt.Close(); err == nil || !strings.Contains(err.Error(), "observer boom") {
		t.Fatalf("Runtime.Close error = %v, want recovered observer panic", err)
	}
	select {
	case <-later:
	case <-time.After(2 * time.Second):
		t.Fatal("later exit observer did not run")
	}
	_ = parent.Close()
}

func TestWorkersStoppedBeforeLaunchSkipsCallback(t *testing.T) {
	var calls atomic.Int32
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{
		hostNext: func(HostModule, []uint64, []uint64) { calls.Add(1) },
		onExit:   func(ctx *WorkerExitContext) { exits <- *ctx },
	}
	rt := NewRuntime()
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	var launch func()
	rt.workers.launch = func(wr *worker, dispatchBase uintptr) {
		launch = func() { wr.run(dispatchBase) }
	}
	entry := uint32(0)
	mod, err := rt.Compile(workerModule([]byte{0x0b}, nil, &entry))
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
	if launch == nil {
		t.Fatal("worker launch was not captured")
	}
	if err := ext.workers.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	launch()
	ex := waitWorkerExit(t, exits)
	if ex.Kind != WorkerKilled || !errors.Is(ex.Err, ErrWorkerKilled) {
		t.Fatalf("exit = %+v, want killed", ex)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("callback side effects = %d, want 0", got)
	}
}

func TestWorkersRuntimeQuotasAndAccounting(t *testing.T) {
	exits := make(chan WorkerExitContext, 4)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime(WithWorkerLimits(WorkerLimits{MaxLiveWorkers: 2, MaxQueueBytes: 8}))
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
	opts := WorkerOptions{QueueCapacity: 1, MaxPayloadBytes: 4, MaxQueueBytes: 4}
	id1, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, opts)
	if err != nil {
		t.Fatalf("Spawn first: %v", err)
	}
	id2, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, opts)
	if err != nil {
		t.Fatalf("Spawn second: %v", err)
	}
	if id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, opts); id != 0 || !errors.Is(err, ErrWorkerQuotaExceeded) {
		t.Fatalf("Spawn over quota = (%d, %v), want ErrWorkerQuotaExceeded", id, err)
	}
	if err := ext.workers.Kill(id1); err != nil {
		t.Fatalf("Kill first: %v", err)
	}
	_ = waitWorkerExit(t, exits)
	id3, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, opts)
	if err != nil {
		t.Fatalf("Spawn after release: %v", err)
	}
	for _, id := range []WorkerID{id2, id3} {
		if err := ext.workers.Kill(id); err != nil {
			t.Fatalf("Kill(%d): %v", id, err)
		}
	}
	_ = waitWorkerExit(t, exits)
	_ = waitWorkerExit(t, exits)
}

func TestWorkerQuotaReservationReleasedAfterSpawnFailure(t *testing.T) {
	exits := make(chan WorkerExitContext, 1)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime(WithWorkerLimits(WorkerLimits{MaxLiveWorkers: 1, MaxQueueBytes: 1 << 20}))
	if err := rt.Use(ext); err != nil {
		t.Fatalf("Use: %v", err)
	}
	nullMod, err := rt.Compile(workerModule([]byte{0x0b}, nil, nil))
	if err != nil {
		t.Fatalf("Compile null callback: %v", err)
	}
	nullParent, err := rt.Instantiate(nil, nullMod)
	if err != nil {
		t.Fatalf("Instantiate null callback: %v", err)
	}
	defer nullParent.Close()
	if id, err := ext.workers.Spawn(instanceHostModule{in: nullParent}, 0, WorkerOptions{}); id != 0 || err == nil {
		t.Fatalf("Spawn null callback = (%d, %v), want validation error", id, err)
	}

	validMod, err := rt.Compile(workerReceiveModule())
	if err != nil {
		t.Fatalf("Compile valid callback: %v", err)
	}
	validParent, err := rt.Instantiate(nil, validMod)
	if err != nil {
		t.Fatalf("Instantiate valid callback: %v", err)
	}
	defer validParent.Close()
	id, err := ext.workers.Spawn(instanceHostModule{in: validParent}, 0, WorkerOptions{})
	if err != nil {
		t.Fatalf("Spawn after failed reservation: %v", err)
	}
	if err := ext.workers.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	_ = waitWorkerExit(t, exits)
}

func TestWorkersConcurrentSpawnHonorsQuota(t *testing.T) {
	const (
		limit    = 4
		attempts = 16
	)
	exits := make(chan WorkerExitContext, limit)
	ext := &workerTestExt{onExit: func(ctx *WorkerExitContext) { exits <- *ctx }}
	rt := NewRuntime(WithWorkerLimits(WorkerLimits{MaxLiveWorkers: limit, MaxQueueBytes: limit << 10}))
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

	ids := make(chan WorkerID, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := ext.workers.Spawn(instanceHostModule{in: parent}, 0, WorkerOptions{QueueCapacity: 1, MaxPayloadBytes: 1, MaxQueueBytes: 1 << 10})
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
	var live []WorkerID
	for id := range ids {
		live = append(live, id)
	}
	if len(live) != limit {
		t.Fatalf("successful concurrent spawns = %d, want %d", len(live), limit)
	}
	for err := range errs {
		if !errors.Is(err, ErrWorkerQuotaExceeded) {
			t.Fatalf("concurrent Spawn error = %v, want ErrWorkerQuotaExceeded", err)
		}
	}
	for _, id := range live {
		if err := ext.workers.Kill(id); err != nil {
			t.Fatalf("Kill(%d): %v", id, err)
		}
	}
	for range live {
		_ = waitWorkerExit(t, exits)
	}
}

func TestWorkerLargeConcurrentSendDoesNotBlockStop(t *testing.T) {
	wr := &worker{
		queue:           make([]workerMessage, 1),
		maxPayloadBytes: 4 << 20,
		maxQueueBytes:   4 << 20,
		wake:            make(chan struct{}, 1),
	}
	payload := make([]byte, 4<<20)
	const senders = 4
	started := make(chan struct{}, senders)
	done := make(chan error, senders)
	for i := 0; i < senders; i++ {
		go func() {
			started <- struct{}{}
			done <- wr.enqueue(1, payload)
		}()
	}
	for i := 0; i < senders; i++ {
		<-started
	}
	if !wr.requestStop(ErrWorkerKilled) {
		t.Fatal("requestStop was not accepted")
	}
	for i := 0; i < senders; i++ {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, ErrWorkerStopping) && !errors.Is(err, ErrWorkerQueueFull) {
				t.Fatalf("enqueue = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("large concurrent Send did not complete after stop")
		}
	}
}

type workerHostScopeBenchExt struct{}

func (*workerHostScopeBenchExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "worker-host-scope-bench", Version: "1.0.0", Stability: Experimental}
}

func (*workerHostScopeBenchExt) Register(r *Registry) error {
	_, _ = NewWorkers(r)
	r.ImportModule("env").
		Func("f", HostFunc(func(_ HostModule, params, results []uint64) { results[0] = params[0] + 1 })).
		Params(ValI32).
		Results(ValI32)
	return nil
}

func BenchmarkRuntimeHostCallWithoutWorkers(b *testing.B) {
	rt := NewRuntime()
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(nil, mod, WithImports(Imports{
		"env.f": HostFunc(func(_ HostModule, params, results []uint64) { results[0] = params[0] + 1 }),
	}))
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = results
	}
}

func BenchmarkWorkerScopedHostCall(b *testing.B) {
	rt := NewRuntime()
	if err := rt.Use(&workerHostScopeBenchExt{}); err != nil {
		b.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(nil, mod)
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	defer rt.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := in.Invoke("g", I32(int32(i)))
		if err != nil {
			b.Fatal(err)
		}
		benchResultSink = results
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

	pending, err := NewWorkers(&Registry{info: ExtensionInfo{ID: "pending"}, hooks: &HookRegistry{}})
	if err != nil {
		t.Fatalf("NewWorkers pending: %v", err)
	}
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
