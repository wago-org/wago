package wasm

import (
	"sync"
	"testing"
)

func TestFlatStructuralKeyCachePreservesFormatAndInvalidation(t *testing.T) {
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{I32}, Results: []ValType{I64}}}}}}}
	for _, typ := range []ValType{I32, I64, F64} {
		m.Types[0].SubTypes[0].Comp.Params[0] = typ
		want := StructuralFuncTypeKey(&m.Types[0].SubTypes[0].Comp)
		if got, ok := m.StructuralTypeKeyChecked(0); !ok || got != want {
			t.Fatalf("in-place edit key = %#x,%v, want %#x", got, ok, want)
		}
		if err := ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if got, ok := m.StructuralTypeKeyChecked(0); !ok || got != want {
				t.Fatalf("type %v key = %#x,%v, want %#x", typ, got, ok, want)
			}
		}
	}
	// A copied module with a replacement type section must not reuse the
	// original module's cached flat key, even though its cache pointer is copied.
	copy := *m
	copy.Types = []RecType{{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{V128}}}}}}
	want := StructuralFuncTypeKey(&copy.Types[0].SubTypes[0].Comp)
	if got, ok := copy.StructuralTypeKeyChecked(0); !ok || got != want {
		t.Fatalf("replacement key = %#x,%v, want %#x", got, ok, want)
	}
}

func TestFlatAndIndexedStructuralKeyQueriesDoNotAlias(t *testing.T) {
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{I32}}}}}}}
	flat := StructuralFuncTypeKey(&m.Types[0].SubTypes[0].Comp)
	indexed, ok := m.structuralIndexedFuncTypeKey(0)
	if !ok || indexed == flat {
		t.Fatal("fixture must distinguish indexed and compact-flat formats")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 64; i++ {
				if got, ok := m.StructuralTypeKeyChecked(0); !ok || got != flat {
					t.Errorf("flat key changed: %#x,%v", got, ok)
					return
				}
				if got, ok := m.structuralIndexedFuncTypeKey(0); !ok || got != indexed {
					t.Errorf("indexed key changed: %#x,%v", got, ok)
					return
				}
			}
		}()
	}
	wg.Wait()
}
