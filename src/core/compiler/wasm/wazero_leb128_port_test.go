package wasm

import (
	"math"
	"testing"
)

// TestWazeroPortLEB128Decoding ports the decoder boundary tables from
// wazero/internal/leb128/leb128_test.go at c0f3a4ec.
func TestWazeroPortLEB128Decoding(t *testing.T) {
	t.Run("u32", func(t *testing.T) {
		tests := []struct {
			in      []byte
			want    uint32
			wantErr bool
		}{
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, math.MaxUint32, false},
			{[]byte{0x00}, 0, false},
			{[]byte{0x04}, 4, false},
			{[]byte{0x80, 0x00}, 0, false},
			{[]byte{0x80, 0x7f}, 16256, false},
			{[]byte{0xe5, 0x8e, 0x26}, 624485, false},
			{[]byte{0x80, 0x80, 0x80, 0x4f}, 165675008, false},
			{[]byte{0x83, 0x80, 0x80, 0x80, 0x80, 0x00}, 0, true},
			{[]byte{0x82, 0x80, 0x80, 0x80, 0x70}, 0, true},
			{[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00}, 0, true},
		}
		for _, tt := range tests {
			r := NewReader(tt.in)
			got, err := r.U32()
			if tt.wantErr {
				if err == nil {
					t.Errorf("U32(%x) = %d, want error", tt.in, got)
				}
			} else if err != nil || got != tt.want || r.Offset() != len(tt.in) {
				t.Errorf("U32(%x) = %d, offset %d, err %v; want %d", tt.in, got, r.Offset(), err, tt.want)
			}
		}
	})

	t.Run("u64", func(t *testing.T) {
		tests := []struct {
			in      []byte
			want    uint64
			wantErr bool
		}{
			{[]byte{0x04}, 4, false},
			{[]byte{0x80, 0x7f}, 16256, false},
			{[]byte{0xe5, 0x8e, 0x26}, 624485, false},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, math.MaxUint32, false},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, math.MaxUint64, false},
			{[]byte{0x89, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x71}, 0, true},
		}
		for _, tt := range tests {
			r := NewReader(tt.in)
			got, err := r.U64()
			if tt.wantErr {
				if err == nil {
					t.Errorf("U64(%x) = %d, want error", tt.in, got)
				}
			} else if err != nil || got != tt.want || r.Offset() != len(tt.in) {
				t.Errorf("U64(%x) = %d, offset %d, err %v; want %d", tt.in, got, r.Offset(), err, tt.want)
			}
		}
	})

	t.Run("i32", func(t *testing.T) {
		tests := []struct {
			in      []byte
			want    int32
			wantErr bool
		}{
			{[]byte{0x13}, 19, false},
			{[]byte{0xff, 0x00}, 127, false},
			{[]byte{0x81, 0x01}, 129, false},
			{[]byte{0x7f}, -1, false},
			{[]byte{0x81, 0x7f}, -127, false},
			{[]byte{0xff, 0x7e}, -129, false},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0, true},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x4f}, 0, true},
			{[]byte{0x80, 0x80, 0x80, 0x80, 0x70}, 0, true},
		}
		for _, tt := range tests {
			got, err := NewReader(tt.in).I32()
			if tt.wantErr {
				if err == nil {
					t.Errorf("I32(%x) = %d, want error", tt.in, got)
				}
			} else if err != nil || got != tt.want {
				t.Errorf("I32(%x) = %d, err %v; want %d", tt.in, got, err, tt.want)
			}
		}
	})

	t.Run("i64 and s33", func(t *testing.T) {
		for _, tt := range []struct {
			in   []byte
			want int64
		}{
			{[]byte{0x00}, 0},
			{[]byte{0x04}, 4},
			{[]byte{0xff, 0x00}, 127},
			{[]byte{0x81, 0x01}, 129},
			{[]byte{0x7f}, -1},
			{[]byte{0x81, 0x7f}, -127},
			{[]byte{0xff, 0x7e}, -129},
			{[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x7f}, math.MinInt64},
		} {
			got, err := NewReader(tt.in).I64()
			if err != nil || got != tt.want {
				t.Errorf("I64(%x) = %d, err %v; want %d", tt.in, got, err, tt.want)
			}
		}
		for _, tt := range []struct {
			in   []byte
			want int64
		}{
			{[]byte{0x40}, -64},
			{[]byte{0x7f}, -1},
			{[]byte{0x7e}, -2},
			{[]byte{0x7d}, -3},
			{[]byte{0x7c}, -4},
		} {
			got, err := NewReader(tt.in).S33()
			if err != nil || got != tt.want {
				t.Errorf("S33(%x) = %d, err %v; want %d", tt.in, got, err, tt.want)
			}
		}
	})
}
