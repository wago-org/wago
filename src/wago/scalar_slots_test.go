package wago

import (
	"reflect"
	"testing"
)

func TestScalarSlotWidthClass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		wide  []bool
		class scalarSlotWidthClass
		want  []uint64
	}{
		{"empty", nil, scalarSlotWide, []uint64{}},
		{"narrow", []bool{false, false}, scalarSlotNarrow, []uint64{1, 2}},
		{"wide", []bool{true, true}, scalarSlotWide, []uint64{0x100000001, 0x100000002}},
		{"mixed", []bool{false, true}, scalarSlotMixed, []uint64{1, 0x100000002}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyScalarSlotWidths(tc.wide); got != tc.class {
				t.Fatalf("class = %d, want %d", got, tc.class)
			}
			src := []uint64{0x100000001, 0x100000002}[:len(tc.wide)]
			got := make([]uint64, len(src))
			copyPublicScalarSlotsByClass(got, src, tc.wide, tc.class)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("marshal = %v, want %v", got, tc.want)
			}
			copyPublicScalarSlotsByClass(got, src, tc.wide, tc.class)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNarrowScalarSlotCopyUnrolledBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 3, 4, 5, 7, 8, 9, 16, 64, 128} {
		src := make([]uint64, n)
		wide := make([]bool, n)
		for i := range src {
			src[i] = 0xabcdefff00000000 | uint64(i+1)
		}
		for _, inPlace := range []bool{false, true} {
			dst := make([]uint64, n)
			if inPlace {
				copy(dst, src)
				copyPublicScalarSlotsByClass(dst, dst, wide, scalarSlotNarrow)
			} else {
				copyPublicScalarSlotsByClass(dst, src, wide, scalarSlotNarrow)
			}
			for i, got := range dst {
				if want := uint64(uint32(src[i])); got != want {
					t.Fatalf("n=%d inPlace=%v slot=%d: got %x, want %x", n, inPlace, i, got, want)
				}
			}
		}
	}
}
