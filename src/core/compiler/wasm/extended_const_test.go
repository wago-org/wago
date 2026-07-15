package wasm

import (
	"errors"
	"strings"
	"testing"
)

func extULEB(v uint32) []byte {
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

func extVec(items ...[]byte) []byte {
	out := extULEB(uint32(len(items)))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func extSection(id byte, payload []byte) []byte {
	out := []byte{id}
	out = append(out, extULEB(uint32(len(payload)))...)
	return append(out, payload...)
}

func extModule(sections ...[]byte) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 1, 0, 0, 0}
	for _, section := range sections {
		out = append(out, section...)
	}
	return out
}

func extName(s string) []byte { return append(extULEB(uint32(len(s))), []byte(s)...) }

func extGlobalEntry(t ValType, mutable bool, init []byte) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	out := []byte{MustEncodeValType(t), mut}
	return append(out, init...)
}

func extGlobalImportEntry(module, name string, t ValType) []byte {
	out := append(extName(module), extName(name)...)
	return append(out, 3, MustEncodeValType(t), 0)
}

func extendedConstBinaryModule() []byte {
	return extModule(
		extSection(2, extVec(extGlobalImportEntry("env", "seed", I32))),
		extSection(6, extVec(
			extGlobalEntry(I32, false, []byte{0x41, 20, 0x41, 2, 0x6c, 0x41, 2, 0x6b, 0x41, 4, 0x6a, 0x0b}),
			extGlobalEntry(I32, false, []byte{0x23, 0x00, 0x41, 42, 0x6a, 0x0b}),
			extGlobalEntry(I32, false, []byte{0x23, 0x02, 0x41, 3, 0x6c, 0x0b}),
		)),
	)
}

func TestExtendedConstValidationByteBackedAndAST(t *testing.T) {
	data := extendedConstBinaryModule()
	m, err := DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule byte-backed globals: %v", err)
	}
	if err := ValidateByteBackedModule(data); err != nil {
		t.Fatalf("ValidateByteBackedModule: %v", err)
	}

	ast := &Module{
		Imports: []Import{{Module: "env", Name: "seed", Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I32}}}},
		Globals: []Global{
			{Type: GlobalType{Type: I64}, Init: Expr{Instrs: []Instruction{{Kind: InstrI64Const, I64: 20}, {Kind: InstrI64Const, I64: 2}, {Kind: InstrI64Mul}, {Kind: InstrI64Const, I64: 5}, {Kind: InstrI64Add}}}},
			{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 0}, {Kind: InstrI32Const, I32: 1}, {Kind: InstrI32Add}}}},
			{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 2}, {Kind: InstrI32Const, I32: 2}, {Kind: InstrI32Mul}}}},
		},
	}
	if err := ValidateModule(ast); err != nil {
		t.Fatalf("ValidateModule AST globals: %v", err)
	}
}

func TestExtendedConstValidationRejectsStrictly(t *testing.T) {
	validateBoth := func(t *testing.T, data []byte, code ValidationErrorCode) {
		t.Helper()
		for _, validate := range []struct {
			name string
			fn   func([]byte) error
		}{{"AST decode", func(b []byte) error {
			m, err := DecodeModule(b)
			if err != nil {
				return err
			}
			return ValidateModule(m)
		}}, {"byte-backed", ValidateByteBackedModule}} {
			t.Run(validate.name, func(t *testing.T) {
				err := validate.fn(data)
				var ve *ValidationError
				if !errors.As(err, &ve) || ve.Code != code {
					t.Fatalf("error = %v, want validation code %v", err, code)
				}
			})
		}
	}

	t.Run("mixed integer operand types", func(t *testing.T) {
		data := extModule(extSection(6, extVec(
			extGlobalEntry(I32, false, []byte{0x41, 1, 0x42, 2, 0x6a, 0x0b}),
		)))
		validateBoth(t, data, ErrTypeMismatch)
	})
	t.Run("non-constant opcode", func(t *testing.T) {
		data := extModule(extSection(6, extVec(
			extGlobalEntry(I32, false, []byte{0x41, 1, 0x45, 0x0b}),
		)))
		validateBoth(t, data, ErrConstExprRequired)
	})
	t.Run("forward global", func(t *testing.T) {
		data := extModule(extSection(6, extVec(
			extGlobalEntry(I32, false, []byte{0x23, 0x01, 0x0b}),
			extGlobalEntry(I32, false, []byte{0x41, 0, 0x0b}),
		)))
		validateBoth(t, data, ErrConstExprRequired)
	})
	t.Run("mutable prior global", func(t *testing.T) {
		data := extModule(extSection(6, extVec(
			extGlobalEntry(I32, true, []byte{0x41, 0, 0x0b}),
			extGlobalEntry(I32, false, []byte{0x23, 0x00, 0x0b}),
		)))
		validateBoth(t, data, ErrConstExprRequired)
	})
	t.Run("data offset cannot use local global", func(t *testing.T) {
		dataEntry := append([]byte{0x00, 0x23, 0x00, 0x0b}, extULEB(0)...)
		data := extModule(
			extSection(5, extVec([]byte{0x00, 0x01})),
			extSection(6, extVec(extGlobalEntry(I32, false, []byte{0x41, 0, 0x0b}))),
			extSection(11, extVec(dataEntry)),
		)
		for _, validate := range []func([]byte) error{func(b []byte) error {
			m, err := DecodeModule(b)
			if err != nil {
				return err
			}
			return ValidateModule(m)
		}, ValidateByteBackedModule} {
			err := validate(data)
			if err == nil || !strings.Contains(err.Error(), "constant expression required") {
				t.Fatalf("error = %v, want strict local-global offset rejection", err)
			}
		}
	})
}
