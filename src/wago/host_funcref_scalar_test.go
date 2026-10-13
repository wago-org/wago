//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostFuncRefNumericBinding(t *testing.T) {
	for _, test := range []struct {
		name      string
		typ       ValType
		wasmType  wasm.ValType
		fn        any
		arg, want uint64
	}{
		{"i32", ValI32, wasm.I32, func(v int32) int32 { return v + 1 }, I32(7), I32(8)},
		{"i64", ValI64, wasm.I64, func(v int64) int64 { return v + 1 }, I64(7), I64(8)},
		{"f32", ValF32, wasm.F32, func(v float32) float32 { return v + 1 }, F32(7), F32(8)},
		{"f64", ValF64, wasm.F64, func(v float64) float64 { return v + 1 }, F64(7), F64(8)},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			sig := FuncSig{Params: []ValType{test.typ}, Results: []ValType{test.typ}}
			owner, err := rt.NewHostFuncRef(test.fn, sig)
			if err != nil {
				t.Fatal(err)
			}
			if owner.scalarBinding == nil {
				t.Fatal("numeric binding was discarded")
			}
			binding, err := bindSyncHostImport(owner, sig)
			if err != nil {
				t.Fatal(err)
			}
			out := []uint64{0}
			binding.callUnchecked(instanceHostModule{}, []uint64{test.arg}, out)
			if out[0] != test.want {
				t.Fatalf("result=%x, want %x", out[0], test.want)
			}
			binding.scalarKind = syncHostNonScalar
			if owner.scalarBinding.scalarKind == syncHostNonScalar {
				t.Fatal("import binding aliases owner state")
			}
			if _, err := bindSyncHostImport(owner, FuncSig{}); err == nil || !strings.Contains(err.Error(), "signature") {
				t.Fatalf("signature mismatch=%v", err)
			}
			typ := test.wasmType
			bytes := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))),
				wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
			)
			mod, err := rt.Compile(bytes)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("g", test.arg); err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("import result=%x, error=%v, want %x", got, err, test.want)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			if owner.scalarBinding != nil || owner.fn != nil {
				t.Fatal("closed owner retains callbacks")
			}
			if _, err := bindSyncHostImport(owner, sig); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed binding=%v", err)
			}
		})
	}
	if _, err := bindSyncHostImport((*HostFuncRef)(nil), FuncSig{}); err == nil {
		t.Fatal("nil owner accepted")
	}
}

func TestHostFuncRefNumericImportAllocationsAndLifecycle(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, sig)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cache := loadHostThunkCacheState(in.c)
	if cache.bytes[1] == 0 || len(in.thunkMem) == 0 {
		t.Fatal("direct and reference thunks must both exist")
	}
	t.Logf("shared thunk code=%d bytes mapped=%d bytes per Compiled; owned mapping=%d bytes per instance", len(railshotHostIndirectSyncThunk(0, 1, 1)), cache.bytes[1], len(in.thunkMem))
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fn.Invoke(7); err != nil || len(got) != 1 || got[0] != 8 {
		t.Fatalf("invoke=%v,%v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if got, err := fn.Invoke(7); err != nil || len(got) != 1 || got[0] != 8 {
			panic("wrong result")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocations=%v, want 0; scalarKind=%d independent=%t flags=%x", allocs, in.syncHosts[0].scalarKind, in.usesIndependentExecution(), in.executionFlags.Load())
	}
	if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("close with importer=%v", err)
	}
	if owner.scalarBinding == nil {
		t.Fatal("rejected close discarded live binding")
	}
	other := NewRuntime()
	defer other.Close()
	otherMod, err := other.Compile(benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer otherMod.Close()
	if _, err := other.Instantiate(context.Background(), otherMod, WithImports(testImports("env.f", owner))); err == nil || !strings.Contains(err.Error(), "incompatible reference store") {
		t.Fatalf("foreign import=%v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner))); err == nil {
		t.Fatal("closed owner imported")
	}
	if _, err := fn.Invoke(7); err == nil {
		t.Fatal("closed instance invoked")
	}
}

func TestHostFuncRefNumericStaticImportSkipsSharedThunk(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	// Public compilation uses dynamic imports. Check the thunk builder's static
	// branch separately without changing the generated code's import ABI.
	c := *mod.c.executionView()
	c.dynamicImports = false
	imports := resolvedImports{c.functionImportBindingKey(0): owner}
	base, offsets, owned, mem, err := buildHostFuncThunks(&c, imports, true)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(mem)
	if base != 0 || len(offsets) != 0 || loadHostThunkCacheState(&c).bytes != [2]int{} {
		t.Fatal("static import created an unused shared thunk mapping")
	}
	if owned[0] == 0 || len(mem) == 0 {
		t.Fatal("static import lost its owned descriptor thunk")
	}
}

func BenchmarkHostFuncRefNumericImport(b *testing.B) {
	rt := NewRuntime()
	defer rt.CloseContext(context.Background())
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		b.Fatal(err)
	}
	defer owner.Close()
	mod, err := rt.Compile(benchReturningImportModule())
	if err != nil {
		b.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := fn.Invoke(7); err != nil || len(got) != 1 || got[0] != 8 {
			b.Fatalf("invoke=%v,%v", got, err)
		}
	}
}

func TestHostFuncRefNumericImportPanicAndTrap(t *testing.T) {
	for _, trapped := range []bool{false, true} {
		rt := NewRuntime()
		sentinel := errors.New("host failure")
		fail := true
		owner, err := rt.NewHostFuncRef(func(v int32) int32 {
			if fail {
				if trapped {
					panic(HostTrap{Err: sentinel})
				}
				panic(sentinel)
			}
			return v + 1
		}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
		if err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Compile(benchReturningImportModule())
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
		if err != nil {
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("g")
		if err != nil {
			t.Fatal(err)
		}
		func() {
			if !trapped {
				defer func() {
					if got := recover(); got != sentinel {
						t.Fatalf("panic=%v", got)
					}
				}()
			}
			if _, err := fn.Invoke(7); !trapped || !errors.Is(err, sentinel) {
				t.Fatalf("trap=%v", err)
			}
		}()
		fail = false
		if got, err := fn.Invoke(7); err != nil || len(got) != 1 || got[0] != 8 {
			t.Fatalf("after failure=%v,%v", got, err)
		}
		in.Close()
		mod.Close()
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		rt.Close()
	}
}

func TestHostFuncRefCallerAndReferenceBindingFallback(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	for _, test := range []struct {
		fn  any
		sig FuncSig
		gc  bool
	}{
		{CallerHostCallFunc(func(Caller, HostCall) {}), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}, false},
		{func(v ExternRef) ExternRef { return v }, FuncSig{Params: []ValType{ValExternRef}, Results: []ValType{ValExternRef}}, false},
		{func(v GCRef) GCRef { return v }, FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}}, true},
	} {
		var owner *HostFuncRef
		var err error
		if test.gc {
			owner, err = rt.NewGCHostFuncRef(test.fn, test.sig)
		} else {
			owner, err = rt.NewHostFuncRef(test.fn, test.sig)
		}
		if err != nil {
			t.Fatal(err)
		}
		if owner.scalarBinding != nil {
			t.Fatal("scoped callback selected numeric path")
		}
		binding, err := bindSyncHostImport(owner, test.sig)
		if err != nil {
			t.Fatal(err)
		}
		if binding.scalarKind >= syncHostTypedI32 {
			t.Fatal("scoped callback bound as typed numeric")
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostFuncRefMixedImportsAndCallerReentry(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	numeric, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, sig)
	if err != nil {
		t.Fatal(err)
	}
	var active *Instance
	var retained Caller
	callerOwner, err := rt.NewHostFuncRef(CallerHostCallFunc(func(caller Caller, call HostCall) {
		retained = caller
		out, err := active.InvokeFromHost(context.Background(), caller, "leaf", I32(call.I32(0)))
		if err != nil {
			panic(HostTrap{Err: err})
		}
		call.SetI32(0, AsI32(out[0]))
	}), sig)
	if err != nil {
		t.Fatal(err)
	}
	refOwner, err := rt.NewHostFuncRef(func(ref ExternRef) ExternRef { return ref }, FuncSig{Params: []ValType{ValExternRef}, Results: []ValType{ValExternRef}})
	if err != nil {
		t.Fatal(err)
	}
	if callerOwner.scalarBinding != nil || refOwner.scalarBinding != nil {
		t.Fatal("scoped import has numeric binding")
	}
	i32 := []wasm.ValType{wasm.I32}
	refs := []wasm.ValType{wasm.ExternRef}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32), wasmtest.FuncType(refs, refs))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0), importEntry("env", "outer", 0, 0), importEntry("env", "ref", 0, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3), wasmtest.ExportEntry("leaf", 0, 4), wasmtest.ExportEntry("echo", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 2, 0x0b}),
		)),
	)
	mod, err := rt.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	imports := testImports("env.f", numeric, "env.outer", callerOwner, "env.ref", refOwner)
	var instances []*Instance
	for i := 0; i < 2; i++ {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, in)
		defer in.Close()
		if i != 0 && loadHostThunkCacheState(in.c).bases != loadHostThunkCacheState(instances[0].c).bases {
			t.Fatal("instances did not share the import thunk cache")
		}
		active = in
		if got, err := in.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 9 {
			t.Fatalf("mixed import=%v,%v", got, err)
		}
		if _, err := in.InvokeFromHost(context.Background(), retained, "leaf", 7); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("retained caller=%v", err)
		}
		ref, err := rt.NewExternRef("payload")
		if err != nil {
			t.Fatal(err)
		}
		if out, err := in.InvokeValues(context.Background(), "echo", ValueExternRef(ref)); err != nil || len(out) != 1 || out[0].ExternRef() != ref {
			t.Fatalf("reference fallback=%v,%v", out, err)
		}
		rt.ReleaseExternRef(ref)
	}
	if err := instances[0].Close(); err != nil {
		t.Fatal(err)
	}
	if err := numeric.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("second importer protection=%v", err)
	}
	active = instances[1]
	if got, err := active.Invoke("run", 9); err != nil || len(got) != 1 || got[0] != 11 {
		t.Fatalf("remaining importer=%v,%v", got, err)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*HostFuncRef{numeric, callerOwner, refOwner} {
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func hostFuncRefNumericReferenceModule() []byte {
	i32 := []wasm.ValType{wasm.I32}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32), wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1), wasmtest.ExportEntry("get", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x0b}))),
	)
}

func TestHostFuncRefNumericRetainedReference(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(hostFuncRefNumericReferenceModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	producer, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", owner)))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if out, err := producer.Invoke("g", 7); err != nil || len(out) != 1 || out[0] != 8 {
		t.Fatalf("direct=%v,%v", out, err)
	}
	refs, err := producer.InvokeValues(context.Background(), "get")
	if err != nil || len(refs) != 1 || refs[0].FuncRef().IsNull() {
		t.Fatalf("get=%v,%v", refs, err)
	}
	i32 := []wasm.ValType{wasm.I32}
	consumerData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32}, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0x26, 0, 0x20, 1, 0x41, 0, 0x11, 0, 0, 0x0b}))),
	)
	consumerMod, err := rt.Compile(consumerData)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerMod.Close()
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := consumer.InvokeValues(context.Background(), "call", refs[0], ValueI32(7)); err != nil || len(out) != 1 || out[0].I32() != 8 {
		t.Fatalf("retained call=%v,%v", out, err)
	}
	if err := owner.Close(); err == nil {
		t.Fatal("live retained reference owner closed")
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("close=%v token=%t importers=%d runtimeClosed=%t liveInstances=%d", err, owner.tokenLive, owner.importers, rt.refStore.runtimeClosed, rt.refStore.liveInstances)
	}
	if owner.fn != nil || owner.scalarBinding != nil {
		t.Fatal("released owner retains callbacks")
	}
}
