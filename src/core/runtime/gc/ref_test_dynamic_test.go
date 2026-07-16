package gc

import (
	"errors"
	"testing"
)

func refTestTypes(t testing.TB) []TypeDesc {
	t.Helper()
	base, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	base.Final = false
	child, err := NewStructDesc(1, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	child.Final = false
	child.HasSuper, child.Super = true, 0
	sibling, err := NewStructDesc(2, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	sibling.HasSuper, sibling.Super = true, 0
	arrayBase, err := NewArrayDesc(3, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	arrayBase.Final = false
	arrayChild, err := NewArrayDesc(4, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	arrayChild.HasSuper, arrayChild.Super = true, 3
	return []TypeDesc{base, child, sibling, arrayBase, arrayChild, {ID: 5, Kind: KindFunc}}
}

func TestCollectorRefTestDynamicTypes(t *testing.T) {
	c, err := NewCollector(Config{}, refTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	object, err := c.NewStructDefaultWithRoots(1, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	array, err := c.NewArrayDefaultWithRoots(4, 1, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		ref    Ref
		target RefTestTarget
		want   bool
	}{
		{"nullable null", Null(), RefTestTarget{Kind: RefTestDefined, Type: 0, Nullable: true}, true},
		{"non-null null", Null(), RefTestTarget{Kind: RefTestAny}, false},
		{"i31 any", I31New(-1), RefTestTarget{Kind: RefTestAny}, true},
		{"i31 eq", I31New(-1), RefTestTarget{Kind: RefTestEq}, true},
		{"i31 i31", I31New(-1), RefTestTarget{Kind: RefTestI31}, true},
		{"i31 struct", I31New(-1), RefTestTarget{Kind: RefTestStruct}, false},
		{"i31 defined", I31New(-1), RefTestTarget{Kind: RefTestDefined, Type: 0}, false},
		{"object any", object, RefTestTarget{Kind: RefTestAny}, true},
		{"object eq", object, RefTestTarget{Kind: RefTestEq}, true},
		{"object i31", object, RefTestTarget{Kind: RefTestI31}, false},
		{"object struct", object, RefTestTarget{Kind: RefTestStruct}, true},
		{"object array", object, RefTestTarget{Kind: RefTestArray}, false},
		{"object exact", object, RefTestTarget{Kind: RefTestDefined, Type: 1}, true},
		{"object ancestor", object, RefTestTarget{Kind: RefTestDefined, Type: 0}, true},
		{"object sibling", object, RefTestTarget{Kind: RefTestDefined, Type: 2}, false},
		{"array any", array, RefTestTarget{Kind: RefTestAny}, true},
		{"array eq", array, RefTestTarget{Kind: RefTestEq}, true},
		{"array struct", array, RefTestTarget{Kind: RefTestStruct}, false},
		{"array array", array, RefTestTarget{Kind: RefTestArray}, true},
		{"array ancestor", array, RefTestTarget{Kind: RefTestDefined, Type: 3}, true},
		{"array versus struct type", array, RefTestTarget{Kind: RefTestDefined, Type: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.RefTest(tc.ref, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("RefTest(%#x, %+v) = %v, want %v", tc.ref, tc.target, got, tc.want)
			}
		})
	}

	canonical, err := c.NewTypeCanonicalization([]TypeID{0, 1, 1, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.RefTestCanonical(object, RefTestTarget{Kind: RefTestDefined, Type: 2}, canonical); err != nil || !got {
		t.Fatalf("canonical sibling RefTest = %v, %v; want true", got, err)
	}
	if got, err := c.RefTest(object, RefTestTarget{Kind: RefTestDefined, Type: 2}); err != nil || got {
		t.Fatalf("raw sibling RefTest = %v, %v; want false", got, err)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		got, err := c.RefTestCanonical(object, RefTestTarget{Kind: RefTestDefined, Type: 0}, canonical)
		if err != nil || !got {
			panic("ref.test failed")
		}
	}); allocs != 0 {
		t.Fatalf("canonical defined RefTest allocations = %v, want 0", allocs)
	}

	for _, tc := range []struct {
		name      string
		ref       Ref
		target    RefTestTarget
		canonical bool
		want      Ref
		fail      bool
	}{
		{"nullable null", Null(), RefTestTarget{Kind: RefTestAny, Nullable: true}, false, Null(), false},
		{"non-null null", Null(), RefTestTarget{Kind: RefTestAny}, false, Null(), true},
		{"i31 identity", I31New(7), RefTestTarget{Kind: RefTestI31}, false, I31New(7), false},
		{"object identity", object, RefTestTarget{Kind: RefTestDefined, Type: 0}, false, object, false},
		{"object mismatch", object, RefTestTarget{Kind: RefTestArray}, false, Null(), true},
		{"canonical sibling identity", object, RefTestTarget{Kind: RefTestDefined, Type: 2}, true, object, false},
	} {
		t.Run("cast "+tc.name, func(t *testing.T) {
			var got Ref
			var err error
			if tc.canonical {
				got, err = c.RefCastCanonical(tc.ref, tc.target, canonical)
			} else {
				got, err = c.RefCast(tc.ref, tc.target)
			}
			if tc.fail {
				if !errors.Is(err, ErrCastFailure) {
					t.Fatalf("RefCast(%#x, %+v) = %#x, %v; want cast failure", tc.ref, tc.target, got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("RefCast(%#x, %+v) = %#x, %v; want %#x", tc.ref, tc.target, got, err, tc.want)
			}
		})
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		got, err := c.RefCastCanonical(object, RefTestTarget{Kind: RefTestDefined, Type: 2}, canonical)
		if err != nil || got != object {
			panic("ref.cast failed")
		}
	}); allocs != 0 {
		t.Fatalf("canonical defined RefCast allocations = %v, want 0", allocs)
	}
}

func TestCollectorRefTestRejectsInvalidState(t *testing.T) {
	c, err := NewCollector(Config{StressNurseryBytes: 128}, refTestTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.NewStructDefaultWithRoots(1, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewCheckedGlobalSlot(live); err != nil {
		t.Fatal(err)
	}
	stale, err := c.NewStructDefaultWithRoots(2, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}

	if _, err := c.NewTypeCanonicalization([]TypeID{0}); err == nil {
		t.Fatal("short canonical type map succeeded")
	}
	if _, err := c.NewTypeCanonicalization([]TypeID{0, 1, 2, 3, 4, 99}); err == nil {
		t.Fatal("out-of-range canonical representative succeeded")
	}
	if _, err := c.NewTypeCanonicalization([]TypeID{5, 1, 2, 3, 4, 5}); err == nil {
		t.Fatal("cross-kind canonical representative succeeded")
	}

	for _, tc := range []struct {
		name   string
		ref    Ref
		target RefTestTarget
	}{
		{"stale", stale, RefTestTarget{Kind: RefTestStruct}},
		{"forged", Ref(0xfffe), RefTestTarget{Kind: RefTestAny}},
		{"unknown target kind", live, RefTestTarget{Kind: 99}},
		{"unknown target type", live, RefTestTarget{Kind: RefTestDefined, Type: 99}},
		{"function target type", live, RefTestTarget{Kind: RefTestDefined, Type: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.RefTest(tc.ref, tc.target); err == nil {
				t.Fatalf("RefTest(%#x, %+v) succeeded", tc.ref, tc.target)
			}
		})
	}

	if _, err := c.RefCast(stale, RefTestTarget{Kind: RefTestStruct}); errors.Is(err, ErrCastFailure) || err == nil {
		t.Fatalf("stale RefCast error = %v, want specific stale-reference rejection", err)
	}
	if _, err := c.RefCast(Ref(0xfffe), RefTestTarget{Kind: RefTestAny}); errors.Is(err, ErrCastFailure) || err == nil {
		t.Fatalf("forged RefCast error = %v, want specific forged-reference rejection", err)
	}

	c.Close()
	if _, err := c.RefTest(Null(), RefTestTarget{Kind: RefTestAny, Nullable: true}); !errors.Is(err, errCollectorClosed) {
		t.Fatalf("closed RefTest error = %v, want collector closed", err)
	}
	if _, err := c.RefCast(Null(), RefTestTarget{Kind: RefTestAny, Nullable: true}); !errors.Is(err, errCollectorClosed) {
		t.Fatalf("closed RefCast error = %v, want collector closed", err)
	}
}

func BenchmarkCollectorRefTestDefined(b *testing.B) {
	c, err := NewCollector(Config{}, refTestTypes(b))
	if err != nil {
		b.Fatal(err)
	}
	object, err := c.NewStructDefaultWithRoots(1, EmptyRoots{})
	if err != nil {
		b.Fatal(err)
	}
	target := RefTestTarget{Kind: RefTestDefined, Type: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := c.RefTest(object, target)
		if err != nil || !got {
			b.Fatalf("RefTest = %v, %v", got, err)
		}
	}
}

func BenchmarkCollectorRefTestCanonical(b *testing.B) {
	c, err := NewCollector(Config{}, refTestTypes(b))
	if err != nil {
		b.Fatal(err)
	}
	object, err := c.NewStructDefaultWithRoots(1, EmptyRoots{})
	if err != nil {
		b.Fatal(err)
	}
	canonical, err := c.NewTypeCanonicalization([]TypeID{0, 1, 1, 3, 4, 5})
	if err != nil {
		b.Fatal(err)
	}
	target := RefTestTarget{Kind: RefTestDefined, Type: 2}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := c.RefTestCanonical(object, target, canonical)
		if err != nil || !got {
			b.Fatalf("RefTestCanonical = %v, %v", got, err)
		}
	}
}
