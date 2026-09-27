package gc

import (
	"fmt"
	"testing"
)

// Check the public admission paths as well as the validator. In particular,
// AddTypes must not let a later module bypass construction-time validation.
func checkSubtypeAdmission(t *testing.T, descs []TypeDesc, valid bool) {
	t.Helper()
	if err := ValidateTypeDescs(descs); (err == nil) != valid {
		t.Errorf("ValidateTypeDescs = %v, want accepted=%v", err, valid)
	}
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		t.Run(fmt.Sprintf("profile=%d", profile), func(t *testing.T) {
			c, err := NewCollector(Config{Profile: profile}, descs)
			if c != nil {
				c.Close()
			}
			if (err == nil) != valid {
				t.Errorf("NewCollector = %v, want accepted=%v", err, valid)
			}
			// These tables put the root first. Forward-parent tables are tested
			// separately because they must be admitted as one batch.
			c, err = NewCollector(Config{Profile: profile}, descs[:1])
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.AddTypes(descs[1:]); (err == nil) != valid {
				t.Errorf("AddTypes = %v, want accepted=%v", err, valid)
			}
			wantLen := 1
			if valid {
				wantLen = len(descs)
			}
			if len(c.types) != wantLen {
				t.Errorf("AddTypes left %d descriptors, want %d", len(c.types), wantLen)
			}
		})
	}
}

func TestValidateSubtypeLayouts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parent []StorageKind
		child  []StorageKind
		valid  bool
		shift  bool
	}{
		{"hidden reference", []StorageKind{StorageRefNull}, []StorageKind{StorageI32}, false, false},
		{"shifted inherited field", []StorageKind{StorageI32}, []StorageKind{StorageI32}, false, true},
		{"missing inherited field", []StorageKind{StorageI32, StorageRefNull}, []StorageKind{StorageI32}, false, false},
		{"unchanged layout", []StorageKind{StorageI64, StorageRefNull}, []StorageKind{StorageI64, StorageRefNull}, true, false},
		{"reference narrowing", []StorageKind{StorageRefNull}, []StorageKind{StorageRef}, true, false},
		{"reference widening", []StorageKind{StorageRef}, []StorageKind{StorageRefNull}, false, false},
		{"append aligned field", []StorageKind{StorageRefNull}, []StorageKind{StorageRef, StorageV128}, true, false},
		// A new field may use the parent's tail padding; inherited field
		// extents, rather than the parent's padded Size, define the prefix.
		{"append in tail padding", []StorageKind{StorageI64, StorageI8}, []StorageKind{StorageI64, StorageI8, StorageI8}, true, false},
		{"empty parent", nil, []StorageKind{StorageRefNull}, true, false},
		{"reordered inherited fields", []StorageKind{StorageRefNull, StorageI32}, []StorageKind{StorageI32, StorageRefNull}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, err := NewStructDesc(0, tc.parent)
			if err != nil {
				t.Fatal(err)
			}
			parent.Final = false
			child, err := NewStructDesc(1, tc.child)
			if err != nil {
				t.Fatal(err)
			}
			child.HasSuper = true
			if tc.shift {
				child.Fields[0].Offset += 4
				child.Size += 4
			}
			checkSubtypeAdmission(t, []TypeDesc{parent, child}, tc.valid)
		})
	}
}

func TestValidateSubtypeStorageKinds(t *testing.T) {
	// An explicit matrix, independent of referenceStorageCompatible, covers
	// both directions, all scalar widths, and all three reference families.
	kinds := []StorageKind{
		StorageI8, StorageI16, StorageI32, StorageI64, StorageF32, StorageF64, StorageV128,
		StorageRef, StorageRefNull, StorageFuncRef, StorageFuncRefNull, StorageExternRef, StorageExternRefNull,
	}
	for _, parentKind := range kinds {
		for _, childKind := range kinds {
			valid := parentKind == childKind ||
				(parentKind == StorageRefNull && childKind == StorageRef) ||
				(parentKind == StorageFuncRefNull && childKind == StorageFuncRef) ||
				(parentKind == StorageExternRefNull && childKind == StorageExternRef)
			for _, array := range []bool{false, true} {
				t.Run(fmt.Sprintf("array=%v/parent=%d/child=%d", array, parentKind, childKind), func(t *testing.T) {
					var parent, child TypeDesc
					if array {
						parent, _ = NewArrayDesc(0, parentKind)
						child, _ = NewArrayDesc(1, childKind)
					} else {
						parent, _ = NewStructDesc(0, []StorageKind{parentKind})
						child, _ = NewStructDesc(1, []StorageKind{childKind})
					}
					parent.Final = false
					child.HasSuper = true
					checkSubtypeAdmission(t, []TypeDesc{parent, child}, valid)
				})
			}
		}
	}
}

func TestValidateSubtypeChains(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, kind := range []TypeKind{KindStruct, KindArray, KindFunc} {
			t.Run(fmt.Sprintf("reverse=%v/kind=%d", reverse, kind), func(t *testing.T) {
				const depth = 4096
				descs := subtypeChain(depth, kind, reverse)
				if err := ValidateTypeDescs(descs); err != nil {
					t.Fatal(err)
				}
				for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
					c, err := NewCollector(Config{Profile: profile}, descs)
					if err != nil {
						t.Fatal(err)
					}
					c.Close()
				}
				root := 0
				if reverse {
					root = depth - 1
				}
				// Every direct layout remains compatible, so this tests cycles
				// independently of layout checks, including function sentinels.
				descs[root].HasSuper, descs[root].Super = true, TypeID(depth/2)
				if err := ValidateTypeDescs(descs); err == nil {
					t.Fatal("accepted cyclic super chain")
				}
			})
		}
	}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("narrowing/reverse=%v", reverse), func(t *testing.T) {
			base, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
			mid, _ := NewStructDesc(1, []StorageKind{StorageRef, StorageI32})
			child, _ := NewStructDesc(2, []StorageKind{StorageRef, StorageI32, StorageI64})
			base.Final, mid.Final = false, false
			mid.HasSuper, child.HasSuper, child.Super = true, true, 1
			descs := []TypeDesc{base, mid, child}
			childIndex := 2
			if reverse {
				descs = []TypeDesc{child, mid, base}
				descs[0].ID, descs[2].ID, descs[1].Super = 0, 2, 2
				childIndex = 0
			}
			if err := ValidateTypeDescs(descs); err != nil {
				t.Fatal(err)
			}
			// C widens B's inherited storage back to A's nullable storage.
			// Checking only the root ancestor would incorrectly accept C.
			descs[childIndex].Fields[0].Kind = StorageRefNull
			if err := ValidateTypeDescs(descs); err == nil {
				t.Fatal("accepted widening through an intermediate supertype")
			}
		})
	}
}

func TestValidateSubtypeMalformedMetadata(t *testing.T) {
	for _, super := range []TypeID{1, 1 << 31, ^TypeID(0)} {
		if err := ValidateTypeDescs([]TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: super}}); err == nil {
			t.Errorf("accepted out-of-range super %d", super)
		}
	}
	for _, mutate := range []func(*TypeDesc){
		func(d *TypeDesc) { d.ID = 0 },
		func(d *TypeDesc) { d.Kind = 255 },
		func(d *TypeDesc) { d.Fields[0].Kind = 255 },
		func(d *TypeDesc) { d.Fields[0].Offset = ^uint32(0) },
		func(d *TypeDesc) { d.Align = 0 },
		func(d *TypeDesc) { d.Size = 0 },
		func(d *TypeDesc) { d.HasRefs = false },
	} {
		// A forward super must be checked structurally before its fields are
		// used to validate a child, regardless of table order.
		child, _ := NewStructDesc(0, []StorageKind{StorageRefNull})
		parent, _ := NewStructDesc(1, []StorageKind{StorageRefNull})
		child.HasSuper, child.Super, parent.Final = true, 1, false
		mutate(&parent)
		if err := ValidateTypeDescs([]TypeDesc{child, parent}); err == nil {
			t.Error("accepted malformed forward super")
		}
	}
}

func subtypeChain(depth int, kind TypeKind, reverse bool) []TypeDesc {
	descs := make([]TypeDesc, depth)
	for i := range descs {
		switch kind {
		case KindStruct:
			descs[i], _ = NewStructDesc(TypeID(i), []StorageKind{StorageI8, StorageRefNull, StorageI64, StorageV128})
		case KindArray:
			descs[i], _ = NewArrayDesc(TypeID(i), StorageRefNull)
		case KindFunc:
			descs[i] = TypeDesc{ID: TypeID(i), Kind: KindFunc}
		}
		descs[i].Final = false
		if reverse && i+1 < depth {
			descs[i].HasSuper, descs[i].Super = true, TypeID(i+1)
		} else if !reverse && i > 0 {
			descs[i].HasSuper, descs[i].Super = true, TypeID(i-1)
		}
	}
	return descs
}

func BenchmarkValidateSubtypeLayouts(b *testing.B) {
	for _, kind := range []TypeKind{KindStruct, KindArray} {
		for _, depth := range []int{4, 64, 4096} {
			b.Run(fmt.Sprintf("kind=%d/depth=%d", kind, depth), func(b *testing.B) {
				// Child-before-parent order also exercises a full-depth path in
				// the existing iterative, linear-time cycle detector.
				descs := subtypeChain(depth, kind, true)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := ValidateTypeDescs(descs); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSubtypeCollectionPreservesInheritedReference(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		for _, array := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile=%d/array=%v", profile, array), func(t *testing.T) {
				valueType, _ := NewStructDesc(0, []StorageKind{StorageI32})
				base, _ := NewStructDesc(1, []StorageKind{StorageRefNull})
				mid, _ := NewStructDesc(2, []StorageKind{StorageRef, StorageI32})
				child, _ := NewStructDesc(3, []StorageKind{StorageRef, StorageI32, StorageI64})
				if array {
					base, _ = NewArrayDesc(1, StorageRefNull)
					mid, _ = NewArrayDesc(2, StorageRef)
					child, _ = NewArrayDesc(3, StorageRef)
				}
				base.Final, mid.Final = false, false
				mid.HasSuper, mid.Super, child.HasSuper, child.Super = true, 1, true, 2
				c, err := NewCollector(Config{Profile: profile}, []TypeDesc{valueType, base, mid, child})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				target, err := c.NewStructWithRoots(0, []Value{I32Value(42)}, EmptyRoots{})
				if err != nil {
					t.Fatal(err)
				}
				targetRoot := Root(target)
				var owner Ref
				if array {
					owner, err = c.NewArrayWithRoots(3, 2, RefValue(target), Slots{&targetRoot})
				} else {
					owner, err = c.NewStructWithRoots(3, []Value{RefValue(target), I32Value(7), I64Value(8)}, Slots{&targetRoot})
				}
				if err != nil {
					t.Fatal(err)
				}
				root := Root(owner)
				// Only the subtype object is rooted. Its concrete descriptor
				// must trace the target through the inherited reference slot.
				if err := c.CollectFull(Slots{&root}); err != nil {
					t.Fatal(err)
				}
				for required := TypeID(1); required <= 3; required++ {
					var value Value
					var actual TypeID
					var matched bool
					if array {
						value, actual, matched, err = c.ArrayGetTyped(Ref(root), required, false, 0)
					} else {
						value, actual, matched, err = c.StructGetTyped(Ref(root), required, false, 0)
					}
					if err != nil || !matched || actual != 3 {
						t.Fatalf("access as type %d: actual=%d matched=%v err=%v", required, actual, matched, err)
					}
					got, err := c.StructGet(value.Ref, 0)
					if err != nil || got.I32() != 42 {
						t.Fatalf("inherited reference lost target: value=%v err=%v", got, err)
					}
				}
			})
		}
	}
}
