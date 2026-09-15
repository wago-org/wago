package wasm

import "testing"

func TestDecodeSIMDInstructionDescriptor(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
		kind  InstrKind
		class SIMDEffectClass
		lane  LaneIdx
	}{
		{"const", append([]byte{0x0c}, make([]byte, 16)...), InstrV128Const, SIMDEffectConst, 0},
		{"shuffle", append([]byte{0x0d}, make([]byte, 16)...), InstrI8x16Shuffle, SIMDEffectBinary, 0},
		{"extract", []byte{0x1b, 0x03}, InstrI32x4ExtractLane, SIMDEffectExtract, 3},
		{"load lane", []byte{0x54, 0x00, 0x00, 0x0f}, InstrV128Load8Lane, SIMDEffectLoadLane, 15},
		{"binary", []byte{0x4e}, InstrV128And, SIMDEffectBinary, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := ReaderFrom(test.bytes)
			d, err := DecodeSIMDInstruction(&r, &Module{}, false)
			if err != nil {
				t.Fatal(err)
			}
			if d.Kind != test.kind || d.Class != test.class || d.Lane != test.lane || r.HasNext() {
				t.Fatalf("descriptor=%#v remaining=%d", d, r.BytesLeft())
			}
		})
	}
}

func TestDecodeSIMDInstructionDescriptorRejectsLaneOutOfRange(t *testing.T) {
	r := ReaderFrom([]byte{0x1b, 0x04}) // i32x4.extract_lane 4
	if _, err := DecodeSIMDInstruction(&r, &Module{}, false); err == nil {
		t.Fatal("out-of-range lane was accepted")
	}
}
