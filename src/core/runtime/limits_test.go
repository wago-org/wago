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
}

func TestInstantiateArenaNeedRejectsImpossibleTableShape(t *testing.T) {
	tests := []struct {
		name string
		fp   InstantiateFootprint
		want string
	}{
		{name: "table size without table", fp: InstantiateFootprint{TableSize: 1}, want: "without table"},
		{name: "elements without table", fp: InstantiateFootprint{ElemCount: 1}, want: "without table"},
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
