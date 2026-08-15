package wasm

import (
	"errors"
	"testing"
)

func TestModuleMemargWidthsScansImportedAndLocalMemories(t *testing.T) {
	memory32 := NewMemExternType(MemType{Limits: Limits{Min: 1}})
	memory64 := NewMemExternType(MemType{Limits: Limits{Min: 1, Addr64: true}})
	uniform := &Module{Imports: []Import{{Type: memory64}, {Type: memory64}}}
	if got := moduleMemargWidths(uniform); len(got.indexed64) != 0 || !got.fixed64 {
		t.Fatalf("uniform imported memory64 widths = %+v", got)
	}
	mixed := &Module{Imports: []Import{{Type: memory32}}, Memories: []MemType{{Limits: Limits{Min: 1, Addr64: true}}}}
	if got := moduleMemargWidths(mixed); len(got.indexed64) != 1 || got.offset64(0) || !got.offset64(1) {
		t.Fatalf("mixed imported/local widths = %+v, want [memory32,memory64]", got)
	}
}

func BenchmarkModuleMemargWidthsManyImports(b *testing.B) {
	const memoryCount = 10000
	m := &Module{Imports: make([]Import, memoryCount)}
	memory64 := NewMemExternType(MemType{Limits: Limits{Min: 1, Addr64: true}})
	for i := range m.Imports {
		m.Imports[i].Type = memory64
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := moduleMemargWidths(m); len(got.indexed64) != 0 || !got.fixed64 {
			b.Fatal(got)
		}
	}
}

func BenchmarkModuleInstructionClassifierManyImportsAndOps(b *testing.B) {
	const count = 10000
	m := &Module{Imports: make([]Import, count)}
	memory32 := NewMemExternType(MemType{Limits: Limits{Min: 1}})
	for i := range m.Imports {
		m.Imports[i].Type = memory32
	}
	body := make([]byte, count)
	for i := range body {
		body[i] = 0x01 // nop; the old per-op module helper still rescanned imports
	}
	b.ReportAllocs()
	for b.Loop() {
		classifier := NewModuleInstructionClassifier(m, true)
		r := NewReader(body)
		var imm InstructionImmediate
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				b.Fatal(err)
			}
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkModuleInstructionClassifierMixedLateMemory(b *testing.B) {
	const count = 10000
	m := &Module{Imports: make([]Import, count)}
	memory32 := NewMemExternType(MemType{Limits: Limits{Min: 1}})
	memory64 := NewMemExternType(MemType{Limits: Limits{Min: 1, Addr64: true}})
	for i := range m.Imports {
		if i&1 == 0 {
			m.Imports[i].Type = memory32
		} else {
			m.Imports[i].Type = memory64
		}
	}
	body := make([]byte, 0, 5*count)
	for range count {
		body = append(body, 0x28, 0x40, 0x8f, 0x4e, 0x00) // i32.load memory 9999, offset 0
	}
	b.ReportAllocs()
	for b.Loop() {
		classifier := NewModuleInstructionClassifier(m, true)
		r := NewReader(body)
		var imm InstructionImmediate
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				b.Fatal(err)
			}
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestDecodeMemoryOffsetWidthFollowsMemoryType(t *testing.T) {
	paths := []struct {
		name   string
		decode func([]byte) (*Module, error)
	}{
		{name: "AST", decode: decodeModuleASTForTest},
		{name: "byte-backed", decode: DecodeModule},
	}

	validU32 := []struct {
		name   string
		offset []byte
		want   uint64
	}{
		{name: "literal", offset: []byte{0x02}, want: 2},
		{name: "non-minimal-five-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x00}, want: 2},
		{name: "max-u32", offset: []byte{0xff, 0xff, 0xff, 0xff, 0x0f}, want: 1<<32 - 1},
	}
	for _, tc := range validU32 {
		t.Run("memory32/accept-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModule(false, false, tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					m, err := path.decode(data)
					if err != nil {
						t.Fatalf("decode rejected valid u32 offset: %v", err)
					}
					if path.name == "AST" {
						got := m.Code[0].Body.Instrs[1].MemArg().Offset
						if got != tc.want {
							t.Fatalf("offset=%d, want %d", got, tc.want)
						}
					}
				})
			}
		})
	}

	invalidU32 := []struct {
		name   string
		offset []byte
	}{
		{name: "six-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}},
		{name: "fifth-byte-unused-bit-4", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}},
		{name: "fifth-byte-unused-bit-6", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x40}},
	}
	for _, tc := range invalidU32 {
		t.Run("memory32/reject-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModule(false, false, tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					err := func() error { _, err := path.decode(data); return err }()
					assertMalformedMemoryOffset(t, err, true)
				})
			}
		})
	}

	validU64 := []struct {
		name   string
		offset []byte
		want   uint64
	}{
		{name: "six-byte-non-minimal", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}, want: 2},
		{name: "above-u32", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}, want: 1<<32 + 2},
		{name: "max-u64", offset: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, want: ^uint64(0)},
	}
	for _, imported := range []bool{false, true} {
		owner := "local"
		if imported {
			owner = "imported"
		}
		for _, tc := range validU64 {
			t.Run("memory64/"+owner+"/accept-"+tc.name, func(t *testing.T) {
				data := memoryOffsetModule(true, imported, tc.offset)
				for _, path := range paths {
					t.Run(path.name, func(t *testing.T) {
						m, err := path.decode(data)
						if err != nil {
							t.Fatalf("decode rejected valid u64 offset: %v", err)
						}
						if path.name == "AST" {
							got := m.Code[0].Body.Instrs[1].MemArg().Offset
							if got != tc.want {
								t.Fatalf("offset=%d, want %d", got, tc.want)
							}
						}
					})
				}
			})
		}
	}

	for _, tc := range invalidU32 {
		t.Run("no-memory/reject-"+tc.name, func(t *testing.T) {
			data := memoryOffsetModuleWithoutMemory(tc.offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					err := func() error { _, err := path.decode(data); return err }()
					assertMalformedMemoryOffset(t, err, true)
				})
			}
		})
	}
}

func TestDecodeMixedMemoryOffsetWidthFollowsSelectedMemory(t *testing.T) {
	paths := []struct {
		name   string
		decode func([]byte) (*Module, error)
	}{
		{name: "AST", decode: decodeModuleASTForTest},
		{name: "byte-backed", decode: DecodeModule},
	}
	offset := []byte{0x82, 0x80, 0x80, 0x80, 0x10} // 2^32 + 2
	cases := []struct {
		name       string
		import64   bool
		import32   bool
		selected   uint32
		selected64 bool
	}{
		{name: "two-local-select-memory64", selected: 1, selected64: true},
		{name: "import-memory32-local-memory64", import32: true, selected: 1, selected64: true},
		{name: "import-memory64-local-memory32", import64: true, selected: 0, selected64: true},
		{name: "two-local-select-memory32", selected: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mixedMemoryOffsetModule(tc.import64, tc.import32, tc.selected, tc.selected64, offset)
			for _, path := range paths {
				t.Run(path.name, func(t *testing.T) {
					m, err := path.decode(data)
					if !tc.selected64 {
						assertMalformedMemoryOffset(t, err, true)
						return
					}
					if err != nil {
						t.Fatalf("decode selected memory64 offset: %v", err)
					}
					if err := ValidateModuleWithFeatures(m, ValidationFeatures{MultiMemory: true}); err != nil {
						t.Fatalf("validate selected memory64 offset: %v", err)
					}
					if path.name == "AST" {
						ma := m.Code[0].Body.Instrs[1].MemArg()
						if ma.Mem == nil || uint32(*ma.Mem) != tc.selected || ma.Offset != 1<<32+2 {
							t.Fatalf("memarg = %#v, want memory %d offset %d", ma, tc.selected, uint64(1<<32+2))
						}
					}
				})
			}
		})
	}
}

func TestClassifyMixedMemoryOffsetWidthAcrossPrefixes(t *testing.T) {
	m := &Module{Memories: []MemType{{Limits: Limits{Min: 1}}, {Limits: Limits{Min: 1, Addr64: true}}}}
	offset := []byte{0x82, 0x80, 0x80, 0x80, 0x10}
	cases := []struct {
		name string
		op   byte
		imm  []byte
	}{
		{name: "scalar", op: 0x28, imm: append([]byte{0x42, 0x01}, offset...)},
		{name: "SIMD", op: 0xfd, imm: append([]byte{0x00, 0x42, 0x01}, offset...)},
		{name: "atomic", op: 0xfe, imm: append([]byte{0x10, 0x42, 0x01}, offset...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(tc.imm)
			var imm InstructionImmediate
			if err := ClassifyInstructionImmediateIntoWithModuleFeatures(r, tc.op, &imm, m, true); err != nil {
				t.Fatal(err)
			}
			if r.HasNext() || !imm.HasMemIndex || imm.MemIndex != 1 || imm.MemOffset != 1<<32+2 || !imm.TouchesMemory {
				t.Fatalf("classification = %#v, remaining=%v", imm, r.HasNext())
			}
		})
	}
}

func TestValidateByteBackedMemoryOffsetWidth(t *testing.T) {
	valid := memoryOffsetModule(false, false, []byte{0x00})
	m, err := DecodeModule(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		offset []byte
	}{
		{name: "six-byte", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x80, 0x00}},
		{name: "fifth-byte-unused-bits", offset: []byte{0x82, 0x80, 0x80, 0x80, 0x10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.Code[0].BodyBytes = memoryOffsetBody(false, tc.offset)[1:]
			err := ValidateModule(m)
			assertMalformedMemoryOffset(t, err, false)
		})
	}
}

func assertMalformedMemoryOffset(t *testing.T, err error, wantSection bool) {
	t.Helper()
	var de *DecodeError
	if !errors.As(err, &de) || de.Code != ErrMalformedLEB {
		t.Fatalf("error=%#v / %v, want ErrMalformedLEB", de, err)
	}
	if wantSection && (de.SectionID != secCode || de.SectionStart <= 0 || de.SectionEnd <= de.SectionStart) {
		t.Fatalf("decode section diagnostics=%#v, want code-section span", de)
	}
}

func memoryOffsetModule(addr64, imported bool, offset []byte) []byte {
	sections := [][]byte{
		section(secType, 0x01, 0x60, 0x00, 0x00),
	}
	memType := []byte{0x00, 0x01}
	if addr64 {
		memType = []byte{0x04, 0x01}
	}
	if imported {
		entry := []byte{0x01, 'm', 0x01, 'n', byte(ExternMem)}
		entry = append(entry, memType...)
		sections = append(sections, section(secImport, append([]byte{0x01}, entry...)...))
	}
	sections = append(sections, section(secFunction, 0x01, 0x00))
	if !imported {
		sections = append(sections, section(secMemory, append([]byte{0x01}, memType...)...))
	}
	body := memoryOffsetBody(addr64, offset)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	sections = append(sections, section(secCode, code...))
	return module(sections...)
}

func mixedMemoryOffsetModule(import64, import32 bool, selected uint32, selected64 bool, offset []byte) []byte {
	sections := [][]byte{section(secType, 0x01, 0x60, 0x00, 0x00)}
	memory32 := []byte{0x00, 0x01}
	memory64 := []byte{0x04, 0x01}
	if import64 || import32 {
		memType := memory32
		if import64 {
			memType = memory64
		}
		entry := []byte{0x01, 'm', 0x01, 'n', byte(ExternMem)}
		entry = append(entry, memType...)
		sections = append(sections, section(secImport, append([]byte{0x01}, entry...)...))
	}
	sections = append(sections, section(secFunction, 0x01, 0x00))
	if import64 {
		sections = append(sections, section(secMemory, append([]byte{0x01}, memory32...)...))
	} else if import32 {
		sections = append(sections, section(secMemory, append([]byte{0x01}, memory64...)...))
	} else {
		payload := []byte{0x02}
		payload = append(payload, memory32...)
		payload = append(payload, memory64...)
		sections = append(sections, section(secMemory, payload...))
	}
	constOp := byte(0x41)
	if selected64 {
		constOp = 0x42
	}
	body := []byte{0x00, constOp, 0x00, 0x28, 0x42}
	body = append(body, u32(selected)...)
	body = append(body, offset...)
	body = append(body, 0x1a, 0x0b)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	sections = append(sections, section(secCode, code...))
	return module(sections...)
}

func memoryOffsetModuleWithoutMemory(offset []byte) []byte {
	body := memoryOffsetBody(false, offset)
	code := append([]byte{0x01}, u32(uint32(len(body)))...)
	code = append(code, body...)
	return module(
		section(secType, 0x01, 0x60, 0x00, 0x00),
		section(secFunction, 0x01, 0x00),
		section(secCode, code...),
	)
}

func memoryOffsetBody(addr64 bool, offset []byte) []byte {
	constOp := byte(0x41)
	if addr64 {
		constOp = 0x42
	}
	body := []byte{0x00, constOp, 0x00, 0x28, 0x02}
	body = append(body, offset...)
	return append(body, 0x1a, 0x0b)
}
