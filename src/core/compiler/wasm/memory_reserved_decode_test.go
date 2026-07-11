package wasm

import (
	"errors"
	"fmt"
	"testing"
)

func TestDecodeMemoryReservedZeroImmediate(t *testing.T) {
	paths := []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "AST", decode: func(b []byte) error { _, err := decodeModuleASTForTest(b); return err }},
		{name: "byte-backed", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
	}
	for _, op := range []struct {
		name string
		code byte
	}{
		{name: "memory.size", code: 0x3f},
		{name: "memory.grow", code: 0x40},
	} {
		t.Run(op.name+"/literal-zero", func(t *testing.T) {
			b := memoryReservedImmediateModule(op.code, []byte{0x00})
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					if err := path.decode(b); err != nil {
						t.Fatalf("decode rejected literal zero: %v", err)
					}
				})
			}
		})
		for _, immediate := range [][]byte{
			{0x01},
			{0x80, 0x00},
			{0x80, 0x80, 0x00},
			{0x80, 0x80, 0x80, 0x00},
			{0x80, 0x80, 0x80, 0x80, 0x00},
		} {
			name := fmt.Sprintf("reject-%x", immediate)
			t.Run(op.name+"/"+name, func(t *testing.T) {
				b := memoryReservedImmediateModule(op.code, immediate)
				for _, path := range paths {
					t.Run(path.name, func(t *testing.T) {
						err := path.decode(b)
						var de *DecodeError
						if !errors.As(err, &de) || de.Code != ErrInvalidInstruction {
							t.Fatalf("decode error=%#v / %v, want ErrInvalidInstruction", de, err)
						}
						if de.SectionID != secCode || de.SectionStart <= 0 || de.SectionEnd <= de.SectionStart {
							t.Fatalf("decode section diagnostics=%#v, want code-section span", de)
						}
					})
				}
			})
		}
	}
}

func memoryReservedImmediateModule(op byte, immediate []byte) []byte {
	body := []byte{0x00}
	if op == 0x40 {
		body = append(body, 0x41, 0x00)
	}
	body = append(body, op)
	body = append(body, immediate...)
	body = append(body, 0x1a, 0x0b)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	return module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secMemory, 0x01, 0x00, 0x01),
		section(secCode, code...),
	)
}
