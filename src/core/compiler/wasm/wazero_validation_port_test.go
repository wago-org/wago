package wasm

import "testing"

// TestWazeroPortRejectsRedundantControlTerminators ports validation fuzz
// regressions from wazero/internal/wasm/func_validation_test.go at c0f3a4ec.
func TestWazeroPortRejectsRedundantControlTerminators(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"redundant end", []byte{0x0b, 0x0b}},
		{"else after function end", []byte{0x0b, 0x05}},
		{"else outside if", []byte{0x05, 0x0b}},
		{"else in block", []byte{0x02, 0x40, 0x05, 0x0b, 0x0b}},
		{"second else in if", []byte{0x41, 0x00, 0x04, 0x40, 0x05, 0x05, 0x0b, 0x0b}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := append([]byte{0x00}, tt.body...) // zero local declarations
			binary := module(
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
			)
			m, err := DecodeModule(binary)
			if err == nil {
				err = ValidateModule(m)
			}
			if err == nil {
				t.Fatal("malformed control sequence was accepted")
			}
		})
	}
}

func TestWazeroPortDecodesLargeMixedResultSignature(t *testing.T) {
	results := make([]byte, 138)
	for i := range results {
		results[i] = 0x7b // v128
	}
	typePayload := []byte{0x01, 0x60, 0x00}
	typePayload = append(typePayload, u32(uint32(len(results)))...)
	typePayload = append(typePayload, results...)
	if _, err := DecodeModule(module(section(secType, typePayload...))); err != nil {
		t.Fatalf("large v128 result signature: %v", err)
	}
}
