package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestCompiledRejectsIncompatibleGCSubtypeLayouts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, child []gc.StorageKind
		array, shift  bool
	}{
		{name: "hidden struct reference", parent: []gc.StorageKind{gc.StorageRefNull}, child: []gc.StorageKind{gc.StorageI32}},
		{name: "shifted field", parent: []gc.StorageKind{gc.StorageI32}, child: []gc.StorageKind{gc.StorageI32}, shift: true},
		{name: "missing field", parent: []gc.StorageKind{gc.StorageI32, gc.StorageRefNull}, child: []gc.StorageKind{gc.StorageI32}},
		{name: "changed array element", parent: []gc.StorageKind{gc.StorageRefNull}, child: []gc.StorageKind{gc.StorageI32}, array: true},
		{name: "widened array reference", parent: []gc.StorageKind{gc.StorageRef}, child: []gc.StorageKind{gc.StorageRefNull}, array: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, _ := gc.NewStructDesc(0, tc.parent)
			child, _ := gc.NewStructDesc(1, tc.child)
			if tc.array {
				parent, _ = gc.NewArrayDesc(0, tc.parent[0])
				child, _ = gc.NewArrayDesc(1, tc.child[0])
			}
			if tc.shift {
				child.Fields[0].Offset += 4
				child.Size += 4
			}
			parent.Final, child.HasSuper = false, true
			c := &Compiled{GCTypeDescs: []gc.TypeDesc{parent, child}}
			defer c.Close()
			// Serialization preserves the supplied table; each admission path
			// must reject it before making compiled code usable.
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			for _, load := range []struct {
				name string
				fn   func(*Compiled) error
			}{
				{"UnmarshalBinary", func(out *Compiled) error { return out.UnmarshalBinary(blob) }},
				{"ReadFrom", func(out *Compiled) error { _, err := out.ReadFrom(bytes.NewReader(blob)); return err }},
			} {
				t.Run(load.name, func(t *testing.T) {
					var out Compiled
					defer out.Close()
					if err := load.fn(&out); err == nil {
						t.Fatal("accepted incompatible subtype layout")
					}
				})
			}
			if out, err := LoadTrustedArtifact(blob); err == nil {
				out.Close()
				t.Error("LoadTrustedArtifact accepted incompatible subtype layout")
			}
			if err := c.validate(); err == nil {
				t.Error("direct metadata validation accepted incompatible subtype layout")
			}
			// Freeze the execution snapshot so this also exercises the memoized
			// path used by instantiation, rather than repeated direct checks.
			if _, err := c.freezeExecution(0); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := c.validateCached(); err == nil {
					t.Error("cached validation accepted incompatible subtype layout")
				}
			}
		})
	}
}
