package gc

import "testing"

func TestSubtypeIntervalsCoverForestAndAppendedChildren(t *testing.T) {
	root, _ := NewStructDesc(0, []StorageKind{StorageI32})
	root.Final = false
	other, _ := NewArrayDesc(1, StorageI32)
	other.Final = false
	child, _ := NewStructDesc(2, []StorageKind{StorageI32})
	child.HasSuper, child.Super = true, 0
	child.Final = false
	grandchild, _ := NewStructDesc(3, []StorageKind{StorageI32})
	grandchild.HasSuper, grandchild.Super = true, 2

	c := newTestCollectorWithTypes(t, Config{}, []TypeDesc{root, other, child, grandchild})
	saved := gcSubtypeIntervalsEnabled
	defer func() { gcSubtypeIntervalsEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		gcSubtypeIntervalsEnabled = enabled
		for _, tc := range []struct {
			actual, required TypeID
			want             bool
		}{
			{0, 0, true}, {2, 0, true}, {3, 0, true}, {3, 2, true},
			{0, 2, false}, {2, 3, false}, {1, 0, false}, {3, 1, false},
		} {
			got, err := c.TypeSubtype(tc.actual, tc.required)
			if err != nil || got != tc.want {
				t.Fatalf("intervals=%v TypeSubtype(%d, %d) = %v, %v; want %v", enabled, tc.actual, tc.required, got, err, tc.want)
			}
		}
	}

	appended, _ := NewStructDesc(4, []StorageKind{StorageI32})
	appended.HasSuper, appended.Super = true, 0
	if err := c.AddTypes([]TypeDesc{appended}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.TypeSubtype(4, 0); err != nil || !got {
		t.Fatalf("appended subtype = %v, %v; want true", got, err)
	}
	if got, err := c.TypeSubtype(4, 2); err != nil || got {
		t.Fatalf("appended sibling subtype = %v, %v; want false", got, err)
	}
}

func TestExactDefinedRefTestRejectsProperSubtype(t *testing.T) {
	root, _ := NewStructDesc(0, nil)
	root.Final = false
	child, _ := NewStructDesc(1, nil)
	child.HasSuper, child.Super = true, 0
	c := newTestCollectorWithTypes(t, Config{}, []TypeDesc{root, child})
	ref, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if matched, err := c.RefTest(ref, RefTestTarget{Kind: RefTestDefined, Type: 0}); err != nil || !matched {
		t.Fatalf("ordinary subtype test = %v, %v; want true", matched, err)
	}
	if matched, err := c.RefTest(ref, RefTestTarget{Kind: RefTestDefined, Type: 0, Exact: true}); err != nil || matched {
		t.Fatalf("exact supertype test = %v, %v; want false", matched, err)
	}
	if matched, err := c.RefTest(ref, RefTestTarget{Kind: RefTestDefined, Type: 1, Exact: true}); err != nil || !matched {
		t.Fatalf("exact dynamic-type test = %v, %v; want true", matched, err)
	}
	if _, err := c.RefTest(ref, RefTestTarget{Kind: RefTestStruct, Exact: true}); err == nil {
		t.Fatal("exact abstract ref.test target accepted")
	}
}

func TestSubtypeIntervalsRejectUnknownPair(t *testing.T) {
	d, _ := NewStructDesc(0, nil)
	c := newTestCollectorWithTypes(t, Config{}, []TypeDesc{d})
	if _, err := c.typeSubtypeIDs(1, 0); err == nil {
		t.Fatal("unknown actual type accepted")
	}
	if _, err := c.typeSubtypeIDs(0, 1); err == nil {
		t.Fatal("unknown required type accepted")
	}
}
