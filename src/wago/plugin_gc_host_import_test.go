package wago

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

type pluginGCHostImportTestPlugin struct{ state *pluginGCHostImportTestState }

type pluginGCHostImportTestState struct {
	mu                sync.Mutex
	resolver          *CallerResolver
	invocationContext context.Context
	retainedStorage   GuestStorage
	retainedRef       GuestGCRef
	otherViewRejected bool
}

func pluginGCHostImportTestProvider(state *pluginGCHostImportTestState) PluginProvider {
	def := testDefinition("example.com/plugin-gc-host-import")
	def.Authorities = []AuthorityRequest{
		{
			Name:   AuthorityHostImportDefine,
			Mode:   AuthorityRequired,
			Reason: "define GC-reference host imports",
			Scope:  AuthorityScope{Modules: []string{"plugin_gc"}},
		},
		{
			Name:   AuthorityHostCallerIdentify,
			Mode:   AuthorityRequired,
			Reason: "test GC host invocation context",
		},
	}
	return PluginProvider{
		Definition: def,
		New: func() Plugin {
			return pluginGCHostImportTestPlugin{state: state}
		},
	}
}

func pluginGCHostTrap(err error) {
	if err != nil {
		panic(HostTrap{Err: err})
	}
}

func requirePluginGCArrayType(storage GuestStorage, result bool, index int) (DefinedTypeDescriptor, error) {
	var value ValueTypeDescriptor
	var ok bool
	if result {
		value, ok = storage.ImportResultType(index)
	} else {
		value, ok = storage.ImportParamType(index)
	}
	if !ok || value.Kind != ValueTypeReference || !value.Ref.Heap.Defined {
		return DefinedTypeDescriptor{}, fmt.Errorf("plugin GC value %d has no caller-defined reference type", index)
	}
	defined, ok := storage.DefinedType(value.Ref.Heap.TypeIndex)
	if !ok || defined.Kind != CompositeTypeArray {
		return DefinedTypeDescriptor{}, fmt.Errorf("plugin GC value %d is not an array type", index)
	}
	if !defined.Array.Storage.Packed || defined.Array.Storage.PackedType != PackedTypeI8 {
		return DefinedTypeDescriptor{}, fmt.Errorf("plugin GC array %d is not array<i8>", index)
	}
	return defined, nil
}

func (p pluginGCHostImportTestPlugin) Register(reg *Registrar) error {
	resolver, err := reg.HostCallers()
	if err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	p.state.resolver = resolver
	module, err := imports.Module("plugin_gc")
	if err != nil {
		return err
	}
	module.Func("null_result", func(_ HostModule, _, results []uint64) {
		results[0] = 0
	}).Results(ValAnyRef)
	module.Func("null_param", func(_ HostModule, params, results []uint64) {
		if params[0] != 0 {
			panic(HostTrap{Err: fmt.Errorf("null parameter arrived as %#x", params[0])})
		}
		results[0] = 1
	}).Params(ValAnyRef).Results(ValI32)
	module.Func("create", func(m HostModule, _, results []uint64) {
		storageHost, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("plugin GC create has no GuestStorage")})
		}
		pluginGCHostTrap(storageHost.WithGuestStorage(func(storage GuestStorage) error {
			_, err := requirePluginGCArrayType(storage, true, 0)
			return err
		}))
		allocator, ok := m.(GuestGCArrayAllocatorHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("plugin GC create has no array allocator")})
		}
		token, err := allocator.NewGCArrayResult(0, 4, func(payload []byte, info GuestGCArrayInfo) error {
			if info.Storage != GuestGCArrayI8 || len(payload) != 4 {
				return fmt.Errorf("allocated array storage/length = %v/%d", info.Storage, len(payload))
			}
			copy(payload, []byte{1, 2, 3, 4})
			return nil
		})
		pluginGCHostTrap(err)
		if token == 0 || token == uint64(uint32(token)) {
			panic(HostTrap{Err: fmt.Errorf("allocated GC result is not an opaque token: %#x", token)})
		}
		results[0] = token
	}).Results(ValAnyRef)
	module.Func("consume", func(m HostModule, params, results []uint64) {
		if params[0] == 0 || params[0] == uint64(uint32(params[0])) {
			panic(HostTrap{Err: fmt.Errorf("plugin received raw or null GC parameter %#x", params[0])})
		}
		storageHost, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("plugin GC consume has no GuestStorage")})
		}
		var first GuestGCRef
		var firstByte byte
		pluginGCHostTrap(storageHost.WithGuestStorage(func(storage GuestStorage) error {
			if _, err := requirePluginGCArrayType(storage, false, 0); err != nil {
				return err
			}
			ref, err := storage.GCRef(params[0])
			if err != nil {
				return err
			}
			if _, err := storage.GCRef(params[0] + 1); err == nil {
				return fmt.Errorf("forged GC token was accepted")
			}
			first = ref
			info, err := storage.GCArrayInfo(ref)
			if err != nil {
				return err
			}
			payload, byteInfo, err := storage.GCArrayBytes(ref, GuestStorageRead)
			if err != nil {
				return err
			}
			if info != byteInfo || info.Storage != GuestGCArrayI8 || info.Length != 4 || len(payload) != 4 {
				return fmt.Errorf("unexpected GC array view: info=%+v byteInfo=%+v bytes=%d", info, byteInfo, len(payload))
			}
			firstByte = payload[0]
			p.state.mu.Lock()
			p.state.retainedStorage = storage
			p.state.retainedRef = ref
			p.state.mu.Unlock()
			return nil
		}))
		pluginGCHostTrap(storageHost.WithGuestStorage(func(storage GuestStorage) error {
			if _, err := storage.GCArrayInfo(first); err == nil {
				return fmt.Errorf("GC handle from another guest-storage view was accepted")
			}
			p.state.mu.Lock()
			p.state.otherViewRejected = true
			p.state.mu.Unlock()
			return nil
		}))
		results[0] = uint64(firstByte)
	}).Params(ValAnyRef).Results(ValI32)
	module.Func("mutate", func(m HostModule, params, results []uint64) {
		storageHost, ok := m.(GuestStorageHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("plugin GC mutate has no GuestStorage")})
		}
		pluginGCHostTrap(storageHost.WithGuestStorage(func(storage GuestStorage) error {
			ref, err := storage.GCRef(params[0])
			if err != nil {
				return err
			}
			payload, info, err := storage.GCArrayBytes(ref, GuestStorageWrite)
			if err != nil {
				return err
			}
			if !info.Mutable || info.Storage != GuestGCArrayI8 || len(payload) != 4 {
				return fmt.Errorf("unexpected writable GC array view: %+v/%d", info, len(payload))
			}
			payload[0] = 9
			return nil
		}))
		results[0] = 1
	}).Params(ValAnyRef).Results(ValI32)
	module.Func("collect", func(m HostModule, _, _ []uint64) {
		collector, ok := m.(GCHostModule)
		if !ok {
			panic(HostTrap{Err: fmt.Errorf("plugin GC collect has no collector")})
		}
		pluginGCHostTrap(collector.CollectGC())
	})
	module.Func("raw_result", func(_ HostModule, _, results []uint64) {
		results[0] = 2 // compact object-shaped bits are not a valid host token
	}).Results(ValAnyRef)
	module.Func("scalar", func(_ HostModule, _, results []uint64) {
		results[0] = 11
	}).Results(ValI32)
	module.Func("context", func(m HostModule, params, results []uint64) {
		if params[0] != 0 {
			panic(HostTrap{Err: fmt.Errorf("context parameter arrived as %#x, want null", params[0])})
		}
		ctx, err := p.state.resolver.InvocationContext(m)
		pluginGCHostTrap(err)
		p.state.mu.Lock()
		p.state.invocationContext = ctx
		p.state.mu.Unlock()
		results[0] = 1
	}).Params(ValAnyRef).Results(ValI32)
	return nil
}

func pluginGCCodeWithLocals(body []byte) []byte {
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}

func pluginGCImport(module, name string, typeIndex uint32) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, 0x00)
	return append(entry, wasmtest.ULEB(typeIndex)...)
}

func pluginGCNullResultModule(name string, nullable bool) []byte {
	arrayType := []byte{0x5e, 0x78, 0x01} // (array (mut i8))
	refCode := byte(0x63)
	if !nullable {
		refCode = 0x64
	}
	importType := []byte{0x60, 0x00, 0x01, refCode, 0x00}
	callerType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x10, 0x00, 0xd1, 0x0b} // call 0; ref.is_null; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, importType, callerType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", name, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func pluginGCNullParamModule() []byte {
	return pluginGCNullParamModuleNamed("null_param")
}

func pluginGCNullParamModuleNamed(name string) []byte {
	arrayType := []byte{0x5e, 0x78, 0x01}
	importType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	callerType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0xd0, 0x00, 0x10, 0x00, 0x0b} // ref.null 0; call 0; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, importType, callerType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", name, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func pluginGCArrayRoundTripModule(mutable, mutate bool) []byte {
	storage := byte(0x78)
	mutability := byte(0)
	if mutable {
		mutability = 1
	}
	arrayType := []byte{0x5e, storage, mutability}
	createType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	arrayParamType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	collectType := wasmtest.FuncType(nil, nil)
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // local 0: (ref null 0)
		0x10, 0x00, 0x21, 0x00, // local = create()
		0x10, 0x03, // collect while local remains live
		0x20, 0x00, 0x10, 0x01, 0x1a, // consume(local); drop
	}
	if mutate {
		body = append(body, 0x20, 0x00, 0x10, 0x02, 0x1a) // mutate(local); drop
	}
	body = append(body,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0d, 0x00, // array.get_u 0 at index 0
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, createType, arrayParamType, collectType, runType)),
		wasmtest.Section(2, wasmtest.Vec(
			pluginGCImport("plugin_gc", "create", 1),
			pluginGCImport("plugin_gc", "consume", 2),
			pluginGCImport("plugin_gc", "mutate", 2),
			pluginGCImport("plugin_gc", "collect", 3),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(pluginGCCodeWithLocals(body))),
	)
}

func pluginGCWrongStorageResultModule() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	createType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, createType, runType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "create", 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0xd1, 0x0b}))),
	)
}

func pluginGCStructResultModule() []byte {
	structType := []byte{0x5f, 0x00}
	createType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, createType, runType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "create", 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0xd1, 0x0b}))),
	)
}

func pluginGCWrongABIModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "create", 0))),
	)
}

func pluginGCScalarModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "scalar", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func newPluginGCTestRuntime(t testing.TB) (*Runtime, *pluginGCHostImportTestState) {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	state := new(pluginGCHostImportTestState)
	provider := pluginGCHostImportTestProvider(state)
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		rt.Close()
		t.Fatal(err)
	}
	return rt, state
}

func TestPluginGCHostImportsBoundaryOnlyNulls(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, _ := newPluginGCTestRuntime(t)
	defer rt.Close()

	if _, ok := rt.imports["plugin_gc.null_result"].(HostFunc); !ok {
		t.Fatalf("GC plugin binding = %T, want HostFunc", rt.imports["plugin_gc.null_result"])
	}
	if _, ok := rt.imports["plugin_gc.scalar"].(HostFunc); !ok {
		t.Fatalf("scalar plugin binding = %T, want HostFunc", rt.imports["plugin_gc.scalar"])
	}

	for name, wasmBytes := range map[string][]byte{
		"null-result": pluginGCNullResultModule("null_result", true),
		"null-param":  pluginGCNullParamModule(),
	} {
		t.Run(name, func(t *testing.T) {
			mod, err := rt.Compile(wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			admission := mod.c.GCNativeRootAdmission()
			if !admission.Required || !admission.Exact || admission.Callsites == 0 || admission.Safepoints != 0 {
				t.Fatalf("boundary-only native root admission = %+v", admission)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			values, err := in.Call(context.Background(), "call")
			if err != nil || len(values) != 1 || values[0].I32() != 1 {
				t.Fatalf("call = %v, %v; want i32(1)", values, err)
			}
		})
	}
}

func TestPluginGCHostImportInvocationContext(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, state := newPluginGCTestRuntime(t)
	defer rt.Close()
	module, err := rt.Compile(pluginGCNullParamModuleNamed("context"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	deadline := time.Now().Add(time.Hour)
	parent, cancel := invocationContextTestParent(context.Background(), deadline)
	defer cancel()
	result, err := in.Call(parent, "call")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].I32() != 1 {
		t.Fatalf("call result = %v, want i32(1)", result)
	}
	state.mu.Lock()
	ctx := state.invocationContext
	state.mu.Unlock()
	if ctx == nil {
		t.Fatal("GC host import did not receive an invocation context")
	}
	if !invocationContextTestDeadline(ctx, deadline) {
		gotDeadline, ok := ctx.Deadline()
		t.Fatalf("GC host deadline = %v, %v; want %v, supported=%v", gotDeadline, ok, deadline, nativeCancellationSupported())
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("GC host context after callback = %v, want context.Canceled", ctx.Err())
	}
}

func TestPluginGCHostImportsNonNullRoundTripAndZeroCopyWrite(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, state := newPluginGCTestRuntime(t)
	defer rt.Close()
	mod, err := rt.Compile(pluginGCArrayRoundTripModule(true, true))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if !mod.c.needsRuntimeGCCollectorDomain() {
		t.Fatal("boundary module did not request a Runtime GC collector domain")
	}
	roots := mod.c.genericGCFrameRoots()
	if roots == nil || len(roots.callsites) < 4 || len(roots.safepoints) != 0 {
		t.Fatalf("boundary-only root map = %+v", roots)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, VerifyAfterCollect: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc == nil || in.gcInvocationDomain() == nil {
		t.Fatal("boundary-only plugin instance has no Runtime GC domain")
	}
	values, err := in.Call(context.Background(), "run")
	if err != nil || len(values) != 1 || values[0].I32() != 9 {
		t.Fatalf("non-null GC round trip = %v, %v; want zero-copy mutation value 9", values, err)
	}
	in.refStore.mu.Lock()
	liveTokens := len(in.refStore.gcByToken)
	in.refStore.mu.Unlock()
	if liveTokens != 0 {
		t.Fatalf("plugin call retained %d temporary GC token(s)", liveTokens)
	}
	if public := in.existingPublicGCState(); public != nil {
		public.mu.Lock()
		resultTokens, activations := public.resultTokenCount, public.hostActivationCount
		argumentRoots, resultRoots := uint8(0), uint8(0)
		for i := range public.hostArgumentRootCount {
			argumentRoots += public.hostArgumentRootCount[i]
			resultRoots += public.hostResultRootCount[i]
		}
		public.mu.Unlock()
		if resultTokens != 0 || activations != 0 || argumentRoots != 0 || resultRoots != 0 {
			t.Fatalf("plugin GC callback state leaked: tokens=%d activations=%d argumentRoots=%d resultRoots=%d", resultTokens, activations, argumentRoots, resultRoots)
		}
	}
	state.mu.Lock()
	storage, ref, rejected := state.retainedStorage, state.retainedRef, state.otherViewRejected
	state.mu.Unlock()
	if !rejected {
		t.Fatal("GC handle from another GuestStorage view was not rejected")
	}
	if storage == nil {
		t.Fatal("plugin did not retain the expired test view")
	}
	if _, err := storage.GCArrayInfo(ref); err == nil {
		t.Fatal("expired GuestStorage view accepted a retained GC handle")
	}
	if _, err := storage.GCRef(0xfeed); err == nil {
		t.Fatal("expired GuestStorage view accepted a forged token")
	}
}

func TestPluginGCHostImportDifferentExactTypesAndDomains(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, order := range []string{"mutable-first", "immutable-first"} {
		t.Run(order, func(t *testing.T) {
			rt, _ := newPluginGCTestRuntime(t)
			mutableMod, err := rt.Compile(pluginGCArrayRoundTripModule(true, false))
			if err != nil {
				t.Fatal(err)
			}
			immutableMod, err := rt.Compile(pluginGCArrayRoundTripModule(false, false))
			if err != nil {
				mutableMod.Close()
				t.Fatal(err)
			}
			mutable, err := rt.Instantiate(context.Background(), mutableMod)
			if err != nil {
				mutableMod.Close()
				immutableMod.Close()
				t.Fatal(err)
			}
			immutable, err := rt.Instantiate(context.Background(), immutableMod)
			if err != nil {
				mutable.Close()
				mutableMod.Close()
				immutableMod.Close()
				t.Fatal(err)
			}
			if mutable.gc == nil || immutable.gc == nil || mutable.gc == immutable.gc {
				t.Fatalf("non-equivalent exact caller types share collector domain: mutable=%p immutable=%p", mutable.gc, immutable.gc)
			}
			type callResult struct {
				name   string
				values []Value
				err    error
			}
			calls := make(chan callResult, 2)
			for _, call := range []struct {
				name string
				in   *Instance
			}{{"mutable", mutable}, {"immutable", immutable}} {
				call := call
				go func() {
					values, callErr := call.in.Call(context.Background(), "run")
					calls <- callResult{name: call.name, values: values, err: callErr}
				}()
			}
			for range 2 {
				result := <-calls
				if result.err != nil || len(result.values) != 1 || result.values[0].I32() != 1 {
					t.Fatalf("%s caller = %v, %v; want 1", result.name, result.values, result.err)
				}
			}
			closePair := func(first, second *Instance, firstMod, secondMod *Module) {
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
				if err := firstMod.Close(); err != nil {
					t.Fatal(err)
				}
				if err := second.Close(); err != nil {
					t.Fatal(err)
				}
				if err := secondMod.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if order == "mutable-first" {
				closePair(mutable, immutable, mutableMod, immutableMod)
			} else {
				closePair(immutable, mutable, immutableMod, mutableMod)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginGCHostImportCodecRoundTrip(t *testing.T) {
	requireCompleteCore3Backend(t)
	if guardPageBuilt {
		t.Skip("signals-based compiled modules are intentionally not serializable")
	}
	rt, _ := newPluginGCTestRuntime(t)
	defer rt.Close()
	original, err := rt.Compile(pluginGCArrayRoundTripModule(true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	encoded, err := original.c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(encoded); err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	if decoded.nativeGCABIRequirement() == 0 || decoded.genericGCFrameRoots() == nil || !decoded.hasCollectorReferenceCallBoundary() {
		t.Fatalf("decoded GC boundary metadata ABI=%d roots=%v boundary=%v", decoded.nativeGCABIRequirement(), decoded.genericGCFrameRoots() != nil, decoded.hasCollectorReferenceCallBoundary())
	}
	mod, err := rt.Module(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if values, err := in.Call(context.Background(), "run"); err != nil || len(values) != 1 || values[0].I32() != 1 {
		t.Fatalf("decoded boundary call = %v, %v", values, err)
	}

	var missingRoots Compiled
	if err := missingRoots.UnmarshalBinary(encoded); err != nil {
		t.Fatal(err)
	}
	defer missingRoots.Close()
	missingRoots.validateMemo.gcFrameRoots = nil
	missingMod, err := rt.Module(&missingRoots)
	if err != nil {
		t.Fatal(err)
	}
	defer missingMod.Close()
	if _, err := rt.Instantiate(context.Background(), missingMod); err == nil || !strings.Contains(err.Error(), "exact native root maps") {
		t.Fatalf("decoded boundary without roots error = %v", err)
	}
}

func TestPluginGCHostImportValidation(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, _ := newPluginGCTestRuntime(t)
	defer rt.Close()

	t.Run("wrong ABI", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCWrongABIModule())
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		if _, err := rt.Instantiate(context.Background(), mod); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
			t.Fatalf("wrong plugin ABI error = %v", err)
		}
	})

	t.Run("struct where plugin requires array", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCStructResultModule())
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Call(context.Background(), "run"); err == nil || !strings.Contains(err.Error(), "not an array type") {
			t.Fatalf("struct plugin contract error = %v", err)
		}
	})

	t.Run("wrong array storage", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCWrongStorageResultModule())
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Call(context.Background(), "run"); err == nil || !strings.Contains(err.Error(), "not array<i8>") {
			t.Fatalf("wrong plugin array storage error = %v", err)
		}
	})

	t.Run("non-null rejects null", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCNullResultModule("null_result", false))
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Call(context.Background(), "call"); err == nil || !strings.Contains(err.Error(), "non-null result") {
			t.Fatalf("non-null plugin result error = %v", err)
		}
	})

	t.Run("raw compact result rejects", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCNullResultModule("raw_result", true))
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Call(context.Background(), "call"); err == nil || !strings.Contains(err.Error(), "raw compact GC reference") {
			t.Fatalf("raw compact plugin result error = %v", err)
		}
	})

	t.Run("immutable write rejects", func(t *testing.T) {
		mod, err := rt.Compile(pluginGCArrayRoundTripModule(false, true))
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Call(context.Background(), "run"); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("immutable plugin write error = %v", err)
		}
	})
}

func TestPluginScalarHostImportStaysOrdinary(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, _ := newPluginGCTestRuntime(t)
	defer rt.Close()
	mod, err := rt.Compile(pluginGCScalarModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if mod.c.needsExactNativeGCRoots() || mod.c.genericGCFrameRoots() != nil {
		t.Fatalf("scalar plugin import acquired GC root metadata: required=%v roots=%v", mod.c.needsExactNativeGCRoots(), mod.c.genericGCFrameRoots())
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc != nil {
		t.Fatalf("scalar plugin import acquired a GC collector: %p", in.gc)
	}
	if values, err := in.Call(context.Background(), "run"); err != nil || len(values) != 1 || values[0].I32() != 11 {
		t.Fatalf("scalar plugin call = %v, %v", values, err)
	}
}

func TestPluginGCHostImportLosesRuntimeAuthorityWhenCopiedAsHostFunc(t *testing.T) {
	requireCompleteCore3Backend(t)
	first, _ := newPluginGCTestRuntime(t)
	defer first.Close()
	create := first.imports["plugin_gc.create"]
	second := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer second.Close()
	low, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), pluginGCArrayRoundTripModule(true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer low.Close()
	imports := Imports{
		"plugin_gc.create":  create,
		"plugin_gc.consume": first.imports["plugin_gc.consume"],
		"plugin_gc.mutate":  first.imports["plugin_gc.mutate"],
		"plugin_gc.collect": first.imports["plugin_gc.collect"],
	}
	if _, err := instantiateCore(low, InstantiateOptions{Imports: imports, store: second.refStore}); err == nil || !strings.Contains(err.Error(), "cannot transfer collector references") {
		t.Fatalf("copied plugin HostFunc error = %v", err)
	}
}

func TestPluginGCHostImportRawLowLevelHostFuncRejected(t *testing.T) {
	requireCompleteCore3Backend(t)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	compiled, err := Compile(cfg, pluginGCNullResultModule("raw_result", true))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	mod, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if _, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"plugin_gc.raw_result": HostFunc(func(HostModule, []uint64, []uint64) {}),
	})); err == nil || !strings.Contains(err.Error(), "cannot transfer collector references") {
		t.Fatalf("raw low-level GC HostFunc error = %v", err)
	}
}
