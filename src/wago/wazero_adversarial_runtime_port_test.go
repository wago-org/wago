//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestWazeroPortCallArityAndPreparedStack(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	in := mustInstantiateWazeroAdversarial(t, watToWasmCA(t, `(module
  (func (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add))`), nil)
	defer in.Close()
	if _, err := in.Invoke("add"); err == nil || !strings.Contains(err.Error(), "expects 2 arg slot(s), got 0") {
		t.Fatalf("zero-argument call error = %v", err)
	}
	if _, err := in.Invoke("add", I32(1), I32(2), I32(3)); err == nil || !strings.Contains(err.Error(), "expects 2 arg slot(s), got 3") {
		t.Fatalf("three-argument call error = %v", err)
	}
	if got, err := in.Invoke("add", I32(20), I32(22)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("add(20,22) = %v, %v; want 42", got, err)
	}
	prepared, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := prepared.Invoke(I32(40), I32(2)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("prepared add(40,2) = %v, %v; want 42", got, err)
	}
}

func TestWazeroPortImportedMutableGlobalUpdate(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	rt := NewRuntime()
	defer rt.Close()
	providerCode, err := rt.Compile(watToWasmCA(t, `(module
  (global (export "g") (mut i32) (i32.const 1)))`))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := rt.Instantiate(context.Background(), providerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode, err := rt.Compile(watToWasmCA(t, `(module
  (import "env" "g" (global (mut i32)))
  (func (export "update") (result i32)
    global.get 0
    i32.const 2
    global.set 0)
  (export "g" (global 0)))`))
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerCode, WithImports(Imports{"env.g": global}))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	got, err := consumer.Invoke("update")
	if err != nil || len(got) != 1 || AsI32(got[0]) != 1 {
		t.Fatalf("update = %v, %v; want previous value 1", got, err)
	}
	for name, in := range map[string]*Instance{"provider": provider, "consumer re-export": consumer} {
		v, err := in.GlobalValue("g")
		if err != nil || v.Type() != ValI32 || v.I32() != 2 {
			t.Fatalf("%s global = %v, %v; want i32(2)", name, v, err)
		}
	}
	reexport, err := consumer.ExportedGlobalObject("g")
	if err != nil || reexport != global {
		t.Fatalf("consumer re-export = %p, %v; want shared global %p", reexport, err, global)
	}
}

func TestWazeroPortCallImportedHostFunctionIndirectly(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	calls := 0
	in := mustInstantiateWazeroAdversarial(t, watToWasmCA(t, `(module
  (type $host-type (func (param i32) (result i32)))
  (import "env" "host" (func $host (type $host-type)))
  (table 1 funcref)
  (elem (i32.const 0) $host)
  (func (export "call") (param i32) (result i32)
    local.get 0
    i32.const 0
    call_indirect (type $host-type)))`), Imports{"env.host": HostFunc(func(_ HostModule, params, results []uint64) {
		calls++
		results[0] = I32(AsI32(params[0]) + 1)
	})})
	defer in.Close()
	got, err := in.Invoke("call", I32(41))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 || calls != 1 {
		t.Fatalf("indirect host call = %v, %v, calls %d; want 42, nil, 1", got, err, calls)
	}
}

func TestWazeroPortMemoryGrowThroughHostReentry(t *testing.T) {
	type0 := wasmtest.FuncType(nil, nil)
	imp := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	imp = append(imp, 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(type0)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x0a})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("main", 0, 1),
			wasmtest.ExportEntry("grow", 0, 2),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x41, 0x00, 0x28, 0x02, 0x00, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x1a, 0x0b}),
		)),
	)
	compiled := MustCompile(mod)
	defer compiled.Close()
	var in *Instance
	calls := 0
	var nestedErr error
	in, nestedErr = Instantiate(compiled, Imports{"env.reenter": HostFunc(func(_ HostModule, _, _ []uint64) {
		calls++
		_, nestedErr = in.Invoke("grow")
	})})
	if nestedErr != nil {
		t.Fatalf("instantiate: %v", nestedErr)
	}
	defer in.Close()
	if _, err := in.Invoke("main"); err != nil {
		t.Fatalf("main: %v", err)
	}
	if nestedErr != nil || calls != 1 {
		t.Fatalf("nested grow = calls %d err %v, want one successful call", calls, nestedErr)
	}
	if got := len(in.Memory().Bytes()); got != 2*65536 {
		t.Fatalf("memory length after nested grow = %d, want %d", got, 2*65536)
	}
}

func TestWazeroPortMemoryViewRemainsCoherentAcrossGrow(t *testing.T) {
	types := wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	)
	mod := wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("store", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("load", 0, 2),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0x36, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b}),
		)),
	)
	in := mustInstantiateWazeroAdversarial(t, mod, nil)
	defer in.Close()
	old := in.Memory().Bytes()
	if _, err := in.Invoke("store", I32(0x11223344)); err != nil {
		t.Fatal(err)
	}
	if old[0] != 0x44 || old[3] != 0x11 {
		t.Fatalf("old host view did not observe wasm store: %x", old[:4])
	}
	if got, err := in.Invoke("grow", I32(1)); err != nil || len(got) != 1 || AsI32(got[0]) != 1 {
		t.Fatalf("grow = %v, %v; want old size 1", got, err)
	}
	if got := len(in.Memory().Bytes()); got != 2*65536 {
		t.Fatalf("grown memory length = %d", got)
	}
	old[0] = 9
	if got, err := in.Invoke("load"); err != nil || len(got) != 1 || AsI32(got[0]) != 0x11223309 {
		t.Fatalf("load after old-view write = %v, %v; want %#x", got, err, uint32(0x11223309))
	}
}

func requireWazeroInterruptedTrap(t *testing.T, err error) {
	t.Helper()
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapInterrupted {
		t.Fatalf("error = %v, want interrupted trap", err)
	}
}

func TestWazeroPortCloseWhileHostCallInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mod := wazeroBlockingImportModule()
	in := mustInstantiateWazeroAdversarial(t, mod, Imports{"env.block": HostFunc(func(_ HostModule, params, results []uint64) {
		once.Do(func() { close(entered) })
		<-release
		results[0] = params[0]
	})})

	callDone := make(chan error, 1)
	go func() {
		got, err := in.Invoke("call", I32(7))
		if err == nil && (len(got) != 1 || AsI32(got[0]) != 7) {
			err = errors.New("in-flight call returned the wrong value")
		}
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host call did not enter")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- in.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close during in-flight call: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked behind the in-flight host call")
	}
	close(release)
	select {
	case err := <-callDone:
		requireWazeroInterruptedTrap(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight call did not finish")
	}
	if _, err := in.Invoke("call", I32(8)); err == nil {
		t.Fatal("closed instance remained callable")
	}
}

func TestWazeroPortNestedHostPanicDoesNotCorruptRuntime(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	rt := NewRuntime()
	defer rt.Close()
	producerCode, err := rt.Compile(wazeroImportForwardModule("panic"))
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), producerCode, WithImports(Imports{"env.panic": HostFunc(func(_ HostModule, params, results []uint64) {
		if AsI32(params[0]) == 0 {
			panic(errors.New("wazero-port-host-panic"))
		}
		results[0] = I32(AsI32(params[0]) + 1)
	})}))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	target, err := producer.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode, err := rt.Compile(wazeroForwardingImportModule())
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerCode, WithImports(Imports{"env.target": target}))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()

	func() {
		defer func() {
			r := recover()
			if err, ok := r.(error); !ok || err.Error() != "wazero-port-host-panic" {
				t.Fatalf("nested host panic = %T %v, want exact sentinel error", r, r)
			}
		}()
		_, _ = consumer.Invoke("call", I32(0))
	}()
	if got, err := consumer.Invoke("call", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("call after nested host panic = %v, %v; want 42", got, err)
	}
}

func TestWazeroPortCloseTableOwnerOrWriterKeepsEntriesCallable(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	const tableOwnerWAT = `(module
  (type $entry (func (result i32)))
  (type $caller (func (param i32) (result i32)))
  (table (export "t") 10 funcref)
  (func (export "call") (type $caller) (param i32) (result i32)
    local.get 0
    call_indirect (type $entry)))`
	const tableWriterWAT = `(module
  (type $entry (func (result i32)))
  (import "env" "t" (table 10 funcref))
  (func $one (type $entry) (result i32) i32.const 1)
  (func $two (type $entry) (result i32) i32.const 2)
  (elem (i32.const 5) $one $two))`
	const initializedOwnerWAT = `(module
  (type $entry (func (result i32)))
  (table (export "t") 10 funcref)
  (func $one (type $entry) (result i32) i32.const 1)
  (func $two (type $entry) (result i32) i32.const 2)
  (elem (i32.const 5) $one $two))`
	const tableConsumerWAT = `(module
  (type $entry (func (result i32)))
  (import "env" "t" (table 10 funcref))
  (func (export "call") (param i32) (result i32)
    local.get 0
    call_indirect (type $entry)))`

	assertCalls := func(t *testing.T, caller *Instance) {
		t.Helper()
		for i := 0; i < 10; i++ {
			if _, err := caller.Invoke("call", I32(0)); err == nil {
				t.Fatal("null table entry remained callable")
			} else {
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapIndirectOutOfBounds {
					t.Fatalf("null table call = %v, want indirect out-of-bounds trap", err)
				}
			}
			for index, want := range map[int]int32{5: 1, 6: 2} {
				got, err := caller.Invoke("call", I32(int32(index)))
				if err != nil || len(got) != 1 || AsI32(got[0]) != want {
					t.Fatalf("call(%d) = %v, %v; want %d", index, got, err, want)
				}
			}
			runtime.GC()
		}
	}

	t.Run("close table owner", func(t *testing.T) {
		rt := NewRuntime()
		defer rt.Close()
		owner, err := rt.Instantiate(context.Background(), mustCompileWazeroAdversarial(t, rt, initializedOwnerWAT))
		if err != nil {
			t.Fatal(err)
		}
		table, err := owner.ExportedTable("t")
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := rt.Instantiate(context.Background(), mustCompileWazeroAdversarial(t, rt, tableConsumerWAT), WithImports(Imports{"env.t": table}))
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		assertCalls(t, consumer)
	})

	t.Run("close table writer", func(t *testing.T) {
		rt := NewRuntime()
		defer rt.Close()
		owner, err := rt.Instantiate(context.Background(), mustCompileWazeroAdversarial(t, rt, tableOwnerWAT))
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		table, err := owner.ExportedTable("t")
		if err != nil {
			t.Fatal(err)
		}
		writer, err := rt.Instantiate(context.Background(), mustCompileWazeroAdversarial(t, rt, tableWriterWAT), WithImports(Imports{"env.t": table}))
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		assertCalls(t, owner)
	})
}

func TestWazeroPortCloseInterruptsInfiniteInvocation(t *testing.T) {
	if !requireExternalWAT(t) {
		return
	}
	entered := make(chan struct{})
	in := mustInstantiateWazeroAdversarial(t, watToWasmCA(t, `(module
  (import "env" "entered" (func $entered))
  (func (export "infinite_loop")
    call $entered
    loop $again
      br $again
    end))`), Imports{"env.entered": HostFunc(func(_ HostModule, _, _ []uint64) { close(entered) })})
	callDone := make(chan error, 1)
	go func() {
		_, err := in.Invoke("infinite_loop")
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("infinite invocation did not enter")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-callDone:
		requireWazeroInterruptedTrap(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not interrupt the infinite invocation")
	}
}

func TestWazeroPortHostCallbackClosesCallingModules(t *testing.T) {
	for _, tc := range []struct {
		name          string
		closeProducer bool
		closeConsumer bool
	}{
		{name: "calling module", closeConsumer: true},
		{name: "imported module", closeProducer: true},
		{name: "both", closeProducer: true, closeConsumer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			var producer, consumer *Instance
			var closeErr error
			producerCode, err := rt.Compile(wazeroBlockingImportModule())
			if err != nil {
				t.Fatal(err)
			}
			producer, err = rt.Instantiate(context.Background(), producerCode, WithImports(Imports{"env.block": HostFunc(func(_ HostModule, params, results []uint64) {
				if tc.closeConsumer {
					closeErr = errors.Join(closeErr, consumer.Close())
				}
				if tc.closeProducer {
					closeErr = errors.Join(closeErr, producer.Close())
				}
				results[0] = params[0]
			})}))
			if err != nil {
				t.Fatal(err)
			}
			target, err := producer.ExportedFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			consumerCode, err := rt.Compile(wazeroForwardingImportModule())
			if err != nil {
				t.Fatal(err)
			}
			consumer, err = rt.Instantiate(context.Background(), consumerCode, WithImports(Imports{"env.target": target}))
			if err != nil {
				t.Fatal(err)
			}
			got, err := consumer.Invoke("call", I32(5))
			if closeErr != nil {
				t.Fatalf("callback Close: %v", closeErr)
			}
			if tc.closeConsumer {
				requireWazeroInterruptedTrap(t, err)
			} else if err != nil || len(got) != 1 || AsI32(got[0]) != 5 {
				t.Fatalf("call while only imported module closes = %v, %v; want 5", got, err)
			}
			if tc.closeConsumer {
				if _, err := consumer.Invoke("call", I32(6)); err == nil {
					t.Fatal("callback-closed consumer remained callable")
				}
			}
			if tc.closeProducer {
				if _, err := producer.Invoke("call", I32(6)); err == nil {
					t.Fatal("callback-closed producer remained callable")
				}
			}
			_ = consumer.Close()
			_ = producer.Close()
		})
	}
}

func TestWazeroPortCloseWhilePreparedCallInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	in := mustInstantiateWazeroAdversarial(t, wazeroBlockingImportModule(), Imports{"env.block": HostFunc(func(_ HostModule, params, results []uint64) {
		close(entered)
		<-release
		results[0] = params[0]
	})})
	prepared, err := in.PrepareFunction("call")
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() {
		got, err := prepared.Invoke(I32(17))
		if err == nil && (len(got) != 1 || AsI32(got[0]) != 17) {
			err = errors.New("prepared in-flight call returned the wrong value")
		}
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("prepared host call did not enter")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-callDone:
		requireWazeroInterruptedTrap(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("prepared call did not finish")
	}
	if _, err := prepared.Invoke(I32(18)); err == nil {
		t.Fatal("prepared function remained callable after close")
	}
}

func TestWazeroPortCloseImportedOrImportingModuleWhileCallInFlight(t *testing.T) {
	for _, tc := range []struct {
		name          string
		closeProducer bool
		closeConsumer bool
	}{
		{name: "importing", closeConsumer: true},
		{name: "imported", closeProducer: true},
		{name: "both", closeProducer: true, closeConsumer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			rt := NewRuntime()
			defer rt.Close()
			producerCode, err := rt.Compile(wazeroBlockingImportModule())
			if err != nil {
				t.Fatal(err)
			}
			producer, err := rt.Instantiate(context.Background(), producerCode, WithImports(Imports{"env.block": HostFunc(func(_ HostModule, params, results []uint64) {
				once.Do(func() { close(entered) })
				<-release
				results[0] = params[0]
			})}))
			if err != nil {
				t.Fatal(err)
			}
			target, err := producer.ExportedFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			consumerCode, err := rt.Compile(wazeroForwardingImportModule())
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerCode, WithImports(Imports{"env.target": target}))
			if err != nil {
				t.Fatal(err)
			}

			callDone := make(chan error, 1)
			go func() {
				got, err := consumer.Invoke("call", I32(11))
				if err == nil && (len(got) != 1 || AsI32(got[0]) != 11) {
					err = errors.New("cross-instance in-flight call returned the wrong value")
				}
				callDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("cross-instance host call did not enter")
			}
			if tc.closeConsumer {
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if tc.closeProducer {
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case err := <-callDone:
				if tc.closeConsumer {
					requireWazeroInterruptedTrap(t, err)
				} else if err != nil {
					t.Fatalf("call while only imported module closes: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cross-instance in-flight call did not finish")
			}
			_ = consumer.Close()
			_ = producer.Close()
		})
	}
}

// This is the bounded-memory half of wazero's huge-binary regression: it keeps
// the 40,000-function index/relocation layout while using small bodies. The
// arm64 backend's separate TestPatchCallRelocsRangeChecks pins BL displacement
// boundaries without allocating the upstream test's hundreds-of-megabytes image.
func TestWazeroPortManyFunctionRelocationLayout(t *testing.T) {
	const functionCount = 40000
	const additions = 8
	addBody := make([]byte, 0, additions*7+3)
	var want uint32
	for i := 0; i < additions; i++ {
		addBody = append(addBody, 0x20, 0x00, 0x41)
		addBody = append(addBody, wasmtest.SLEB32(int32(i))...)
		addBody = append(addBody, 0x6a, 0x21, 0x00)
		want += uint32(i)
	}
	addBody = append(addBody, 0x20, 0x00, 0x0b)

	funcs := make([][]byte, functionCount)
	codes := make([][]byte, functionCount)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(0)
	}
	first := []byte{0x20, 0x00, 0x10}
	first = append(first, wasmtest.ULEB(functionCount-2)...)
	first = append(first, 0x0b)
	funcsBody := wasmtest.Code(addBody)
	codes[0] = wasmtest.Code(first)
	for i := 1; i < functionCount-1; i++ {
		codes[i] = funcsBody
	}
	codes[functionCount-1] = wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b})
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("first", 0, 0),
			wasmtest.ExportEntry("last", 0, functionCount-1),
		)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	in := mustInstantiateWazeroAdversarial(t, mod, nil)
	defer in.Close()
	for _, export := range []string{"first", "last"} {
		got, err := in.Invoke(export, I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != want {
			t.Fatalf("%s(0) = %v, %v; want %d", export, got, err, want)
		}
	}
}

func TestWazeroPortRepeatedRuntimeCompileInstantiateDoesNotRetainHeap(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	run := func(n int) {
		for i := 0; i < n; i++ {
			rt := NewRuntime()
			ref, err := rt.NewExternRef(i)
			if err != nil {
				t.Fatal(err)
			}
			global, err := rt.NewExternRefGlobal(ref, true)
			if err != nil {
				t.Fatal(err)
			}
			table, err := rt.NewExternRefTable(8, 16)
			if err != nil {
				t.Fatal(err)
			}
			owners := make([]*HostFuncRef, 32)
			for j := range owners {
				owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, _ []uint64) {}), FuncSig{})
				if err != nil {
					t.Fatal(err)
				}
				owners[j] = owner
			}
			compiled, err := rt.Compile(mod)
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), compiled)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("f"); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
				t.Fatalf("iteration %d f() = %v, %v", i, got, err)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
			for _, owner := range owners {
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := global.Close(); err != nil {
				t.Fatal(err)
			}
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	run(8)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	run(200)
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc+8<<20 {
		t.Fatalf("retained heap grew from %d to %d bytes", before.HeapAlloc, after.HeapAlloc)
	}
}

func wazeroBlockingImportModule() []byte {
	return wazeroImportForwardModule("block")
}

func wazeroForwardingImportModule() []byte {
	return wazeroImportForwardModule("target")
}

func wazeroImportForwardModule(name string) []byte {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(wasmtest.Name("env"), wasmtest.Name(name)...)
	imp = append(imp, 0x00, 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
}

func mustCompileWazeroAdversarial(t *testing.T, rt *Runtime, wat string) *Module {
	t.Helper()
	compiled, err := rt.Compile(watToWasmCA(t, wat))
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func mustInstantiateWazeroAdversarial(t *testing.T, mod []byte, imports Imports) *Instance {
	t.Helper()
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	return in
}
