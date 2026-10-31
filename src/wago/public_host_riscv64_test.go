//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	goruntime "runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicHostModule(params, results []wasm.ValType) []byte {
	body := []byte{}
	for i := range params {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(i))...)
	}
	body = append(body, 0x10, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestRV64PublicSynchronousHostTypedCarriersArtifactAndSession(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, loaded := range []bool{false} {
		c, err := Compile(nil, rv64PublicHostModule([]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if !c.needsFuncRefContextHeader || c.codeCache.base != 0 {
			t.Fatal("host context/deferred publication")
		}
		if loaded {

			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			c = c
			defer c.Close()
			if !c.needsFuncRefContextHeader {
				t.Fatal("loaded context anchor")
			}
		}
		input := []uint64{0xfeedface80000001, 0x8123456789abcdef, 0xa5a5a5a57f800123, 0x7ff0000000000001}
		output := []uint64{0xabcdabcdfffffff7, 0xffedcba987654321, 0xeeeeeeee80000000, 0xfff8000000000123}
		calls := 0
		host := HostCallFunc(func(call HostCall) {
			calls++
			if call.ParamCount() != 4 || call.ResultCount() != 4 {
				panic("host shape")
			}
			for i, v := range input {
				if i == 0 || i == 2 {
					v = uint64(uint32(v))
				}
				if call.ParamSlots()[i] != v {
					panic("typed argument")
				}
				call.ResultSlots()[i] = output[i]
			}
		})
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", host)})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if !in.syncMode || len(in.funcRefDescs) != 40 || in.nativeContext == 0 {
			t.Fatal("host identity layout")
		}
		want := slices.Clone(output)
		want[0] = uint64(uint32(want[0]))
		want[2] = uint64(uint32(want[2]))
		got, err := in.Invoke("call", input...)
		if err != nil || !slices.Equal(got, want) || calls != 1 {
			t.Fatal(loaded, got, err, calls)
		}
		if alloc := testing.AllocsPerRun(50, func() {
			got, err = in.Invoke("call", input...)
			if err != nil || !slices.Equal(got, want) {
				panic("warm public host")
			}
		}); alloc != 0 {
			t.Fatal("warm host allocations", alloc)
		}
		fn, err := in.WasmFunc("call")
		if err != nil {
			t.Fatal(err)
		}
		got, err = fn.Invoke(input...)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("prepared host", got, err)
		}
		session, err := fn.OpenSession()
		if err != nil {
			t.Fatal(err)
		}
		got, err = session.Invoke4(input[0], input[1], input[2], input[3])
		session.Close()
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("host session", got, err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		goruntime.GC()
		got, err = in.Invoke("call", input...)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("live instance owner", got, err)
		}
	}
}

func TestRV64PublicSynchronousHostScalarShapesAndTrapReuse(t *testing.T) {
	for _, tc := range []struct {
		typ       wasm.ValType
		fn        any
		arg, want uint64
	}{
		{wasm.I32, func(v int32) int32 { return v + 7 }, 0xfeedfacefffffffe, 5},
		{wasm.I64, func(v int64) int64 { return v ^ 0x123456789 }, 0x8123456789abcdef, 0x81234566aaeeaa66},
		{wasm.F32, func(v float32) float32 { return math.Float32frombits(math.Float32bits(v) ^ 0x80000000) }, 0xdeadbeef7f800123, 0xff800123},
		{wasm.F64, func(v float64) float64 { return math.Float64frombits(math.Float64bits(v) ^ 0x8000000000000000) }, 0x7ff0000000000001, 0xfff0000000000001},
	} {
		c, err := Compile(nil, rv64PublicHostModule([]wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", tc.fn)})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("call", tc.arg)
		if err != nil || !slices.Equal(got, []uint64{tc.want}) {
			t.Fatal(tc.typ, got, tc.want, err)
		}
		in.Close()
		c.Close()
	}
	c, err := Compile(nil, rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	panicNext := true
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", func(v int32) int32 {
		if panicNext {
			panic("host sentinel")
		}
		return v + 1
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	func() {
		defer func() {
			if value := recover(); value != "host sentinel" {
				t.Fatal("ordinary host panic semantics", value)
			}
		}()
		in.Invoke("call", 3)
		t.Fatal("ordinary host panic did not propagate")
	}()
	panicNext = false
	if got, err := in.Invoke("call", 3); err != nil || !slices.Equal(got, []uint64{4}) {
		t.Fatal("host panic reuse", got, err)
	}
}

func rv64PublicHostSideEffectsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 1, 1, 2}),
		wasmtest.Section(6, []byte{1, 0x7f, 1, 0x41, 0, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("flag", 3, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x41, 0, 0x28, 2, 0, 0x6a, 0x0b}))),
	)
}

func TestRV64PublicSynchronousHostCancellationDoesNotResumeGuest(t *testing.T) {
	data := rv64PublicHostSideEffectsModule()
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", func(v int32) int32 { entered <- struct{}{}; <-release; return v + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		values []uint64
		err    error
	}
	done := make(chan outcome, 1)
	go func() { values, err := in.InvokeContext(ctx, "call", 7); done <- outcome{values, err} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("host callback did not park")
	}
	cancel()
	// Cancellation is delivered by the existing asynchronous watcher. Observe
	// its actual trap publication before releasing the parked callback; immediate
	// callback completion can legitimately win that scheduling race.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
		goruntime.Gosched()
	}
	if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
		unblock()
		t.Fatal("watcher did not publish interruption")
	}
	unblock()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || len(result.values) != 0 {
			t.Fatal("host cancellation", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled callback did not unwind")
	}
	if flag, err := in.Global("flag"); err != nil || flag != 0 {
		t.Fatal("cancelled host resumed guest write", flag, err)
	}
	if got, err := in.Invoke("call", 7); err != nil || !slices.Equal(got, []uint64{8}) {
		t.Fatal("post-cancel reuse", got, err)
	}
}

func TestRV64PublicSynchronousHostBindingAndArtifactGatesBeforeMapping(t *testing.T) {
	data := rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	producer, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	owner, err := Instantiate(producer, InstantiateOptions{Imports: testImports("env.host", func(v int32) int32 { return v })})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	export, err := owner.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []any{export, (*HostFuncRef)(nil), I32HostEvent(func(int32) {}), func(int64) int64 { return 0 }, nil} {
		c, err := Compile(nil, data)
		if err != nil {
			t.Fatal(err)
		}
		imports := NewImports()
		testSetImport(imports, "env.host", binding)
		in, err := Instantiate(c, InstantiateOptions{Imports: imports})
		if in != nil || err == nil || c.codeCache.base != 0 || len(c.codeCache.mem) != 0 {
			t.Fatal("invalid binding published code", binding, err)
		}
		c.Close()
	}
	view := *producer.executionView()
	view.needsFuncRefContextHeader = false
	if err := view.validate(); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("artifact missing host anchor", err)
	}
	wide := make([]wasm.ValType, railshot.MaxVectorFunctionValues+1)
	for i := range wide {
		wide[i] = wasm.I32
	}
	if c, err := Compile(nil, rv64PublicHostModule(wide, wide)); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("public host arity", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := NewRuntime()
	defer rt.Close()
	m, err := rt.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	if in, err := rt.Instantiate(ctx, m, WithImports(testImports("env.host", func(v int32) int32 { return v }))); in != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled instantiate", err)
	}
}

func TestRV64PublicSynchronousHostFrozenMetadataBindingPolicy(t *testing.T) {
	data := rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	producer, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	in, err := Instantiate(producer, InstantiateOptions{Imports: testImports("env.host", func(v int32) int32 { return v })})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	export, err := in.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	ref, err := rt.NewHostFuncRef(func(v int32) int32 { return v }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer ref.Close()
	for _, loaded := range []bool{false} {
		for i, binding := range []any{func(v int32) int32 { return v + 1 }, export, ref, I32HostEvent(func(int32) {})} {
			c, err := Compile(nil, data)
			if err != nil {
				t.Fatal(err)
			}
			if loaded {

				c.Close()
				c = c
			}
			// Source and loader publication already froze authoritative metadata.
			// Public container edits cannot bypass policy or reject valid bindings.
			c.NumImports = 0
			c.Imports = nil
			c.Funcs = nil
			// Artifact loading already owns an RW image. Binding rejection must
			// leave that image unsealed and acquire no instance/code reference.
			beforeBase, beforeBytes, beforeRefs := c.codeCache.base, len(c.codeCache.mem), c.codeCache.refs
			if c.codeCache.sealed {
				t.Fatal("binding fixture already callable")
			}
			instance, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.host", binding)})
			if i == 0 || i == 1 {
				if err != nil {
					t.Fatal("frozen supported binding", loaded, err)
				}
				got, err := instance.Invoke("call", 8)
				want := uint64(9)
				if i == 1 {
					want = 8
				}
				if err != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatal(got, err)
				}
				instance.Close()
			} else if instance != nil || err == nil || (i != 2 && !errors.Is(err, railshot.ErrUnsupported)) || c.codeCache.base != beforeBase || len(c.codeCache.mem) != beforeBytes || c.codeCache.refs != beforeRefs || c.codeCache.sealed {
				t.Fatal("mutable metadata bypassed frozen binding policy", loaded, i, err)
			}
			c.Close()
		}
	}
}

func TestRV64PublicSynchronousHostCallbackAccessesCallerAndInstanceState(t *testing.T) {
	c, err := Compile(nil, rv64PublicHostSideEffectsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	callback := CallerHostCallFunc(func(caller Caller, call HostCall) {
		if _, err := in.GlobalValue("flag"); err != nil {
			panic(err)
		}
		if err := in.SetGlobalValue("flag", ValueI32(11)); err != nil {
			panic(err)
		}
		if value, err := in.GlobalValue("flag"); err != nil || value.I32() != 11 {
			panic("callback global access")
		}
		memory := caller.Memory()
		if len(memory) != 65536 {
			panic("caller memory")
		}
		binary.LittleEndian.PutUint32(memory, 42)
		goruntime.GC()
		call.SetI32(0, call.I32(0)+1)
	})
	in, err = Instantiate(c, InstantiateOptions{Imports: testImports("env.host", callback)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("call", 7); err != nil || !slices.Equal(got, []uint64{50}) {
		t.Fatal("caller state after host resume", got, err)
	}
	if value, err := in.Global("flag"); err != nil || value != 8 {
		t.Fatal("guest global after callback", value, err)
	}
	if value, ok := in.ReadUint32Le(0); !ok || value != 42 {
		t.Fatal("caller memory write", value, ok)
	}
}

func TestRV64PublicSynchronousHostCloseWhileCallbackParked(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x0b}))),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	// The host-owned imported cell remains readable after instance teardown;
	// an instance-exported Global.Get would return zero once its owner closed.
	flag := NewGlobalI32(0, true)
	defer flag.Close()
	imports := testImports("env.host", func(v int32) int32 { close(entered); <-release; return v + 1 })
	imports.Global("env", "flag", flag)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() { _, err := in.Invoke("call", 7); callDone <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("close host did not park")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- in.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			unblock()
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("close blocked behind parked callback")
	}
	unblock()
	select {
	case err := <-callDone:
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
			t.Fatal("close interruption", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closed callback did not unwind")
	}
	if value := flag.Get(); value != 0 || flag.owner.closed || len(flag.cell) < 8 {
		t.Fatal("close resumed guest store", value)
	}
	if _, err := in.Invoke("call", 7); err == nil {
		t.Fatal("closed instance callable")
	}
}
