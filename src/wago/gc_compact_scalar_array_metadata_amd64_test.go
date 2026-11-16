//go:build linux && amd64 && (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || wago_gc_compact_range_sse2) || wago_gc_compact_range_stride))))

package wago

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestCompactNativeGCScalarArrayPrivateMetadata(t *testing.T) {
	in := newCompactScalarArrayInstance(t, compactScalarArrayModule(0x7e, 0x7e, 11))
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	if _, e := in.Invoke("set", 1, 77); e != nil {
		t.Fatal(e)
	}
	view := in.gc.CompactView()
	saved := *view
	arena := unsafe.Slice((*byte)(unsafe.Pointer(view.Arena)), int(view.ArenaBytes))
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	length := binary.LittleEndian.Uint32(arena[ref+4:])
	for _, kind := range []string{"length-overflow", "active-stride", "header-type"} {
		typ := binary.LittleEndian.Uint32(arena[ref:])
		switch kind {
		case "length-overflow":
			binary.LittleEndian.PutUint32(arena[ref+4:], 0xffffffff)
		case "active-stride":
			if ref >= view.NurseryStart && ref < view.NurseryBump {
				view.NurseryBump = ref + 31
			} else {
				view.OldBump = ref + 31
			}
		case "header-type":
			binary.LittleEndian.PutUint32(arena[ref:], 0xffffffff)
		}
		_, se := in.Invoke("set", 1, 99)
		_, ge := in.Invoke("get", 1)
		*view = saved
		binary.LittleEndian.PutUint32(arena[ref+4:], length)
		binary.LittleEndian.PutUint32(arena[ref:], typ)
		if se == nil || ge == nil {
			t.Fatal("accepted invalid compact metadata", kind, se, ge)
		}
		got, e := in.Invoke("get", 1)
		if e != nil || len(got) != 1 || got[0] != 77 {
			t.Fatal("failed store modified payload", got, e)
		}
	}
}
