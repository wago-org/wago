package wasm

import (
	"errors"
	"testing"
)

func TestDecodeMemoryOffsetWidthFollowsMemoryType(t *testing.T) {
	paths := []struct {
		name   string
		decode func([]byte) (*Module, error)
	}{
		{name: "AST", decode: decodeModuleASTForTest},
		{name: "byte-backed", decode: DecodeModule},
	}

	validU32 := []struct {
		name   string
		offset []byte
		want   uint64
	}{
		{name: "literal", offset: []byte{0x02}, want: 2},
		{name: "non-minimal-five-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x00}, want: 2},
		{name: "max-u32", offset: []byte{0xff, 0xff, 0xff, 0xff, 0x0f}, want: 1<<32 - 1},
	}
	for _, tc := range validU32 {
		t.Run("memory32/accept-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModule(false, false, tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					m, err := path.decode(data)
					if err != nil {
						t.Fatalf("decode rejected valid u32 offset: %v", err)
					}
					if path.name == "AST" {
						got := m.Code[0].Body.Instrs[1].MemArg().Offset
						if got != tc.want {
							t.Fatalf("offset=%d, want %d", got, tc.want)
						}
					}
				})
			}
		})
	}

	invalidU32 := []struct {
		name   string
		offset []byte
	}{
		{name: "six-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}},
		{name: "fifth-byte-unused-bit-4", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}},
		{name: "fifth-byte-unused-bit-6", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x40}},
	}
	for _, tc := range invalidU32 {
		t.Run("memory32/reject-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModule(false, false, tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					err := func() error { _, err := path.decode(data); return err }()
					assertMalformedMemoryOffset(t, err, true)
				})
			}
		})
	}

	validU64 := []struct {
		name   string
		offset []byte
		want   uint64
	}{
		{name: "six-byte-non-minimal", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}, want: 2},
		{name: "above-u32", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}, want: 1<<32 + 2},
		{name: "max-u64", offset: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, want: ^uint64(0)},
	}
	for _, imported := range []bool{false, true} {
		owner := "local"
		if imported {
			owner = "imported"
		}
		for _, tc := range validU64 {
			t.Run("memory64/"+owner+"/accept-"+tc.name, func(t *testing.T) {
				data := memoryOffsetModule(true, imported, tc.offset)
				for _, path := range paths {
					t.Run(path.name, func(t *testing.T) {
						m, err := path.decode(data)
						if err != nil {
							t.Fatalf("decode rejected valid u64 offset: %v", err)
						}
						if path.name == "AST" {
							got := m.Code[0].Body.Instrs[1].MemArg().Offset
							if got != tc.want {
								t.Fatalf("offset=%d, want %d", got, tc.want)
							}
						}
					})
				}
			})
		}
	}

	for _, tc := range invalidU32 {
		t.Run("no-memory/reject-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModuleWithoutMemory(tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					err := func() error { _, err := path.decode(data); return err }()
					assertMalformedMemoryOffset(t, err, true)
				})
			}
		})
	}
}

func TestValidateByteBackedMemoryOffsetWidth(t *testing.T) {
	valid := memoryOffsetModule(false, false, []byte{0x00})
	m, err := DecodeModule(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		offset []byte
	}{
		{name: "six-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}},
		{name: "fifth-byte-unused-bits", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.Code[0].BodyBytes = memoryOffsetBody(false, tc.offset)[1:]
			err := ValidateModule(m)
			assertMalformedMemoryOffset(t, err, false)
		})
	}
}

func assertMalformedMemoryOffset(t *testing.T, err error, wantSection bool) {
	t.Helper()
	var de *DecodeError
	if !errors.As(err, &de) || de.Code != ErrMalformedLEB {
		t.Fatalf("error=%#v / %v, want ErrMalformedLEB", de, err)
	}
	if wantSection && (de.SectionID != secCode || de.SectionStart <= 0 || de.SectionEnd <= de.SectionStart) {
		t.Fatalf("decode section diagnostics=%#v, want code-section span", de)
	}
}

func memoryOffsetModule(addr64, imported bool, offset []byte) []byte {
	sections := [][]byte{
		section(secType, 0x01, 0x60, 0x00, 0x00),
	}
	memType := []byte{0x00, 0x01}
	if addr64 {
		memType = []byte{0x04, 0x01}
	}
	if imported {
		entry := []byte{0x01, 'm', 0x01, 'n', byte(ExternMem)}
		entry = append(entry, memType...)
		sections = append(sections, section(secImport, append([]byte{0x01}, entry...)...))
	}
	sections = append(sections, section(secFunction, 0x01, 0x00))
	if !imported {
		sections = append(sections, section(secMemory, append([]byte{0x01}, memType...)...))
	}
	body := memoryOffsetBody(addr64, offset)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	sections = append(sections, section(secCode, code...))
	return module(sections...)
}

func memoryOffsetModuleWithoutMemory(offset []byte) []byte {
	body := memoryOffsetBody(false, offset)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	return module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, code...),
	)
}

func memoryOffsetBody(addr64 bool, offset []byte) []byte {
	constOp := byte(0x41)
	if addr64 {
		constOp = 0x42
	}
	body := []byte{0x00, constOp, 0x00, 0x28, 0x02}
	body = append(body, offset...)
	return append(body, 0x1a, 0x0b)
}
