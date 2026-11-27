package wasm

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func TestValidateModuleRejectsMultipleMemories(t *testing.T) {
	memoryImport := func() Import {
		return Import{Type: NewMemExternType(MemType{Limits: Limits{Min: 1}})}
	}

	for _, tc := range []struct {
		name string
		m    *Module
	}{
		{
			name: "two imported",
			m:    &Module{Imports: []Import{memoryImport(), memoryImport()}},
		},
		{
			name: "imported and local",
			m: &Module{
				Imports:  []Import{memoryImport()},
				Memories: []MemType{{Limits: Limits{Min: 1}}},
			},
		},
		{
			name: "two local",
			m:    &Module{Memories: []MemType{{Limits: Limits{Min: 1}}, {Limits: Limits{Min: 1}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectValidateErr(t, tc.m, ErrUnsupportedFeature)
		})
	}
}

func TestValidateModuleWithFeaturesAcceptsIndexedMemories(t *testing.T) {
	memories := []MemType{{Limits: Limits{Min: 1}}, {Limits: Limits{Min: 2}}}
	mem1 := MemIdx(1)
	for _, tc := range []struct {
		name string
		m    *Module
	}{
		{
			name: "memory.size",
			m:    modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrMemorySize, Index: 1}),
		},
		{
			name: "memory.grow",
			m: modWithFunc(nil, []ValType{I32},
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: InstrMemoryGrow, Index: 1},
			),
		},
		{
			name: "indexed load",
			m: modWithFunc(nil, []ValType{I32},
				Instruction{Kind: InstrI32Const},
				Instruction{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Mem: &mem1}}},
			),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.m.Memories = append([]MemType(nil), memories...)
			err := ValidateModuleWithFeatures(tc.m, ValidationFeatures{MultiMemory: true})
			if buildfeatures.MultiMemory == 0 {
				var unsupported *ValidationError
				if !errors.As(err, &unsupported) || unsupported.Code != ErrUnsupportedFeature {
					t.Fatalf("ValidateModuleWithFeatures: %v, want unsupported multiple memories", err)
				}
			} else if err != nil {
				t.Fatalf("ValidateModuleWithFeatures: %v", err)
			}
		})
	}

	if buildfeatures.MultiMemory == 0 {
		return
	}
	unknown := modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrMemorySize, Index: 2})
	unknown.Memories = memories
	var ve *ValidationError
	if err := ValidateModuleWithFeatures(unknown, ValidationFeatures{MultiMemory: true}); !errors.As(err, &ve) || ve.Code != ErrUnknownMemory {
		t.Fatalf("unknown memory validation error = %v, want ErrUnknownMemory", err)
	}
}

func TestValidateByteBackedModuleWithFeaturesDecodesMemoryIndex(t *testing.T) {
	data := module(
		section(1, 0x01, 0x60, 0x00, 0x01, 0x7f),
		section(3, 0x01, 0x00),
		section(5, 0x02, 0x00, 0x01, 0x00, 0x02),
		section(10, 0x01, 0x04, 0x00, 0x3f, 0x01, 0x0b),
	)
	decoded, err := DecodeModule(data)
	if buildfeatures.MultiMemory == 0 {
		for _, err := range []error{err, ValidateByteBackedModuleWithFeatures(data, ValidationFeatures{MultiMemory: true})} {
			var invalid *DecodeError
			if !errors.As(err, &invalid) || invalid.Code != ErrInvalidInstruction {
				t.Fatalf("indexed memory.size = %v, want invalid reserved byte", err)
			}
		}
		return
	}
	if err != nil {
		t.Fatalf("DecodeModule indexed memory.size: %v", err)
	}
	if err := ValidateModuleWithFeatures(decoded, ValidationFeatures{MultiMemory: true}); err != nil {
		t.Fatalf("ValidateModuleWithFeatures indexed memory.size: %v", err)
	}
	if err := ValidateByteBackedModuleWithFeatures(data, ValidationFeatures{MultiMemory: true}); err != nil {
		t.Fatalf("ValidateByteBackedModuleWithFeatures indexed memory.size: %v", err)
	}
	if err := ValidateModule(decoded); err == nil {
		t.Fatal("default validation unexpectedly admitted multiple memories")
	}
}

func TestValidateModuleAcceptsSingleMemoryShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *Module
	}{
		{
			name: "one imported",
			m: &Module{Imports: []Import{{Type: NewMemExternType(

				MemType{Limits: Limits{Min: 1}})}}},
		},
		{
			name: "one local",
			m:    &Module{Memories: []MemType{{Limits: Limits{Min: 1}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateModule(tc.m); err != nil {
				t.Fatalf("ValidateModule: %v", err)
			}
		})
	}
}

func TestMemoryInstructionBuildFeature(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    []byte
		indexed bool
	}{
		{"size memory zero", []byte{0x3f, 0}, false},
		{"grow memory zero", []byte{0x40, 0}, false},
		{"size overlong index", []byte{0x3f, 0x80, 0}, true},
		{"grow secondary index", []byte{0x40, 1}, true},
		{"implicit load index", []byte{0x28, 2, 0}, false},
		{"explicit load index", []byte{0x28, 0x42, 0, 0}, true},
		{"copy implicit indexes", []byte{0xfc, 0x0a, 0, 0}, false},
		{"copy secondary index", []byte{0xfc, 0x0a, 0, 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.code[0] == 0xfc && buildfeatures.BulkMemoryOperations == 0 {
				t.Skip("bulk memory is disabled")
			}
			op, immediates := tc.code[0], tc.code[1:]
			_, treeErr := decodeInstruction(newReader(tc.code), 0)
			var imm InstructionImmediate
			classifyErr := ClassifyInstructionImmediateIntoWithFeatures(NewReader(immediates), op, &imm, false, true)
			classifier := NewModuleInstructionClassifier(nil, true)
			skipErr := classifier.SkipImmediate(NewReader(immediates), op)
			for _, err := range []error{treeErr, classifyErr, skipErr} {
				if !tc.indexed || buildfeatures.MultiMemory != 0 {
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				var invalid *DecodeError
				if !errors.As(err, &invalid) || invalid.Code != ErrInvalidInstruction {
					t.Fatalf("indexed instruction error = %v, want invalid instruction", err)
				}
			}
		})
	}
}
