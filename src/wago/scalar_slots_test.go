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
