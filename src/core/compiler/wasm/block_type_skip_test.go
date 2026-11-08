package wasm

import (
	"errors"
	"testing"
)

func TestSkipBlockTypeRejectsMalformedEncoding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		code   DecodeErrorCode
		offset int
	}{
		{"missing", nil, ErrInvalidBlockType, 0},
		{"nullable missing heap type", []byte{0x63}, ErrIndexOutOfBounds, 1},
		{"non-null truncated heap type", []byte{0x64, 0x80}, ErrIndexOutOfBounds, 2},
		{"truncated type index", []byte{0x80}, ErrInvalidBlockType, 1},
		{"nullable invalid negative heap type", []byte{0x63, 0x7f}, ErrInvalidType, 2},
		{"non-null invalid negative heap type", []byte{0x64, 0x7f}, ErrInvalidType, 2},
		{"nullable heap type overflow", []byte{0x63, 0x80, 0x80, 0x80, 0x80, 0x20}, ErrMalformedLEB, 6},
		{"non-null heap type overflow", []byte{0x64, 0x80, 0x80, 0x80, 0x80, 0x20}, ErrMalformedLEB, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ReaderFrom(tc.data)
			err := SkipBlockType(&r)
			var decodeErr *DecodeError
			if !errors.As(err, &decodeErr) || decodeErr.Code != tc.code || decodeErr.Offset != tc.offset {
				t.Fatalf("skip %x: got %v, want %v at offset %d", tc.data, err, tc.code, tc.offset)
			}
			if r.Offset() != tc.offset {
				t.Fatalf("reader offset = %d, want %d", r.Offset(), tc.offset)
			}
		})
	}
}
