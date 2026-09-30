package wasm

import "testing"

func TestReferenceConversionNullability(t *testing.T) {
	for _, direction := range []struct {
		name             string
		from, to, opcode byte
	}{
		{"to_any", 0x6f, 0x6e, 0x1a}, {"to_extern", 0x6e, 0x6f, 0x1b},
	} {
		for _, tc := range []struct {
			name                                                     string
			nullable, resultNullable, unreachable, scalar, wrongHeap bool
			valid                                                    bool
		}{
			{name: "non_null", valid: true},
			{name: "nullable", nullable: true, resultNullable: true, valid: true},
			{name: "widen", resultNullable: true, valid: true},
			{name: "reject_narrow", nullable: true},
			{name: "unreachable", unreachable: true, valid: true},
			{name: "scalar", scalar: true},
			{name: "wrong_heap", wrongHeap: true},
		} {
			t.Run(direction.name+"/"+tc.name, func(t *testing.T) {
				input := []byte{0x64, direction.from}
				if tc.nullable {
					input[0] = 0x63
				}
				if tc.scalar {
					input = []byte{0x7f}
				}
				if tc.wrongHeap {
					input = []byte{0x70}
				}
				output := byte(0x64)
				if tc.resultNullable {
					output = 0x63
				}
				typ := append([]byte{1, 0x60, 1}, input...)
				typ = append(typ, 1, output, direction.to)
				body := []byte{0, 0x20, 0, 0xfb, direction.opcode, 0x0b}
				if tc.unreachable {
					body = []byte{0, 0x00, 0xfb, direction.opcode, 0x0b}
				}
				code := append([]byte{1, byte(len(body))}, body...)
				data := module(section(secType, typ...), section(secFunction, 1, 0), section(secCode, code...))
				for _, ast := range []bool{false, true} {
					var err error
					if ast {
						var m *Module
						m, err = decodeModuleASTForTest(data)
						if err == nil {
							err = ValidateModule(m)
						}
					} else {
						err = ValidateByteBackedModule(data)
					}
					if (err == nil) != tc.valid {
						t.Errorf("ast=%v: validate=%v, want valid=%v", ast, err, tc.valid)
					}
				}
			})
		}
	}
}
