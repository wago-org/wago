package wasm

import (
	"strings"
	"testing"
)

func TestValidateRefFuncRequiresDeclaration(t *testing.T) {
	cases := []struct {
		name     string
		sections [][]byte
	}{
		{
			name: "function alone does not declare itself",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
		{
			name: "start does not declare function",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secStart, 0x00),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRefFuncValidationBothPaths(t, module(tc.sections...), false)
		})
	}
}

func TestValidateRefFuncDeclarationsOutsideFunctions(t *testing.T) {
	cases := []struct {
		name     string
		sections [][]byte
	}{
		{
			name: "export",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secExport, 0x01, 0x01, 'f', 0x00, 0x00),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
		{
			name: "global initializer",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secGlobal, 0x01, 0x70, 0x00, 0xd2, 0x00, 0x0b),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
		{
			name: "element function index",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secElement, 0x01, 0x03, 0x00, 0x01, 0x00),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
		{
			name: "element expression",
			sections: [][]byte{
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secElement, 0x01, 0x07, 0x70, 0x01, 0xd2, 0x00, 0x0b),
				section(secCode, 0x01, 0x05, 0x00, 0xd2, 0x00, 0x1a, 0x0b),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRefFuncValidationBothPaths(t, module(tc.sections...), true)
		})
	}
}

func assertRefFuncValidationBothPaths(t *testing.T, binary []byte, valid bool) {
	t.Helper()
	paths := []struct {
		name     string
		validate func([]byte) error
	}{
		{name: "AST", validate: decodeThenValidate},
		{name: "byte-backed", validate: byteBackedDecodeThenValidate},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			err := path.validate(binary)
			if valid {
				if err != nil {
					t.Fatalf("validation failed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validation succeeded, want undeclared function reference error")
			}
			if !isValidationCode(err, ErrUnknownFunc) || !strings.Contains(err.Error(), "undeclared function reference") {
				t.Fatalf("validation error = %v, want undeclared function reference", err)
			}
		})
	}
}
