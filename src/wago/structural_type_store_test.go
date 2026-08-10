package wago

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompiledStructuralCallIdentityCacheLifecycle(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	module, err := rt.Compile(scalarFunctionModule(wasm.I64))
	if err != nil {
		t.Fatal(err)
	}
	compiled := module.Compiled()
	want, err := compiledStructuralCallIdentity(compiled, 0)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.validateMemo.structuralCallIdentities != nil {
		t.Fatal("structural identity cache built before instantiation")
	}

	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.validateMemo.structuralCallIdentities != structuralCallIdentitySeenSentinel {
		t.Fatal("structural identity cache built for one-shot instantiation")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	in, err = rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := compiled.cachedStructuralCallIdentity(0)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("cached identity = %x, %v; want %x", got, ok, want)
	}
	cache := compiled.validateMemo.structuralCallIdentities
	retained := structuralCallIdentityCacheHeaderBytes + cap(cache.spans)*structuralCallIdentitySpanBytes + cap(cache.identities)
	if retained > maxStructuralCallIdentityCacheBytes {
		t.Fatalf("identity cache retains %d bytes; budget %d", retained, maxStructuralCallIdentityCacheBytes)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if compiled.validateMemo.structuralCallIdentities == nil {
		t.Fatal("Close released identity cache while an instance was live")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if compiled.validateMemo.structuralCallIdentities != nil {
		t.Fatal("identity cache retained after compiled module and final instance closed")
	}
}

func TestCompiledStructuralCallIdentityCacheBudget(t *testing.T) {
	typeCount := (maxStructuralCallIdentityCacheBytes-structuralCallIdentityCacheHeaderBytes)/structuralCallIdentitySpanBytes + 1
	compiled := &Compiled{
		Funcs:        []FuncSig{{HasTypeIndex: true}},
		Types:        make([]DefinedTypeDescriptor, typeCount),
		FuncTypeID:   []uint64{1},
		validateMemo: &validateMemo{},
		codeCache:    &compiledCodeCache{},
	}
	compiled.Types[0].Kind = CompositeTypeFunction
	if err := compiled.prepareStructuralCallIdentities(); err != nil {
		t.Fatal(err)
	}
	if err := compiled.prepareStructuralCallIdentities(); err != nil {
		t.Fatal(err)
	}
	cache := compiled.validateMemo.structuralCallIdentities
	if cache == nil {
		t.Fatal("oversized module did not record disabled identity cache")
	}
	if len(cache.spans) != 0 || len(cache.identities) != 0 {
		t.Fatalf("oversized module retained spans=%d identities=%d", len(cache.spans), len(cache.identities))
	}
}

func compiledStoreType(key uint64, param ValueTypeKind) *Compiled {
	abi := ValI32
	if param == ValueTypeI64 {
		abi = ValI64
	}
	return &Compiled{
		Funcs:      []FuncSig{{Params: []ValType{abi}, HasTypeIndex: true, TypeIndex: 0}},
		FuncTypeID: []uint64{key},
		Types: []DefinedTypeDescriptor{{
			Final:  true,
			Kind:   CompositeTypeFunction,
			Params: []ValueTypeDescriptor{{Kind: param}},
		}},
	}
}

func TestReferenceStoreResolvesNativeStructuralKeyCollisionsExactly(t *testing.T) {
	const forcedKey = uint64(0x1234)
	store := newReferenceStore(false)
	first := &Instance{c: compiledStoreType(forcedKey, ValueTypeI32)}
	if err := store.registerInstance(first); err != nil {
		t.Fatal(err)
	}

	equivalent := &Instance{c: compiledStoreType(forcedKey, ValueTypeI32)}
	if err := store.registerInstance(equivalent); err != nil {
		t.Fatalf("equivalent cross-module type rejected: %v", err)
	}

	distinct := &Instance{c: compiledStoreType(forcedKey, ValueTypeI64)}
	if err := store.registerInstance(distinct); err == nil || !strings.Contains(err.Error(), "collides with a distinct store type") {
		t.Fatalf("forced distinct collision error = %v", err)
	}
	if _, published := store.instances[distinct]; published {
		t.Fatal("collision failure partially registered instance")
	}

	closeStoreInstance := func(in *Instance) {
		store.advanceInstanceLifetime(in, referenceLifetimeClosed)
		store.advanceInstanceLifetime(in, referenceLifetimeQuiesced)
		store.advanceInstanceLifetime(in, referenceLifetimeResourcesReleased)
	}
	closeStoreInstance(first)
	if err := store.registerInstance(distinct); err == nil {
		t.Fatal("collision admitted while equivalent owner remained live")
	}
	closeStoreInstance(equivalent)
	if err := store.registerInstance(distinct); err != nil {
		t.Fatalf("type key was not released with final physical owner: %v", err)
	}
	closeStoreInstance(distinct)
}

func scalarFunctionModule(param wasm.ValType) []byte {
	body := []byte{0x41, 0x00, 0x0b}
	result := []wasm.ValType{wasm.I32}
	if param == wasm.I64 {
		body = []byte{0x20, 0x00, 0xa7, 0x0b} // local.get 0; i32.wrap_i64
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(func() []wasm.ValType {
			if param == (wasm.ValType{}) {
				return nil
			}
			return []wasm.ValType{param}
		}(), result))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestRuntimeRejectsForcedHashEqualDistinctNativeTarget(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	first, err := rt.Compile(scalarFunctionModule(wasm.ValType{}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := rt.Compile(scalarFunctionModule(wasm.I64))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Compiled().Close()
	defer second.Compiled().Close()
	second.c.FuncTypeID[0] = first.c.FuncTypeID[0] // deterministic collision injection seam

	in, err := rt.Instantiate(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := rt.Instantiate(context.Background(), second); err == nil || !strings.Contains(err.Error(), "collides with a distinct store type") {
		t.Fatalf("forced collision instantiate error = %v", err)
	}
}

func TestReferenceStoreRejectsStructuralKeyCollisionWithinModule(t *testing.T) {
	c := &Compiled{
		Funcs: []FuncSig{
			{Params: []ValType{ValI32}, HasTypeIndex: true, TypeIndex: 0},
			{Params: []ValType{ValI64}, HasTypeIndex: true, TypeIndex: 1},
		},
		FuncTypeID: []uint64{7, 7},
		Types: []DefinedTypeDescriptor{
			{Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}}},
			{Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI64}}},
		},
	}
	store := newReferenceStore(false)
	if err := store.registerInstance(&Instance{c: c}); err == nil || !strings.Contains(err.Error(), "collides within module") {
		t.Fatalf("within-module collision error = %v", err)
	}
}
