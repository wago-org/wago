//go:build linux && amd64 && (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || wago_gc_compact_weighted_sum)) || wago_gc_compact_range_stride)

package wago

import (
	"encoding/binary"
	"fmt"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
	"unsafe"
)

func compactCopyModule(storage, value, get byte) []byte {
	init := []byte{0x41, 8, 0xfb, 7, 0, 0x24, 0, 0x41, 8, 0xfb, 7, 0, 0x24, 1, 0x0b}
	set := func(g byte) []byte { return []byte{0x23, g, 0x20, 0, 0x20, 1, 0xfb, 14, 0, 0x0b} }
	read := func(g byte) []byte { return []byte{0x23, g, 0x20, 0, 0xfb, get, 0, 0x0b} }
	cp := func(d, s byte) []byte {
		return []byte{0x23, d, 0x20, 0, 0x23, s, 0x20, 1, 0x20, 2, 0xfb, 17, 0, 0, 0x0b}
	}
	nullsrc := []byte{0xd0, 0, 0x24, 1, 0x0b}
	bodies := [][]byte{init, set(0), set(1), read(0), read(1), cp(0, 1), cp(0, 0), cp(1, 0), nullsrc}
	names := []string{"init", "setA", "setB", "getA", "getB", "copy", "self", "reverse", "nullsrc"}
	exports, codes := [][]byte{}, [][]byte{}
	for i, b := range bodies {
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
		codes = append(codes, wasmtest.Code(b))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, []byte{0x60, 0, 0}, []byte{0x60, 2, 0x7f, value, 0}, []byte{0x60, 1, 0x7f, 1, value}, []byte{0x60, 3, 0x7f, 0x7f, 0x7f, 0})), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{2}, []byte{3}, []byte{3}, []byte{4}, []byte{4}, []byte{4}, []byte{1})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(codes...)))
}

func TestCompactNativeGCCopyOverlapAndBounds(t *testing.T) {
	for _, tc := range []struct {
		s, v, g byte
		bits    uint64
	}{{0x78, 0x7f, 13, 0x80}, {0x77, 0x7f, 13, 0xf000}, {0x7f, 0x7f, 11, 0xdead0000}, {0x7e, 0x7e, 11, 0x123456789000}, {0x7d, 0x7d, 11, 0x7fc12340}, {0x7c, 0x7c, 11, 0x7ff8123456789000}} {
		t.Run(fmt.Sprintf("%x", tc.s), func(t *testing.T) {
			in := newCompactScalarArrayInstance(t, compactCopyModule(tc.s, tc.v, tc.g))
			if _, e := in.Invoke("copy", 0, 0, 0); e == nil {
				t.Fatal("null empty copy accepted")
			}
			if _, e := in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			expect := make([]uint64, 8)
			reset := func() {
				for i := 0; i < 8; i++ {
					expect[i] = tc.bits + uint64(i)
					for _, n := range []string{"setA", "setB"} {
						if _, e := in.Invoke(n, uint64(i), expect[i]); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			check := func(name string, values []uint64) {
				for i, want := range values {
					got, e := in.Invoke(name, uint64(i))
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatal(name, i, got, e, want)
					}
				}
			}
			const sentinel = 0x81234567
			slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
			for _, span := range [][3]int{{1, 0, 7}, {0, 1, 7}, {0, 0, 8}, {4, 1, 3}, {8, 8, 0}, {2, 3, 0}} {
				reset()
				copy(expect[span[0]:span[0]+span[2]], expect[span[1]:span[1]+span[2]])
				binary.LittleEndian.PutUint32(slot, sentinel)
				if _, e := in.Invoke("self", uint64(span[0]), uint64(span[1]), uint64(span[2])); e != nil {
					t.Fatal(span, e)
				}
				if binary.LittleEndian.Uint32(slot) != sentinel {
					t.Fatal("copy used helper")
				}
				check("getA", expect)
			}
			for _, name := range []string{"copy", "reverse"} {
				reset()
				binary.LittleEndian.PutUint32(slot, sentinel)
				if _, e := in.Invoke(name, 0x100000001, 2, 5); e != nil {
					t.Fatal(e)
				}
				if binary.LittleEndian.Uint32(slot) != sentinel {
					t.Fatal("distinct copy used helper")
				}
				copy(expect[1:6], expect[2:7])
				getter := "getA"
				if name == "reverse" {
					getter = "getB"
				}
				check(getter, expect)
			}
			for _, span := range [][3]uint64{{9, 0, 0}, {0, 9, 0}, {7, 0, 2}, {0, 7, 2}, {0xffffffff, 0, 1}, {0, 0xffffffff, 1}, {0, 0, 0xffffffff}} {
				reset()
				if _, e := in.Invoke("copy", span[0], span[1], span[2]); e == nil {
					t.Fatal("invalid span", span)
				}
				check("getA", expect)
			}
			if _, e := in.Invoke("nullsrc"); e != nil {
				t.Fatal(e)
			}
			if _, e := in.Invoke("copy", 0xffffffff, 0, 1); e == nil || !strings.Contains(e.Error(), "null") {
				t.Fatal("source null must precede destination bounds", e)
			}
			if in.gc.Stats().FullCollections == 0 {
				t.Fatal("no moving collection")
			}
		})
	}
}

func TestCompactNativeGCCopyPhysicalFailureDoesNotWrite(t *testing.T) {
	in := newCompactScalarArrayInstance(t, compactCopyModule(0x7f, 0x7f, 11))
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		if _, e := in.Invoke("setA", uint64(i), uint64(100+i)); e != nil {
			t.Fatal(e)
		}
		if _, e := in.Invoke("setB", uint64(i), uint64(200+i)); e != nil {
			t.Fatal(e)
		}
	}
	v := in.gc.CompactView()
	arena := unsafe.Slice((*byte)(unsafe.Pointer(v.Arena)), int(v.ArenaBytes))
	for _, g := range []int{0, 1} {
		ref := uint32(readGlobalObject(in.globalCells[g], in.c.Globals[g].Type))
		size := binary.LittleEndian.Uint32(arena[ref+4:])
		binary.LittleEndian.PutUint32(arena[ref+4:], 0xffffffff)
		_, e := in.Invoke("copy", 0, 0, 8)
		binary.LittleEndian.PutUint32(arena[ref+4:], size)
		if e == nil {
			t.Fatal("short physical extent accepted", g)
		}
		for i := 0; i < 8; i++ {
			got, e := in.Invoke("getA", uint64(i))
			if e != nil || got[0] != uint64(100+i) {
				t.Fatal("partial destination write", g, i, got, e)
			}
		}
	}
}
