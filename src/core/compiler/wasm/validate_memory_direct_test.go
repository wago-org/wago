package wasm

import (
	"errors"
	"testing"
)

func TestDirectScalarMemoryValidationParity(t *testing.T) {
	for opcode := byte(0x28); opcode <= 0x3e; opcode++ {
		effect := opEffects[memOpcodeKind[opcode]]
		for _, scenario := range []string{"memory32", "memory64", "unknown-memory", "overalign", "indexed-zero", "indexed-unknown", "wrong-address", "underflow", "truncated"} {
			body := []byte{0}
			address := byte(0x41)
			if scenario == "memory64" {
				address = 0x42
			}
			if scenario == "wrong-address" {
				address = 0x42
			}
			if scenario != "underflow" {
				body = append(body, address, 0)
			}
			if effect.cat == effStore {
				switch effect.a {
				case effectI32:
					body = append(body, 0x41, 0)
				case effectI64:
					body = append(body, 0x42, 0)
				case effectF32:
					body = append(body, 0x43, 0, 0, 0, 0)
				case effectF64:
					body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
				}
			}
			align := uint32(effect.align)
			if scenario == "overalign" {
				align++
			}
			if scenario == "indexed-zero" || scenario == "indexed-unknown" {
				align |= 64
			}
			body = append(body, opcode)
			if scenario != "truncated" {
				body = append(body, u32(align)...)
				if scenario == "indexed-zero" {
					body = append(body, 0)
				}
				if scenario == "indexed-unknown" {
					body = append(body, 1)
				}
				body = append(body, 0)
				if effect.cat == effLoad {
					body = append(body, 0x1a)
				}
				body = append(body, 0x0b)
			}
			sections := [][]byte{section(secType, 1, 0x60, 0, 0), section(secFunction, 1, 0)}
			if scenario != "unknown-memory" {
				flags := byte(0)
				if scenario == "memory64" {
					flags = 4
				}
				sections = append(sections, section(secMemory, 1, flags, 1))
			}
			code := append([]byte{1}, u32(uint32(len(body)))...)
			code = append(code, body...)
			sections = append(sections, section(secCode, code...))
			data := module(sections...)
			features := ValidationFeatures{MultiMemory: true}
			reference := decodeThenValidateWithFeatures(data, features)
			direct := byteBackedDecodeThenValidateWithFeatures(data, features)
			good := scenario == "memory32" || scenario == "memory64" || scenario == "indexed-zero"
			if (reference == nil) != good || (direct == nil) != good {
				t.Fatalf("opcode %#x %s: AST=%v direct=%v want valid=%t", opcode, scenario, reference, direct, good)
			}
			var a, b *ValidationError
			if errors.As(reference, &a) && errors.As(direct, &b) {
				if a.Code != b.Code || a.Detail != b.Detail {
					t.Fatalf("opcode %#x %s: errors differ: %v / %v", opcode, scenario, a, b)
				}
			}
		}
	}
}
