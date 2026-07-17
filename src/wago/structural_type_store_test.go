package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func compiledStoreType(key uint64, param ValueTypeKind) *Compiled {
	return &Compiled{
		Funcs:      []FuncSig{{HasTypeIndex: true, TypeIndex: 0}},
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

	store.instanceClosed(first)
	if err := store.registerInstance(distinct); err == nil {
		t.Fatal("collision admitted while equivalent owner remained live")
	}
	store.instanceClosed(equivalent)
	if err := store.registerInstance(distinct); err != nil {
		t.Fatalf("type key was not released with final live owner: %v", err)
	}
	store.instanceClosed(distinct)
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
			{HasTypeIndex: true, TypeIndex: 0},
			{HasTypeIndex: true, TypeIndex: 1},
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
