package wasm

import (
	"errors"
	"testing"
)

// TestRejectsRedundantControlTerminators pins validation fuzz regressions for
// malformed control flow.
func TestRejectsRedundantControlTerminators(t *testing.T) {
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

func TestDecodesLargeMixedResultSignature(t *testing.T) {
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

func TestRejectsRefIsNullWithNumericOperand(t *testing.T) {
	code := []byte{0x00, 0x41, 0x00, 0xd1, 0x1a, 0x0b} // locals; i32.const 0; ref.is_null; drop; end
	binary := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
	)

	for _, validate := range []struct {
		name string
		fn   func() error
	}{
		{name: "decoded module", fn: func() error {
			m, err := DecodeModule(binary)
			if err != nil {
				return err
			}
			return ValidateModule(m)
		}},
		{name: "byte-backed module", fn: func() error { return ValidateByteBackedModule(binary) }},
		{name: "programmatic module", fn: func() error {
			return ValidateModule(modWithFunc(nil, nil,
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: InstrRefIsNull},
				Instruction{Kind: InstrDrop},
			))
		}},
	} {
		t.Run(validate.name, func(t *testing.T) {
			var ve *ValidationError
			if err := validate.fn(); !errors.As(err, &ve) || ve.Code != ErrTypeMismatch {
				t.Fatalf("validation error = %v, want ErrTypeMismatch", err)
			}
		})
	}
}

func TestRejectsInvalidGCFieldGetsAndCasts(t *testing.T) {
	tests := []struct {
		name     string
		compType []byte
		funcBody []byte
	}{
		{
			name:     "struct.get_s with unpacked field",
			compType: []byte{0x5f, 0x01, 0x7f, 0x01}, // (struct (field (mut i32)))
			funcBody: []byte{0xfb, 0x01, 0x00, 0xfb, 0x03, 0x00, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "struct.get_u with unpacked field",
			compType: []byte{0x5f, 0x01, 0x7f, 0x01}, // (struct (field (mut i32)))
			funcBody: []byte{0xfb, 0x01, 0x00, 0xfb, 0x04, 0x00, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "struct.get with packed field",
			compType: []byte{0x5f, 0x01, 0x78, 0x01}, // (struct (field (mut i8)))
			funcBody: []byte{0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "array.get_s with unpacked field",
			compType: []byte{0x5e, 0x7f, 0x01}, // (array (mut i32))
			funcBody: []byte{0x41, 0x00, 0xfb, 0x07, 0x00, 0x41, 0x00, 0xfb, 0x0c, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "array.get_u with unpacked field",
			compType: []byte{0x5e, 0x7f, 0x01}, // (array (mut i32))
			funcBody: []byte{0x41, 0x00, 0xfb, 0x07, 0x00, 0x41, 0x00, 0xfb, 0x0d, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "array.get with packed field",
			compType: []byte{0x5e, 0x78, 0x01}, // (array (mut i8))
			funcBody: []byte{0x41, 0x00, 0xfb, 0x07, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x1a, 0x0b},
		},
		{
			name:     "ref.cast across disjoint reference hierarchies",
			compType: []byte{0x5e, 0x7f, 0x01}, // (array (mut i32))
			funcBody: []byte{0xd0, 0x70, 0xfb, 0x16, 0x00, 0x1a, 0x0b},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			typePayload := []byte{0x02}
			typePayload = append(typePayload, tc.compType...)
			typePayload = append(typePayload, 0x60, 0x00, 0x00) // () -> ()
			code := append([]byte{0x00}, tc.funcBody...)        // zero local declarations
			binary := module(
				section(secType, typePayload...),
				section(secFunction, 0x01, 0x01),
				section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
			)

			for _, validate := range []struct {
				name string
				fn   func() error
			}{
				{name: "decoded module", fn: func() error {
					m, err := DecodeModule(binary)
					if err != nil {
						return err
					}
					return ValidateModule(m)
				}},
				{name: "byte-backed module", fn: func() error { return ValidateByteBackedModule(binary) }},
			} {
				t.Run(validate.name, func(t *testing.T) {
					var ve *ValidationError
					if err := validate.fn(); !errors.As(err, &ve) || ve.Code != ErrTypeMismatch {
						t.Fatalf("validation error = %v, want ErrTypeMismatch", err)
					}
				})
			}
		})
	}
}

func TestAcceptsAtomicInstructionsOnUnsharedMemory(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{
			name: "atomic load",
			body: []byte{0x41, 0x00, 0xfe, 0x10, 0x02, 0x00, 0x1a, 0x0b},
		},
		{
			name: "atomic notify",
			body: []byte{0x41, 0x00, 0x41, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x1a, 0x0b},
		},
		{
			name: "atomic wait32",
			body: []byte{0x41, 0x00, 0x41, 0x00, 0x42, 0x00, 0xfe, 0x01, 0x02, 0x00, 0x1a, 0x0b},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := append([]byte{0x00}, tc.body...) // zero local declarations
			binary := module(
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secMemory, 0x01, 0x00, 0x01), // unshared memory, minimum one page
				section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
			)

			m, err := DecodeModule(binary)
			if err != nil {
				t.Fatalf("DecodeModule: %v", err)
			}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
			if err := ValidateByteBackedModule(binary); err != nil {
				t.Fatalf("ValidateByteBackedModule: %v", err)
			}
		})
	}
}

func TestBrOnCastPreservesLabelPrefixOnFallthrough(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind InstrKind
		sub  byte
	}{
		{name: "br_on_cast", kind: InstrBrOnCast, sub: 0x18},
		{name: "br_on_cast_fail", kind: InstrBrOnCastFail, sub: 0x19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := []byte{
				0x00,       // zero local declarations
				0x02, 0x00, // block (type 0), whose results are i32 and funcref
				0x41, 0x00, // label-prefix i32
				0xd0, 0x70, // ref.null func
				0xfb, tc.sub, 0x03, 0x00, 0x70, 0x70, // cast flags, label 0, funcref -> funcref
				0x0b, // end block
				0x0b, // end function
			}
			binary := module(
				section(secType, 0x01, 0x60, 0x00, 0x02, 0x7f, 0x70),
				section(secFunction, 0x01, 0x00),
				section(secCode, append([]byte{0x01}, append(u32(uint32(len(code))), code...)...)...),
			)

			funcType := ft(nil, []ValType{I32, FuncRef})
			programmatic := &Module{
				Types:     []RecType{funcType},
				FuncTypes: []TypeIdx{{Index: 0}},
				Code: []Func{{Body: Expr{Instrs: []Instruction{{
					Kind: InstrBlock,
					ext: &instrExt{
						BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}},
						Body: Expr{Instrs: []Instruction{
							{Kind: InstrI32Const},
							{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}},
							{Kind: tc.kind, Index: 0, Cast: CastOp{SourceNullable: true, TargetNullable: true}, ext: &instrExt{HeapType: AbsHeap(HeapFunc), HeapType2: AbsHeap(HeapFunc)}},
						}},
					},
				}}}}},
			}

			for _, validate := range []struct {
				name string
				fn   func() error
			}{
				{name: "decoded module", fn: func() error {
					m, err := DecodeModule(binary)
					if err != nil {
						return err
					}
					return ValidateModule(m)
				}},
				{name: "byte-backed module", fn: func() error { return ValidateByteBackedModule(binary) }},
				{name: "programmatic module", fn: func() error { return ValidateModule(programmatic) }},
			} {
				t.Run(validate.name, func(t *testing.T) {
					if err := validate.fn(); err != nil {
						t.Fatalf("validation rejected preserved label prefix: %v", err)
					}
				})
			}
		})
	}
}
