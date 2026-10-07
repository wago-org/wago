package wasm

import "testing"

func TestDescriptorInstructionsUseModuleTypeIndexes(t *testing.T) {
	for _, test := range []struct {
		name     string
		funcType []byte
		body     []byte
	}{
		{
			name:     "struct.new_default_desc",
			funcType: []byte{0x60, 0x01, 0x64, 0x62, 0x01, 0x01, 0x64, 0x00},
			body:     []byte{0x00, 0x20, 0x00, 0xfb, 0x21, 0x00, 0x0b},
		},
		{
			name:     "ref.get_desc",
			funcType: []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x62, 0x01},
			body:     []byte{0x00, 0x20, 0x00, 0xfb, 0x22, 0x00, 0x0b},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// A recursion group has a described struct at type 0 and its
			// descriptor at type 1. Metadata indexes are group relative.
			types := []byte{0x02, 0x4e, 0x02, 0x4d, 0x01, 0x5f, 0x00, 0x4c, 0x00, 0x5f, 0x00}
			types = append(types, test.funcType...)
			data := module(
				section(secType, types...),
				section(secFunction, 0x01, 0x02),
				section(secCode, append([]byte{0x01, byte(len(test.body))}, test.body...)...),
			)
			for _, validate := range []struct {
				name string
				fn   func([]byte) error
			}{
				{"AST", func(data []byte) error {
					decoded, err := decodeModuleASTForTest(data)
					if err != nil {
						return err
					}
					return ValidateModule(decoded)
				}},
				{"byte backed", ValidateByteBackedModule},
			} {
				if err := validate.fn(data); err != nil {
					t.Errorf("%s: %v", validate.name, err)
				}
			}
		})
	}
}
