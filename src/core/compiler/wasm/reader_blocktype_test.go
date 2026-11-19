package wasm

import "testing"

func TestReaderBlockTypeConsumesCompleteType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		encoded []byte
		kind    BlockTypeKind
	}{
		{"void", []byte{0x40}, BlockVoid},
		{"scalar", []byte{0x7f}, BlockVal},
		{"index", []byte{0xea, 0x00}, BlockTypeIndex},
		{"nullable", []byte{0x63, 0xea, 0x00}, BlockVal},
		{"non-null", []byte{0x64, 0xea, 0x00}, BlockVal},
		{"nullable-exact", []byte{0x63, 0x62, 0xea, 0x00}, BlockVal},
		{"non-null-exact", []byte{0x64, 0x62, 0xea, 0x00}, BlockVal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append(append([]byte(nil), tc.encoded...), 0x0b)
			r := ReaderFrom(body)
			block, err := r.BlockType()
			if err != nil || block.Kind != tc.kind {
				t.Fatalf("block = %+v, %v; want kind %v", block, err, tc.kind)
			}
			if r.Offset() != len(tc.encoded) {
				t.Fatalf("cursor = %d, want %d", r.Offset(), len(tc.encoded))
			}
			next, err := r.Byte()
			if err != nil || next != 0x0b {
				t.Fatalf("next opcode = %#x, %v; want end", next, err)
			}
		})
	}
}

func TestReaderBlockTypeRejectsMalformedType(t *testing.T) {
	for _, body := range [][]byte{nil, {0x63}, {0x64, 0xea}, {0x64, 0x62}, {0x80}, {0x62}} {
		r := ReaderFrom(body)
		if block, err := r.BlockType(); err == nil {
			t.Errorf("accepted malformed block type %x: %+v", body, block)
		}
	}
}
