package wasm

import (
	"errors"
	"testing"
)

func TestValidateModuleRejectsMultipleMemories(t *testing.T) {
	memoryImport := func() Import {
		return Import{Type: ExternType{Kind: ExternMem, Mem: MemType{Limits: Limits{Min: 1}}}}
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
			if err := ValidateModuleWithFeatures(tc.m, ValidationFeatures{MultiMemory: true}); err != nil {
				t.Fatalf("ValidateModuleWithFeatures: %v", err)
			}
		})
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
			m: &Module{Imports: []Import{{Type: ExternType{
				Kind: ExternMem,
				Mem:  MemType{Limits: Limits{Min: 1}},
			}}}},
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
