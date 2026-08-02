package wasm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWazeroPortTypedFunctionReferenceValidationEdges(t *testing.T) {
	root := filepath.Clean("../../../../tests/regressions/spectest-proposals/typed-function-references")
	for _, tc := range []struct {
		file string
		code ValidationErrorCode
	}{
		{"local_init.1.wasm", ErrUninitializedLocal},
		{"local_init.2.wasm", ErrUninitializedLocal},
		{"local_init.3.wasm", ErrUninitializedLocal},
		{"local_init.4.wasm", ErrUninitializedLocal},
		{"type-equivalence.3.wasm", ErrUnknownType},
		{"select.2.wasm", ErrTypeMismatch},
		{"select.3.wasm", ErrTypeMismatch},
		{"table.26.wasm", ErrTypeMismatch},
		{"table.27.wasm", ErrTypeMismatch},
		{"table.28.wasm", ErrTypeMismatch},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			module, err := DecodeModule(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			err = ValidateModule(module)
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Code != tc.code {
				t.Fatalf("validation error = %v, want %v", err, tc.code)
			}
		})
	}

	// The proposal-era type-equivalence.2 fixture expected a compact singleton
	// type not to recurse. The standardized rectype grammar makes every singleton
	// an implicit one-member recursive group, so the same binary is valid now.
	for _, file := range []string{"local_init.0.wasm", "local_init.5.wasm", "type-equivalence.2.wasm"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				t.Fatal(err)
			}
			module, err := DecodeModule(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := ValidateModule(module); err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
	}
}

func TestNonDefaultableLocalInitializationStructuredValidation(t *testing.T) {
	nonNullExtern := RefVal(Ref(false, AbsHeap(HeapExtern), false))
	functionType := ft([]ValType{nonNullExtern}, nil)
	base := func(body ...Instruction) *Module {
		return &Module{
			Types:     []RecType{functionType},
			FuncTypes: []TypeIdx{{Index: 0}},
			Code: []Func{{
				Locals: Locals{Runs: []LocalRun{{Count: 1, Type: nonNullExtern}}},
				Body:   Expr{Instrs: body},
			}},
		}
	}

	t.Run("get-before-set", func(t *testing.T) {
		expectValidateErr(t, base(
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrDrop},
		), ErrUninitializedLocal)
	})
	t.Run("set-then-get", func(t *testing.T) {
		if err := ValidateModule(base(
			Instruction{Kind: InstrLocalGet, Index: 0},
			Instruction{Kind: InstrLocalSet, Index: 1},
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrDrop},
		)); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	t.Run("block-initialization-does-not-escape", func(t *testing.T) {
		expectValidateErr(t, base(
			Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{
				{Kind: InstrLocalGet, Index: 0},
				{Kind: InstrLocalSet, Index: 1},
			}}}},
			Instruction{Kind: InstrLocalGet, Index: 1},
			Instruction{Kind: InstrDrop},
		), ErrUninitializedLocal)
	})
}
