package wasm3

import (
	"errors"
	"testing"
)

func TestDecodeRejectsSectionOrderDuplicateAndTrailingPayload(t *testing.T) {
	t.Run("section order", func(t *testing.T) {
		_, err := DecodeModule(module(section(secFunction, 0x00), section(secType, 0x00)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrSectionOrder || de.SectionID != secType {
			t.Fatalf("expected section order detail, got %v", err)
		}
	})
	t.Run("duplicate section", func(t *testing.T) {
		_, err := DecodeModule(module(section(secType, 0x00), section(secType, 0x00)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrDuplicateSection || de.SectionID != secType {
			t.Fatalf("expected duplicate section detail, got %v", err)
		}
	})
	t.Run("trailing payload bytes", func(t *testing.T) {
		_, err := DecodeModule(module(section(secStart, append(u32(0), 0x00)...)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrSectionSizeMismatch || de.SectionID != secStart {
			t.Fatalf("expected section size mismatch detail, got %v", err)
		}
	})
}

func TestDecodeRejectsGlobalTypeWithoutMutability(t *testing.T) {
	t.Run("defined global", func(t *testing.T) {
		_, err := DecodeModule(module(section(secGlobal, 0x01, 0x7f, 0x41, 0x00, 0x0b)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrInvalidType {
			t.Fatalf("expected invalid global mutability detail, got %v", err)
		}
	})
	t.Run("imported global", func(t *testing.T) {
		imp := append(u32(3), []byte("env")...)
		imp = append(imp, u32(1)...)
		imp = append(imp, 'g', byte(ExternGlobal), byte(NumI32))
		_, err := DecodeModule(module(section(secImport, append([]byte{0x01}, imp...)...)))
		var de *DecodeError
		if !errors.As(err, &de) {
			t.Fatalf("expected imported global mutability decode error, got %v", err)
		}
	})
}

func TestDecodeLEBBoundaries(t *testing.T) {
	t.Run("u33 accepts upper bound", func(t *testing.T) {
		r := newReader([]byte{0xff, 0xff, 0xff, 0xff, 0x1f})
		got, err := r.u33()
		if err != nil || got != 0x1ffffffff {
			t.Fatalf("u33=%x err=%v", got, err)
		}
	})
	t.Run("u33 rejects terminal unused bits", func(t *testing.T) {
		r := newReader([]byte{0xff, 0xff, 0xff, 0xff, 0x3f})
		if _, err := r.u33(); err == nil {
			t.Fatal("expected u33 malformed terminal bits")
		}
	})
	t.Run("s33 accepts negative one sign extension", func(t *testing.T) {
		r := newReader([]byte{0x7f})
		got, err := r.s33()
		if err != nil || got != -1 {
			t.Fatalf("s33=%d err=%v", got, err)
		}
	})
	t.Run("fixed width floats reject oob", func(t *testing.T) {
		r := newReader([]byte{0x43, 0x00, 0x00})
		if _, err := decodeInstruction(r, 0); err == nil {
			t.Fatal("expected f32.const EOF")
		}
	})
}

func TestDecodeTypeForms(t *testing.T) {
	t.Run("rec group final subtype descriptors", func(t *testing.T) {
		m, err := DecodeModule(module(section(secType,
			0x01,
			0x4e, 0x02,
			0x4f, 0x00, 0x4d, 0x00, 0x5f, 0x00,
			0x50, 0x01, 0x00, 0x4c, 0x00, 0x5e, 0x7f, 0x00,
		)))
		if err != nil {
			t.Fatalf("DecodeModule: %v", err)
		}
		if len(m.Types) != 1 || len(m.Types[0].SubTypes) != 2 {
			t.Fatalf("rectype=%#v", m.Types)
		}
		if !m.Types[0].SubTypes[0].Final || m.Types[0].SubTypes[0].Metadata.Descriptor == nil {
			t.Fatalf("final descriptor subtype not decoded: %#v", m.Types[0].SubTypes[0])
		}
		if m.Types[0].SubTypes[1].Final || len(m.Types[0].SubTypes[1].Supers) != 1 || m.Types[0].SubTypes[1].Metadata.Describes == nil {
			t.Fatalf("open describes subtype not decoded: %#v", m.Types[0].SubTypes[1])
		}
	})
	t.Run("exact reference heap type", func(t *testing.T) {
		r := newReader([]byte{0x64, 0x62, 0x03})
		rt, err := decodeRefType(r)
		if err != nil || rt.Nullable || !rt.Exact || rt.Heap.Type.Index != 3 {
			t.Fatalf("rt=%#v err=%v", rt, err)
		}
	})
	t.Run("memory64 and shared memory encodings", func(t *testing.T) {
		r := newReader([]byte{0x07, 0x02, 0x04})
		mt, err := decodeMemType(r)
		if err != nil || !mt.Shared || !mt.Limits.Addr64 || mt.Limits.Min != 2 || mt.Limits.Max == nil || *mt.Limits.Max != 4 {
			t.Fatalf("memtype=%#v err=%v", mt, err)
		}
	})
}

func TestDecodeInstructionImmediates(t *testing.T) {
	t.Run("struct field immediates", func(t *testing.T) {
		r := newReader([]byte{0xfb, 0x02, 0x01, 0x07})
		in, err := decodeInstruction(r, 0)
		if err != nil || in.Kind != InstrStructGet || in.Index != 1 || in.Index2 != 7 {
			t.Fatalf("instr=%#v err=%v", in, err)
		}
	})
	t.Run("array.new_fixed length immediate", func(t *testing.T) {
		r := newReader([]byte{0xfb, 0x08, 0x02, 0x03})
		in, err := decodeInstruction(r, 0)
		if err != nil || in.Kind != InstrArrayNewFixed || in.Index != 2 || in.Index2 != 3 {
			t.Fatalf("instr=%#v err=%v", in, err)
		}
	})
	t.Run("descriptor br_on_cast immediate order", func(t *testing.T) {
		r := newReader([]byte{0xfb, 0x18, 0x03, 0x02, 0x6e, 0x6d})
		in, err := decodeInstruction(r, 0)
		if err != nil || in.Kind != InstrBrOnCast || in.Index != 2 || !in.Cast.SourceNullable || !in.Cast.TargetNullable || in.HeapType.Abs != HeapAny || in.HeapType2.Abs != HeapEq {
			t.Fatalf("instr=%#v err=%v", in, err)
		}
	})
	t.Run("atomic.fence rejects nonzero immediate", func(t *testing.T) {
		r := newReader([]byte{0xfe, 0x03, 0x01})
		if _, err := decodeInstruction(r, 0); err == nil {
			t.Fatal("expected invalid atomic.fence immediate")
		}
	})
	// An if body has at most one else marker. A second 0x05 is not an
	// instruction and must not be treated as a harmless separator.
	// if void; else; else; end
	if _, err := decodeInstruction(newReader([]byte{0x04, 0x40, 0x05, 0x05, 0x0b}), 0); err == nil {
		t.Fatal("expected duplicate else marker to fail decoding")
	}
	t.Run("memarg with explicit memory index", func(t *testing.T) {
		r := newReader([]byte{0x28, 0x42, 0x05, 0x09})
		in, err := decodeInstruction(r, 0)
		if err != nil || in.Kind != InstrI32Load || in.MemArg.Align != 2 || in.MemArg.Mem == nil || *in.MemArg.Mem != 5 || in.MemArg.Offset != 9 {
			t.Fatalf("instr=%#v err=%v", in, err)
		}
	})
	t.Run("v128.const and i8x16.shuffle subopcodes", func(t *testing.T) {
		vbytes := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
		vin, err := decodeInstruction(newReader(vbytes), 0)
		if err != nil || vin.Kind != InstrV128Const {
			t.Fatalf("v128.const=%#v err=%v", vin, err)
		}
		sbytes := []byte{0xfd, 0x0d}
		for i := 0; i < 16; i++ {
			sbytes = append(sbytes, byte(i))
		}
		sin, err := decodeInstruction(newReader(sbytes), 0)
		if err != nil || sin.Kind != InstrI8x16Shuffle || sin.Lanes[15] != 15 {
			t.Fatalf("shuffle=%#v err=%v", sin, err)
		}
	})
}
