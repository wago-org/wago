package runtime

import (
	"strings"
	"testing"
)

func TestInstantiateArenaNeedZeroLengthTableDescriptor(t *testing.T) {
	base := InstantiateFootprint{}
	withoutTable, err := InstantiateArenaNeed(base)
	if err != nil {
		t.Fatalf("InstantiateArenaNeed without table: %v", err)
	}
	withZeroTable, err := InstantiateArenaNeed(InstantiateFootprint{HasTable: true})
	if err != nil {
		t.Fatalf("InstantiateArenaNeed with zero-length table: %v", err)
	}
	if got, want := withZeroTable-withoutTable, 8; got != want {
		t.Fatalf("zero-length table footprint delta = %d, want descriptor header %d", got, want)
	}
	withPassiveData, err := InstantiateArenaNeed(InstantiateFootprint{PassiveDataCount: 2})
	if err != nil {
		t.Fatalf("InstantiateArenaNeed with passive data: %v", err)
	}
	if got, want := withPassiveData-withoutTable, 2*PassiveDataDescBytes; got != want {
		t.Fatalf("passive data footprint delta = %d, want %d", got, want)
	}
}

func TestInstantiateArenaNeedExcludesImportedTableDescriptors(t *testing.T) {
	oneImported, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0},
		ImportedTableCount: 1,
	})
	if err != nil {
		t.Fatalf("one imported table footprint: %v", err)
	}
	twoImported, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0, 0},
		ImportedTableCount: 2,
	})
	if err != nil {
		t.Fatalf("two imported tables footprint: %v", err)
	}
	if got, want := twoImported-oneImported, 16; got != want {
		t.Fatalf("second imported table footprint delta = %d, want 16-byte directory", got)
	}
	withLocal, err := InstantiateArenaNeed(InstantiateFootprint{
		HasTable:           true,
		TableCapacities:    []int{0, 0, 1},
		ImportedTableCount: 2,
	})
	if err != nil {
		t.Fatalf("two imported plus local footprint: %v", err)
	}
	if got, want := withLocal-twoImported, 48; got != want {
		t.Fatalf("local table after two imports footprint delta = %d, want 40-byte descriptor plus 8-byte directory growth", got)
	}
}

func TestInstantiateArenaNeedRejectsImpossibleTableShape(t *testing.T) {
	tests := []struct {
		name string
		fp   InstantiateFootprint
		want string
	}{
		{name: "table size without table", fp: InstantiateFootprint{TableSize: 1}, want: "without table"},
		{name: "elements without table", fp: InstantiateFootprint{ElemCount: 1}, want: "without table"},
		{name: "too many imported tables", fp: InstantiateFootprint{HasTable: true, TableCapacities: []int{0}, ImportedTableCount: 2}, want: "exceeds table count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InstantiateArenaNeed(tt.fp)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("InstantiateArenaNeed error = %v, want %q", err, tt.want)
			}
		})
	}
}
