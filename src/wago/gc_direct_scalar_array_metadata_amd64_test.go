//go:build linux && amd64 && (wago_gc_direct_scalar_arrays || wago_gc_direct_copy) && !wago_gc_direct_trusted_arrays && !wago_gc_direct_trusted_refstruct && !wago_gc_direct_trusted_alloc && !wago_gc_direct_trusted_loop

package wago

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestDirectGCScalarArrayPrivateMetadata(t *testing.T) {
	in := newDirectScalarArrayInstance(t, directScalarArrayModule(0x7e, 0x7e, 11))
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	if _, e := in.Invoke("set", 1, 77); e != nil {
		t.Fatal(e)
	}
	checkUnchanged := func() {
		got, e := in.Invoke("get", 1)
		if e != nil || len(got) != 1 || got[0] != 77 {
			t.Fatal("failed store mutated array", got, e)
		}
	}
	view := in.gc.DirectView()
	arena := unsafe.Slice((*byte)(unsafe.Pointer(view.Arena)), int(view.ArenaBytes))
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	originalSize := binary.LittleEndian.Uint32(arena[ref+4:])
	originalAux := binary.LittleEndian.Uint32(arena[ref+8:])
	for _, which := range []string{"short-size", "aux-overflow"} {
		if which == "short-size" {
			binary.LittleEndian.PutUint32(arena[ref+4:], 16)
		} else {
			binary.LittleEndian.PutUint32(arena[ref+8:], 0xffffffff)
		}
		idx := uint64(1)
		if which == "aux-overflow" {
			idx = 0xfffffffe
		}
		_, se := in.Invoke("set", idx, 99)
		_, ge := in.Invoke("get", idx)
		binary.LittleEndian.PutUint32(arena[ref+4:], originalSize)
		binary.LittleEndian.PutUint32(arena[ref+8:], originalAux)
		if se == nil || ge == nil {
			t.Fatal("accepted malformed extent", which, se, ge)
		}
		checkUnchanged()
	}
}
