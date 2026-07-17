package wago

import (
	"strings"
	"testing"
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
