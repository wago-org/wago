package wasm

import (
	"errors"
	"testing"
)

func TestPrimitiveBinaryEdgeMatrix(t *testing.T) {
	t.Run("u33 and s33 expanded boundary tables", func(t *testing.T) {
		u33Cases := []struct {
			bytes []byte
			want  uint64
		}{
			{[]byte{0x00}, 0},
			{[]byte{0x01}, 1},
			{[]byte{0x7f}, 127},
			{[]byte{0x80, 0x01}, 128},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xffffffff},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x1f}, 0x1ffffffff},
		}
		for _, tc := range u33Cases {
			got, err := newReader(tc.bytes).u33()
			if err != nil || got != tc.want {
				t.Fatalf("u33 %x = %#x err=%v", tc.bytes, got, err)
			}
		}
		s33Cases := []struct {
			bytes []byte
			want  int64
		}{
			{[]byte{0x00}, 0},
			{[]byte{0x01}, 1},
			{[]byte{0x7f}, -1},
			{[]byte{0x3f}, 63},
			{[]byte{0x40}, -64},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xffffffff},
			{[]byte{0x80, 0x80, 0x80, 0x80, 0x70}, -0x100000000},
		}
		for _, tc := range s33Cases {
			got, err := newReader(tc.bytes).s33()
			if err != nil || got != tc.want {
				t.Fatalf("s33 %x = %#x err=%v", tc.bytes, got, err)
			}
		}
	})
	t.Run("invalid decode vectors", func(t *testing.T) {
		vectors := []struct {
			name string
			data []byte
			code DecodeErrorCode
		}{
			{"bad magic", []byte{0x01, 0x61, 0x73, 0x6d, 0x01, 0, 0, 0}, ErrBadMagic},
			{"bad version", []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0, 0, 0}, ErrBadVersion},
			{"payload overflow", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0, 0, 0, secCustom, 0x02, 0x00}, ErrIndexOutOfBounds},
			{"invalid type payload", module(section(secType, 0x01, 0xff)), ErrInvalidType},
			{"invalid opcode", module(section(secType, 0x01, 0x60, 0, 0), section(secFunction, 1, 0), section(secCode, 1, 3, 0, 0xff, 0x0b)), ErrInvalidInstruction},
		}
		for _, tc := range vectors {
			_, err := DecodeModule(tc.data)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != tc.code {
				t.Fatalf("%s: expected %v, got %v", tc.name, tc.code, err)
			}
		}
	})
	t.Run("generic list decode propagates nested failures", func(t *testing.T) {
		decodeBool := func(r *reader) (bool, error) {
			b, err := r.byte()
			if err != nil {
				return false, err
			}
			if b > 1 {
				return false, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
			}
			return b == 1, nil
		}
		if _, err := readVec(newReader(nil), decodeBool); err == nil {
			t.Fatal("expected vector length EOF")
		}
		if _, err := readVec(newReader([]byte{1, 2}), decodeBool); err == nil {
			t.Fatal("expected nested bool failure")
		}
		got, err := readVec(newReader([]byte{2, 0, 1}), decodeBool)
		if err != nil || len(got) != 2 || got[0] || !got[1] {
			t.Fatalf("bool vec=%v err=%v", got, err)
		}
	})
	t.Run("fixed width float payload preservation", func(t *testing.T) {
		f32Payload := []byte{0x43, 0x01, 0x00, 0xc0, 0x7f}
		in, err := decodeInstruction(newReader(f32Payload), 0)
		if err != nil || in.F32Bits != 0x7fc00001 {
			t.Fatalf("f32 const bits=%#x err=%v", in.F32Bits, err)
		}
		f64Payload := []byte{0x44, 0x01, 0, 0, 0, 0, 0, 0xf8, 0x7f}
		in, err = decodeInstruction(newReader(f64Payload), 0)
		if err != nil || in.F64Bits != 0x7ff8000000000001 {
			t.Fatalf("f64 const bits=%#x err=%v", in.F64Bits, err)
		}
	})
	t.Run("start code data-count wrapper edge cases", func(t *testing.T) {
		if m, err := DecodeModule(module(section(secType, 0), section(secStart, 0))); err != nil || m.Start == nil || *m.Start != 0 {
			t.Fatalf("start wrapper failed m=%#v err=%v", m, err)
		}
		if m, err := DecodeModule(module(section(secDataCount, 0))); err != nil || m.DataCount == nil || *m.DataCount != 0 {
			t.Fatalf("data count wrapper failed m=%#v err=%v", m, err)
		}
		_, err := DecodeModule(module([]byte{secStart, 0x00}))
		if err == nil {
			t.Fatal("expected legacy non-length start section failure")
		}
		_, err = DecodeModule(module([]byte{secDataCount, 0x00}))
		if err == nil {
			t.Fatal("expected legacy non-length data count section failure")
		}
	})
}

func TestScalarSignExtensionInstructionDecodeMatrix(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		kind  InstrKind
	}{
		{"i32.extend8_s", []byte{0xc0}, InstrI32Extend8S},
		{"i32.extend16_s", []byte{0xc1}, InstrI32Extend16S},
		{"i64.extend8_s", []byte{0xc2}, InstrI64Extend8S},
		{"i64.extend16_s", []byte{0xc3}, InstrI64Extend16S},
		{"i64.extend32_s", []byte{0xc4}, InstrI64Extend32S},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := decodeInstruction(newReader(tc.bytes), 0)
			if err != nil || in.Kind != tc.kind {
				t.Fatalf("decode %x = %#v err=%v, want %v", tc.bytes, in.Kind, err, tc.kind)
			}
		})
	}
}

func TestSIMDAndRelaxedSIMDDecodeMatrix(t *testing.T) {
	cases := []struct {
		sub  uint32
		kind InstrKind
	}{
		{55, InstrI32x4Eq}, {64, InstrI32x4GeU}, {65, InstrF32x4Eq}, {76, InstrF64x2Ge},
		{77, InstrV128Not}, {82, InstrV128Bitselect}, {94, InstrF32x4DemoteF64x2Zero},
		{96, InstrI8x16Abs}, {127, InstrI32x4ExtaddPairwiseI16x8U}, {128, InstrI16x8Abs},
		{160, InstrI32x4Abs}, {192, InstrI64x2Abs}, {224, InstrF32x4Abs}, {255, InstrF64x2ConvertLowI32x4U},
		{256, InstrI8x16RelaxedSwizzle}, {257, InstrI32x4RelaxedTruncF32x4S}, {258, InstrI32x4RelaxedTruncF32x4U},
		{259, InstrI32x4RelaxedTruncZeroF64x2S}, {260, InstrI32x4RelaxedTruncZeroF64x2U},
		{261, InstrF32x4RelaxedMadd}, {262, InstrF32x4RelaxedNmadd}, {263, InstrF64x2RelaxedMadd}, {264, InstrF64x2RelaxedNmadd},
		{265, InstrI8x16RelaxedLaneselect}, {266, InstrI16x8RelaxedLaneselect}, {267, InstrI32x4RelaxedLaneselect}, {268, InstrI64x2RelaxedLaneselect},
		{269, InstrF32x4RelaxedMin}, {270, InstrF32x4RelaxedMax}, {271, InstrF64x2RelaxedMin}, {272, InstrF64x2RelaxedMax},
		{273, InstrI16x8RelaxedQ15mulrS}, {274, InstrI16x8RelaxedDotI8x16I7x16S}, {275, InstrI32x4RelaxedDotI8x16I7x16AddS},
	}
	for _, tc := range cases {
		b := append([]byte{0xfd}, u32(tc.sub)...)
		in, err := decodeInstruction(newReader(b), 0)
		if err != nil || in.Kind != tc.kind {
			t.Fatalf("sub %d decoded %#v err=%v", tc.sub, in.Kind, err)
		}
	}
}

func TestAtomicAndGCInstructionDecodeMatrix(t *testing.T) {
	atomicCases := []struct {
		bytes []byte
		kind  InstrKind
		op    uint32
	}{
		{[]byte{0xfe, 0x1e, 0, 0}, InstrAtomicRmw, 30},
		{[]byte{0xfe, 0x47, 0, 0}, InstrAtomicRmw, 71},
		{[]byte{0xfe, 0x48, 0, 0}, InstrAtomicCmpxchg, 72},
		{[]byte{0xfe, 0x4e, 0, 0}, InstrAtomicCmpxchg, 78},
	}
	for _, tc := range atomicCases {
		in, err := decodeInstruction(newReader(tc.bytes), 0)
		if err != nil || in.Kind != tc.kind || in.AtomicOp != tc.op {
			t.Fatalf("%x -> %#v err=%v", tc.bytes, in, err)
		}
	}
	structAtomic := []struct {
		bytes []byte
		kind  InstrKind
		ord   AtomicOrder
		typeI uint32
		fld   uint32
	}{
		{[]byte{0xfe, 0x5c, 0x01, 0x00, 0x00}, InstrStructAtomicGet, AcqRel, 0, 0},
		{[]byte{0xfe, 0x5d, 0x00, 0x01, 0x02}, InstrStructAtomicGetS, SeqCst, 1, 2},
		{[]byte{0xfe, 0x5e, 0x01, 0x02, 0x03}, InstrStructAtomicGetU, AcqRel, 2, 3},
	}
	for _, tc := range structAtomic {
		in, err := decodeInstruction(newReader(tc.bytes), 0)
		if err != nil || in.Kind != tc.kind || in.AtomicOrder != tc.ord || in.Index != tc.typeI || in.Index2 != tc.fld {
			t.Fatalf("%x -> %#v err=%v", tc.bytes, in, err)
		}
	}
	if _, err := decodeInstruction(newReader([]byte{0xfe, 0x5c, 0x02, 0x00, 0x00}), 0); err == nil {
		t.Fatal("expected invalid struct atomic order")
	}
}

func TestNameSectionDetailSpan(t *testing.T) {
	bad := module(custom("name", 0x01, 0x07, 0x02, 0x01, 0x01, 'b', 0x00, 0x01, 'a'))
	_, err := DecodeModule(bad)
	var de *DecodeError
	if !errors.As(err, &de) || de.Code != ErrInvalidSection || de.SectionID != secCustom || de.SectionStart != 10 || de.SectionEnd != len(bad) {
		t.Fatalf("expected malformed name detail with custom span, got %#v / %v", de, err)
	}
}
