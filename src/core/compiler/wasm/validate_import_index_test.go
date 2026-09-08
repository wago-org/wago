package wasm

import (
	"reflect"
	"testing"
)

func TestValidatorImportIndexesPreserveKindOrder(t *testing.T) {
	m := &Module{Imports: []Import{
		{Type: ExternType{Kind: ExternGlobal}},
		{Type: ExternType{Kind: ExternFunc}},
		{Type: ExternType{Kind: ExternMem}},
		{Type: ExternType{Kind: ExternTag}},
		{Type: ExternType{Kind: ExternTable}},
		{Type: ExternType{Kind: ExternFunc}},
		{Type: ExternType{Kind: ExternGlobal}},
	}}
	v := moduleValidator{m: m}
	for kind, want := range map[ExternKind][]uint32{
		ExternFunc: {1, 5}, ExternTable: {4}, ExternMem: {2}, ExternGlobal: {0, 6}, ExternTag: {3},
	} {
		if got := v.importsOfKind(kind); !reflect.DeepEqual(got, want) {
			t.Fatalf("kind %d: %v, want %v", kind, got, want)
		}
	}
}
