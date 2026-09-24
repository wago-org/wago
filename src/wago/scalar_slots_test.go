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
	for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 16, 63, 64, 65, 127, 128, 129} {
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

func TestNarrowScalarSlotCopyCanaries(t *testing.T) {
	const canary = uint64(0xdeadbeefcafebabe)
	for n := 0; n <= 129; n++ {
		src := make([]uint64, n+2)
		dst := make([]uint64, n+2)
		src[0], src[n+1] = canary, canary
		dst[0], dst[n+1] = canary, canary
		for i := 0; i < n; i++ {
			src[i+1] = 0xffffffff00000000 | uint64(i+1)
		}
		copyNarrowScalarSlots(dst[1:n+1], src[1:n+1], n)
		if dst[0] != canary || dst[n+1] != canary || src[0] != canary || src[n+1] != canary {
			t.Fatalf("n=%d: copy touched a canary", n)
		}
		for i := 0; i < n; i++ {
			if got, want := dst[i+1], uint64(i+1); got != want {
				t.Fatalf("n=%d slot=%d: got %x, want %x", n, i, got, want)
			}
		}
	}
}
