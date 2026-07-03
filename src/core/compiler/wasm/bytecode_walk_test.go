package wasm

import "testing"

func TestSkipInstructionImmediateRepresentativeFormats(t *testing.T) {
	cases := []struct {
		name string
		op   byte
		imm  []byte
	}{
		{"blocktype void", 0x02, []byte{0x40}},
		{"call", 0x10, []byte{0x2a}},
		{"call_indirect", 0x11, []byte{0x01, 0x00}},
		{"br_table", 0x0e, []byte{0x02, 0x00, 0x01, 0x02}},
		{"memarg", 0x28, []byte{0x02, 0x80, 0x01}},
		{"memory.grow", 0x40, []byte{0x00}},
		{"fc trunc_sat", 0xfc, []byte{0x00}},
		{"fc memory.copy", 0xfc, []byte{0x0a, 0x00, 0x00}},
		{"fd v128.const", 0xfd, append([]byte{0x0c}, make([]byte, 16)...)},
		{"fb ref.test heaptype", 0xfb, []byte{0x14, 0x6e}},
		{"fb ref.test nullable heaptype", 0xfb, []byte{0x15, 0x6e}},
		{"fb br_on_cast", 0xfb, []byte{0x18, 0x03, 0x00, 0x6e, 0x6d}},
		{"fb br_on_cast_fail", 0xfb, []byte{0x19, 0x03, 0x00, 0x6e, 0x6d}},
		{"try_table", 0x1f, []byte{0x40, 0x01, 0x00, 0x00, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(tc.imm)
			if err := SkipInstructionImmediate(r, tc.op); err != nil {
				t.Fatalf("SkipInstructionImmediate: %v", err)
			}
			if r.HasNext() {
				t.Fatalf("reader left %d byte(s)", r.BytesLeft())
			}
		})
	}
}

func TestSkipInstructionImmediateRejectsMalformedVectors(t *testing.T) {
	cases := []struct {
		name string
		op   byte
		imm  []byte
	}{
		{"truncated br_table", 0x0e, []byte{0x01, 0x00}},
		{"truncated try_table catch", 0x1f, []byte{0x40, 0x01, 0x00, 0x00}},
		{"invalid br_on_cast flags", 0xfb, []byte{0x18, 0x04, 0x00, 0x6e, 0x6d}},
		{"invalid br_on_cast_fail flags", 0xfb, []byte{0x19, 0x04, 0x00, 0x6e, 0x6d}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SkipInstructionImmediate(NewReader(tc.imm), tc.op); err == nil {
				t.Fatal("SkipInstructionImmediate succeeded, want error")
			}
		})
	}
}

func TestSkipInstructionImmediateGCRefTestRejectsExactHeapMarker(t *testing.T) {
	for _, sub := range []byte{0x14, 0x15} {
		if err := SkipInstructionImmediate(NewReader([]byte{sub, 0x62, 0x00}), 0xfb); err == nil {
			t.Fatalf("SkipInstructionImmediate ref.test subop %#x succeeded with exact heap marker, want error", sub)
		}
	}
}
