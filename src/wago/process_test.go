package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/testutil/wasmtest"
)

// impSpec describes one host-function import for procModule.
type impSpec struct {
	module, name string
	params       []byte
	results      []byte
}

// procModule builds a module importing the given host functions (as funcs 0..n-1)
// and exporting run():()->runResults (func n) with the given body. When memPages
// > 0 it declares and exports a linear memory of that many pages.
func procModule(t *testing.T, imports []impSpec, memPages int, runResults, body []byte) *Module {
	t.Helper()
	var types [][]byte
	for _, im := range imports {
		types = append(types, functypeBytes(im.params, im.results))
	}
	types = append(types, functypeBytes(nil, runResults)) // run's type, index len(imports)

	var importEntries [][]byte
	for i, im := range imports {
		e := append(wasmtest.Name(im.module), wasmtest.Name(im.name)...)
		e = append(e, 0x00)                        // func import
		e = append(e, wasmtest.ULEB(uint32(i))...) // type index i
		importEntries = append(importEntries, e)
	}
	runIdx := uint32(len(imports))

	secs := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntries...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(runIdx))), // run uses type index len(imports)
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, runIdx)}
	if memPages > 0 {
		memType := append([]byte{0x00}, wasmtest.ULEB(uint32(memPages))...)
		secs = append(secs, wasmtest.Section(5, wasmtest.Vec(memType)))
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	secs = append(secs, wasmtest.Section(7, wasmtest.Vec(exports...)))
	secs = append(secs, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))

	m, err := NewRuntime().Compile(wasmtest.Module(secs...))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}

func functypeBytes(params, results []byte) []byte {
	out := []byte{0x60}
	out = append(out, wasmtest.ULEB(uint32(len(params)))...)
	out = append(out, params...)
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	out = append(out, results...)
	return out
}

// classFor wraps a module in a single-instance-capable class.
func classFor(t *testing.T, rt *Runtime, mod *Module) *Class {
	t.Helper()
	c, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{MaxInstances: 8}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	return c
}

const i32b, i64b = byte(0x7f), byte(0x7e)

func TestSpawnSelfPID(t *testing.T) {
	rt := NewRuntime()
	// run() -> i64 = wago_process.self()
	mod := procModule(t, []impSpec{{"wago_process", "self", nil, []byte{i64b}}}, 0,
		[]byte{i64b}, []byte{0x10, 0x00, 0x0b}) // call 0; end
	class := classFor(t, rt, mod)
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	ev := <-mustMonitor(t, rt, pid)
	if !ev.Reason.Normal {
		t.Fatalf("exit reason = %s, want normal", ev.Reason)
	}
	if len(ev.Reason.Results) != 1 || PID(ev.Reason.Results[0].I64()) != pid {
		t.Fatalf("self() result = %v, want pid %d", ev.Reason.Results, pid)
	}
}

func TestSpawnPrepareAndReceiveMessage(t *testing.T) {
	rt := NewRuntime()
	// run() -> i32:
	//   prepare_receive(length_ptr=256, tag=0, timeout=-1)
	//   drop
	//   message.receive(ptr=0, len=i32.load(256))
	body := []byte{0x41}
	body = append(body, wasmtest.SLEB32(256)...) // i32.const 256 (length_ptr)
	body = append(body, 0x42, 0x00)             // i64.const 0 (untagged)
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(-1)...) // i64.const -1 (timeout: block)
	body = append(body, 0x10, 0x00)             // call 0 prepare_receive
	body = append(body, 0x1a)                   // drop status
	body = append(body, 0x41, 0x00)             // i32.const 0 (message dst)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(256)...) // i32.const 256
	body = append(body, 0x28, 0x02, 0x00)        // i32.load align=2 offset=0
	body = append(body, 0x10, 0x01, 0x0b)        // call 1 message.receive; end
	mod := procModule(t, []impSpec{
		{"wago_mailbox", "prepare_receive", []byte{i32b, i64b, i64b}, []byte{i32b}},
		{"wago_message", "receive", []byte{i32b, i32b}, []byte{i32b}},
	}, 1, []byte{i32b}, body)
	class := classFor(t, rt, mod)
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := rt.Send(context.Background(), pid, []byte("hi")); err != nil {
		t.Fatalf("send: %v", err)
	}
	ev := <-mustMonitor(t, rt, pid)
	if !ev.Reason.Normal || len(ev.Reason.Results) != 1 || ev.Reason.Results[0].I32() != statusOK {
		t.Fatalf("receive exit = %s results=%v, want normal status 0", ev.Reason, ev.Reason.Results)
	}
	// Sending to the exited process now fails.
	if err := rt.Send(context.Background(), pid, []byte("x")); err == nil {
		t.Fatal("expected send to exited process to fail")
	}
	if err := rt.Send(context.Background(), PID(99999), []byte("x")); !errors.Is(err, ErrNoProcess) {
		t.Fatalf("send to unknown pid = %v, want ErrNoProcess", err)
	}
}

// blockingReceiveModule builds run() -> i32 that blocks forever on prepare_receive.
func blockingReceiveModule(t *testing.T) *Module {
	body := []byte{0x41}
	body = append(body, wasmtest.SLEB32(256)...)
	body = append(body, 0x42, 0x00)
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(-1)...)
	body = append(body, 0x10, 0x00, 0x0b)
	return procModule(t, []impSpec{{"wago_mailbox", "prepare_receive", []byte{i32b, i64b, i64b}, []byte{i32b}}},
		1, []byte{i32b}, body)
}

func TestKillCooperative(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingReceiveModule(t))
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mon := mustMonitor(t, rt, pid)
	if err := rt.Kill(context.Background(), pid, ExitReason{}); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case ev := <-mon:
		if !ev.Reason.Killed {
			t.Fatalf("exit reason = %s, want killed", ev.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("killed process did not exit")
	}
}

func TestLinkPropagation(t *testing.T) {
	rt := NewRuntime()
	class := classFor(t, rt, blockingReceiveModule(t))
	defer class.Close()

	a, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err != nil {
		t.Fatalf("spawn a: %v", err)
	}
	b, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run", Links: []PID{a}})
	if err != nil {
		t.Fatalf("spawn b: %v", err)
	}
	monB := mustMonitor(t, rt, b)
	// Killing A (abnormal exit) must propagate and kill linked B.
	if err := rt.Kill(context.Background(), a, ExitReason{}); err != nil {
		t.Fatalf("kill a: %v", err)
	}
	select {
	case ev := <-monB:
		if !ev.Reason.Killed {
			t.Fatalf("linked B exit = %s, want killed", ev.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("linked process B did not exit after A was killed")
	}
}

func mustMonitor(t *testing.T, rt *Runtime, pid PID) <-chan ExitEvent {
	t.Helper()
	ch, err := rt.Monitor(context.Background(), pid)
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	return ch
}
