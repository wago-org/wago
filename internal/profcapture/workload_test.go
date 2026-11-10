package profcapture

import (
	"reflect"
	"testing"
)

func TestSlotListPreservesExactValues(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []uint64
	}{
		{"", []uint64{}}, {"[]", []uint64{}}, {" [ ] ", []uint64{}},
		{"20,22", []uint64{20, 22}}, {" [ 20, 22 ] ", []uint64{20, 22}},
		{"0xffffffffffffffff,0x8000000000000000", []uint64{^uint64(0), 1 << 63}},
	} {
		got, err := ParseSlots(tt.input)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%q: %v, %v", tt.input, got, err)
		}
	}
}

func TestSlotListRejectsMalformedConfiguration(t *testing.T) {
	for _, input := range []string{
		"[37", "37]", "]37[", "[[37]]", "[37]]", "[[37]", "][", "[", "]",
		"1,,2", "[1,]", "[,1]", "1 2", "-1", "18446744073709551616", "0x10000000000000000",
	} {
		if got, err := ParseSlots(input); err == nil {
			t.Fatalf("accepted malformed slot list %q as %v", input, got)
		}
	}
}
