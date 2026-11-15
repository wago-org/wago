// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "testing"

func TestObjectTypeFlagsProjection(t *testing.T) {
	for _, tc := range []struct {
		name string
		desc TypeDesc
		want uint32
	}{
		{"struct", TypeDesc{ID: 7, Kind: KindStruct}, FlagTypeFastValid | FlagTypeStruct},
		{"final array", TypeDesc{ID: 9, Kind: KindArray, Final: true}, FlagTypeFastValid | FlagTypeArray | FlagTypeFinal},
		{"function", TypeDesc{ID: 11, Kind: KindFunc}, 0},
		{"full id", TypeDesc{ID: TypeID(^uint32(0)), Kind: KindStruct, Final: true}, FlagTypeFastValid | FlagTypeStruct | FlagTypeFinal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := ObjectTypeFlags(tc.desc)
			if flags != tc.want {
				t.Fatalf("flags = %#x, want %#x", flags, tc.want)
			}
		})
	}
}

func TestObjectTypeFlagsSurviveCollection(t *testing.T) {
	base, err := NewStructDesc(0, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	base.Final = false
	child, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	child.Super, child.HasSuper = 0, true
	c, err := NewCollector(Config{NurseryBytes: 1 << 16}, []TypeDesc{base, child})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.tenuringThreshold = 1
	r, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	want := ObjectTypeFlags(child)
	if header := c.header(r); header.TypeID != uint32(child.ID) || header.Flags&want != want {
		t.Fatalf("new header = %+v, want type %d flags %#x", header, child.ID, want)
	}
	roots := RefSliceRoots{r}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if header := c.header(roots[0]); header.TypeID != uint32(child.ID) || header.Flags&want != want {
		t.Fatalf("promoted header = %+v, want type %d flags %#x", header, child.ID, want)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if header := c.header(roots[0]); header.TypeID != uint32(child.ID) || header.Flags&want != want {
		t.Fatalf("major header = %+v, want type %d flags %#x", header, child.ID, want)
	}
}

func TestObjectTypeFamilyFlagsAndLegacyFallback(t *testing.T) {
	structure, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(1, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{DisableCollection: true}, []TypeDesc{structure, array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	structureRef, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	arrayRef, err := c.NewArrayDefault(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ref  Ref
		kind RefTestKind
		id   TypeID
	}{
		{"struct", structureRef, RefTestStruct, 0},
		{"array", arrayRef, RefTestArray, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if matched, err := c.RefTest(tc.ref, RefTestTarget{Kind: tc.kind}); err != nil || !matched {
				t.Fatalf("packed family test = %t, %v", matched, err)
			}
			header := c.header(tc.ref)
			if header.Flags&FlagTypeFastValid == 0 {
				t.Fatalf("packed header flags = %#x", header.Flags)
			}
			header.Flags &^= FlagTypeFastValid | FlagTypeStruct | FlagTypeArray | FlagTypeFinal
			c.writeHeader(tc.ref, header)
			if matched, err := c.RefTest(tc.ref, RefTestTarget{Kind: tc.kind}); err != nil || !matched {
				t.Fatalf("legacy family fallback = %t, %v", matched, err)
			}
		})
	}
}
