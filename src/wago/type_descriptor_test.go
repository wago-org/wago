package wago

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTypeDescriptorFunctionValuesAreMutationAndAppendIsolated(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{
		{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}},
		{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F32}, Results: []wasm.ValType{wasm.F64}}},
	}}}}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("type descriptors: %v", err)
	}
	if cap(types[0].Params) != len(types[0].Params) || cap(types[0].Results) != len(types[0].Results) {
		t.Fatalf("first function capacities = params %d/%d results %d/%d", len(types[0].Params), cap(types[0].Params), len(types[0].Results), cap(types[0].Results))
	}
	types[0].Params[0].Kind = ValueTypeV128
	if got := types[0].Results[0].Kind; got != ValueTypeI64 {
		t.Fatalf("param mutation changed result to %v", got)
	}
	if got := types[1].Params[0].Kind; got != ValueTypeF32 {
		t.Fatalf("param mutation changed next function to %v", got)
	}
	types[0].Params = append(types[0].Params, ValueTypeDescriptor{Kind: ValueTypeReference})
	if got := types[0].Results[0].Kind; got != ValueTypeI64 {
		t.Fatalf("param append changed result to %v", got)
	}
	if got := types[1].Params[0].Kind; got != ValueTypeF32 {
		t.Fatalf("param append changed next function to %v", got)
	}
}

func TestTypeDescriptorConverterLargeGroupFallback(t *testing.T) {
	const groups = wasmTypeDescriptorInlineGroups + 1
	m := &wasm.Module{Types: make([]wasm.RecType, groups)}
	for i := range m.Types {
		m.Types[i].SubTypes = []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{
			wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(i)}), false)),
		}}}}
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("type descriptors: %v", err)
	}
	if len(types) != groups {
		t.Fatalf("types = %d, want %d", len(types), groups)
	}
	for i := range types {
		if got := types[i].Results[0].Ref.Heap.TypeIndex; got != uint32(i) {
			t.Fatalf("type %d result index = %d", i, got)
		}
	}
}

func TestTypeDescriptorCorpusAllocations(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	for _, rel := range []string{"synthetic/tiny.wasm", "synthetic/dispatch.wasm", "assemblyscript/blake-as.wasm"} {
		t.Run(rel, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "..", "corpus", "workloads", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(src)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			var sink []DefinedTypeDescriptor
			allocs := testing.AllocsPerRun(100, func() {
				sink, err = typeDescriptorsFromWasm(m)
				if err != nil {
					panic(err)
				}
			})
			if len(sink) == 0 {
				t.Fatal("no type descriptors")
			}
			if allocs > 2 {
				t.Fatalf("type descriptor allocations = %.0f, budget = 2 (descriptor and coalesced value backing)", allocs)
			}
			separateValueSlices := 0
			for gi := range m.Types {
				for si := range m.Types[gi].SubTypes {
					comp := &m.Types[gi].SubTypes[si].Comp
					if comp.Kind == wasm.CompFunc {
						if len(comp.Params) != 0 {
							separateValueSlices++
						}
						if len(comp.Results) != 0 {
							separateValueSlices++
						}
					}
				}
			}
			// Before coalescing, conversion allocated the descriptor result, one
			// group-offset slice, and one backing array for every non-empty
			// Params or Results slice. The two-allocation ceiling therefore keeps
			// at least separateValueSlices allocations removed.
			if separateValueSlices < 2 {
				t.Fatalf("allocation reduction = %d, want at least 2", separateValueSlices)
			}
			t.Logf("allocation reduction >= %d", separateValueSlices)
		})
	}
}

func TestResolveTypeFuncCorpusAllocationReduction(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	for _, rel := range []string{"synthetic/tiny.wasm", "synthetic/dispatch.wasm", "assemblyscript/blake-as.wasm"} {
		t.Run(rel, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "..", "corpus", "workloads", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(src)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			var typeIndexes []uint32
			var index uint32
			for gi := range m.Types {
				for si := range m.Types[gi].SubTypes {
					if m.Types[gi].SubTypes[si].Comp.Kind == wasm.CompFunc {
						typeIndexes = append(typeIndexes, index)
					}
					index++
				}
			}
			publicAllocs := testing.AllocsPerRun(100, func() {
				for _, typeIndex := range typeIndexes {
					if _, ok := m.ResolvedTypeFunc(typeIndex); !ok {
						panic("public type resolution failed")
					}
				}
			})
			internalAllocs := testing.AllocsPerRun(100, func() {
				var dst wasm.CompType
				for _, typeIndex := range typeIndexes {
					if !m.ResolveTypeFunc(typeIndex, &dst) {
						panic("internal type resolution failed")
					}
				}
			})
			if reduction := publicAllocs - internalAllocs; reduction < 2 {
				t.Fatalf("allocation reduction = %.0f (%.0f -> %.0f), want at least 2", reduction, publicAllocs, internalAllocs)
			} else {
				t.Logf("exact allocation reduction = %.0f (%.0f -> %.0f)", reduction, publicAllocs, internalAllocs)
			}
		})
	}
}

func TestTypeDescriptorAggregateBuildFeature(t *testing.T) {
	field := wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Var)
	for _, tt := range []struct {
		name string
		comp wasm.CompType
		kind CompositeTypeKind
	}{
		{"struct", wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{field}}, CompositeTypeStruct},
		{"array", wasm.CompType{Kind: wasm.CompArray, Array: field}, CompositeTypeArray},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: tt.comp}}}}}
			types, err := typeDescriptorsFromWasm(m)
			if !wasmGCBuilt {
				if err == nil || !strings.Contains(err.Error(), "gc is disabled") || types != nil {
					t.Fatalf("aggregate conversion = %v, %v, want disabled-GC error", types, err)
				}
				return
			}
			if err != nil || len(types) != 1 || types[0].Kind != tt.kind {
				t.Fatalf("aggregate conversion = %v, %v, want %v", types, err, tt.kind)
			}
			got := types[0].Array
			if tt.kind == CompositeTypeStruct {
				if len(types[0].Fields) != 1 {
					t.Fatalf("struct field count = %d, want 1", len(types[0].Fields))
				}
				got = types[0].Fields[0]
			}
			if !got.Mutable || got.Storage.Packed || got.Storage.Value.Kind != ValueTypeI64 {
				t.Fatalf("aggregate field = %+v, want mutable i64", got)
			}
		})
	}
}

func TestCompiledTypeDescriptorAggregateBuildFeature(t *testing.T) {
	field := FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: ValueTypeDescriptor{Kind: ValueTypeI64}}, Mutable: true}
	for _, d := range []DefinedTypeDescriptor{
		{Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{field}},
		{Final: true, Kind: CompositeTypeArray, Array: field},
	} {
		var writer compiledWriter
		err := writer.typeDescriptors([]DefinedTypeDescriptor{d})
		if !wasmGCBuilt {
			var unsupported *UnsupportedFeatureError
			if !errors.As(err, &unsupported) || unsupported.Requested != CoreFeatureGC || len(writer.buf) != 0 {
				t.Fatalf("serialize aggregate kind %d = %x, %v, want GC feature error", d.Kind, writer.buf, err)
			}
			continue
		}
		want := []byte{1, 0, 0, 0, 0, 1, 0, 0, 0, byte(d.Kind)}
		if d.Kind == CompositeTypeStruct {
			want = append(want, 1)
		}
		want = append(want, 0, byte(ValueTypeI64), 1)
		if err != nil || !bytes.Equal(writer.buf, want) {
			t.Fatalf("serialize aggregate kind %d = %x, %v, want %x", d.Kind, writer.buf, err, want)
		}
		d.Params = []ValueTypeDescriptor{{Kind: ValueTypeI64}}
		if err = validateDefinedTypeDescriptors([]DefinedTypeDescriptor{d}); err == nil {
			t.Fatalf("accepted aggregate kind %d carrying function parameters", d.Kind)
		}
	}
}

func TestTypeDescriptorsPreserveRecursiveReferenceStructure(t *testing.T) {
	recRef := func(index uint32) wasm.ValType {
		return wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: true}), true))
	}
	absRef := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	describes := wasm.TypeIdx{Index: 0, Rec: true}
	descriptor := wasm.TypeIdx{Index: 1, Rec: true}
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}},
		{SubTypes: []wasm.SubType{
			{
				Supers:   []wasm.TypeIdx{{Index: 0}},
				Metadata: wasm.TypeMetadata{Describes: wasm.SomeTypeIdx(describes), Descriptor: wasm.SomeTypeIdx(descriptor)},
				Comp: wasm.CompType{Kind: wasm.CompFunc,
					Params:  []wasm.ValType{recRef(0), absRef},
					Results: []wasm.ValType{recRef(1)},
				},
			},
			{
				Final: true,
				Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{
					wasm.NewFieldType(wasm.StorageVal(recRef(0)), wasm.Var),
					wasm.NewFieldType(wasm.StoragePacked(wasm.PackI16), wasm.Const),
				}},
			},
		}},
	}}

	got, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("typeDescriptorsFromWasm: %v", err)
	}
	if len(got) != 3 || got[0].RecGroup != 0 || got[1].RecGroup != 1 || got[2].RecGroup != 1 {
		t.Fatalf("flattened groups = %#v", got)
	}
	fn := got[1]
	if fn.Kind != CompositeTypeFunction || len(fn.Supers) != 1 || fn.Supers[0] != 0 {
		t.Fatalf("function descriptor = %#v", fn)
	}
	if !fn.HasDescribes || fn.Describes != 1 || !fn.HasDescriptor || fn.Descriptor != 2 {
		t.Fatalf("descriptor metadata = describes(%v,%d) descriptor(%v,%d)", fn.HasDescribes, fn.Describes, fn.HasDescriptor, fn.Descriptor)
	}
	if ref := fn.Params[0].Ref; !ref.Exact || ref.Nullable || !ref.Heap.Defined || ref.Heap.TypeIndex != 1 {
		t.Fatalf("recursive param = %#v, want non-null exact type 1", ref)
	}
	if ref := fn.Params[1].Ref; !ref.Nullable || ref.Heap.TypeIndex != 0 {
		t.Fatalf("absolute param = %#v, want nullable type 0", ref)
	}
	if ref := fn.Results[0].Ref; ref.Heap.TypeIndex != 2 {
		t.Fatalf("recursive result = %#v, want type 2", ref)
	}
	st := got[2]
	if st.Kind != CompositeTypeStruct || len(st.Fields) != 2 || !st.Fields[0].Mutable || st.Fields[0].Storage.Value.Ref.Heap.TypeIndex != 1 {
		t.Fatalf("struct descriptor = %#v", st)
	}
	if !st.Fields[1].Storage.Packed || st.Fields[1].Storage.PackedType != PackedTypeI16 {
		t.Fatalf("packed field = %#v", st.Fields[1])
	}
}

func TestTypeDescriptorsCompactEmptyRecursiveGroups(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
		{},
		{},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
	}}
	got, err := typeDescriptorsFromWasm(m)
	if err != nil {
		t.Fatalf("typeDescriptorsFromWasm: %v", err)
	}
	if len(got) != 2 || got[0].RecGroup != 0 || got[1].RecGroup != 1 {
		t.Fatalf("compacted recursive groups = %#v", got)
	}
	if err := validateDefinedTypeDescriptors(got); err != nil {
		t.Fatalf("compacted recursive groups rejected: %v", err)
	}
}

func TestDefinedTypeEquivalentDistinguishesBoundAndExternalRecursiveReferences(t *testing.T) {
	provider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkProviderPin))
	if err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	consumer, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkConsumerPin))
	if err != nil {
		t.Fatalf("decode consumer: %v", err)
	}
	providerTypes, err := typeDescriptorsFromWasm(provider)
	if err != nil {
		t.Fatalf("provider descriptors: %v", err)
	}
	consumerTypes, err := typeDescriptorsFromWasm(consumer)
	if err != nil {
		t.Fatalf("consumer descriptors: %v", err)
	}
	actual := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 4}}
	required := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 2}}
	if referenceTypeSubtype(actual, providerTypes, required, consumerTypes) {
		t.Fatal("external first-group reference unexpectedly matched the consumer's bound self-reference")
	}
}

func TestTypeDescriptorsRejectMalformedRecursiveIndex(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{
		Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{
			wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1, Rec: true}), false)),
		}},
	}}}}}
	if _, err := typeDescriptorsFromWasm(m); err == nil || !strings.Contains(err.Error(), "recursive type index 1 out of range") {
		t.Fatalf("malformed recursive type error = %v", err)
	}
}

func TestValueTypeDescriptorABITypeKeepsReferenceCategories(t *testing.T) {
	indexed := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	types := []DefinedTypeDescriptor{{Kind: CompositeTypeFunction}}
	if got, ok := indexed.ABIType(types); !ok || got != ValFuncRef {
		t.Fatalf("indexed ABI type = %v, %v; want funcref,true", got, ok)
	}
	if got, ok := indexed.ABIType([]DefinedTypeDescriptor{{Kind: CompositeTypeStruct}}); !ok || got != ValAnyRef {
		t.Fatalf("indexed struct ABI type = %v, %v; want anyref,true", got, ok)
	}
	if got, ok := indexed.ABIType([]DefinedTypeDescriptor{{Kind: CompositeTypeArray}}); !ok || got != ValAnyRef {
		t.Fatalf("indexed array ABI type = %v, %v; want anyref,true", got, ok)
	}
	any := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	if got, ok := any.ABIType(nil); !ok || got != ValAnyRef {
		t.Fatalf("anyref ABI type = %v, %v; want anyref,true", got, ok)
	}
}

func TestWasmTypeDescriptorConverterReusesFlattenedIndex(t *testing.T) {
	m := &wasm.Module{Types: make([]wasm.RecType, 4096)}
	converter := newWasmTypeDescriptorConverter(m)
	values := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	var sink []ValType
	allocs := testing.AllocsPerRun(100, func() {
		var err error
		sink, err = converter.abiTypes(values, nil)
		if err != nil {
			panic(err)
		}
	})
	if len(sink) != len(values) {
		t.Fatalf("ABI values = %v", sink)
	}
	if allocs > 2 {
		t.Fatalf("reused converter allocations = %.2f, want at most 2 result slices", allocs)
	}
}

func TestWasmTypeDescriptorConverterReusesFlattenedIndexForDescriptors(t *testing.T) {
	// AllocsPerRun reads process-wide counters. Windows CI can still run
	// background cleanup from earlier native/GC tests during these large
	// allocations; use a fresh process while retaining the exact budget.
	const childEnv = "WAGO_TYPE_DESCRIPTOR_ALLOCATION_CHILD"
	if runtime.GOOS == "windows" && os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWasmTypeDescriptorConverterReusesFlattenedIndexForDescriptors$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "--- PASS: TestWasmTypeDescriptorConverterReusesFlattenedIndexForDescriptors") {
			t.Fatalf("isolated descriptor allocation check: %v\n%s", err, output)
		}
		return
	}
	m := &wasm.Module{Types: make([]wasm.RecType, 4096)}
	for i := range m.Types {
		m.Types[i].SubTypes = []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}
	}
	converter := newWasmTypeDescriptorConverter(m)
	var sink []DefinedTypeDescriptor
	allocs := testing.AllocsPerRun(100, func() {
		var err error
		sink, err = converter.typeDescriptors()
		if err != nil {
			panic(err)
		}
	})
	if len(sink) != len(m.Types) {
		t.Fatalf("defined types = %d, want %d", len(sink), len(m.Types))
	}
	if allocs > 1 {
		t.Fatalf("reused converter descriptor allocations = %.0f, want one result backing allocation", allocs)
	}
}
