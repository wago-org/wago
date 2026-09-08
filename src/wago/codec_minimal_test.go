package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestCompiledCodecRoundTripsMultipleEmptyDataSegments(t *testing.T) {
	input := &Compiled{
		Data: []DataInit{
			{Offset: OffsetInit{Base: 0}},
			{Offset: OffsetInit{Base: 1}},
			{Offset: OffsetInit{Base: 2}},
		},
	}

	got := publicArtifactRoundTrip(t, input)
	if len(got.Data) != len(input.Data) {
		t.Fatalf("Data length = %d, want %d", len(got.Data), len(input.Data))
	}
	for i, seg := range got.Data {
		if !reflect.DeepEqual(seg.Offset, input.Data[i].Offset) {
			t.Fatalf("Data[%d].Offset = %+v, want %+v", i, seg.Offset, input.Data[i].Offset)
		}
		if len(seg.Bytes) != 0 {
			t.Fatalf("Data[%d].Bytes length = %d, want 0", i, len(seg.Bytes))
		}
	}
}

func TestCompiledCodecRoundTripsTableMaximum(t *testing.T) {
	input := &Compiled{HasTable: true, TableSize: 2, TableMax: 4}

	got := publicArtifactRoundTrip(t, input)
	if !got.HasTable || got.TableSize != input.TableSize || got.TableMax != input.TableMax {
		t.Fatalf("table shape after round trip = HasTable %v size %d max %d, want HasTable true size %d max %d", got.HasTable, got.TableSize, got.TableMax, input.TableSize, input.TableMax)
	}
	encoded, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary for reused receiver: %v", err)
	}
	reused := &Compiled{tableExports: map[string]int{"stale": 0}, hasTableExportMetadata: true}
	if err := reused.UnmarshalBinary(encoded); err != nil {
		t.Fatalf("UnmarshalBinary reused receiver: %v", err)
	}
	if len(reused.tableExports) != 0 {
		t.Fatalf("reused receiver retained table exports: %#v", reused.tableExports)
	}
	inst, err := Instantiate(reused)
	if err != nil {
		t.Fatalf("Instantiate round-tripped table: %v", err)
	}
	defer inst.Close()
	if _, err := inst.ExportedTable("advisory"); err == nil {
		t.Fatal("codec table without declared export metadata exposed advisory table 0")
	}
}

func TestCompiledCodecRoundTripsTableImport(t *testing.T) {
	input := &Compiled{HasTable: true, tableImport: "env.t", tableImportMin: 2, tableImportMax: 4, tableImportHasMax: true}

	got := publicArtifactRoundTrip(t, input)
	if got.tableImport != input.tableImport || got.tableImportMin != input.tableImportMin || got.tableImportMax != input.tableImportMax || got.tableImportHasMax != input.tableImportHasMax {
		t.Fatalf("table import after round trip = %q min %d max %d hasMax %v, want %q min %d max %d hasMax %v", got.tableImport, got.tableImportMin, got.tableImportMax, got.tableImportHasMax, input.tableImport, input.tableImportMin, input.tableImportMax, input.tableImportHasMax)
	}
}

func TestCompiledCodecRoundTripsMultipleEmptyElementSegments(t *testing.T) {
	input := &Compiled{
		HasTable:  true,
		TableSize: 0,
		Elems: []ElemInit{
			{RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 0}},
			{RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 1}},
			{RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 2}},
		},
	}

	got := publicArtifactRoundTrip(t, input)
	if len(got.Elems) != len(input.Elems) {
		t.Fatalf("Elems length = %d, want %d", len(got.Elems), len(input.Elems))
	}
	for i, seg := range got.Elems {
		if !reflect.DeepEqual(seg.Offset, input.Elems[i].Offset) {
			t.Fatalf("Elems[%d].Offset = %+v, want %+v", i, seg.Offset, input.Elems[i].Offset)
		}
		if len(seg.Values) != 0 {
			t.Fatalf("Elems[%d].Values length = %d, want 0", i, len(seg.Values))
		}
	}
}

func TestCompiledCodecRoundTripsEmptyStrings(t *testing.T) {
	input := newHandBuiltCompiled([]byte{0}, Compiled{
		Entry:          []int{0},
		NumImports:     1,
		dynamicImports: true,
		Imports:        []string{""},
		importFuncSigs: []FuncSig{{}},
		Funcs:          []FuncSig{{}},
		FuncTypeID:     []uint64{0, 0},
		Exports:        map[string]int{"": 1},
	})

	got := publicArtifactRoundTrip(t, input)
	if len(got.Imports) != 1 || got.Imports[0] != "" {
		t.Fatalf("Imports = %#v, want one empty string", got.Imports)
	}
	if len(got.importFuncSigs) != 1 || len(got.importFuncSigs[0].Params) != 0 || len(got.importFuncSigs[0].Results) != 0 {
		t.Fatalf("importFuncSigs = %#v, want one empty signature", got.importFuncSigs)
	}
	idx, ok := got.Exports[""]
	if !ok || idx != 1 {
		t.Fatalf("Exports[empty] = %d, %t; want 1, true", idx, ok)
	}
}

func TestCompiledCodecRoundTripsMinimalGCDescriptor(t *testing.T) {
	desc, err := gc.NewStructDesc(0, nil)
	if err != nil {
		t.Fatalf("NewStructDesc: %v", err)
	}
	input := &Compiled{GCTypeDescs: []gc.TypeDesc{desc}}

	got := publicArtifactRoundTrip(t, input)
	if len(got.GCTypeDescs) != 1 {
		t.Fatalf("GCTypeDescs length = %d, want 1", len(got.GCTypeDescs))
	}
	gotDesc := got.GCTypeDescs[0]
	if gotDesc.ID != 0 || gotDesc.Kind != gc.KindStruct || gotDesc.Size != 0 || gotDesc.Align != 1 || gotDesc.HasRefs {
		t.Fatalf("GCTypeDescs[0] = %+v, want minimal zero-field struct descriptor", gotDesc)
	}
	if len(gotDesc.Fields) != 0 {
		t.Fatalf("GCTypeDescs[0].Fields length = %d, want 0", len(gotDesc.Fields))
	}
}

func TestCompiledCodecRoundTripsV128ArrayDescriptorAndSIMDRequirement(t *testing.T) {
	desc, err := gc.NewArrayDesc(0, gc.StorageV128)
	if err != nil {
		t.Fatal(err)
	}
	input := &Compiled{
		Types: []DefinedTypeDescriptor{{
			Final: true,
			Kind:  CompositeTypeArray,
			Array: FieldTypeDescriptor{Mutable: true, Storage: StorageTypeDescriptor{
				Value: ValueTypeDescriptor{Kind: ValueTypeV128},
			}},
		}},
		GCTypeDescs: []gc.TypeDesc{desc},
	}
	if got := compiledStructuralRequiredFeatures(input); got&CoreFeatureSIMD == 0 {
		t.Fatalf("structural requirements = %#x, want SIMD", got)
	}
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var got Compiled
	if err := unmarshalCompiled(&got, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	if len(got.GCTypeDescs) != 1 || got.GCTypeDescs[0].Elem != gc.StorageV128 || got.GCTypeDescs[0].ElemSize != 16 || got.GCTypeDescs[0].Align != 16 {
		t.Fatalf("round-tripped v128 descriptor = %+v", got.GCTypeDescs)
	}
	if got.requiredFeatures&CoreFeatureSIMD == 0 {
		t.Fatalf("round-tripped required features = %#x, want SIMD", got.requiredFeatures)
	}
}

func TestCompiledCodecRoundTripsMixedGCDescriptors(t *testing.T) {
	nilStruct, err := gc.NewStructDesc(1, nil)
	if err != nil {
		t.Fatalf("nil-field struct: %v", err)
	}
	nilStruct.Fields = nil
	emptyStruct, err := gc.NewStructDesc(2, []gc.StorageKind{})
	if err != nil {
		t.Fatalf("empty-field struct: %v", err)
	}
	fieldStruct, err := gc.NewStructDesc(3, []gc.StorageKind{gc.StorageI8, gc.StorageRefNull, gc.StorageI64})
	if err != nil {
		t.Fatalf("field struct: %v", err)
	}
	arrayDesc, err := gc.NewArrayDesc(4, gc.StorageRef)
	if err != nil {
		t.Fatalf("array desc: %v", err)
	}
	input := &Compiled{GCTypeDescs: []gc.TypeDesc{
		{ID: 0, Kind: gc.KindFunc, Final: true},
		nilStruct,
		emptyStruct,
		fieldStruct,
		arrayDesc,
	}}
	if err := input.validate(); err != nil {
		t.Fatalf("input validate: %v", err)
	}

	got := publicArtifactRoundTrip(t, input)
	if err := got.validate(); err != nil {
		t.Fatalf("round-tripped validate: %v", err)
	}
	if len(got.GCTypeDescs) != len(input.GCTypeDescs) {
		t.Fatalf("GCTypeDescs length = %d, want %d", len(got.GCTypeDescs), len(input.GCTypeDescs))
	}
	if got.GCTypeDescs[1].Fields != nil {
		t.Fatalf("nil field slice decoded as %#v, want nil", got.GCTypeDescs[1].Fields)
	}
	if got.GCTypeDescs[2].Fields == nil || len(got.GCTypeDescs[2].Fields) != 0 {
		t.Fatalf("empty field slice decoded as %#v, want empty non-nil", got.GCTypeDescs[2].Fields)
	}
	if !got.GCTypeDescs[3].HasRefs || got.GCTypeDescs[4].Elem != gc.StorageRef || !got.GCTypeDescs[4].HasRefs {
		t.Fatalf("mixed descriptors lost ref metadata: %+v", got.GCTypeDescs)
	}
}

func publicArtifactRoundTrip(t testing.TB, input *Compiled) *Compiled {
	t.Helper()
	encoded, err := input.MarshalBinary()
	if err != nil {
		if guardPageBuilt {
			// Signals-based (guard-page) modules deliberately refuse serialization
			// (their code embeds the fault-handling bounds elision). Do not return the
			// original object from a helper that promises a public artifact load.
			t.Skipf("public artifact round-trip unavailable in guard-page build: %v", err)
		}
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := LoadTrustedArtifact(encoded)
	if err != nil {
		t.Fatalf("LoadTrustedArtifact: %v", err)
	}
	return got
}

// privatePayloadRoundTrip is only for tests that explicitly exercise a staged
// product whose public artifact admission is intentionally unsupported.
func privatePayloadRoundTrip(t testing.TB, input *Compiled) *Compiled {
	t.Helper()
	encoded, err := marshalCompiled(input)
	if err != nil {
		t.Fatalf("marshalCompiled: %v", err)
	}
	var got Compiled
	if err := unmarshalCompiled(&got, encoded[5:]); err != nil {
		t.Fatalf("unmarshalCompiled: %v", err)
	}
	if len(got.tableExports) == 0 {
		got.tableExports = nil
	}
	got.hasTableExportMetadata = true
	if got.memoryDir == nil {
		got.memoryDir = &compiledMemoryDirectory{}
	}
	if len(got.memoryDir.exports) == 0 {
		got.memoryDir.exports = nil
	}
	got.memoryDir.exactExports = true
	return &got
}
