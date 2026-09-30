package gc

import (
	"strings"
	"testing"
)

func constructorEdgeTypes(t *testing.T) []TypeDesc {
	t.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := NewStructDesc(1, []StorageKind{StorageI32, StorageRefNull, StorageV128})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(2, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	nonDefaultStruct, err := NewStructDesc(3, []StorageKind{StorageRef})
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{leaf, mixed, refs, nonDefaultStruct}
}

func newConstructorEdgeCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := NewCollector(Config{
		NurseryBytes:        4096,
		ThroughputHeapBytes: 64 << 10,
		CollectEveryAlloc:   true,
		VerifyAfterCollect:  true,
	}, constructorEdgeTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestPrevalidatedConstructorsPreserveInitializerRoots(t *testing.T) {
	c := newConstructorEdgeCollector(t)

	child, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	words := []uint64{123, uint64(child), 0x1122334455667788, 0x99aabbccddeeff00}
	var wordScratch InitializerWordRootScratch
	mixed, err := c.NewStructWordsPrevalidatedWithRootScratch(1, words, EmptyRoots{}, &wordScratch)
	if err != nil {
		t.Fatal(err)
	}
	if wordScratch.active {
		t.Fatal("word scratch remained active after constructor")
	}
	if got, err := c.StructGet(mixed, 0); err != nil || int32(got.Bits) != 123 {
		t.Fatalf("mixed i32 = %+v, %v", got, err)
	}
	if got, err := c.StructGet(mixed, 1); err != nil || got.Ref != child {
		t.Fatalf("mixed ref = %+v, %v; want %v", got, err, child)
	}
	if got, err := c.StructGet(mixed, 2); err != nil || got.Bits != words[2] || got.BitsHi != words[3] {
		t.Fatalf("mixed v128 = %+v, %v", got, err)
	}
	if typ, err := c.ObjectType(child); err != nil || typ != 0 {
		t.Fatalf("word initializer child type = %d, %v", typ, err)
	}

	child, err = c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := c.NewArrayFixedPrevalidatedWithRoots(2, []Value{RefValue(child), RefValue(child)}, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.ArrayGet(fixed, 1); err != nil || got.Ref != child {
		t.Fatalf("prevalidated fixed ref = %+v, %v; want %v", got, err, child)
	}

	child, err = c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	var arrayScratch ArrayInitializerRootScratch
	fixed, err = c.NewArrayFixedPrevalidatedWithRootScratch(2, []Value{RefValue(child)}, EmptyRoots{}, &arrayScratch)
	if err != nil {
		t.Fatal(err)
	}
	if arrayScratch.active {
		t.Fatal("array scratch remained active after constructor")
	}
	if got, err := c.ArrayGet(fixed, 0); err != nil || got.Ref != child {
		t.Fatalf("prevalidated scratch ref = %+v, %v; want %v", got, err, child)
	}

	child, err = c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	fixed, err = c.NewArrayFixedPrevalidatedWithRootScratch(2, []Value{RefValue(child)}, EmptyRoots{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.ArrayGet(fixed, 0); err != nil || got.Ref != child {
		t.Fatalf("prevalidated nil-scratch ref = %+v, %v; want %v", got, err, child)
	}
}

func TestReconstructionAndPrevalidatedDefaultConstructors(t *testing.T) {
	c := newConstructorEdgeCollector(t)
	child, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	root := Root(child)

	if _, err := c.NewStructDefaultWithRoots(3, Slots{&root}); err == nil {
		t.Fatal("default constructor accepted non-defaultable struct")
	}
	reconstructed, err := c.NewStructUninitializedWithRoots(3, Slots{&root})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(reconstructed, 0, RefValue(Ref(root))); err != nil {
		t.Fatal(err)
	}
	if got, err := c.StructGet(reconstructed, 0); err != nil || got.Ref != Ref(root) {
		t.Fatalf("reconstructed struct ref = %+v, %v; want %v", got, err, Ref(root))
	}

	array, err := c.NewArrayUninitializedWithRoots(2, 3, Slots{&root})
	if err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 3; i++ {
		if got, err := c.ArrayGet(array, i); err != nil || !got.Ref.IsNull() {
			t.Fatalf("uninitialized nullable array[%d] = %+v, %v", i, got, err)
		}
	}

	array, err = c.NewArrayDefaultPrevalidatedWithRoots(2, 2, Slots{&root})
	if err != nil {
		t.Fatal(err)
	}
	if length, err := c.ArrayLen(array); err != nil || length != 2 {
		t.Fatalf("prevalidated default length = %d, %v", length, err)
	}
	if err := c.Verify(Slots{&root}); err != nil {
		t.Fatal(err)
	}
}

func TestPrevalidatedConstructorShapeAndScratchErrorsAreTransactional(t *testing.T) {
	c := newConstructorEdgeCollector(t)
	child, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}

	var wordScratch InitializerWordRootScratch
	for _, words := range [][]uint64{
		{1, uint64(child), 2},
		{1, uint64(child), 2, 3, 4},
	} {
		before := c.Stats().Allocations
		if _, err := c.NewStructWordsPrevalidatedWithRootScratch(1, words, EmptyRoots{}, &wordScratch); err == nil || !strings.Contains(err.Error(), "word shape mismatch") {
			t.Fatalf("words %v error = %v", words, err)
		}
		if c.Stats().Allocations != before || wordScratch.active {
			t.Fatalf("rejected words mutated allocation/scratch state: allocations=%d/%d active=%v", before, c.Stats().Allocations, wordScratch.active)
		}
	}
	if _, err := c.NewStructWordsPrevalidatedWithRootScratch(2, nil, EmptyRoots{}, &wordScratch); err == nil || !strings.Contains(err.Error(), "struct initializer shape mismatch") {
		t.Fatalf("array-as-struct error = %v", err)
	}

	wordScratch.active = true
	if _, err := c.NewStructWordsPrevalidatedWithRootScratch(1, []uint64{1, uint64(child), 2, 3}, EmptyRoots{}, &wordScratch); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("active word scratch error = %v", err)
	}
	wordScratch.clear()

	var arrayScratch ArrayInitializerRootScratch
	arrayScratch.active = true
	if _, err := c.NewArrayFixedPrevalidatedWithRootScratch(2, []Value{RefValue(child)}, EmptyRoots{}, &arrayScratch); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("active array scratch error = %v", err)
	}
	arrayScratch.clear()
	if _, err := c.NewArrayFixedPrevalidatedWithRoots(1, nil, EmptyRoots{}); err == nil || !strings.Contains(err.Error(), "not array") {
		t.Fatalf("struct-as-array error = %v", err)
	}
}
