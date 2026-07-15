package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func indexedFuncRef(typeIndex uint32, nullable bool) wasm.ValType {
	return wasm.RefVal(wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
}

func stagedTypedStorageCompile(t *testing.T, module []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("staged typed-reference compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func encodedNullableIndexedRef(typeIndex uint32) []byte {
	return append([]byte{0x63}, wasmtest.ULEB(typeIndex)...)
}

func typedTableModule(typeDefs [][]byte, typeIndex uint32, importTable, exportTable bool) []byte {
	refType := encodedNullableIndexedRef(typeIndex)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(typeDefs...))}
	if importTable {
		entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
		entry = append(entry, 0x01)
		entry = append(entry, refType...)
		entry = append(entry, 0x01, 0x01, 0x01) // min=1, max=1
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	} else {
		entry := append([]byte(nil), refType...)
		entry = append(entry, 0x01, 0x01, 0x01) // min=1, max=1
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec(entry)))
	}
	if exportTable {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))))
	}
	return wasmtest.Module(sections...)
}

func typedGlobalModule(typeDefs [][]byte, typeIndex uint32, importGlobal, exportGlobal bool) []byte {
	refType := encodedNullableIndexedRef(typeIndex)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(typeDefs...))}
	if importGlobal {
		entry := append(wasmtest.Name("env"), wasmtest.Name("global")...)
		entry = append(entry, 0x03)
		entry = append(entry, refType...)
		entry = append(entry, 0x00) // immutable
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	} else {
		init := append([]byte{0xd0}, wasmtest.ULEB(typeIndex)...)
		init = append(init, 0x0b)
		entry := append([]byte(nil), refType...)
		entry = append(entry, 0x00) // immutable
		entry = append(entry, init...)
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(entry)))
	}
	if exportGlobal {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("global", 3, 0))))
	}
	return wasmtest.Module(sections...)
}

func TestStagedTypedStorageExactImports(t *testing.T) {
	equivalent := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64})
	dummy := wasmtest.FuncType(nil, nil)
	mismatch := wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64})

	t.Run("public gate remains closed", func(t *testing.T) {
		_, err := Compile(typedTableModule([][]byte{equivalent}, 0, false, true))
		if err == nil || !strings.Contains(err.Error(), "typed-function-references disabled") {
			t.Fatalf("public typed table compile error = %v", err)
		}
	})

	t.Run("table equivalent indexes link", func(t *testing.T) {
		producer := stagedTypedStorageCompile(t, typedTableModule([][]byte{equivalent}, 0, false, true))
		consumer := stagedTypedStorageCompile(t, typedTableModule([][]byte{dummy, equivalent}, 1, true, false))
		store := newReferenceStore(false)
		producerInstance, err := instantiateCore(producer, InstantiateOptions{store: store})
		if err != nil {
			t.Fatalf("instantiate typed table producer: %v", err)
		}
		defer producerInstance.Close()
		table, err := producerInstance.ExportedTable("table")
		if err != nil {
			t.Fatalf("export typed table: %v", err)
		}
		consumerInstance, err := instantiateCore(consumer, InstantiateOptions{Imports: Imports{"env.table": table}, store: store})
		if err != nil {
			t.Fatalf("instantiate equivalent typed table import: %v", err)
		}
		defer consumerInstance.Close()

		bad := stagedTypedStorageCompile(t, typedTableModule([][]byte{dummy, mismatch}, 1, true, false))
		if _, err := instantiateCore(bad, InstantiateOptions{Imports: Imports{"env.table": table}, store: store}); err == nil || !strings.Contains(err.Error(), "exact element type") {
			t.Fatalf("mismatched typed table import error = %v", err)
		}
	})

	t.Run("immutable global covariance links", func(t *testing.T) {
		producer := stagedTypedStorageCompile(t, typedGlobalModule([][]byte{equivalent}, 0, false, true))
		consumer := stagedTypedStorageCompile(t, typedGlobalModule([][]byte{dummy, equivalent}, 1, true, false))
		store := newReferenceStore(false)
		producerInstance, err := instantiateCore(producer, InstantiateOptions{store: store})
		if err != nil {
			t.Fatalf("instantiate typed global producer: %v", err)
		}
		defer producerInstance.Close()
		global, err := producerInstance.ExportedGlobalObject("global")
		if err != nil {
			t.Fatalf("export typed global: %v", err)
		}
		consumerInstance, err := instantiateCore(consumer, InstantiateOptions{Imports: Imports{"env.global": global}, store: store})
		if err != nil {
			t.Fatalf("instantiate equivalent typed global import: %v", err)
		}
		defer consumerInstance.Close()

		bad := stagedTypedStorageCompile(t, typedGlobalModule([][]byte{dummy, mismatch}, 1, true, false))
		if _, err := instantiateCore(bad, InstantiateOptions{Imports: Imports{"env.global": global}, store: store}); err == nil || !strings.Contains(err.Error(), "exact type is incompatible") {
			t.Fatalf("mismatched typed global import error = %v", err)
		}
	})
}

func TestTypedStorageMetadataRejectsIdentityCollapse(t *testing.T) {
	indexed0 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	indexed1 := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 1}}}
	c := &Compiled{
		Code:                []byte{0xc3},
		Entry:               []int{0},
		InternalEntry:       []int{0},
		Funcs:               []FuncSig{{TypeIndex: 0, HasTypeIndex: true}},
		Types:               []DefinedTypeDescriptor{{RecGroup: 0, Final: true, Kind: CompositeTypeFunction}, {RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}}}},
		ValueTypes:          []ValueTypeDescriptor{indexed0, indexed1},
		Exports:             map[string]int{},
		GlobalExports:       map[string]int{},
		FuncTypeID:          []uint32{0},
		HasTable:            true,
		TableType:           ValFuncRef,
		TableSize:           1,
		TableMax:            1,
		TableHasMax:         true,
		TableHasValueType:   true,
		TableValueTypeIndex: 0,
		Elems:               []ElemInit{{TableIndex: 0, RefType: ValFuncRef, HasValueType: true, ValueTypeIndex: 1, Mode: ElemModeActive, Values: []RefInit{{FuncIndex: 0}}}},
		NeedsFuncRefDescs:   true,
	}
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "structural type does not match table") {
		t.Fatalf("collapsed active element metadata error = %v", err)
	}

}
