package wasm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateModuleWithWorkersParity(t *testing.T) {
	data := benchmarkDecodeValidateModule(256)
	m, err := DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("serial validation: %v", err)
	}
	for _, workers := range []int{0, 1, 2, 4, 8, 512} {
		for run := 0; run < 10; run++ {
			if err := ValidateModuleWithWorkers(m, workers); err != nil {
				t.Fatalf("workers=%d run=%d: %v", workers, run, err)
			}
		}
	}
}

func TestValidateModuleWithWorkersCorpusParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large validation corpus parity in short mode")
	}
	corpus := filepath.Join("..", "..", "..", "..", "bench", "corpus")
	for _, name := range []string{
		"tiny.wasm", "many_funcs.wasm", "json-as.wasm", "json-as-simd.wasm",
		"lua.wasm", "sqlite3.wasm", "ruby.wasm", "esbuild.wasm",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpus, name))
			if err != nil {
				t.Fatal(err)
			}
			m, err := DecodeModule(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("serial: %v", err)
			}
			for _, workers := range []int{2, 4, 8} {
				if err := ValidateModuleWithWorkers(m, workers); err != nil {
					t.Fatalf("p%d: %v", workers, err)
				}
			}
		})
	}
}

func TestValidateModuleWithWorkersLowestFunctionError(t *testing.T) {
	const funcs = 16
	m := &Module{
		Types:     []RecType{ft(nil, nil)},
		FuncTypes: make([]TypeIdx, funcs),
		Code:      make([]Func, funcs),
	}
	m.Code[3].Body.Instrs = []Instruction{{Kind: InstrLocalGet, Index: 0}}
	m.Code[11].Body.Instrs = []Instruction{{Kind: InstrCall, Index: 99}}

	want := ValidateModule(m)
	var wantValidation *ValidationError
	if !errors.As(want, &wantValidation) {
		t.Fatalf("serial error = %T %v, want ValidationError", want, want)
	}
	if wantValidation.Func != 3 || wantValidation.Code != ErrUnknownLocal {
		t.Fatalf("serial error = %+v, want function 3 unknown local", wantValidation)
	}

	for _, workers := range []int{2, 4, 8, 32} {
		for run := 0; run < 100; run++ {
			got := ValidateModuleWithWorkers(m, workers)
			var gotValidation *ValidationError
			if !errors.As(got, &gotValidation) {
				t.Fatalf("workers=%d run=%d error = %T %v, want ValidationError", workers, run, got, got)
			}
			if *gotValidation != *wantValidation {
				t.Fatalf("workers=%d run=%d error = %+v, want %+v", workers, run, gotValidation, wantValidation)
			}
		}
	}
}

func TestValidateModuleWithWorkersInvalidTypeCacheMiss(t *testing.T) {
	m, err := DecodeModule(benchmarkDecodeValidateModule(64))
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	// call_indirect type 127 misses the frozen valid-type cache. Every fourth
	// function uses it so several workers can hit the malformed index together.
	for i := 0; i < len(m.Code); i += 4 {
		m.Code[i].BodyBytes = []byte{0x11, 0x7f, 0x00, 0x0b}
	}
	want := ValidateModule(m)
	if want == nil {
		t.Fatal("serial validation unexpectedly accepted invalid type index")
	}
	for run := 0; run < 100; run++ {
		got := ValidateModuleWithWorkers(m, 8)
		if got == nil || got.Error() != want.Error() {
			t.Fatalf("run=%d parallel error = %v, want %v", run, got, want)
		}
	}
}

func TestValidateDecodedByteBackedModuleWithWorkers(t *testing.T) {
	dm, err := DecodeModuleByteBacked(benchmarkDecodeValidateModule(256))
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked: %v", err)
	}
	for _, workers := range []int{1, 2, 4, 8} {
		if err := ValidateDecodedByteBackedModuleWithWorkers(dm, workers); err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
	}
}

func TestValidateModuleWithWorkersTableInitElementExprRace(t *testing.T) {
	t.Run("valid func and typed expressions", func(t *testing.T) {
		m := tableInitExprModule(AbsRef(HeapFunc), false)
		assertTableInitWorkerParity(t, func(workers int) error {
			return ValidateModuleWithWorkers(m, workers)
		}, false)
	})

	t.Run("deterministic type mismatch", func(t *testing.T) {
		m := tableInitExprModule(AbsRef(HeapExtern), false)
		validate := func(workers int) error { return ValidateModuleWithWorkers(m, workers) }
		assertTableInitWorkerParity(t, validate, true)
		var verr *ValidationError
		if err := validate(1); !errors.As(err, &verr) || verr.Func != 0 || verr.Code != ErrTypeMismatch {
			t.Fatalf("table/element mismatch error = %v, want function 0 type mismatch", err)
		}
	})

	t.Run("initializer still validates at module level", func(t *testing.T) {
		m := tableInitExprModule(AbsRef(HeapFunc), true)
		assertTableInitWorkerParity(t, func(workers int) error {
			return ValidateModuleWithWorkers(m, workers)
		}, true)
		var verr *ValidationError
		if err := ValidateModuleWithWorkers(m, 8); !errors.As(err, &verr) || verr.Func != -1 || verr.Code != ErrTypeMismatch {
			t.Fatalf("initializer error = %v, want module-level type mismatch", err)
		}
	})

	t.Run("function phase does not revalidate initializers", func(t *testing.T) {
		m := tableInitExprModule(AbsRef(HeapFunc), false)
		v := &moduleValidator{m: m, funcIndex: -1}
		if err := v.validateModule(); err != nil {
			t.Fatalf("module validation: %v", err)
		}
		for i := range m.Elements {
			m.Elements[i].Kind.Exprs[0].BodyBytes = []byte{0x41, 0x00, 0x0b}
		}
		if err := v.validateFunctions(8); err != nil {
			t.Fatalf("function validation re-read initializer expressions: %v", err)
		}
	})

	t.Run("defensive metadata errors", func(t *testing.T) {
		m := tableInitExprModule(AbsRef(HeapFunc), false)
		fv := funcValidator{moduleValidator: &moduleValidator{m: m}, funcIndex: 7}
		if _, err := fv.elemRefType(uint32(len(m.Elements))); !isValidationCode(err, ErrUnknownTable) {
			t.Fatalf("out-of-range element error = %v", err)
		}
		m.Elements[0].Kind.Kind = ElemKindKind(0xff)
		if _, err := fv.elemRefType(0); !isValidationCode(err, ErrTypeMismatch) {
			t.Fatalf("unknown element kind error = %v", err)
		}
	})
}

func TestValidateDecodedByteBackedModuleWithWorkersTableInitElementExprRace(t *testing.T) {
	t.Run("valid func and typed expressions", func(t *testing.T) {
		dm, err := DecodeModuleByteBacked(tableInitExprModuleBytes(0x70, true, false))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		assertTableInitWorkerParity(t, func(workers int) error {
			return ValidateDecodedByteBackedModuleWithWorkers(dm, workers)
		}, false)
	})

	t.Run("deterministic type mismatch", func(t *testing.T) {
		dm, err := DecodeModuleByteBacked(tableInitExprModuleBytes(0x6f, false, false))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		validate := func(workers int) error {
			return ValidateDecodedByteBackedModuleWithWorkers(dm, workers)
		}
		assertTableInitWorkerParity(t, validate, true)
		var verr *ValidationError
		if err := validate(1); !errors.As(err, &verr) || verr.Func != 0 || verr.Code != ErrTypeMismatch {
			t.Fatalf("table/element mismatch error = %v, want function 0 type mismatch", err)
		}
	})

	t.Run("initializer still validates at module level", func(t *testing.T) {
		dm, err := DecodeModuleByteBacked(tableInitExprModuleBytes(0x70, false, true))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		assertTableInitWorkerParity(t, func(workers int) error {
			return ValidateDecodedByteBackedModuleWithWorkers(dm, workers)
		}, true)
		var verr *ValidationError
		if err := ValidateDecodedByteBackedModuleWithWorkers(dm, 8); !errors.As(err, &verr) || verr.Func != -1 || verr.Code != ErrTypeMismatch {
			t.Fatalf("initializer error = %v, want module-level type mismatch", err)
		}
	})

	t.Run("function phase does not revalidate initializers", func(t *testing.T) {
		dm, err := DecodeModuleByteBacked(tableInitExprModuleBytes(0x70, true, false))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		v := &moduleValidator{m: dm.Module, funcIndex: -1, direct: &dm.direct}
		if err := v.validateModule(); err != nil {
			t.Fatalf("module validation: %v", err)
		}
		for i := range dm.direct.elements {
			dm.direct.elements[i].exprs[0].body = []byte{0x41, 0x00, 0x0b}
		}
		if err := v.validateFunctions(8); err != nil {
			t.Fatalf("function validation re-read direct initializer expressions: %v", err)
		}
	})

	t.Run("defensive metadata errors", func(t *testing.T) {
		direct := &directValidationEnv{elements: []directElem{{kind: ElemKindKind(0xff)}}}
		fv := funcValidator{moduleValidator: &moduleValidator{m: &Module{}, direct: direct}, funcIndex: 7}
		if _, err := fv.directElemRefType(1); !isValidationCode(err, ErrUnknownTable) {
			t.Fatalf("out-of-range direct element error = %v", err)
		}
		if _, err := fv.directElemRefType(0); !isValidationCode(err, ErrTypeMismatch) {
			t.Fatalf("unknown direct element kind error = %v", err)
		}
	})
}

func assertTableInitWorkerParity(t *testing.T, validate func(int) error, wantError bool) {
	t.Helper()
	want := validate(1)
	if wantError && want == nil {
		t.Fatal("serial validation unexpectedly succeeded")
	}
	if !wantError && want != nil {
		t.Fatalf("serial validation: %v", want)
	}
	if wantError {
		var verr *ValidationError
		if !errors.As(want, &verr) {
			t.Fatalf("serial error = %T %v, want ValidationError", want, want)
		}
	}
	for _, workers := range []int{2, 4, 8} {
		for run := 0; run < 10; run++ {
			got := validate(workers)
			if !sameValidationResult(got, want) {
				t.Fatalf("workers=%d run=%d error = %v, want %v", workers, run, got, want)
			}
		}
	}
}

func sameValidationResult(got, want error) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	var gotValidation, wantValidation *ValidationError
	if errors.As(got, &gotValidation) && errors.As(want, &wantValidation) {
		return *gotValidation == *wantValidation
	}
	return got.Error() == want.Error()
}

func tableInitExprModule(tableRef RefType, badInitializer bool) *Module {
	const funcs = 64
	firstExpr := []byte{0xd0, 0x70, 0x0b} // ref.null func
	if badInitializer {
		firstExpr = []byte{0x41, 0x00, 0x0b} // i32.const 0, not funcref
	}
	m := &Module{
		Types:     []RecType{ft(nil, nil)},
		FuncTypes: make([]TypeIdx, funcs),
		Code:      make([]Func, funcs),
		Tables:    []Table{{Type: TableType{Ref: tableRef, Limits: Limits{Min: 1}}}},
		Elements: []Elem{
			{
				Mode: ElemMode{Kind: ElemPassive},
				Kind: ElemKind{Kind: ElemFuncExprs, Exprs: []Expr{{BodyBytes: firstExpr}}},
			},
			{
				Mode: ElemMode{Kind: ElemPassive},
				Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapFunc), Exprs: []Expr{{BodyBytes: []byte{0xd2, 0x00, 0x0b}}}}, // ref.func 0
			},
		},
	}
	body := tableInitExprBody(0, 1)
	for i := range m.Code {
		m.Code[i].BodyBytes = body
	}
	return m
}

func tableInitExprModuleBytes(tableType byte, bothElementKinds, badInitializer bool) []byte {
	const funcs = 64
	funcTypes := append([]byte{}, u32(funcs)...)
	funcTypes = append(funcTypes, make([]byte, funcs)...)

	elemPayload := u32(1)
	firstExpr := []byte{0xd0, 0x70, 0x0b} // ref.null func
	if badInitializer {
		firstExpr = []byte{0x41, 0x00, 0x0b} // i32.const 0, not funcref
	}
	// flags=5: passive typed expression segment, declared funcref.
	elemPayload = append(elemPayload, 0x05, 0x70, 0x01)
	elemPayload = append(elemPayload, firstExpr...)
	indexes := []uint32{0}
	if bothElementKinds {
		elemPayload[0] = 0x02
		// flags=4: active implicit-table function-expression segment. This covers
		// the direct ElemFuncExprs representation with a raw ref.func initializer.
		elemPayload = append(elemPayload, 0x04, 0x41, 0x00, 0x0b, 0x01, 0xd2, 0x00, 0x0b)
		indexes = append(indexes, 1)
	}

	bodyExpr := tableInitExprBody(indexes...)
	funcBody := append([]byte{0x00}, bodyExpr...) // zero local declarations
	codePayload := append([]byte{}, u32(funcs)...)
	for range funcs {
		codePayload = append(codePayload, u32(uint32(len(funcBody)))...)
		codePayload = append(codePayload, funcBody...)
	}

	return module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, funcTypes...),
		section(secTable, 0x01, tableType, 0x00, 0x01),
		section(secElement, elemPayload...),
		section(secCode, codePayload...),
	)
}

func tableInitExprBody(elementIndexes ...uint32) []byte {
	body := make([]byte, 0, len(elementIndexes)*10+1)
	for _, index := range elementIndexes {
		body = append(body,
			0x41, 0x00, // destination table offset
			0x41, 0x00, // source element offset
			0x41, 0x00, // length
			0xfc, 0x0c, // table.init
		)
		body = append(body, u32(index)...)
		body = append(body, 0x00) // table index 0
	}
	return append(body, 0x0b)
}
