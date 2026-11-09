package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func typeRelationRef(index uint32, nullable, exact bool) ValueTypeDescriptor {
	return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Exact: exact, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
}

func typeRelationField(v ValueTypeDescriptor) FieldTypeDescriptor {
	return FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: v}, Mutable: true}
}

// One recursive group: a struct, a function over it, an array of it, and a
// subtype of the struct; plus a singleton group referring back into it.
func typeRelationGraph() []DefinedTypeDescriptor {
	return []DefinedTypeDescriptor{
		{RecGroup: 0, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{typeRelationField(typeRelationRef(1, true, false)), typeRelationField(ValueTypeDescriptor{Kind: ValueTypeI32})}},
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{typeRelationRef(0, true, false)}, Results: []ValueTypeDescriptor{typeRelationRef(2, false, true)}},
		{RecGroup: 0, Kind: CompositeTypeArray, Array: typeRelationField(typeRelationRef(0, true, false))},
		{RecGroup: 0, Supers: []uint32{0}, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{typeRelationField(typeRelationRef(1, true, false)), typeRelationField(ValueTypeDescriptor{Kind: ValueTypeI32}), typeRelationField(typeRelationRef(3, true, false))}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{typeRelationRef(3, true, false)}},
	}
}

// A closed graph makes every type equivalent to itself, which is the fact the
// execution fast path relies on; open graphs must be rejected by the proof.
func TestDefinedTypesClosedImpliesReflexiveEquivalence(t *testing.T) {
	cases := map[string]func([]DefinedTypeDescriptor){
		"closed":           func([]DefinedTypeDescriptor) {},
		"field-out-range":  func(g []DefinedTypeDescriptor) { g[0].Fields[0] = typeRelationField(typeRelationRef(9, true, false)) },
		"param-out-range":  func(g []DefinedTypeDescriptor) { g[1].Params[0] = typeRelationRef(5, true, false) },
		"array-out-range":  func(g []DefinedTypeDescriptor) { g[2].Array = typeRelationField(typeRelationRef(7, true, false)) },
		"super-out-range":  func(g []DefinedTypeDescriptor) { g[3].Supers = []uint32{11} },
		"describes-range":  func(g []DefinedTypeDescriptor) { g[4].HasDescribes, g[4].Describes = true, 5 },
		"descriptor-range": func(g []DefinedTypeDescriptor) { g[4].HasDescriptor, g[4].Descriptor = true, 6 },
		"unknown-kind":     func(g []DefinedTypeDescriptor) { g[2].Kind = CompositeTypeKind(99) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := typeRelationGraph()
			mutate(g)
			closed := definedTypesClosed(g)
			if closed != (name == "closed") {
				t.Fatalf("closed=%t", closed)
			}
			reflexive := true
			for a := range g {
				eq := definedTypeEquivalent(uint32(a), g, uint32(a), g)
				if closed && !eq {
					t.Fatalf("closed graph type %d not equivalent to itself", a)
				}
				reflexive = reflexive && eq
			}
			if !closed && reflexive && name != "describes-range" && name != "descriptor-range" {
				t.Fatal("open graph unexpectedly reflexive; the case does not exercise the proof")
			}
		})
	}
}

// The snapshot fast path must decide exactly as the general subtype check for
// every reference pair over its own graph, and copies must not use it.
func TestExecutionValueTypeSubtypeMatchesGeneralCheck(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), typedFunctionFieldModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	exec := c.executionView()
	if exec == c || !exec.executionTypesClosed() {
		t.Fatal("compiled execution snapshot graph is not proved closed")
	}
	copied := *exec
	if copied.executionTypesClosed() {
		t.Fatal("copied view reused the snapshot proof")
	}
	other := typeRelationGraph()
	values := []ValueTypeDescriptor{{Kind: ValueTypeI32}, {Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}}
	for i := range exec.Types {
		for _, nullable := range []bool{false, true} {
			for _, exact := range []bool{false, true} {
				values = append(values, typeRelationRef(uint32(i), nullable, exact))
			}
		}
	}
	for _, actual := range values {
		for _, required := range values {
			for _, actualTypes := range [][]DefinedTypeDescriptor{exec.Types, other} {
				want := valueTypeSubtype(actual, actualTypes, required, exec.Types)
				if got := exec.executionValueTypeSubtype(actual, actualTypes, required); got != want {
					t.Fatalf("actual %+v required %+v: fast %t, general %t", actual, required, got, want)
				}
				if got := copied.executionValueTypeSubtype(actual, actualTypes, required); got != want {
					t.Fatalf("copy: actual %+v required %+v: %t, want %t", actual, required, got, want)
				}
			}
		}
	}
}

// type 0: func; type 1: struct (mut (ref null 0)); run stores a typed function
// reference into a fresh struct and publishes it; clear drops it.
func typedFunctionFieldModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0}, []byte{0x5f, 1, 0x63, 0, 1})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0}, []byte{0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 1, 0xfb, 0x1b, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0x6f, 0x24, 0, 0x0b}))),
	)
}

// Typed function fields take the snapshot fast path on every collecting store.
func TestTypedFunctionFieldStoresUnderCollection(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), typedFunctionFieldModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 64; i++ {
		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(fmt.Errorf("run %d: %w", i, err))
		}
		if i%8 == 7 {
			if _, err := in.Invoke("clear"); err != nil {
				t.Fatal(err)
			}
		}
	}
}
