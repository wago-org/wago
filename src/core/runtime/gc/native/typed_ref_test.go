package gc

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"
)

func typedRefDescs(t *testing.T) []TypeDesc {
	t.Helper()
	root, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	root.Final, root.GuestType = false, true
	child := root
	child.ID, child.HasSuper, child.Super = 1, true, 0
	grandchild := child
	grandchild.ID, grandchild.Super, grandchild.Final = 2, 1, true
	array, err := NewArrayDesc(3, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	array.GuestType = true
	private := root
	private.ID, private.GuestType, private.Final = 5, false, true
	return []TypeDesc{root, child, grandchild, array, {ID: 4, Kind: KindFunc, Final: true, GuestType: true}, private}
}

func TestTypedRefMatchesCheckedTypes(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		t.Run(map[Profile]string{ProfileThroughput: "throughput", ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			c, err := NewCollector(Config{Profile: profile}, typedRefDescs(t))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			refs := []Ref{Null(), I31New(-1)}
			for _, id := range []TypeID{0, 1, 2, 5} {
				ref, err := c.NewStructDefaultWithRoots(id, EmptyRoots{})
				if err != nil {
					t.Fatal(err)
				}
				refs = append(refs, ref)
			}
			array, err := c.NewArrayDefaultWithRoots(3, 2, EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			refs = append(refs, array)
			var targets []RefTestTarget
			for _, kind := range []RefTestKind{RefTestAny, RefTestEq, RefTestI31, RefTestStruct, RefTestArray, RefTestNone} {
				for _, nullable := range []bool{false, true} {
					targets = append(targets, RefTestTarget{Kind: kind, Nullable: nullable})
				}
			}
			for _, id := range []TypeID{0, 1, 2, 3, 5} {
				for _, exact := range []bool{false, true} {
					for _, nullable := range []bool{false, true} {
						targets = append(targets, RefTestTarget{Kind: RefTestDefined, Type: id, Exact: exact, Nullable: nullable})
					}
				}
			}
			targets = append(targets, RefTestTarget{Kind: RefTestDefined, Type: 4}, RefTestTarget{Kind: RefTestDefined, Type: 99}, RefTestTarget{Kind: RefTestAny, Exact: true}, RefTestTarget{Kind: 255})
			for _, raw := range refs {
				typed, err := c.TypedReference(raw)
				if err != nil {
					t.Fatal(err)
				}
				if typed.Ref() != raw || !typed.Equal(UnknownTypedRef(raw)) {
					t.Fatalf("carrier changed reference identity: %x", raw)
				}
				for _, value := range []TypedRef{typed, UnknownTypedRef(raw)} {
					for _, target := range targets {
						want, oldErr := c.RefTest(raw, target)
						got, newErr := c.RefTestTyped(value, target)
						if got != want || (newErr == nil) != (oldErr == nil) {
							t.Fatalf("ref=%x target=%+v: typed=%v/%v checked=%v/%v", raw, target, got, newErr, want, oldErr)
						}
						if oldErr != nil && newErr.Error() != oldErr.Error() {
							t.Fatalf("target rejection changed: typed=%v checked=%v", newErr, oldErr)
						}
						cast, castErr := c.RefCastTyped(value, target)
						if oldErr != nil {
							if castErr == nil || castErr.Error() != oldErr.Error() {
								t.Fatalf("invalid target has wrong rejection: %+v => %v, want %v", target, castErr, oldErr)
							}
						} else if !want {
							if !errors.Is(castErr, ErrCastFailure) {
								t.Fatalf("wrong cast failure: %v", castErr)
							}
						} else if castErr != nil || cast != value {
							t.Fatalf("cast changed carrier: %v/%v", cast, castErr)
						}
					}
				}
			}
		})
	}
}

func TestTypedRefProjectionPublication(t *testing.T) {
	c, err := NewCollector(Config{}, typedRefDescs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if unsafe.Sizeof(TypedRef{}) != 8 || unsafe.Sizeof(c.typeWords[0]) != 4 || len(c.typeWords) != len(c.types) {
		t.Fatal("carrier or projection has wrong fixed width/count")
	}
	if unsafe.Sizeof(TypeDesc{}) != 64 {
		t.Fatalf("descriptor grew: %d", unsafe.Sizeof(TypeDesc{}))
	}
	wantRoot := NativeTypeWordAny | NativeTypeWordEq | NativeTypeWordStruct | NativeTypeWordFastValid | uint32(KindStruct)<<NativeTypeWordKindShift
	if c.typeWords[0] != wantRoot || c.typeWords[1]&NativeTypeWordLeaf != 0 || c.typeWords[2]&(NativeTypeWordLeaf|NativeTypeWordFinal) != NativeTypeWordLeaf|NativeTypeWordFinal {
		t.Fatalf("wrong graph projection: %x", c.typeWords)
	}
	if c.typeWords[4]&NativeTypeWordFunc == 0 || c.typeWords[4]&NativeTypeWordFastValid != 0 || c.typeWords[5]&(NativeTypeWordFastValid|NativeTypeWordAny|NativeTypeWordEq|NativeTypeWordStruct) != 0 {
		t.Fatal("function/private type marked as a fast guest object")
	}
	before := append([]uint32(nil), c.typeWords...)
	oldIntervals := append([]uint64(nil), c.subtypeIntervals...)
	child := c.types[1]
	child.ID, child.Super = 6, 1
	if err := c.AddTypes([]TypeDesc{child}); err != nil {
		t.Fatal(err)
	}
	if len(c.typeWords) != 7 || c.typeWords[6]&NativeTypeWordLeaf == 0 || c.typeWords[1]&NativeTypeWordLeaf != 0 || reflect.DeepEqual(oldIntervals, c.subtypeIntervals[:6]) {
		t.Fatal("child append did not publish current rows/intervals")
	}
	for i := range before {
		if c.typeWords[i] != before[i] {
			t.Fatalf("unrelated row changed: type%d", i)
		}
	}
	before = append([]uint32(nil), c.typeWords...)
	view := *c.nativeView
	child.ID, child.Super = 7, 99
	if err := c.AddTypes([]TypeDesc{child}); err == nil {
		t.Fatal("invalid child accepted")
	}
	if !reflect.DeepEqual(before, c.typeWords) || *c.nativeView != view {
		t.Fatal("failed registration changed published words/view")
	}
	if c.nativeView.TypeWordCount != uint32(len(c.types)) || c.nativeView.TypeWords != sliceData(c.typeWords) {
		t.Fatal("native word view is stale")
	}
	c.Close()
	if c.nativeView.TypeWordCount != 0 || c.nativeView.TypeWords != 0 || c.typeWords != nil {
		t.Fatal("close retained projection backing")
	}
}

func TestTypedRefDenseBoundary(t *testing.T) {
	// These are real distinct admitted descriptors, not a sparse slice with
	// unregistered IDs. Seventeen binary field choices distinguish 65537 types.
	descs := make([]TypeDesc, 65537)
	for i := range descs {
		var fields [17]StorageKind
		for bit := range fields {
			fields[bit] = StorageI32
			if i&(1<<bit) != 0 {
				fields[bit] = StorageI64
			}
		}
		d, err := NewStructDesc(TypeID(i), fields[:])
		if err != nil {
			t.Fatal(err)
		}
		d.GuestType = true
		descs[i] = d
	}
	c, err := NewCollector(Config{}, descs)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range []TypeID{0, 32767, 32768, 65534, 65535, 65536} {
		ref, err := c.NewStructDefaultWithRoots(id, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		value, err := c.TypedReference(ref)
		if err != nil {
			t.Fatal(err)
		}
		index, known := value.TypeIndex()
		if known != (id <= 65535) || known && TypeID(index) != id {
			t.Fatalf("id%d: index%d known%v", id, index, known)
		}
		for _, targetID := range []TypeID{0, 65534, 65535, 65536} {
			target := RefTestTarget{Kind: RefTestDefined, Type: targetID, Exact: true}
			match, err := c.RefTestTyped(value, target)
			if err != nil || match != (id == targetID) {
				t.Fatalf("actual%d target%d matched%v: %v", id, targetID, match, err)
			}
		}
	}
}

func TestTypedRefKnownZeroAndNoAllocations(t *testing.T) {
	c, err := NewCollector(Config{}, typedRefDescs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := c.TypedReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id, known := value.TypeIndex(); id != 0 || !known {
		t.Fatal("known type0 lost")
	}
	if _, known := UnknownTypedRef(raw).TypeIndex(); known {
		t.Fatal("raw reference mistaken for known type0")
	}
	for _, r := range []Ref{Null(), I31New(5)} {
		v, err := c.TypedReference(r)
		if err != nil || v.NativeWord() != uint64(r) {
			t.Fatal("null/i31 representation changed")
		}
	}
	target := RefTestTarget{Kind: RefTestDefined, Type: 0}
	if allocations := testing.AllocsPerRun(100, func() {
		match, err := c.RefTestTyped(value, target)
		if !match || err != nil {
			panic("typed identity failed")
		}
	}); allocations != 0 {
		t.Fatalf("typed check allocates: %v", allocations)
	}
	c.Close()
	if _, err := c.RefTestTyped(value, target); !errors.Is(err, errCollectorClosed) {
		t.Fatalf("closed collector accepted carrier: %v", err)
	}
}

func TestTypedRefUsesCurrentSubtypeIntervals(t *testing.T) {
	c, err := NewCollector(Config{}, typedRefDescs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, err := c.NewStructDefaultWithRoots(2, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := c.TypedReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	old := c.subtypeIntervals[1]
	child := c.types[1]
	child.ID, child.Super = 6, 0
	if err := c.AddTypes([]TypeDesc{child}); err != nil {
		t.Fatal(err)
	}
	if c.subtypeIntervals[1] == old {
		t.Fatal("fixture did not shift an existing target interval")
	}
	for _, target := range []RefTestTarget{
		{Kind: RefTestDefined, Type: 0},
		{Kind: RefTestDefined, Type: 1},
		{Kind: RefTestDefined, Type: 2, Exact: true},
		{Kind: RefTestDefined, Type: 6},
	} {
		want, oldErr := c.RefTest(raw, target)
		got, newErr := c.RefTestTyped(value, target)
		if oldErr != nil || newErr != nil || want != got {
			t.Fatalf("stale graph result for %+v: typed%v/%v checked%v/%v", target, got, newErr, want, oldErr)
		}
	}
}

func TestTypedRefRetainsExtentAndLivenessChecks(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		c, err := NewCollector(Config{Profile: profile}, typedRefDescs(t))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		value, err := c.TypedReference(raw)
		if err != nil {
			t.Fatal(err)
		}
		entry := &c.handles[handleOf(raw)]
		original := *entry
		for _, mutate := range []func(*handleEntry){
			func(e *handleEntry) { e.space = spaceFree },
			func(e *handleEntry) { e.space = NativeSpaceCount },
			func(e *handleEntry) { e.off = ^uint32(0) },
			func(e *handleEntry) { e.size = 0 },
			func(e *handleEntry) { e.size = ^uint32(0) },
		} {
			mutate(entry)
			if match, err := c.RefTestTyped(value, RefTestTarget{Kind: RefTestDefined, Type: 0}); match || !errors.Is(err, errTypedObject) {
				t.Fatalf("invalid extent accepted: %+v => %v/%v", *entry, match, err)
			}
			*entry = original
		}
		if err := c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if match, err := c.RefTestTyped(value, RefTestTarget{Kind: RefTestDefined, Type: 0}); match || !errors.Is(err, errTypedObject) {
			t.Fatalf("unrooted reclaimed object accepted: %v/%v", match, err)
		}
		c.Close()
	}
}
