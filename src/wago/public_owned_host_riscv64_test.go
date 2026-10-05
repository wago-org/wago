//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicOwnedHostScalarCarriersAndLifetime(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, loaded := range []bool{false, true} {
		rt := NewRuntime()
		defer rt.Close()
		c, err := Compile(nil, rv64PublicHostModule([]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}))
		if err != nil {
			t.Fatal(err)
		}
		if loaded {
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			copy, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			c.Close()
			c = copy
		}
		defer c.Close()
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		input := []uint64{0xeeeeeeee80000001, 0x8123456789abcdef, 0xeeeeeeee7f800123, 0x7ff0000000000001}
		output := []uint64{0xeeeeeeeefffffff7, 0xffedcba987654321, 0xeeeeeeee80000000, 0xfff8000000000123}
		calls := 0
		owner, err := rt.NewHostFuncRef(HostCallFunc(func(call HostCall) {
			calls++
			for i, v := range input {
				if i == 0 || i == 2 {
					v = uint64(uint32(v))
				}
				if call.ParamSlots()[i] != v {
					panic("owned typed argument")
				}
				call.ResultSlots()[i] = output[i]
			}
		}), FuncSig{Params: []ValType{ValI32, ValI64, ValF32, ValF64}, Results: []ValType{ValI32, ValI64, ValF32, ValF64}})
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		instances := make([]*Instance, 2)
		want := slices.Clone(output)
		want[0], want[2] = uint64(uint32(want[0])), uint64(uint32(want[2]))
		for i := range instances {
			instances[i], err = rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", owner)))
			if err != nil {
				t.Fatal(err)
			}
			defer instances[i].Close()
			if !instances[i].syncMode || len(instances[i].thunkMem) == 0 {
				t.Fatal("missing private owned thunk")
			}
			got, err := instances[i].Invoke("call", input...)
			if err != nil || !slices.Equal(got, want) {
				t.Fatal(got, err)
			}
		}
		if &instances[0].thunkMem[0] == &instances[1].thunkMem[0] || instances[0].nativeContext == instances[1].nativeContext {
			t.Fatal("private ownership alias")
		}
		if state := loadHostThunkCacheState(instances[0].c); state.bytes != [2]int{} {
			t.Fatal("owned import populated ordinary cache", state)
		}
		owner.mu.Lock()
		count := owner.importers
		owner.mu.Unlock()
		if count != 2 || calls != 2 {
			t.Fatal("importer/callback count", count, calls)
		}
		if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
			t.Fatal("closed live owner", err)
		}
		if err := mod.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		goruntime.GC()
		if got, err := instances[0].Invoke("call", input...); err != nil || !slices.Equal(got, want) {
			t.Fatal("closed compiled owner", got, err)
		}
		instances[0].Close()
		if len(instances[0].thunkMem) != 0 {
			t.Fatal("private thunk retained after final instance release")
		}
		if err := owner.Close(); err == nil {
			t.Fatal("remaining importer did not retain owner")
		}
		if got, err := instances[1].Invoke("call", input...); err != nil || !slices.Equal(got, want) {
			t.Fatal("second instance lost owner", got, err)
		}
		instances[1].Close()
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		owner.mu.Lock()
		count, closed := owner.importers, owner.closed
		owner.mu.Unlock()
		if count != 0 || !closed || len(instances[1].thunkMem) != 0 {
			t.Fatal("owner/thunk teardown", count, closed)
		}
	}
}

func TestRV64PublicOwnedHostInvalidBindingBeforeMapping(t *testing.T) {
	rt, foreign := NewRuntime(), NewRuntime()
	defer rt.Close()
	defer foreign.Close()
	validSig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	newOwner := func(runtime *Runtime, sig FuncSig) *HostFuncRef {
		h, err := runtime.NewHostFuncRef(HostCallFunc(func(HostCall) {}), sig)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.Close() })
		return h
	}
	closed := newOwner(rt, validSig)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for name, owner := range map[string]*HostFuncRef{
		"nil": nil, "closed": closed, "foreign": newOwner(foreign, validSig),
		"signature": newOwner(rt, FuncSig{Params: []ValType{ValI64}, Results: []ValType{ValI64}}),
	} {
		t.Run(name, func(t *testing.T) {
			mod, err := rt.Compile(rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			c := mod.Compiled()
			if c.codeCache.base != 0 {
				t.Fatal("already mapped fixture")
			}
			in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", owner)))
			if in != nil || err == nil || c.codeCache.base != 0 || len(c.codeCache.mem) != 0 || c.codeCache.refs != 0 {
				t.Fatal("invalid owner published code", in, err)
			}
			if owner != nil {
				owner.mu.Lock()
				count := owner.importers
				owner.mu.Unlock()
				if count != 0 {
					t.Fatal("failed binding retained importer", count)
				}
			}
		})
	}
}

func TestRV64PublicOwnedHostDuplicateImportsAndMaximumSlots(t *testing.T) {
	for _, n := range []int{0, 1, 64} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			params, results := make([]wasm.ValType, n), make([]wasm.ValType, n)
			sig := FuncSig{Params: make([]ValType, n), Results: make([]ValType, n)}
			args, want := make([]uint64, n), make([]uint64, n)
			for i := range params {
				params[i], results[i] = wasm.I64, wasm.I64
				sig.Params[i], sig.Results[i] = ValI64, ValI64
				args[i], want[i] = uint64(i+1), uint64(i+3)
			}
			body := []byte{}
			for i := range params {
				body = append(body, 0x20)
				body = append(body, wasmtest.ULEB(uint32(i))...)
			}
			body = append(body, 0x10, 0, 0x10, 1, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
				wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("a")...), 0, 0), append(append(wasmtest.Name("env"), wasmtest.Name("b")...), 0, 0))),
				wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			mod, err := rt.Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			calls := 0
			owner, err := rt.NewHostFuncRef(HostCallFunc(func(call HostCall) {
				calls++
				for i, v := range call.ParamSlots() {
					call.ResultSlots()[i] = v + 1
				}
			}), sig)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.a", owner, "env.b", owner)))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("call", args...)
			if err != nil || !slices.Equal(got, want) || calls != 2 {
				t.Fatal(got, want, calls, err)
			}
			owner.mu.Lock()
			count := owner.importers
			owner.mu.Unlock()
			if count != 1 {
				t.Fatal("duplicate retained twice", count)
			}
			in.Close()
			if err := owner.Close(); err != nil {
				t.Fatal("duplicate detach", err)
			}
		})
	}
}

func TestRV64PublicOwnedHostTrapReuseAndNestedEntry(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	rt := NewRuntime()
	defer rt.Close()
	innerMod, err := rt.Compile(rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	if err != nil {
		t.Fatal(err)
	}
	defer innerMod.Close()
	inner, err := rt.Instantiate(context.Background(), innerMod, WithImports(testImports("env.host", func(v int32) int32 { return v + 2 })))
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	mod, err := rt.Compile(rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	wantErr := errors.New("owned host trap")
	fail := true
	var in *Instance
	owner, err := rt.NewHostFuncRef(CallerHostCallFunc(func(caller Caller, call HostCall) {
		if fail {
			panic(HostTrap{Err: wantErr})
		}
		got, err := inner.Invoke("call", uint64(uint32(call.I32(0))))
		if err != nil {
			panic(err)
		}
		goruntime.GC()
		if !caller.valid() || caller.in != in {
			panic("wrong owned caller identity")
		}
		call.SetI32(0, int32(got[0])+3)
	}), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	in, err = rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", owner)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("call", 7); got != nil || !errors.Is(err, wantErr) {
		t.Fatal("owned host trap", got, err)
	}
	fail = false
	if got, err := in.Invoke("call", 7); err != nil || !slices.Equal(got, []uint64{12}) {
		t.Fatal("owned nested reuse", got, err)
	}
}

func TestRV64PublicOwnedHostPublicationRollbackAndWarmReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	indexes := mod.Compiled().ensureCompileIndexes()
	failure := errors.New("owned private publication failure")
	attempts := 0
	indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) { attempts++; return nil, 0, failure }
	defer func() { indexes.mapThunkCode = nil }()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", owner)))
	if in != nil || !errors.Is(err, failure) || attempts != 1 {
		t.Fatal("private publication failure", in, err, attempts)
	}
	owner.mu.Lock()
	count := owner.importers
	owner.mu.Unlock()
	if count != 0 || mod.Compiled().codeCache.refs != 0 || len(indexes.failedMappings) != 0 {
		t.Fatal("failed construction retained owner/code", count)
	}
	if _, ok := owner.dispatchBinding(mod.Compiled().executionView(), 0); ok {
		t.Fatal("failed construction retained exact dispatch binding")
	}
	indexes.mapThunkCode = nil
	in, err = rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", owner)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("call", 7); err != nil || !slices.Equal(got, []uint64{8}) {
		t.Fatal("reuse after failure", got, err)
	}
	allocs := testing.AllocsPerRun(50, func() {
		got, err := in.Invoke("call", 7)
		if err != nil || len(got) != 1 || got[0] != 8 {
			panic("owned warm call")
		}
	})
	// Owned dispatch uses the shared callback-scoped capability path. Keep
	// its bounded allocation budget while the ordinary scalar lane stays zero.
	t.Logf("owned warm allocations per invocation: %g", allocs)
	if allocs > 2 {
		t.Fatal("owned warm allocation budget", allocs)
	}
	in.Close()
	if err := owner.Close(); err != nil {
		t.Fatal("rollback/detach owner", err)
	}
}

func TestRV64PublicOwnedHostCloseWhileCallbackParked(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(rv64PublicOwnedHostCloseModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	global := NewGlobalI32(0, true)
	defer global.Close()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { entered <- struct{}{}; <-release; return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(ownedHostImports(global, owner)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	done := make(chan error, 1)
	go func() { _, err := in.Invoke("call", 7); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("owned callback did not park")
	}
	if err := owner.Close(); err == nil {
		t.Fatal("owner closed during callback")
	}
	closed := make(chan error, 1)
	go func() { closed <- in.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logical Close blocked on owned callback")
	}
	owner.mu.Lock()
	count := owner.importers
	owner.mu.Unlock()
	if count != 1 || len(in.thunkMem) == 0 {
		t.Fatal("parked native lifetime released early", count)
	}
	if err := owner.Close(); err == nil {
		t.Fatal("parked importer did not retain owner after logical Close")
	}
	unblock()
	select {
	case err := <-done:
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
			t.Fatal("parked Close trap", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned invocation did not finish")
	}
	if global.Get() != 0 || global.owner.closed || len(global.cell) < 8 {
		t.Fatal("closed owned callback resumed guest write")
	}
	owner.mu.Lock()
	count = owner.importers
	owner.mu.Unlock()
	if count != 0 || len(in.thunkMem) != 0 {
		t.Fatal("completed parked thunk/owner retained", count)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func ownedHostImports(global *Global, owner *HostFuncRef) *Imports {
	imports := testImports("env.host", owner)
	imports.Global("env", "flag", global)
	return imports
}

func rv64PublicOwnedHostCloseModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x0b}))),
	)
}
