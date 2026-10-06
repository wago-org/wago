//go:build linux && !tinygo

package riscv64

import "testing"

func TestPrivateNativeTableThunkBounds(t *testing.T) {
	if nativeTableFrameBytes%16 != 0 || nativeTableFrameBytes != 5424 {
		t.Fatal("native table frame layout", nativeTableFrameBytes)
	}
	for _, key := range []uint64{0, 0xfedcba9876543210} {
		for _, slots := range []int{0, 1, 64, 255, 256} {
			for _, results := range []int{0, 1, 256} {
				code, err := nativeTableThunk(key, slots, results, MaxFunctions-1)
				if err != nil || len(code) == 0 || len(code)%4 != 0 || len(code) > 16<<10 {
					t.Fatal("bounded table transport emission", key, slots, results, len(code), err)
				}
			}
		}
	}
	for _, limits := range [][3]int{{-1, 0, 0}, {257, 0, 0}, {0, -1, 0}, {0, 257, 0}, {0, 0, MaxFunctions}} {
		if code, err := nativeTableThunk(0, limits[0], limits[1], uint32(limits[2])); code != nil || err == nil {
			t.Fatal("unbounded table transport emitted", limits, err)
		}
	}
}
