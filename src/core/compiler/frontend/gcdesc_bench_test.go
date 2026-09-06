package frontend

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

var gcTypeDescBenchmarkSink []gc.TypeDesc

func gcTypeLoweringFixture(types, fields int) []wasm.RecType {
	out := make([]wasm.RecType, types)
	for i := range out {
		var comp wasm.CompType
		switch i % 3 {
		case 0:
			comp = wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I64, wasm.FuncRef}, Results: []wasm.ValType{wasm.I32}}
		case 1:
			fs := make([]wasm.FieldType, fields)
			for j := range fs {
				mut := wasm.Const
				if j&1 != 0 {
					mut = wasm.Var
				}
				switch j % 5 {
				case 0:
					fs[j] = wasm.NewFieldType(wasm.StorageVal(wasm.I32), mut)
				case 1:
					fs[j] = wasm.NewFieldType(wasm.StorageVal(wasm.V128), mut)
				case 2:
					fs[j] = wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), mut)
				case 3:
					fs[j] = wasm.NewFieldType(wasm.StoragePacked(wasm.PackI16), mut)
				case 4:
					fs[j] = wasm.NewFieldType(wasm.StorageVal(wasm.AnyRef), mut)
				}
			}
			comp = wasm.CompType{Kind: wasm.CompStruct, Fields: fs}
		case 2:
			comp = wasm.CompType{Kind: wasm.CompArray, Array: wasm.NewFieldType(wasm.StorageVal(wasm.AnyRef), wasm.Var)}
		}
		out[i] = wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: comp}}}
	}
	return out
}

func BenchmarkGCTypeLowering(b *testing.B) {
	for _, shape := range []struct{ types, fields int }{{10, 64}, {100, 16}, {1000, 4}, {10000, 1}} {
		fixture := gcTypeLoweringFixture(shape.types, shape.fields)
		b.Run(fmt.Sprintf("types=%d/fields=%d", shape.types, shape.fields), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				descs, err := LowerGCTypeDescs(fixture)
				if err != nil || len(descs) != shape.types {
					b.Fatalf("lowering: descriptors=%d err=%v", len(descs), err)
				}
				gcTypeDescBenchmarkSink = descs
			}
		})
	}
}
