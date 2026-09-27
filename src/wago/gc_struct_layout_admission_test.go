package wago

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestCompiledGCStructLayoutAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		desc  gc.TypeDesc
		valid bool
	}{
		{"overlapping reference", gc.TypeDesc{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageRefNull}, {Kind: gc.StorageI32}}, Size: 4, Align: 4, HasRefs: true}, false},
		{"partial overlap", gc.TypeDesc{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageI64}, {Kind: gc.StorageRefNull, Offset: 4}}, Size: 8, Align: 8, HasRefs: true}, false},
		{"understated alignment", gc.TypeDesc{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageV128}}, Size: 16, Align: 1}, false},
		{"over-aligned", gc.TypeDesc{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageI32}}, Size: 16, Align: 16}, true},
		{"sparse reordered", gc.TypeDesc{Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageI8, Offset: 262144}, {Kind: gc.StorageI8}}, Size: 262145, Align: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{GCTypeDescs: []gc.TypeDesc{tc.desc}}
			defer c.Close()
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
					if err := load.fn(&out); (err == nil) != tc.valid {
						t.Fatalf("load = %v, want accepted=%v", err, tc.valid)
					}
					if tc.valid && !reflect.DeepEqual(out.GCTypeDescs, c.GCTypeDescs) {
						t.Fatal("artifact load changed field layout or indexes")
					}
				})
			}
			out, err := LoadTrustedArtifact(blob)
			if out != nil {
				out.Close()
			}
			if (err == nil) != tc.valid {
				t.Errorf("LoadTrustedArtifact = %v, want accepted=%v", err, tc.valid)
			}
			if err := c.validate(); (err == nil) != tc.valid {
				t.Errorf("validate = %v, want accepted=%v", err, tc.valid)
			}
			if _, err := c.freezeExecution(0); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := c.validateCached(); (err == nil) != tc.valid {
					t.Errorf("validateCached = %v, want accepted=%v", err, tc.valid)
				}
			}
		})
	}
}
