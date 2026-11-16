package wago

import (
	"encoding/json"
	compact "github.com/wago-org/wago/experiments/compactgc"
	direct "github.com/wago-org/wago/experiments/directgc"
	"os"
	"testing"
)

func TestGCCompactGeometryProbe(t *testing.T) {
	if os.Getenv("GC_GEOMETRY_PROBE") != "1" {
		t.Skip("set GC_GEOMETRY_PROBE=1")
	}
	const eden, old = 2 << 20, 8 << 20
	d, e := direct.NewWithTypes(eden, old, []direct.Descriptor{{Kind: direct.KindStruct, PayloadBytes: 4, Align: 8}, {Kind: direct.KindArray, ElementSize: 4, Align: 8}, {Kind: direct.KindStruct, PayloadBytes: 12, Align: 8, RefOffsets: []uint32{4, 8}}})
	if e != nil {
		t.Fatal(e)
	}
	c, e := compact.NewWithTypes(eden, old, []compact.Descriptor{{Kind: compact.KindStruct, PayloadBytes: 4, Align: 4}, {Kind: compact.KindArray, ElementSize: 4, Align: 4}, {Kind: compact.KindStruct, PayloadBytes: 12, Align: 4, RefOffsets: []uint32{4, 8}}})
	if e != nil {
		t.Fatal(e)
	}
	rows := []map[string]any{}
	for _, x := range []struct {
		name  string
		id    uint32
		array bool
		n     uint32
	}{{"i32-struct", 0, false, 0}, {"i32-array16", 1, true, 16}, {"tree-i32-two-refs", 2, false, 0}} {
		var dr, dnext direct.Ref
		var cr, cnext compact.Ref
		if x.array {
			dr, e = d.NewArray(direct.TypeID(x.id), x.n)
			if e == nil {
				dnext, e = d.NewArray(direct.TypeID(x.id), x.n)
			}
		} else {
			dr, e = d.NewStruct(direct.TypeID(x.id))
			if e == nil {
				dnext, e = d.NewStruct(direct.TypeID(x.id))
			}
		}
		if e != nil {
			t.Fatal(e)
		}
		if x.array {
			cr, e = c.NewArray(compact.TypeID(x.id), x.n)
			if e == nil {
				cnext, e = c.NewArray(compact.TypeID(x.id), x.n)
			}
		} else {
			cr, e = c.NewStruct(compact.TypeID(x.id))
			if e == nil {
				cnext, e = c.NewStruct(compact.TypeID(x.id))
			}
		}
		if e != nil {
			t.Fatal(e)
		}
		bytes, e := d.ObjectBytes(dr)
		if e != nil {
			t.Fatal(e)
		}
		h, e := c.Header(cr)
		if e != nil {
			t.Fatal(e)
		}
		rows = append(rows, map[string]any{"shape": x.name, "direct_extent": len(bytes), "direct_stride": uint32(dnext - dr), "direct_payload_offset": 16, "compact_header": h, "compact_stride_observed": uint32(cnext - cr)})
	}
	b, e := json.Marshal(map[string]any{"eden_bytes": eden, "old_semispace_bytes": old, "direct_fixed_backing_bytes": d.FixedBytes(), "compact_fixed_backing_bytes": c.FixedBytes(), "scope": "independent equivalent substrate descriptors; excludes adapter/type metadata and Go overhead", "shapes": rows})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("GEOMETRY_RESULT " + string(b))
}
