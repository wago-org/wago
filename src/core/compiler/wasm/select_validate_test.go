package wasm

import "testing"

func TestValidateSelectForms(t *testing.T) {
	t.Run("implicit numeric", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64},
			Instruction{Kind: InstrI64Const},
			Instruction{Kind: InstrI64Const},
			Instruction{Kind: InstrI32Const},
			Instruction{Kind: InstrSelect},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	t.Run("implicit vector", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{V128},
			Instruction{Kind: InstrV128Const},
			Instruction{Kind: InstrV128Const},
			Instruction{Kind: InstrI32Const},
			Instruction{Kind: InstrSelect},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	for _, refType := range []ValType{FuncRef, ExternRef} {
		refType := refType
		t.Run("implicit "+refType.String(), func(t *testing.T) {
			m := modWithFunc([]ValType{refType}, nil,
				Instruction{Kind: InstrLocalGet, Index: 0},
				Instruction{Kind: InstrLocalGet, Index: 0},
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: InstrSelect},
				Instruction{Kind: InstrDrop},
			)
			expectValidateErr(t, m, ErrTypeMismatch)
		})

		t.Run("typed "+refType.String(), func(t *testing.T) {
			m := modWithFunc([]ValType{refType}, []ValType{refType},
				Instruction{Kind: InstrLocalGet, Index: 0},
				Instruction{Kind: InstrLocalGet, Index: 0},
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{refType}}},
			)
			if err := ValidateModule(m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}

	t.Run("implicit bottom remains polymorphic", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrUnreachable},
			Instruction{Kind: InstrSelect},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	t.Run("implicit bottom matches known numeric", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrUnreachable},
			Instruction{Kind: InstrI64Const},
			Instruction{Kind: InstrI32Const},
			Instruction{Kind: InstrSelect},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})

	t.Run("implicit bottom does not hide known reference", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrUnreachable},
			Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapExtern)}},
			Instruction{Kind: InstrI32Const},
			Instruction{Kind: InstrSelect},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}
