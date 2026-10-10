//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

func rv64Core3RefIsNullExecutionSource(t testing.TB) []byte {
	t.Helper()
	source, err := hex.DecodeString("0061736d01000000012107600000600170017f60016f017f60016300017f6000017f60016f0060017f017f030b0a00010203040500060606040b037000026f000263000002075d080766756e6372656600010965787465726e7265660002087265662d6e756c6c000404696e69740005066465696e697400060c66756e637265662d656c656d00070e65787465726e7265662d656c656d0008087265662d656c656d00090912020041010b0100060241010b640001d2000b0a560a02000b05002000d10b05002000d10b05002000d10b0600d00010030b08004101200026010b14004101d07026004101d06f26014101d00026020b08002000250010010b08002000250110020b08002000250210030b")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func rv64Core3RefIsNullExecutionRetainedFixture(t testing.TB) *Compiled {
	t.Helper()
	m, err := wasm.DecodeModule(rv64Core3RefIsNullExecutionSource(t))
	if err != nil {
		t.Fatal(err)
	}
	image, err := railshot.CompileModuleWith(m, railshot.CompileOptions{
		NativeReferenceFunctions: true,
		NativeReferenceCalls:     true,
		ForeignTableCalls:        true,
		InvocationContext:        true,
		Interruptible:            true,
		DeferCodeMapping:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	nullFunc := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}
	nullExtern := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
	nullIndexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true}}}
	nonNullIndexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true}}}
	types := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeFunction},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullFunc}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullExtern}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullIndexed}, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32}},
		{RecGroup: 5, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullExtern}},
		{RecGroup: 6, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32}, Results: []ValueTypeDescriptor{i32}},
	}
	funcs := []FuncSig{
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValFuncRef}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{Params: []ValType{ValFuncRef}, Results: []ValType{ValI32}, TypeIndex: 3, HasTypeIndex: true},
		{Results: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
		{Params: []ValType{ValExternRef}, TypeIndex: 5, HasTypeIndex: true},
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 6, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 6, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 6, HasTypeIndex: true},
	}
	gcTypes := make([]corergc.TypeDesc, 7)
	for i := range gcTypes {
		gcTypes[i] = corergc.TypeDesc{ID: corergc.TypeID(i), Kind: corergc.KindFunc, Final: true}
	}
	return &Compiled{
		code: image.Code, Entry: image.Entry, InternalEntry: image.InternalEntry,
		Funcs: funcs, Types: types, ValueTypes: []ValueTypeDescriptor{nullFunc, nullExtern, nullIndexed, nonNullIndexed},
		Exports: map[string]int{
			"funcref": 1, "externref": 2, "ref-null": 4, "init": 5,
			"deinit": 6, "funcref-elem": 7, "externref-elem": 8, "ref-elem": 9,
		},
		HasTable: true, TableType: ValFuncRef, TableValueTypeIndex: 0, TableHasValueType: true, TableSize: 2, TableMax: 2,
		extraTables: []tableDef{
			{Size: 2, Max: 2, Type: ValExternRef, ValueTypeIndex: 1, HasValueType: true},
			{Size: 2, Max: 2, Type: ValFuncRef, ValueTypeIndex: 2, HasValueType: true},
		},
		FuncTypeID:        []uint64{0xd60da539b2828050, 0x457208c8efc22dab, 0x159570dc55660444, 0x43a91b81cda1964c, 0x87259896b4921f6c, 0x009df426b447def3, 0xd60da539b2828050, 0x57cc9e275fbbb6a5, 0x57cc9e275fbbb6a5, 0x57cc9e275fbbb6a5},
		NeedsFuncRefDescs: true,
		Elems: []ElemInit{
			{TableIndex: 0, RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 1}, Values: []RefInit{{FuncIndex: 0}}},
			{TableIndex: 2, RefType: ValFuncRef, ValueTypeIndex: 3, HasValueType: true, Mode: ElemModeActive, Offset: OffsetInit{Base: 1}, Values: []RefInit{{FuncIndex: 0}}},
		},
		hasTableExportMetadata: true,
		memoryDir:              &compiledMemoryDirectory{exactExports: true},
		GCTypeDescs:            gcTypes,
		requiredFeatures:       CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences,
		nativeContextTrapABI:   true, nativeReferenceFunctionABI: image.NativeReferenceFunctions,
		nativeReferenceControlABI: image.NativeReferenceControl, nativeReferenceCallABI: image.NativeReferenceCalls,
		nativeTableContextABI: true, nativeTableReferenceABI: true, registerABIDisabled: true,
		needsFuncRefContextHeader: true,
	}
}

func TestRV64Core3RefIsNullExecutionRetainedClassifier(t *testing.T) {
	base := rv64Core3RefIsNullExecutionRetainedFixture(t)
	if !rv64Core3RefIsNullExecution(base) {
		t.Fatal("exact retained fixture rejected")
	}
	if err := railshotValidateCompiled(base); err != nil {
		t.Fatal("exact retained fixture failed admission:", err)
	}
	if err := base.validate(); err != nil {
		t.Fatal("exact retained fixture failed common validation:", err)
	}
	if err := railshotValidateReferenceFunctionInstance(base, InstantiateOptions{railshotTailPolicy: railshotInstantiateTailPolicy{referenceFunctions: true}}); err != nil {
		t.Fatal("qualified runtime publication was rejected:", err)
	}
	if err := railshotValidateReferenceFunctionInstance(base, InstantiateOptions{}); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("runtime publication escaped private permission:", err)
	}
	mutations := map[string]func(*Compiled){
		"code":           func(c *Compiled) { c.code = bytes.Clone(c.code); c.code[0] ^= 1 },
		"entry":          func(c *Compiled) { c.Entry[9]-- },
		"internal-entry": func(c *Compiled) { c.InternalEntry[9]-- },
		"type-id":        func(c *Compiled) { c.FuncTypeID[5] ^= 1 },
		"binding":        func(c *Compiled) { c.Funcs[9].TypeIndex = 5 },
		"signature":      func(c *Compiled) { c.Funcs[2].Params[0] = ValFuncRef },
		"type":           func(c *Compiled) { c.Types[2].Params[0].Ref.Heap.Abstract = AbstractHeapFunc },
		"value-type":     func(c *Compiled) { c.ValueTypes[3].Ref.Nullable = true },
		"export":         func(c *Compiled) { c.Exports["ref-elem"] = 8 },
		"table-type":     func(c *Compiled) { c.extraTables[0].Type = ValFuncRef },
		"table-limit":    func(c *Compiled) { c.extraTables[1].Max = 3 },
		"element-table":  func(c *Compiled) { c.Elems[1].TableIndex = 1 },
		"element-type":   func(c *Compiled) { c.Elems[1].ValueTypeIndex = 2 },
		"element-value":  func(c *Compiled) { c.Elems[0].Values[0].FuncIndex = 1 },
		"gc":             func(c *Compiled) { c.GCTypeDescs[6].Final = false },
		"memory":         func(c *Compiled) { c.memoryDir.defs = []memoryDef{{}} },
		"memory-dir":     func(c *Compiled) { c.memoryDir = nil },
		"compact-data":   func(c *Compiled) { c.compactData = &compactActiveData{} },
		"memory-import":  func(c *Compiled) { c.memoryImport = "env.memory" },
		"table-import":   func(c *Compiled) { c.tableImportMin = 1 },
		"host-slots":     func(c *Compiled) { c.syncHostSlots = 1 },
		"passive":        func(c *Compiled) { c.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
		"start":          func(c *Compiled) { c.HasStart = true },
		"names":          func(c *Compiled) { c.Names = &wasm.NameSec{} },
		"feature":        func(c *Compiled) { c.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"function-abi":   func(c *Compiled) { c.nativeReferenceFunctionABI = false },
		"control-abi":    func(c *Compiled) { c.nativeReferenceControlABI = false },
		"descriptor":     func(c *Compiled) { c.NeedsFuncRefDescs = false },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := rv64Core3RefIsNullExecutionRetainedFixture(t)
			mutate(bad)
			if rv64Core3RefIsNullExecution(bad) || railshotValidateCompiled(bad) == nil {
				t.Fatal("hostile retained metadata admitted")
			}
		})
	}
}

func TestRV64Core3RefIsNullExecutionArtifactRoundTrip(t *testing.T) {
	base := rv64Core3RefIsNullExecutionRetainedFixture(t)
	blob, err := base.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	view := loaded.executionView()
	if !rv64Core3RefIsNullExecution(view) {
		t.Fatal("trusted artifact lost exact retained witness")
	}
	if err := railshotValidateReferenceFunctionInstance(loaded, InstantiateOptions{railshotTailPolicy: railshotInstantiateTailPolicy{referenceFunctions: true}}); err != nil {
		t.Fatal("qualified loaded artifact was rejected:", err)
	}
	if err := railshotValidateReferenceFunctionInstance(loaded, InstantiateOptions{}); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("loaded artifact escaped private runtime permission:", err)
	}
}

func TestRV64Core3RefIsNullExecutionSourceStillClosed(t *testing.T) {
	if c, err := rv64NativeTailConfig().Compile(rv64Core3RefIsNullExecutionSource(t)); c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-only checkpoint escaped public source gate", err)
	}
}
