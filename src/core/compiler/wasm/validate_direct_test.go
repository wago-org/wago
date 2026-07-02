package wasm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateModuleDirectSimpleFunction(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x41, 0x07, 0x0b),
	)
	if err := ValidateModuleDirect(b); err != nil {
		t.Fatalf("ValidateModuleDirect(simple): %v", err)
	}
}

func TestValidateModuleDirectRejectsTypeMismatch(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x42, 0x00, 0x0b), // i64.const for i32 result
	)
	err := ValidateModuleDirect(b)
	if !isValidationCode(err, ErrTypeMismatch) {
		t.Fatalf("ValidateModuleDirect(type mismatch) = %v, want %v", err, ErrTypeMismatch)
	}
}

func TestValidateModuleDirectNestedBlock(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x07, 0x00, 0x02, 0x7f, 0x41, 0x01, 0x0b, 0x0b),
	)
	if err := ValidateModuleDirect(b); err != nil {
		t.Fatalf("ValidateModuleDirect(nested block): %v", err)
	}
}

func TestValidateModuleDirectStrictNameSection(t *testing.T) {
	b := module(custom("name"), custom("name"))
	err := ValidateModuleDirect(b)
	if err == nil {
		t.Fatal("ValidateModuleDirect(duplicate name): nil, want error")
	}
}

func TestValidateModuleDirectBulkMemoryUsesDataSummary(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secMemory, 0x01, 0x00, 0x01),
		section(secDataCount, 0x01),
		section(secCode, 0x01,
			0x0c,       // body size
			0x00,       // locals
			0x41, 0x00, // destination
			0x41, 0x00, // source offset in data segment
			0x41, 0x00, // length
			0xfc, 0x08, 0x00, 0x00, // memory.init data 0 memory 0
			0x0b,
		),
		section(secData, 0x01, 0x01, 0x03, 'a', 'b', 'c'),
	)
	if err := ValidateModuleDirect(b); err != nil {
		t.Fatalf("ValidateModuleDirect(bulk memory): %v", err)
	}
}

func TestValidateModuleDirectAllElementSegmentForms(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secTable, 0x01, 0x70, 0x00, 0x01),
		section(secElement,
			0x08,
			// 0: active implicit-table funcidx payload
			0x00, 0x41, 0x00, 0x0b, 0x01, 0x00,
			// 1: passive funcidx payload, exercised by table.init/elem.drop below
			0x01, 0x00, 0x01, 0x00,
			// 2: active explicit-table funcidx payload
			0x02, 0x00, 0x41, 0x00, 0x0b, 0x00, 0x01, 0x00,
			// 3: declarative funcidx payload
			0x03, 0x00, 0x01, 0x00,
			// 4: active implicit-table funcref expr payload
			0x04, 0x41, 0x00, 0x0b, 0x01, 0xd0, 0x70, 0x0b,
			// 5: passive typed expr payload
			0x05, 0x6f, 0x01, 0xd0, 0x6f, 0x0b,
			// 6: active explicit-table typed expr payload
			0x06, 0x00, 0x41, 0x00, 0x0b, 0x70, 0x01, 0xd0, 0x70, 0x0b,
			// 7: declarative typed expr payload
			0x07, 0x70, 0x01, 0xd0, 0x70, 0x0b,
		),
		section(secCode, 0x01,
			0x0f,
			0x00,
			0x41, 0x00,
			0x41, 0x00,
			0x41, 0x00,
			0xfc, 0x0c, 0x01, 0x00,
			0xfc, 0x0d, 0x01,
			0x0b,
		),
	)
	if err := ValidateModuleDirect(b); err != nil {
		t.Fatalf("ValidateModuleDirect(all element forms): %v", err)
	}
	if err := decodeThenValidate(b); err != nil {
		t.Fatalf("DecodeModule+ValidateModule(all element forms): %v", err)
	}
}

func TestValidateModuleDirectConstExprSummaries(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secTable,
			0x01,       // tables
			0x40, 0x00, // table with initializer form
			0x70, 0x00, 0x01, // funcref min=1
			0xd0, 0x70, 0x0b, // ref.null func
		),
		section(secMemory, 0x01, 0x00, 0x01),
		section(secGlobal,
			0x01,
			0x7f, 0x00, // i32 const global
			0x41, 0x00, 0x0b,
		),
		section(secElement,
			0x01,
			0x06,             // active typed expr segment with explicit table
			0x00,             // table 0
			0x41, 0x00, 0x0b, // offset
			0x70,                   // funcref
			0x01, 0xd0, 0x70, 0x0b, // one ref.null func expr
		),
		section(secCode, 0x01, 0x02, 0x00, 0x0b),
		section(secData,
			0x01,
			0x00,             // active mem 0
			0x41, 0x00, 0x0b, // offset
			0x00, // empty payload
		),
	)
	if err := ValidateModuleDirect(b); err != nil {
		t.Fatalf("ValidateModuleDirect(const expr summaries): %v", err)
	}
}

func TestValidateModuleDirectDifferentialTestdata(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "..", "tests", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wasm" {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tests", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			want := decodeThenValidate(b)
			got := ValidateModuleDirect(b)
			if (want == nil) != (got == nil) {
				t.Fatalf("DecodeModule+ValidateModule=%v ValidateModuleDirect=%v", want, got)
			}
			if want != nil && errorClass(want) != errorClass(got) {
				t.Fatalf("DecodeModule+ValidateModule=%v (%s) ValidateModuleDirect=%v (%s)", want, errorClass(want), got, errorClass(got))
			}
		})
	}
}

func TestValidateModuleDirectDifferentialNegativeCases(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
	}{
		{"bad magic", []byte("not wasm")},
		{"function result type mismatch", module(
			section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
			section(secFunction, 0x01, 0x00),
			section(secCode, 0x01, 0x04, 0x00, 0x42, 0x00, 0x0b),
		)},
		{"global init type mismatch", module(
			section(secGlobal, 0x01, 0x7f, 0x00, 0x42, 0x00, 0x0b),
		)},
		{"data offset type mismatch", module(
			section(secMemory, 0x01, 0x00, 0x01),
			section(secData, 0x01, 0x00, 0x42, 0x00, 0x0b, 0x00),
		)},
		{"global init non const instruction", module(
			section(secGlobal, 0x01, 0x7f, 0x00, 0x20, 0x00, 0x0b),
		)},
		{"active element offset non const instruction", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secTable, 0x01, 0x70, 0x00, 0x01),
			section(secElement, 0x01, 0x00, 0x20, 0x00, 0x0b, 0x01, 0x00),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
		)},
		{"element unknown func", module(
			section(secTable, 0x01, 0x70, 0x00, 0x01),
			section(secElement, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00),
		)},
		{"active element table type mismatch", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secTable, 0x01, 0x6f, 0x00, 0x01),
			section(secElement, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
		)},
		{"table init passive element type mismatch", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secTable, 0x01, 0x70, 0x00, 0x01),
			section(secElement, 0x01, 0x05, 0x6f, 0x01, 0xd0, 0x6f, 0x0b),
			section(secCode, 0x01,
				0x0c,
				0x00,
				0x41, 0x00,
				0x41, 0x00,
				0x41, 0x00,
				0xfc, 0x0c, 0x00, 0x00,
				0x0b,
			),
		)},
		{"typed element expression type mismatch", module(
			section(secElement, 0x01, 0x05, 0x6f, 0x01, 0xd0, 0x70, 0x0b),
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := decodeThenValidate(tc.b)
			got := ValidateModuleDirect(tc.b)
			if (want == nil) != (got == nil) {
				t.Fatalf("DecodeModule+ValidateModule=%v ValidateModuleDirect=%v", want, got)
			}
			if want != nil && errorClass(want) != errorClass(got) {
				t.Fatalf("DecodeModule+ValidateModule=%v (%s) ValidateModuleDirect=%v (%s)", want, errorClass(want), got, errorClass(got))
			}
		})
	}
}

func decodeThenValidate(b []byte) error {
	m, err := DecodeModule(b)
	if err != nil {
		return err
	}
	return ValidateModule(m)
}

func errorClass(err error) string {
	var de *DecodeError
	if errors.As(err, &de) {
		return "decode:" + de.Code.String()
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return "validation:" + ve.Code.String()
	}
	if err == nil {
		return "nil"
	}
	return "other"
}
