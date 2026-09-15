package gc_test

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func collector(t testing.TB, profile gc.Profile) *gc.Collector {
	t.Helper()
	structure, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	array, err := gc.NewArrayDesc(1, gc.StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := gc.NewStructDesc(2, []gc.StorageKind{gc.StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	refArray, err := gc.NewArrayDesc(3, gc.StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := gc.NewCollector(gc.Config{Profile: profile}, []gc.TypeDesc{structure, array, refs, refArray})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestCheckedReferenceDomainsAndGenerations(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			a, b := collector(t, profile), collector(t, profile)
			first, err := a.NewStruct(0)
			if err != nil {
				t.Fatal(err)
			}
			other, err := b.NewStruct(0)
			if err != nil {
				t.Fatal(err)
			}
			if gc.RefEq(first, other) {
				t.Fatal("different collectors issued equal references")
			}
			if _, err := b.ObjectType(first); !errors.Is(err, gc.ErrInvalidReference) {
				t.Fatalf("foreign reference = %v", err)
			}
			if err := a.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			fresh, err := a.NewArrayDefault(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if gc.RefEq(first, fresh) {
				t.Fatal("stale reference equals new allocation")
			}
			if _, err := a.ObjectType(first); !errors.Is(err, gc.ErrInvalidReference) {
				t.Fatalf("stale reference = %v", err)
			}
			if typ, err := a.ObjectType(fresh); err != nil || typ != 1 {
				t.Fatalf("fresh type = %d, %v", typ, err)
			}
		})
	}
}
func TestCheckedReferenceIngress(t *testing.T) {
	a, b := collector(t, gc.ProfileThroughput), collector(t, gc.ProfileThroughput)
	foreign, err := a.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := b.NewStructDefault(2)
	if err != nil {
		t.Fatal(err)
	}
	array, err := b.NewArrayDefault(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"struct store":       func() error { return b.StructSet(parent, 0, gc.RefValue(foreign)) },
		"array store":        func() error { return b.ArraySet(array, 0, gc.RefValue(foreign)) },
		"array fill":         func() error { return b.ArrayFill(array, 0, gc.RefValue(foreign), 1) },
		"array copy":         func() error { return b.ArrayCopy(array, 0, foreign, 0, 0) },
		"struct initializer": func() error { _, err := b.NewStructWithRoots(2, []gc.Value{gc.RefValue(foreign)}, nil); return err },
		"array initializer":  func() error { _, err := b.NewArray(3, 1, gc.RefValue(foreign)); return err },
		"fixed initializer":  func() error { _, err := b.NewArrayFixedWithRoots(3, []gc.Value{gc.RefValue(foreign)}, nil); return err },
		"global":             func() error { _, err := b.NewCheckedGlobalSlot(foreign); return err },
		"table":              func() error { _, err := b.NewCheckedTableSlot(foreign); return err },
		"roots":              func() error { return b.CollectFull(gc.RefSliceRoots{foreign}) },
		"test":               func() error { _, err := b.RefTest(foreign, gc.RefTestTarget{Kind: gc.RefTestAny}); return err },
		"cast":               func() error { _, err := b.RefCast(foreign, gc.RefTestTarget{Kind: gc.RefTestAny}); return err },
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !errors.Is(err, gc.ErrInvalidReference) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
func TestCheckedReferenceEgressAndPortableImmediates(t *testing.T) {
	c := collector(t, gc.ProfileThroughput)
	child, err := c.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructWithRoots(2, []gc.Value{gc.RefValue(child)}, gc.RefSliceRoots{child})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(gc.RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	value, err := c.StructGet(parent, 0)
	if err != nil || !gc.RefEq(value.Ref, child) {
		t.Fatalf("child egress = %+v, %v", value, err)
	}
	global, err := c.NewCheckedGlobalSlot(child)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.CheckedGlobalSlot(global)
	if err != nil || !gc.RefEq(got, child) {
		t.Fatalf("global egress = %+v, %v", got, err)
	}
	for _, value := range []gc.Ref{gc.Null(), gc.I31New(-5)} {
		if _, err := c.RefTest(value, gc.RefTestTarget{Kind: gc.RefTestAny, Nullable: true}); err != nil {
			t.Fatal(err)
		}
	}
	c.Close()
	if _, err := c.ObjectType(child); !errors.Is(err, gc.ErrCollectorClosed) {
		t.Fatalf("closed reference = %v", err)
	}
}

func BenchmarkCheckedStructGet(b *testing.B) {
	c := collector(b, gc.ProfileThroughput)
	value, err := c.NewStruct(0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.StructGet(value, 0); err != nil {
			b.Fatal(err)
		}
	}
}
