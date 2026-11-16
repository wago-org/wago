//go:build linux && amd64 && (wago_gc_compact_fill || (wago_gc_compact_copy || wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || (wago_gc_compact_range_sse2 || (wago_gc_compact_weighted_sum || wago_gc_compact_weighted_sse2))) || wago_gc_compact_range_stride))

package wago

import (
	"encoding/binary"
	"fmt"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestCompactNativeGCFillSSE2TailsAndBitPatterns(t *testing.T) {
	for _, tc := range []struct {
		s, v, g    byte
		width      int
		bits, want uint64
	}{
		{0x78, 0x7f, 12, 1, 0x1234ff, 0xffffffff}, {0x78, 0x7f, 13, 1, 0x1234ff, 255},
		{0x77, 0x7f, 12, 2, 0x1234ffff, 0xffffffff}, {0x77, 0x7f, 13, 2, 0x1234ffff, 65535},
		{0x7f, 0x7f, 11, 4, 0xdeadbeef, 0xdeadbeef}, {0x7e, 0x7e, 11, 8, 0x123456789abcdef, 0x123456789abcdef},
		{0x7d, 0x7d, 11, 4, 0x7fc12345, 0x7fc12345}, {0x7c, 0x7c, 11, 8, 0x7ff8123456789abc, 0x7ff8123456789abc},
		{0x7d, 0x7d, 11, 4, 0x80000000, 0x80000000}, {0x7c, 0x7c, 11, 8, 0x8000000000000000, 0x8000000000000000},
	} {
		t.Run(fmt.Sprintf("%x/%x/%d", tc.s, tc.bits, tc.g), func(t *testing.T) {
			lanes := 16 / tc.width
			size := 2*lanes + 5
			in := newCompactScalarArrayInstance(t, compactFillSizedModule(tc.s, tc.v, tc.g, byte(size)))
			if _, e := in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			for _, count := range []int{0, 1, lanes - 1, lanes, lanes + 1, 2*lanes + 3} {
				if _, e := in.Invoke("fill", 0, 0, uint64(size)); e != nil {
					t.Fatal(e)
				}
				const sentinel = 0x81234567
				slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
				binary.LittleEndian.PutUint32(slot, sentinel)
				if _, e := in.Invoke("fill", 1, tc.bits, uint64(count)); e != nil {
					t.Fatal(count, e)
				}
				if binary.LittleEndian.Uint32(slot) != sentinel {
					t.Fatal("fill used helper")
				}
				for i := 0; i < size; i++ {
					got, e := in.Invoke("get", uint64(i))
					want := uint64(0)
					if i >= 1 && i < 1+count {
						want = tc.want
					}
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatal(count, i, got, e, want)
					}
				}
			}
		})
	}
}
