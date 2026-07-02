package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCompiledCodecRoundTripsAllNameSectionMaps(t *testing.T) {
	moduleName := "all-name-maps"
	input := &Compiled{
		Names: &wasm.NameSec{
			ModuleName:    &moduleName,
			FunctionNames: wasm.NameMap{{Index: 0, Name: "func0"}, {Index: 2, Name: "func2"}},
			LocalNames: wasm.IndirectNameMap{
				{Index: 0, Names: wasm.NameMap{{Index: 0, Name: "param0"}, {Index: 1, Name: "local1"}}},
				{Index: 2, Names: wasm.NameMap{{Index: 0, Name: "other-param0"}}},
			},
			LabelNames: wasm.IndirectNameMap{
				{Index: 0, Names: wasm.NameMap{{Index: 0, Name: "block0"}, {Index: 1, Name: "loop1"}}},
			},
			TypeNames:    wasm.NameMap{{Index: 0, Name: "type0"}, {Index: 1, Name: "type1"}},
			TableNames:   wasm.NameMap{{Index: 0, Name: "table0"}},
			MemoryNames:  wasm.NameMap{{Index: 0, Name: "memory0"}},
			GlobalNames:  wasm.NameMap{{Index: 0, Name: "global0"}},
			ElementNames: wasm.NameMap{{Index: 0, Name: "element0"}},
			DataNames:    wasm.NameMap{{Index: 0, Name: "data0"}},
			FieldNames: wasm.IndirectNameMap{
				{Index: 0, Names: wasm.NameMap{{Index: 0, Name: "field0"}, {Index: 1, Name: "field1"}}},
				{Index: 1, Names: wasm.NameMap{{Index: 0, Name: "array-field0"}}},
			},
			TagNames: wasm.NameMap{{Index: 0, Name: "tag0"}},
		},
	}

	got := roundTripCompiled(t, input)
	if !reflect.DeepEqual(got.Names, input.Names) {
		t.Fatalf("Names after round trip = %#v, want %#v", got.Names, input.Names)
	}
}
