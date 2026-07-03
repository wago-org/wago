package wasm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateByteBackedModuleSimpleFunction(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x41, 0x07, 0x0b),
	)
	if err := ValidateByteBackedModule(b); err != nil {
		t.Fatalf("ValidateByteBackedModule(simple): %v", err)
	}
}

func TestDecodeModuleByteBackedKeepsRawFunctionBytes(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x41, 0x07, 0x0b),
	)
	dm, err := DecodeModuleByteBacked(b)
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked(simple): %v", err)
	}
	if err := ValidateDecodedByteBackedModule(dm); err != nil {
		t.Fatalf("ValidateDecodedByteBackedModule(simple): %v", err)
	}
	m := dm.Module
	if len(m.Code) != 1 {
		t.Fatalf("code len = %d, want 1", len(m.Code))
	}
	if got, want := m.Code[0].BodyBytes, []byte{0x41, 0x07, 0x0b}; !bytes.Equal(got, want) {
		t.Fatalf("BodyBytes = %#v, want %#v", got, want)
	}
	if len(m.Code[0].Body.Instrs) != 0 {
		t.Fatalf("function body instruction tree has %d instruction(s), want none", len(m.Code[0].Body.Instrs))
	}

	ast, err := decodeModuleASTForTest(b)
	if err != nil {
		t.Fatalf("decodeModuleASTForTest(simple): %v", err)
	}
	if len(ast.Code[0].Body.Instrs) == 0 || len(ast.Code[0].BodyBytes) != 0 {
		t.Fatalf("AST oracle did not materialize only instructions: instrs=%d bytes=%x", len(ast.Code[0].Body.Instrs), ast.Code[0].BodyBytes)
	}
}

func TestDecodeModuleByteBackedSlicesLargeBrTableWithoutMaterializingLabels(t *testing.T) {
	body := []byte{0x0e}
	body = append(body, u32(4096)...)
	for i := 0; i < 4096; i++ {
		body = append(body, 0x00)
	}
	body = append(body, 0x00, 0x0b)
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, append([]byte{0x01}, append(u32(uint32(1+len(body))), append([]byte{0x00}, body...)...)...)...),
	)
	dm, err := DecodeModuleByteBacked(b)
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked(large br_table): %v", err)
	}
	if got := dm.Module.Code[0].BodyBytes; !bytes.Equal(got, body) {
		t.Fatalf("BodyBytes len=%d, want %d", len(got), len(body))
	}
}

func TestDecodeModuleByteBackedRejectsTruncatedSkippedImmediates(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"br_table missing default", []byte{0x0e, 0x01, 0x00}},
		{"try_table missing catch label", []byte{0x1f, 0x40, 0x01, 0x00, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := module(
				section(secType, 0x01, 0x60, 0x00, 0x00),
				section(secFunction, 0x01, 0x00),
				section(secCode, append([]byte{0x01}, append(u32(uint32(1+len(tc.body))), append([]byte{0x00}, tc.body...)...)...)...),
			)
			if _, err := DecodeModuleByteBacked(b); err == nil {
				t.Fatal("DecodeModuleByteBacked succeeded, want malformed immediate error")
			}
		})
	}
}

func TestValidateByteBackedModuleRejectsTypeMismatch(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x04, 0x00, 0x42, 0x00, 0x0b), // i64.const for i32 result
	)
	err := ValidateByteBackedModule(b)
	if !isValidationCode(err, ErrTypeMismatch) {
		t.Fatalf("ValidateByteBackedModule(type mismatch) = %v, want %v", err, ErrTypeMismatch)
	}
}

func TestValidateByteBackedModuleNestedBlock(t *testing.T) {
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(secFunction, 0x01, 0x00),
		section(secCode, 0x01, 0x07, 0x00, 0x02, 0x7f, 0x41, 0x01, 0x0b, 0x0b),
	)
	if err := ValidateByteBackedModule(b); err != nil {
		t.Fatalf("ValidateByteBackedModule(nested block): %v", err)
	}
}

func TestValidateByteBackedModuleStrictNameSection(t *testing.T) {
	b := module(custom("name"), custom("name"))
	err := ValidateByteBackedModule(b)
	if err == nil {
		t.Fatal("ValidateByteBackedModule(duplicate name): nil, want error")
	}
}

func TestValidateByteBackedModuleBulkMemoryUsesDataSummary(t *testing.T) {
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
	if err := ValidateByteBackedModule(b); err != nil {
		t.Fatalf("ValidateByteBackedModule(bulk memory): %v", err)
	}
}

func TestValidateByteBackedModuleAllElementSegmentForms(t *testing.T) {
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
	if err := ValidateByteBackedModule(b); err != nil {
		t.Fatalf("ValidateByteBackedModule(all element forms): %v", err)
	}
	if err := decodeThenValidate(b); err != nil {
		t.Fatalf("DecodeModule+ValidateModule(all element forms): %v", err)
	}
}

func TestDecodeModuleByteBackedRejectsHugeTruncatedElementVector(t *testing.T) {
	// The declared element payload length is attacker-controlled. The byte-backed
	// decoder must not use it directly as a slice capacity before discovering
	// that the section is truncated.
	b := module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secTable, 0x01, 0x70, 0x00, 0x01),
		section(secElement, append([]byte{
			0x01,             // one element segment
			0x00,             // active implicit table, function-index payload
			0x41, 0x00, 0x0b, // offset
		}, u32(^uint32(0))...)...), // huge declared funcidx vector, no entries
	)
	if err := byteBackedDecodeThenValidate(b); err == nil {
		t.Fatal("DecodeModuleByteBacked truncated huge element vector: nil, want error")
	}
	if err := decodeThenValidate(b); err == nil {
		t.Fatal("DecodeModule truncated huge element vector: nil, want error")
	}
}

func TestValidateByteBackedModuleConstExprSummaries(t *testing.T) {
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
	dm, err := DecodeModuleByteBacked(b)
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked(const expr summaries): %v", err)
	}
	if err := ValidateDecodedByteBackedModule(dm); err != nil {
		t.Fatalf("ValidateDecodedByteBackedModule(const expr summaries): %v", err)
	}
	m := dm.Module
	if m.Tables[0].Init == nil || !bytes.Equal(m.Tables[0].Init.BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		t.Fatalf("table init bytes = %#v", m.Tables[0].Init)
	}
	if !bytes.Equal(m.Globals[0].Init.BodyBytes, []byte{0x41, 0x00, 0x0b}) {
		t.Fatalf("global init bytes = %#v", m.Globals[0].Init.BodyBytes)
	}
	if !bytes.Equal(m.Elements[0].Mode.Offset.BodyBytes, []byte{0x41, 0x00, 0x0b}) || !bytes.Equal(m.Elements[0].Kind.Exprs[0].BodyBytes, []byte{0xd0, 0x70, 0x0b}) {
		t.Fatalf("element expr bytes = offset %#v expr %#v", m.Elements[0].Mode.Offset.BodyBytes, m.Elements[0].Kind.Exprs[0].BodyBytes)
	}
	if !bytes.Equal(m.Data[0].Mode.Offset.BodyBytes, []byte{0x41, 0x00, 0x0b}) {
		t.Fatalf("data offset bytes = %#v", m.Data[0].Mode.Offset.BodyBytes)
	}
}

func TestValidateByteBackedModuleASTDifferentialTestdata(t *testing.T) {
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
			got := ValidateByteBackedModule(b)
			if (want == nil) != (got == nil) {
				t.Fatalf("AST decode+ValidateModule=%v ValidateByteBackedModule=%v", want, got)
			}
			if want != nil && errorPhase(want) != errorPhase(got) {
				t.Fatalf("AST decode+ValidateModule=%v (%s) ValidateByteBackedModule=%v (%s)", want, errorPhase(want), got, errorPhase(got))
			}
		})
	}
}

func TestDecodeModuleByteBackedASTDifferentialEdges(t *testing.T) {
	nameModulePayload := []byte{0x00, 0x04, 0x03, 'm', 'o', 'd'}
	v128Body := []byte{0xfd, 0x0c}
	v128Body = append(v128Body, make([]byte, 16)...)
	v128Body = append(v128Body, 0x1a, 0x0b)

	cases := []struct {
		name string
		b    []byte
	}{
		{"valid custom and name sections", module(
			custom("build", 0x01, 0x02, 0x03),
			custom("name", nameModulePayload...),
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
		)},
		{"malformed custom name utf8", module(section(secCustom, 0x01, 0xff))},
		{"duplicate name custom section", module(custom("name", nameModulePayload...), custom("name", nameModulePayload...))},
		{"name subsection trailing junk", module(custom("name", 0x01, 0x02, 0x00, 0xff))},
		{"numeric const expression edges", module(section(secGlobal,
			0x04,
			0x7f, 0x00, 0x41, 0x00, 0x0b,
			0x7e, 0x00, 0x42, 0x00, 0x0b,
			0x7d, 0x00, 0x43, 0x00, 0x00, 0x00, 0x00, 0x0b,
			0x7c, 0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0b,
		))},
		{"data segment forms", module(
			section(secMemory, 0x01, 0x00, 0x01),
			section(secData,
				0x03,
				0x00, 0x41, 0x00, 0x0b, 0x01, 'a',
				0x01, 0x01, 'b',
				0x02, 0x00, 0x41, 0x00, 0x0b, 0x01, 'c',
			),
		)},
		{"element segment forms", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secTable, 0x01, 0x70, 0x00, 0x01),
			section(secElement,
				0x03,
				0x00, 0x41, 0x00, 0x0b, 0x01, 0x00,
				0x01, 0x00, 0x01, 0x00,
				0x06, 0x00, 0x41, 0x00, 0x0b, 0x70, 0x01, 0xd0, 0x70, 0x0b,
			),
			section(secCode, 0x01, 0x02, 0x00, 0x0b),
		)},
		{"tail-call proposal opcode", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secCode, 0x01, 0x04, 0x00, 0x12, 0x00, 0x0b),
		)},
		{"simd proposal opcode", module(
			section(secType, 0x01, 0x60, 0x00, 0x00),
			section(secFunction, 0x01, 0x00),
			section(secCode, append([]byte{0x01}, append(u32(uint32(1+len(v128Body))), append([]byte{0x00}, v128Body...)...)...)...),
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := decodeThenValidate(tc.b)
			got := byteBackedDecodeThenValidate(tc.b)
			if (want == nil) != (got == nil) {
				t.Fatalf("AST decode+ValidateModule=%v DecodeModuleByteBacked+ValidateDecodedByteBackedModule=%v", want, got)
			}
			if want != nil && errorPhase(want) != errorPhase(got) {
				t.Fatalf("AST decode+ValidateModule=%v (%s) DecodeModuleByteBacked+ValidateDecodedByteBackedModule=%v (%s)", want, errorPhase(want), got, errorPhase(got))
			}
		})
	}
}

func TestValidateByteBackedModuleASTDifferentialNegativeCases(t *testing.T) {
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
			got := ValidateByteBackedModule(tc.b)
			if (want == nil) != (got == nil) {
				t.Fatalf("AST decode+ValidateModule=%v ValidateByteBackedModule=%v", want, got)
			}
			if want != nil && errorPhase(want) != errorPhase(got) {
				t.Fatalf("AST decode+ValidateModule=%v (%s) ValidateByteBackedModule=%v (%s)", want, errorPhase(want), got, errorPhase(got))
			}
		})
	}
}

func decodeThenValidate(b []byte) error {
	m, err := decodeModuleASTForTest(b)
	if err != nil {
		return err
	}
	return ValidateModule(m)
}

func byteBackedDecodeThenValidate(b []byte) error {
	dm, err := DecodeModuleByteBacked(b)
	if err != nil {
		return err
	}
	return ValidateDecodedByteBackedModule(dm)
}

func errorPhase(err error) string {
	var de *DecodeError
	if errors.As(err, &de) {
		return "decode"
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return "validation"
	}
	if err == nil {
		return "nil"
	}
	return "other"
}
