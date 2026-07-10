package wasm

import "testing"

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
