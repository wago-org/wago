package wasm

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func typeContainsGoPointer(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return typeContainsGoPointer(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if typeContainsGoPointer(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

func TestCompilerTypeRepresentationLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout contract records Wago's supported 64-bit compiler targets")
	}
	for _, tc := range []struct {
		name  string
		value any
		got   uintptr
		want  uintptr
	}{
		{"TypeIdx", TypeIdx{}, unsafe.Sizeof(TypeIdx{}), 8},
		{"HeapType", HeapType{}, unsafe.Sizeof(HeapType{}), 16},
		{"RefType", RefType{}, unsafe.Sizeof(RefType{}), 16},
		{"ValType", ValType{}, unsafe.Sizeof(ValType{}), 16},
		{"StorageType", StorageType{}, unsafe.Sizeof(StorageType{}), 16},
		{"FieldType", FieldType{}, unsafe.Sizeof(FieldType{}), 16},
		{"CompType", CompType{}, unsafe.Sizeof(CompType{}), 96},
		{"TypeMetadata", TypeMetadata{}, unsafe.Sizeof(TypeMetadata{}), 16},
		{"SubType", SubType{}, unsafe.Sizeof(SubType{}), 152},
		{"Limits", Limits{}, unsafe.Sizeof(Limits{}), 24},
		{"TableType", TableType{}, unsafe.Sizeof(TableType{}), 40},
		{"MemType", MemType{}, unsafe.Sizeof(MemType{}), 32},
		{"GlobalType", GlobalType{}, unsafe.Sizeof(GlobalType{}), 24},
		{"ExternType", ExternType{}, unsafe.Sizeof(ExternType{}), 40},
		{"Import", Import{}, unsafe.Sizeof(Import{}), 72},
		{"Expr", Expr{}, unsafe.Sizeof(Expr{}), 48},
		{"Func", Func{}, unsafe.Sizeof(Func{}), 104},
		{"ElemMode", ElemMode{}, unsafe.Sizeof(ElemMode{}), 56},
		{"ElemKind", ElemKind{}, unsafe.Sizeof(ElemKind{}), 72},
		{"Elem", Elem{}, unsafe.Sizeof(Elem{}), 128},
		{"DataMode", DataMode{}, unsafe.Sizeof(DataMode{}), 56},
		{"gc.FieldDesc", corergc.FieldDesc{}, unsafe.Sizeof(corergc.FieldDesc{}), 8},
		{"gc.TypeDesc", corergc.TypeDesc{}, unsafe.Sizeof(corergc.TypeDesc{}), 64},
	} {
		if tc.got != tc.want {
			t.Errorf("%s size = %d, want %d", tc.name, tc.got, tc.want)
		}
		t.Logf("%s: size=%d pointer-scanned=%v", tc.name, tc.got, typeContainsGoPointer(reflect.TypeOf(tc.value)))
	}
	for _, value := range []any{TypeIdx{}, HeapType{}, RefType{}, ValType{}, StorageType{}, FieldType{}, OptionalTypeIdx{}, TypeMetadata{}, Limits{}, TableType{}, MemType{}, GlobalType{}, ExternType{}} {
		if typ := reflect.TypeOf(value); typeContainsGoPointer(typ) {
			t.Errorf("%s unexpectedly contains a Go pointer", typ)
		}
	}
}

var (
	typeRepFieldsSink []FieldType
	typeRepBoolSink   bool
	typeRepModuleSink *Module
	typeRepKeySink    uint64
)

func syntheticGCFields(n int) []FieldType {
	fields := make([]FieldType, n)
	for i := range fields {
		mut := Const
		if i&1 != 0 {
			mut = Var
		}
		switch i % 8 {
		case 0:
			fields[i] = NewFieldType(StorageVal(I32), mut)
		case 1:
			fields[i] = NewFieldType(StorageVal(I64), mut)
		case 2:
			fields[i] = NewFieldType(StorageVal(V128), mut)
		case 3:
			fields[i] = NewFieldType(StoragePacked(PackI8), mut)
		case 4:
			fields[i] = NewFieldType(StoragePacked(PackI16), mut)
		case 5:
			fields[i] = NewFieldType(StorageVal(RefVal(Ref(true, AbsHeap(HeapAny), false))), mut)
		case 6:
			fields[i] = NewFieldType(StorageVal(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: uint32(i), Rec: true}), true))), mut)
		case 7:
			fields[i] = NewFieldType(StorageVal(FuncRef), mut)
		}
	}
	return fields
}

func BenchmarkGCTypeConstruction(b *testing.B) {
	for _, types := range []int{10, 100, 1000, 10000} {
		for _, fields := range []int{1, 4, 16, 64} {
			prototype := syntheticGCFields(8)
			b.Run(fmt.Sprintf("types=%d/fields=%d", types, fields), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					out := make([]FieldType, types*fields)
					for j := range out {
						out[j] = prototype[j&7]
					}
					typeRepFieldsSink = out
				}
			})
		}
	}
}

func BenchmarkGCTypeEquality(b *testing.B) {
	for _, fields := range []int{1, 4, 16, 64} {
		a := syntheticGCFields(fields)
		z := append([]FieldType(nil), a...)
		b.Run(fmt.Sprintf("fields=%d", fields), func(b *testing.B) {
			b.ReportAllocs()
			var equal bool
			for i := 0; i < b.N; i++ {
				equal = true
				for j := range a {
					equal = equal && equalFieldType(a[j], z[j])
				}
			}
			if !equal {
				b.Fatal("equal fields compared unequal")
			}
			typeRepBoolSink = equal
		})
	}
}

func appendTypeRepU32(dst []byte, value uint32) []byte {
	for {
		out := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			out |= 0x80
		}
		dst = append(dst, out)
		if value == 0 {
			return dst
		}
	}
}

func syntheticGCTypeModuleBytes(types, fields int) []byte {
	payload := appendTypeRepU32(nil, uint32(types))
	for i := 0; i < types; i++ {
		payload = append(payload, 0x5f)
		payload = appendTypeRepU32(payload, uint32(fields))
		for j := 0; j < fields; j++ {
			switch j % 6 {
			case 0:
				payload = append(payload, 0x7f, byte(Const))
			case 1:
				payload = append(payload, 0x7e, byte(Var))
			case 2:
				payload = append(payload, 0x7b, byte(Const))
			case 3:
				payload = append(payload, byte(PackI8), byte(Var))
			case 4:
				payload = append(payload, byte(PackI16), byte(Const))
			case 5:
				payload = append(payload, 0x63, byte(HeapAny), byte(Var))
			}
		}
	}
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, secType}
	module = appendTypeRepU32(module, uint32(len(payload)))
	return append(module, payload...)
}

func BenchmarkGCTypeDecode(b *testing.B) {
	for _, shape := range []struct{ types, fields int }{{10, 64}, {100, 16}, {1000, 4}, {10000, 1}} {
		data := syntheticGCTypeModuleBytes(shape.types, shape.fields)
		b.Run(fmt.Sprintf("types=%d/fields=%d", shape.types, shape.fields), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := DecodeModule(data)
				if err != nil || len(m.Types) != shape.types {
					b.Fatalf("decode: types=%d err=%v", len(m.Types), err)
				}
				typeRepModuleSink = m
			}
		})
	}
}

func BenchmarkGCTypeValidate(b *testing.B) {
	for _, shape := range []struct{ types, fields int }{{10, 64}, {100, 16}, {1000, 4}, {10000, 1}} {
		m, err := DecodeModule(syntheticGCTypeModuleBytes(shape.types, shape.fields))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("types=%d/fields=%d", shape.types, shape.fields), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := ValidateModule(m); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkGCTypeStructuralKey(b *testing.B) {
	params := make([]ValType, 64)
	results := make([]ValType, 16)
	for i := range params {
		params[i] = syntheticGCFields(1)[0].Storage().Val()
	}
	for i := range results {
		results[i] = RefVal(Ref(i&1 == 0, AbsHeap(HeapFunc), i&1 != 0))
	}
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: params, Results: results}}}}}}
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m.structuralTypeCache = nil
			key, ok := m.structuralIndexedFuncTypeKey(0)
			if !ok {
				b.Fatal("key unavailable")
			}
			typeRepKeySink = key
		}
	})
	if _, ok := m.structuralIndexedFuncTypeKey(0); !ok {
		b.Fatal("warm key unavailable")
	}
	b.Run("warm", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			key, ok := m.structuralIndexedFuncTypeKey(0)
			if !ok {
				b.Fatal("key unavailable")
			}
			typeRepKeySink = key
		}
	})
}

func syntheticTypeMetadataModuleBytes(types int) []byte {
	payload := appendTypeRepU32(nil, uint32(types))
	for i := 0; i < types; i++ {
		// descriptor 0 followed by an empty struct type.
		payload = append(payload, 0x4d, 0x00, 0x5f, 0x00)
	}
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, secType}
	module = appendTypeRepU32(module, uint32(len(payload)))
	return append(module, payload...)
}

func BenchmarkTypeMetadataDecode(b *testing.B) {
	for _, types := range []int{10, 100, 1000, 10000} {
		data := syntheticTypeMetadataModuleBytes(types)
		b.Run(fmt.Sprintf("types=%d", types), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := DecodeModule(data)
				if err != nil || len(m.Types) != types {
					b.Fatalf("decode: types=%d err=%v", len(m.Types), err)
				}
				typeRepModuleSink = m
			}
		})
	}
}

func appendTypeRepSection(module []byte, id byte, payload []byte) []byte {
	module = append(module, id)
	module = appendTypeRepU32(module, uint32(len(payload)))
	return append(module, payload...)
}

func syntheticFunctionMetadataModuleBytes(functions int) []byte {
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	module = appendTypeRepSection(module, secType, []byte{0x01, 0x60, 0x00, 0x00})
	functionPayload := appendTypeRepU32(nil, uint32(functions))
	for i := 0; i < functions; i++ {
		functionPayload = append(functionPayload, 0x00)
	}
	module = appendTypeRepSection(module, secFunction, functionPayload)
	codePayload := appendTypeRepU32(nil, uint32(functions))
	for i := 0; i < functions; i++ {
		codePayload = append(codePayload, 0x02, 0x00, 0x0b)
	}
	return appendTypeRepSection(module, secCode, codePayload)
}

func BenchmarkFunctionMetadataDecode(b *testing.B) {
	for _, functions := range []int{10, 100, 1000, 10000} {
		data := syntheticFunctionMetadataModuleBytes(functions)
		b.Run(fmt.Sprintf("functions=%d", functions), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := DecodeModule(data)
				if err != nil || len(m.Code) != functions {
					b.Fatalf("decode: functions=%d err=%v", len(m.Code), err)
				}
				typeRepModuleSink = m
			}
		})
	}
}
