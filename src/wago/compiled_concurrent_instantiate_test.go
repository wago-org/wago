package wago

import (
	"context"
	"errors"
	"sync"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type hostThunkCacheState struct {
	bytes       [2]int
	bases       [2]uintptr
	firstOffset [2]int
}

func loadHostThunkCacheState(c *Compiled) hostThunkCacheState {
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	var state hostThunkCacheState
	for i := range c.validateMemo.hostThunks {
		cache := &c.validateMemo.hostThunks[i]
		state.bytes[i] = len(cache.mem)
		state.bases[i] = cache.base
		state.firstOffset[i] = noHostThunkOffset
		if len(cache.offsets) != 0 {
			state.firstOffset[i] = cache.offsets[0]
		}
	}
	return state
}

func firstImportDispatchCode(in *Instance) uintptr {
	dispatch := in.jm.CaptureInstanceContext().ImportDispatch
	if dispatch == 0 {
		return 0
	}
	return uintptr(*(*uint64)(unsafe.Add(offHeapPtr(dispatch), coreruntime.ImportDispatchCodePtrOffset)))
}

func identityI32Module() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
}

func hostImportStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func TestHostThunkCacheSkipsNonOrdinaryBindings(t *testing.T) {
	t.Run("cross-instance export", func(t *testing.T) {
		providerCompiled := MustCompile(identityI32Module())
		defer providerCompiled.Close()
		provider, err := Instantiate(providerCompiled)
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		export, err := provider.ExportedFunc("identity")
		if err != nil {
			t.Fatal(err)
		}

		consumerCompiled := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
		defer consumerCompiled.Close()
		consumer, err := Instantiate(consumerCompiled, InstantiateOptions{Imports: testImports("env.f", export)})
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		if state := loadHostThunkCacheState(consumer.c); state.bytes != [2]int{} {
			t.Fatalf("cross-instance import populated ordinary host thunk caches: %v", state.bytes)
		}
		if got, err := consumer.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 41 {
			t.Fatalf("cross-instance call = %v, %v; want 41", got, err)
		}
	})

	t.Run("owned reference then ordinary callback", func(t *testing.T) {
		rt := NewRuntime()
		defer rt.Close()
		mod, err := rt.Compile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		owner, err := rt.NewHostFuncRef(slotHostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = params[0] + 1
		}), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
		if err != nil {
			t.Fatal(err)
		}

		owned, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
		if err != nil {
			t.Fatal(err)
		}
		if len(owned.thunkMem) == 0 {
			t.Fatal("owned HostFuncRef import has no private thunk mapping")
		}
		if state := loadHostThunkCacheState(owned.c); state.bytes != [2]int{} {
			t.Fatalf("owned-only import populated ordinary host thunk caches: %v", state.bytes)
		}
		if got, err := owned.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("owned host call = %v, %v; want 42", got, err)
		}
		if err := owned.Close(); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}

		ordinary, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", slotHostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = params[0] + 2
		}))))
		if err != nil {
			t.Fatal(err)
		}
		defer ordinary.Close()
		if state := loadHostThunkCacheState(ordinary.c); state.bytes[1] == 0 || state.bases[1] == 0 {
			t.Fatalf("ordinary callback did not populate sync host thunk cache: bytes=%v bases=%v", state.bytes, state.bases)
		}
		if got, err := ordinary.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 43 {
			t.Fatalf("ordinary host call = %v, %v; want 43", got, err)
		}
	})
}

func TestHostThunkCacheSurvivesFailedInstantiation(t *testing.T) {
	compiled := MustCompile(hostImportStartModule())
	defer compiled.Close()
	wantErr := errors.New("reject start")
	failed, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) {
		panic(HostTrap{Err: wantErr})
	}))})
	if failed != nil || !errors.Is(err, wantErr) {
		t.Fatalf("failed start = %v, %v; want nil instance and %v", failed, err, wantErr)
	}
	state := loadHostThunkCacheState(compiled.executionView())
	if state.bytes[1] == 0 || state.bases[1] == 0 {
		t.Fatalf("failed instantiation lost compiled-owned host thunk cache: %+v", state)
	}
	compiled.codeCache.mu.Lock()
	refs := compiled.codeCache.refs
	compiled.codeCache.mu.Unlock()
	if refs != 0 {
		t.Fatalf("failed instantiation retained %d compiled-code reference(s)", refs)
	}

	called := 0
	valid, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.f", slotHostFunc(func(HostModule, []uint64, []uint64) {
		called++
	}))})
	if err != nil {
		t.Fatalf("valid instantiation after failed start: %v", err)
	}
	if called != 1 {
		t.Fatalf("valid start callback count = %d, want 1", called)
	}
	if got := loadHostThunkCacheState(valid.c); got.bases[1] != state.bases[1] {
		t.Fatalf("valid instantiation replaced shared thunk mapping: got %#x, want %#x", got.bases[1], state.bases[1])
	}
	if err := valid.Close(); err != nil {
		t.Fatal(err)
	}
}

// A compiled artifact is intentionally reusable by bounded instance pools.
// The first instantiation also seals a compiler-produced writable code image,
// so concurrent first users exercise both publication and reference counting.
func TestConcurrentInstantiateSharedCompiled(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			instance, err := Instantiate(compiled)
			if err != nil {
				errs <- err
				return
			}
			defer instance.Close()
			values, err := instance.Invoke("value")
			if err != nil {
				errs <- err
				return
			}
			if len(values) != 1 || AsI32(values[0]) != 42 {
				errs <- &concurrentInstantiateResultError{values: values}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestConcurrentInstantiateSharesHostThunks(t *testing.T) {
	compiled := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	t.Cleanup(func() { _ = compiled.Close() })

	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	instances := make([]*Instance, workers)
	var wait sync.WaitGroup
	for i := range workers {
		i := i
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			delta := int32(i + 1)
			imports := testImports("env.f", i32ToI32HostFunc(func(value int32) int32 { return value + delta }))
			instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
			if err != nil {
				errs <- err
				return
			}
			instances[i] = instance
			values, err := instance.Invoke("g", I32(41))
			if err != nil {
				errs <- err
				return
			}
			if len(values) != 1 || AsI32(values[0]) != 41+delta {
				errs <- &concurrentInstantiateResultError{values: values}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		for _, instance := range instances {
			if instance != nil {
				_ = instance.Close()
			}
		}
		return
	}

	state := loadHostThunkCacheState(instances[0].c)
	if state.bytes[1] == 0 || state.bases[1] == 0 || state.firstOffset[1] == noHostThunkOffset {
		t.Fatalf("sync host thunk cache is incomplete: %+v", state)
	}
	wantCode := state.bases[1] + uintptr(state.firstOffset[1])
	for i, instance := range instances {
		if len(instance.thunkMem) != 0 {
			t.Fatalf("instance %d retained private ordinary thunk mapping: %d bytes", i, len(instance.thunkMem))
		}
		if got := firstImportDispatchCode(instance); got != wantCode {
			t.Fatalf("instance %d import dispatch code = %#x, want shared %#x", i, got, wantCode)
		}
	}

	if err := compiled.Close(); err != nil {
		t.Fatalf("close Compiled with live instances: %v", err)
	}
	for i, instance := range instances {
		values, err := instance.Invoke("g", I32(100))
		if err != nil || len(values) != 1 || AsI32(values[0]) != int32(101+i) {
			t.Fatalf("instance %d after Compiled.Close = %v, %v; want %d", i, values, err, 101+i)
		}
	}
	for i := 0; i < len(instances)-1; i++ {
		if err := instances[i].Close(); err != nil {
			t.Fatalf("close instance %d: %v", i, err)
		}
	}
	last := instances[len(instances)-1]
	if values, err := last.Invoke("g", I32(100)); err != nil || len(values) != 1 || AsI32(values[0]) != 116 {
		t.Fatalf("last instance after peer closes = %v, %v; want 116", values, err)
	}
	if err := last.Close(); err != nil {
		t.Fatalf("close last instance: %v", err)
	}
	if state := loadHostThunkCacheState(last.c); state.bytes != [2]int{} {
		t.Fatalf("last instance close retained shared thunk caches after Compiled.Close: %v", state.bytes)
	}
}

type concurrentInstantiateResultError struct{ values []uint64 }

func (e *concurrentInstantiateResultError) Error() string {
	return "concurrent shared-Compiled invocation returned an unexpected value"
}
