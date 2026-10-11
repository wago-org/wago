package wasm

import (
	"bytes"
	"testing"
)

func TestWideArithmeticDecodeValidateParity(t *testing.T) {
	for _, sub := range []byte{19, 20, 21, 22} {
		n := 2
		if sub < 21 {
			n = 4
		}
		for _, mode := range []string{"valid", "underflow", "wrong-type", "wrong-results", "unreachable"} {
			params := make([]ValType, n)
			for i := range params {
				params[i] = I64
			}
			body := []byte{}
			count := n
			if mode == "underflow" {
				count--
			}
			if mode == "wrong-type" {
				params[0] = I32
			}
			for i := 0; i < count; i++ {
				body = append(body, 0x20, byte(i))
			}
			if mode == "unreachable" {
				body = []byte{0x00}
			}
			body = append(body, 0xfc, sub, 0x0b)
			results := []ValType{I64, I64}
			if mode == "wrong-results" {
				results = results[:1]
			}
			ty := []byte{1, 0x60, byte(n)}
			for _, p := range params {
				if p == I32 {
					ty = append(ty, 0x7f)
				} else {
					ty = append(ty, 0x7e)
				}
			}
			ty = append(ty, byte(len(results)))
			for range results {
				ty = append(ty, 0x7e)
			}
			code := append([]byte{1, byte(len(body) + 1), 0}, body...)
			raw := module(section(secType, ty...), section(secFunction, 1, 0), section(secCode, code...))
			m, err := decodeModuleASTForTest(raw)
			if err != nil {
				t.Fatalf("op %d %s decode: %v", sub, mode, err)
			}
			aerr := ValidateModule(m)
			berr := ValidateByteBackedModule(raw)
			valid := mode == "valid" || mode == "unreachable"
			if (aerr == nil) != valid || (berr == nil) != valid {
				t.Fatalf("op %d %s: AST %v; bytes %v", sub, mode, aerr, berr)
			}
			if mode == "valid" {
				encoded, err := EncodeExpr(m.Code[0].Body)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(encoded, body) {
					t.Fatalf("roundtrip: %x != %x", encoded, body)
				}
			}
		}
	}
}
