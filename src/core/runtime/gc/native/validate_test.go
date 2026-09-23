package gc

import (
	"strings"
	"testing"
)

func TestValidateTypeDescs(t *testing.T) {
	pf, _ := NewStructDesc(0, []StorageKind{StorageI32, StorageI64})
	pf.Final = false
	ref, _ := NewStructDesc(1, []StorageKind{StorageRef})
	ref.HasSuper = true
	ref.Super = 0
	arr, _ := NewArrayDesc(2, StorageRefNull)
	if err := ValidateTypeDescs([]TypeDesc{pf, ref, arr}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTypeDescsRejectsMalformedSuperMetadata(t *testing.T) {
	structBase, _ := NewStructDesc(0, []StorageKind{StorageI32})
	structBase.Final = false
	structChild, _ := NewStructDesc(1, []StorageKind{StorageRef})
	structChild.HasSuper = true
	structChild.Super = 0
	arrayBase, _ := NewArrayDesc(0, StorageI32)
	arrayBase.Final = false
	arrayChild, _ := NewArrayDesc(1, StorageRefNull)
	arrayChild.HasSuper = true
	arrayChild.Super = 0
	funcBase := TypeDesc{ID: 0, Kind: KindFunc}
	funcChild := TypeDesc{ID: 1, Kind: KindFunc, HasSuper: true}

	valid := [][]TypeDesc{{structBase, structChild}, {arrayBase, arrayChild}, {funcBase, funcChild}}
	for _, descs := range valid {
		if err := ValidateTypeDescs(descs); err != nil {
			t.Fatalf("valid same-kind super rejected: %v", err)
		}
	}

	finalStruct, _ := NewStructDesc(0, []StorageKind{StorageI32})
	childOfFinal, _ := NewStructDesc(1, []StorageKind{StorageRef})
	childOfFinal.HasSuper = true
	childOfFinal.Super = 0
	structSuper, _ := NewStructDesc(0, []StorageKind{StorageI32})
	structSuper.Final = false
	arrayExtendsStruct, _ := NewArrayDesc(1, StorageI32)
	arrayExtendsStruct.HasSuper = true
	arrayExtendsStruct.Super = 0
	arraySuper, _ := NewArrayDesc(0, StorageI32)
	arraySuper.Final = false
	structExtendsArray, _ := NewStructDesc(1, []StorageKind{StorageI32})
	structExtendsArray.HasSuper = true
	structExtendsArray.Super = 0
	heapExtendsFunc, _ := NewStructDesc(1, []StorageKind{StorageI32})
	heapExtendsFunc.HasSuper = true
	heapExtendsFunc.Super = 0
	funcExtendsHeap := TypeDesc{ID: 1, Kind: KindFunc, HasSuper: true}

	cases := []struct {
		name string
		desc []TypeDesc
	}{
		{"final super", []TypeDesc{finalStruct, childOfFinal}},
		{"array extends struct", []TypeDesc{structSuper, arrayExtendsStruct}},
		{"struct extends array", []TypeDesc{arraySuper, structExtendsArray}},
		{"heap extends func", []TypeDesc{{ID: 0, Kind: KindFunc}, heapExtendsFunc}},
		{"func extends heap", []TypeDesc{structSuper, funcExtendsHeap}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs(tc.desc); err == nil {
				t.Fatal("expected malformed super metadata error")
			}
		})
	}
}

func TestValidateSuperAcyclicUsesSharedState(t *testing.T) {
	const n = 1024
	descs := make([]TypeDesc, n)
	for i := range descs {
		descs[i] = TypeDesc{ID: TypeID(i), Kind: KindFunc}
		if i > 0 {
			descs[i].HasSuper = true
			descs[i].Super = TypeID(i - 1)
		}
	}
	allocs := testing.AllocsPerRun(10, func() {
		if err := validateSuperAcyclic(descs); err != nil {
			t.Fatalf("validateSuperAcyclic: %v", err)
		}
	})
	if allocs > 2 {
		t.Fatalf("validateSuperAcyclic allocs = %.0f, want at most 2", allocs)
	}
}

func TestValidateTypeDescsFailures(t *testing.T) {
	base, _ := NewStructDesc(0, []StorageKind{StorageI32})
	cases := []struct {
		name string
		desc []TypeDesc
	}{
		{"id mismatch", []TypeDesc{{ID: 1, Kind: KindFunc}}},
		{"invalid kind", []TypeDesc{{ID: 0, Kind: 99}}},
		{"invalid super", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 2}}},
		{"self super", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 0}}},
		{"indirect super cycle", []TypeDesc{{ID: 0, Kind: KindFunc, HasSuper: true, Super: 1}, {ID: 1, Kind: KindFunc, HasSuper: true, Super: 0}}},
		{"func layout", []TypeDesc{{ID: 0, Kind: KindFunc, Size: 4}}},
		{"ref offset out of bounds", []TypeDesc{{ID: 0, Kind: KindStruct, Fields: []FieldDesc{{Kind: StorageRef, Offset: 8}}, Size: 4, Align: 4, HasRefs: true}}},
		{"bad array elem", []TypeDesc{{ID: 0, Kind: KindArray, Elem: StorageRef, ElemSize: 8, Align: 4, HasRefs: true}}},
		{"has refs mismatch", []TypeDesc{{ID: 0, Kind: KindStruct, Fields: base.Fields, Size: base.Size, Align: base.Align, HasRefs: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs(tc.desc); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestValidateTypeDescsRejectsOverlappingFields(t *testing.T) {
	cases := []struct {
		name string
		desc TypeDesc
	}{
		{
			name: "same offset",
			desc: TypeDesc{ID: 0, Kind: KindStruct, Fields: []FieldDesc{
				{Kind: StorageRefNull, Offset: 0}, {Kind: StorageI32, Offset: 0},
			}, Size: 4, Align: 4, HasRefs: true},
		},
		{
			name: "partial overlap",
			desc: TypeDesc{ID: 0, Kind: KindStruct, Fields: []FieldDesc{
				{Kind: StorageFuncRef, Offset: 0}, {Kind: StorageRefNull, Offset: 4},
			}, Size: 8, Align: 8, HasRefs: true},
		},
		{
			name: "nonadjacent overlap",
			desc: TypeDesc{ID: 0, Kind: KindStruct, Fields: []FieldDesc{
				{Kind: StorageRefNull, Offset: 0}, {Kind: StorageI32, Offset: 16}, {Kind: StorageI64, Offset: 0},
			}, Size: 24, Align: 8, HasRefs: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTypeDescs([]TypeDesc{tc.desc}); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Errorf("ValidateTypeDescs = %v, want overlap error", err)
			}
			for _, cfg := range []Config{{}, {Profile: ProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16}} {
				c, err := NewCollector(cfg, []TypeDesc{tc.desc})
				if c != nil {
					c.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "overlap") {
					t.Errorf("NewCollector profile %d = %v, want overlap error", cfg.Profile, err)
				}
			}
		})
	}
}

func TestValidateTypeDescsAcceptsReorderedDisjointFields(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32, StorageRefNull, StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	desc.Fields[0], desc.Fields[2] = desc.Fields[2], desc.Fields[0]
	if err := ValidateTypeDescs([]TypeDesc{desc}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTypeDescsBoundsReorderedFieldScratch(t *testing.T) {
	fields := make([]StorageKind, maxUnorderedStructFields+1)
	for i := range fields {
		fields[i] = StorageI32
	}
	desc, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTypeDescs([]TypeDesc{desc}); err != nil {
		t.Fatalf("ordered descriptor rejected: %v", err)
	}
	last := len(desc.Fields) - 1
	desc.Fields[0], desc.Fields[last] = desc.Fields[last], desc.Fields[0]
	if err := ValidateTypeDescs([]TypeDesc{desc}); err == nil || !strings.Contains(err.Error(), "too many unordered fields") {
		t.Fatalf("ValidateTypeDescs = %v, want bounded unordered-field error", err)
	}
}

func BenchmarkValidateTypeDescsWideStruct(b *testing.B) {
	fields := make([]StorageKind, 128)
	for i := range fields {
		fields[i] = StorageI32
	}
	desc, err := NewStructDesc(0, fields)
	if err != nil {
		b.Fatal(err)
	}
	descs := []TypeDesc{desc}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateTypeDescs(descs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateTypeDescsReorderedStruct(b *testing.B) {
	fields := make([]StorageKind, 128)
	for i := range fields {
		fields[i] = StorageI32
	}
	desc, err := NewStructDesc(0, fields)
	if err != nil {
		b.Fatal(err)
	}
	for i, j := 0, len(desc.Fields)-1; i < j; i, j = i+1, j-1 {
		desc.Fields[i], desc.Fields[j] = desc.Fields[j], desc.Fields[i]
	}
	descs := []TypeDesc{desc}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateTypeDescs(descs); err != nil {
			b.Fatal(err)
		}
	}
}
