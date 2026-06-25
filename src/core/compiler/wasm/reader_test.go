package wasm

import (
	"errors"
	"math"
	"testing"
)

// canonical LEB128 encoders used as the reference oracle for decode round-trips.
func encodeULEB(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func encodeSLEB(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7 // arithmetic shift
		signSet := b&0x40 != 0
		if (v == 0 && !signSet) || (v == -1 && signSet) {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

func TestLEB128UnsignedRoundTrip(t *testing.T) {
	u32vals := []uint32{0, 1, 2, 63, 64, 127, 128, 255, 256, 16383, 16384,
		1 << 20, math.MaxInt32, math.MaxUint32 - 1, math.MaxUint32}
	for _, v := range u32vals {
		r := NewReader(encodeULEB(uint64(v)))
		got, err := r.U32()
		if err != nil {
			t.Fatalf("U32(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("U32 round-trip: got %d want %d", got, v)
		}
		if r.HasNext() {
			t.Fatalf("U32(%d): %d bytes left after decode", v, r.BytesLeft())
		}
	}
	u64vals := []uint64{0, 1, 1 << 31, 1 << 40, math.MaxUint32 + 1,
		math.MaxInt64, math.MaxUint64 - 1, math.MaxUint64}
	for _, v := range u64vals {
		r := NewReader(encodeULEB(v))
		got, err := r.U64()
		if err != nil {
			t.Fatalf("U64(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("U64 round-trip: got %d want %d", got, v)
		}
	}
}

func TestLEB128SignedRoundTrip(t *testing.T) {
	i32vals := []int32{0, 1, -1, 63, 64, -64, -65, 127, -128,
		math.MaxInt32, math.MinInt32, math.MaxInt32 - 1, math.MinInt32 + 1}
	for _, v := range i32vals {
		r := NewReader(encodeSLEB(int64(v)))
		got, err := r.I32()
		if err != nil {
			t.Fatalf("I32(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("I32 round-trip: got %d want %d", got, v)
		}
	}
	i64vals := []int64{0, 1, -1, 1 << 33, -(1 << 33),
		math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1}
	for _, v := range i64vals {
		r := NewReader(encodeSLEB(v))
		got, err := r.I64()
		if err != nil {
			t.Fatalf("I64(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("I64 round-trip: got %d want %d", got, v)
		}
	}
}

func TestLEB128Malformed(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		read  func(*Reader) error
		want  ErrCode
	}{
		{"truncated continuation", []byte{0x80}, func(r *Reader) error { _, e := r.U32(); return e }, ErrBytecodeOutOfRange},
		{"u32 overlong (6 bytes)", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00}, func(r *Reader) error { _, e := r.U32(); return e }, ErrMalformedLEBOutOfBounds},
		{"u32 bad zero padding", []byte{0x80, 0x80, 0x80, 0x80, 0x70}, func(r *Reader) error { _, e := r.U32(); return e }, ErrMalformedLEBUnsignedPadding},
		{"i32 bad sign padding", []byte{0x80, 0x80, 0x80, 0x80, 0x08}, func(r *Reader) error { _, e := r.I32(); return e }, ErrMalformedLEBSignedPadding},
		{"u64 overlong (11 bytes)", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00}, func(r *Reader) error { _, e := r.U64(); return e }, ErrMalformedLEBOutOfBounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.read(NewReader(c.bytes))
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Fatalf("expected *DecodeError, got %v", err)
			}
			if de.Code != c.want {
				t.Fatalf("got code %v, want %v", de.Code, c.want)
			}
		})
	}
}

// Valid maximal-length encodings must decode (boundary of the padding check).
func TestLEB128MaxLengthValid(t *testing.T) {
	r := NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}) // 5-byte u32 = max
	v, err := r.U32()
	if err != nil || v != math.MaxUint32 {
		t.Fatalf("max u32: got %d err %v", v, err)
	}
	r = NewReader([]byte{0x7F}) // i32 = -1
	iv, err := r.I32()
	if err != nil || iv != -1 {
		t.Fatalf("i32 -1: got %d err %v", iv, err)
	}
}

func TestFixedReads(t *testing.T) {
	r := NewReader([]byte{0x78, 0x56, 0x34, 0x12, 0xEF, 0xBE, 0xAD, 0xDE})
	u32, err := r.LEU32()
	if err != nil || u32 != 0x12345678 {
		t.Fatalf("LEU32: got %#x err %v", u32, err)
	}
	r = NewReader([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80})
	u64, err := r.LEU64()
	if err != nil || u64 != 0x8000000000000001 {
		t.Fatalf("LEU64: got %#x err %v", u64, err)
	}
	// truncated fixed read
	if _, err := NewReader([]byte{0x01, 0x02}).LEU32(); err == nil {
		t.Fatal("expected error on truncated LEU32")
	}
}

func TestBoundsAndSequence(t *testing.T) {
	// Read past end.
	r := NewReader([]byte{0x2A})
	if b, err := r.Byte(); err != nil || b != 0x2A {
		t.Fatalf("Byte: %v %#x", err, b)
	}
	if _, err := r.Byte(); err == nil {
		t.Fatal("expected out-of-range reading past end")
	}
	// JumpTo bounds: len is valid (one-past-end), len+1 is not.
	r = NewReader([]byte{0, 0, 0})
	if err := r.JumpTo(3); err != nil {
		t.Fatalf("JumpTo(len) should be valid: %v", err)
	}
	if err := r.JumpTo(4); err == nil {
		t.Fatal("JumpTo(len+1) should fail")
	}
	// Mixed sequence advances correctly.
	var buf []byte
	buf = append(buf, encodeULEB(300)...)   // U32
	buf = append(buf, encodeSLEB(-5)...)    // I32
	buf = append(buf, 0xAA)                 // Byte
	buf = append(buf, encodeULEB(1<<40)...) // U64
	r = NewReader(buf)
	if v, _ := r.U32(); v != 300 {
		t.Fatalf("seq U32 = %d", v)
	}
	if v, _ := r.I32(); v != -5 {
		t.Fatalf("seq I32 = %d", v)
	}
	if v, _ := r.Byte(); v != 0xAA {
		t.Fatalf("seq Byte = %#x", v)
	}
	if v, _ := r.U64(); v != 1<<40 {
		t.Fatalf("seq U64 = %d", v)
	}
	if r.HasNext() {
		t.Fatalf("expected fully consumed, %d left", r.BytesLeft())
	}
}
