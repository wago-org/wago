package wasm

import (
	"bytes"
	"errors"
	"testing"
)

func module(sections ...[]byte) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}
func section(id byte, payload ...byte) []byte {
	out := []byte{id}
	out = append(out, u32(uint32(len(payload)))...)
	out = append(out, payload...)
	return out
}
func custom(name string, payload ...byte) []byte {
	p := append(u32(uint32(len(name))), []byte(name)...)
	p = append(p, payload...)
	return section(0, p...)
}
func u32(v uint32) []byte {
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

func TestDecodeValidateSimpleFunction(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x41, 0x07, 0x0b),
	)
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if len(m.Code) != 1 || len(m.Code[0].Body.Instrs) != 1 || m.Code[0].Body.Instrs[0].Kind != InstrI32Const {
		t.Fatalf("unexpected code: %#v", m.Code)
	}
	if got, want := m.Code[0].BodyBytes, []byte{0x41, 0x07, 0x0b}; !bytes.Equal(got, want) {
		t.Fatalf("body bytes=%x want %x", got, want)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
}

func TestDecodewasmTypeSection(t *testing.T) {
	b := module(section(secType,
		0x02,
		0x5f, 0x01, 0x7f, 0x00, // struct { i32 const }
		0x5e, 0x64, 0x6b, 0x01, // array (ref struct) var
	))
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if len(m.Types) != 2 {
		t.Fatalf("types=%d", len(m.Types))
	}
	if got := m.Types[0].SubTypes[0].Comp.Kind; got != CompStruct {
		t.Fatalf("type0 kind=%v", got)
	}
	if got := m.Types[1].SubTypes[0].Comp.Kind; got != CompArray {
		t.Fatalf("type1 kind=%v", got)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
}

func TestDecodeStringRefsAndStringConst(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x64), // bare stringref result
		section(secFunction, 0x01, 0x00),
		section(secStringRefs, 0x01, 0x05, 'h', 'e', 'l', 'l', 'o'),
		section(secCode, 0x01, 0x06, 0x00, 0xfb, 0x82, 0x01, 0x00, 0x0b), // string.const 0
	)
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if string(m.StringRefs[0]) != "hello" {
		t.Fatalf("stringref=%q", m.StringRefs[0])
	}
	if got := m.Code[0].Body.Instrs[0]; got.Kind != InstrStringConst || got.Index != 0 {
		t.Fatalf("instr=%#v", got)
	}
}

func TestDecodeElementHeader6ExplicitRefType(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secTable, 0x01, 0x63, 0x70, 0x00, 0x01), // funcref limits min=1
		section(secElement, 0x01,
			0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x70, 0x01, 0xd2, 0x00, 0x0b,
		),
		section(secCode, 0x01, 0x02, 0x00, 0x0b),
	)
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if len(m.Elements) != 1 || m.Elements[0].Kind.Kind != ElemTypedExprs {
		t.Fatalf("elem=%#v", m.Elements)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
}

func TestSharedMemoryWithoutMaxDecodesButValidationRejects(t *testing.T) {
	b := module(section(secMemory, 0x01, 0x02, 0x01))
	m, err := DecodeModule(b)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if !m.Memories[0].Shared || m.Memories[0].Limits.Max != nil {
		t.Fatalf("memory=%#v", m.Memories[0])
	}
	err = ValidateModule(m)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != ErrInvalidSharedMemory {
		t.Fatalf("expected ErrInvalidSharedMemory, got %v", err)
	}
}

func TestDecodeStructuredDeepNestingLimit(t *testing.T) {
	payload := []byte{0x01, 0x60, 0x00, 0x00}
	sections := [][]byte{section(secType, payload...), section(secFunction, 0x01, 0x00)}
	body := []byte{0x00}
	for i := 0; i < 8; i++ {
		body = append(body, 0x02, 0x40)
	}
	for i := 0; i < 9; i++ {
		body = append(body, 0x0b)
	}
	codePayload := append([]byte{0x01}, u32(uint32(len(body)))...)
	codePayload = append(codePayload, body...)
	sections = append(sections, section(secCode, codePayload...))
	m, err := DecodeModule(module(sections...))
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if len(m.Code[0].Body.Instrs) != 1 || m.Code[0].Body.Instrs[0].Kind != InstrBlock {
		t.Fatalf("nested decode failed: %#v", m.Code[0].Body)
	}
}

func TestCustomNameSectionPreserved(t *testing.T) {
	namePayload := []byte{0x00, 0x04, 0x03, 'm', 'o', 'd'}
	m, err := DecodeModule(module(custom("name", namePayload...)))
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if len(m.Customs) != 1 || m.NameSec == nil || m.NameSec.ModuleName == nil || *m.NameSec.ModuleName != "mod" {
		t.Fatalf("name section not decoded: %#v", m)
	}
	if string(m.RawNameSecPayload) != string(namePayload) {
		t.Fatalf("raw name payload mismatch")
	}
}
