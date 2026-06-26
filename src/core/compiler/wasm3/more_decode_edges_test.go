package wasm3

import (
	"errors"
	"testing"
)

func TestMoreLEBAndPrimitiveDecodeEdges(t *testing.T) {
	t.Run("uleb overwide well formed forms", func(t *testing.T) {
		cases := []struct {
			bytes []byte
			want  uint32
		}{{[]byte{0x80, 0x00}, 0}, {[]byte{0x81, 0x00}, 1}, {[]byte{0xff, 0x00}, 127}, {[]byte{0x80, 0x81, 0x00}, 128}}
		for _, tc := range cases {
			r := newReader(tc.bytes)
			got, err := r.u32()
			if err != nil || got != tc.want || r.off() != len(tc.bytes) {
				t.Fatalf("%x -> %d/%d err=%v", tc.bytes, got, r.off(), err)
			}
		}
	})
	t.Run("sleb overwide sign extension forms", func(t *testing.T) {
		cases := []struct {
			bytes []byte
			want  int32
		}{{[]byte{0x80, 0x00}, 0}, {[]byte{0xff, 0x7f}, -1}, {[]byte{0xbf, 0x00}, 63}, {[]byte{0xc0, 0x7f}, -64}}
		for _, tc := range cases {
			r := newReader(tc.bytes)
			got, err := r.i32()
			if err != nil || got != tc.want || r.off() != len(tc.bytes) {
				t.Fatalf("%x -> %d/%d err=%v", tc.bytes, got, r.off(), err)
			}
		}
	})
	t.Run("s33 boundaries", func(t *testing.T) {
		cases := [][]byte{{0x3f}, {0xff, 0xff, 0xff, 0xff, 0x0f}}
		for _, b := range cases {
			if _, err := newReader(b).s33(); err != nil {
				t.Fatalf("s33 %x: %v", b, err)
			}
		}
		if _, err := newReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00}).s33(); err == nil {
			t.Fatal("expected too many-byte s33 failure")
		}
		if _, err := newReader([]byte{0xff, 0xff, 0xff, 0xff, 0x5f}).s33(); err == nil {
			t.Fatal("expected malformed s33 sign-extension failure")
		}
	})
	t.Run("invalid instruction and lane", func(t *testing.T) {
		if _, err := decodeInstruction(newReader([]byte{0xff}), 0); err == nil {
			t.Fatal("expected invalid instruction")
		}
		if _, err := decodeInstruction(newReader(append([]byte{0xfd, 0x0d}, append(make([]byte, 15), 32)...)), 0); err == nil {
			t.Fatal("expected invalid shuffle lane")
		}
	})
}

func TestMoreReferenceDecodeEdges(t *testing.T) {
	t.Run("bare and explicit stringrefs", func(t *testing.T) {
		cases := []struct {
			bytes    []byte
			nullable bool
		}{{[]byte{0x64}, true}, {[]byte{0x63, 0x64}, true}, {[]byte{0x64, 0x64}, false}}
		for _, tc := range cases {
			vt, err := decodeValType(newReader(tc.bytes))
			if err != nil || vt.Kind != ValRef || vt.Ref.Heap.Abs != HeapString || vt.Ref.Nullable != tc.nullable {
				t.Fatalf("%x -> %#v err=%v", tc.bytes, vt, err)
			}
		}
	})
	t.Run("ref.null abstract and exact indexed", func(t *testing.T) {
		in, err := decodeInstruction(newReader([]byte{0xd0, 0x6f}), 0)
		if err != nil || in.Kind != InstrRefNull || in.RefType.Heap.Abs != HeapExtern {
			t.Fatalf("ref.null extern=%#v err=%v", in, err)
		}
		in, err = decodeInstruction(newReader([]byte{0xd0, 0x62, 0x00}), 0)
		if err != nil || in.Kind != InstrRefNull || !in.RefType.Exact || in.RefType.Heap.Type.Index != 0 {
			t.Fatalf("ref.null exact=%#v err=%v", in, err)
		}
	})
	t.Run("exact ref.cast and ref.cast_desc_eq", func(t *testing.T) {
		in, err := decodeInstruction(newReader([]byte{0xfb, 0x16, 0x62, 0x01}), 0)
		if err != nil || in.Kind != InstrRefCast || !in.Cast.SourceNullable || in.HeapType.Type.Index != 1 {
			t.Fatalf("ref.cast=%#v err=%v", in, err)
		}
		in, err = decodeInstruction(newReader([]byte{0xfb, 0x24, 0x62, 0x01}), 0)
		if err != nil || in.Kind != InstrRefCastDescEq || !in.Cast.SourceNullable || !in.Cast.TargetNullable || in.HeapType.Type.Index != 1 {
			t.Fatalf("ref.cast_desc_eq=%#v err=%v", in, err)
		}
	})
}

func TestMoreNameSectionEdges(t *testing.T) {
	t.Run("malformed function name map ordering rejects module", func(t *testing.T) {
		// custom name section: subsection 1 (function names), payload vector
		// [(2,"b"),(1,"a")], which violates the strictly-increasing map order.
		namePayload := []byte{0x01, 0x07, 0x02, 0x02, 0x01, 'b', 0x01, 0x01, 'a'}
		_, err := DecodeModule(module(custom("name", namePayload...)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrInvalidSection {
			t.Fatalf("expected malformed name-section error, got %v", err)
		}
	})
}

func TestMoreModuleDecodeEdges(t *testing.T) {
	t.Run("type section bubbles nested comp type error", func(t *testing.T) {
		_, err := DecodeModule(module(section(secType, 0x01, 0xff)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrInvalidType || de.SectionID != secType {
			t.Fatalf("expected invalid type detail, got %v", err)
		}
	})
	t.Run("custom section payload length out of range", func(t *testing.T) {
		_, err := DecodeModule([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00})
		if err == nil {
			t.Fatal("expected custom decode failure")
		}
	})
	t.Run("deep nesting over limit", func(t *testing.T) {
		body := []byte{0x00}
		for i := 0; i < maxInstructionNestingDepth+1; i++ {
			body = append(body, 0x02, 0x40)
		}
		for i := 0; i < maxInstructionNestingDepth+2; i++ {
			body = append(body, 0x0b)
		}
		code := append([]byte{0x01}, u32(uint32(len(body)))...)
		code = append(code, body...)
		_, err := DecodeModule(module(section(secType, 0x01, 0x60, 0x00, 0x00), section(secFunction, 0x01, 0x00), section(secCode, code...)))
		var de *DecodeError
		if !errors.As(err, &de) || de.Code != ErrInstructionNestingLimitExceeded {
			t.Fatalf("expected nesting limit, got %v", err)
		}
	})
}
