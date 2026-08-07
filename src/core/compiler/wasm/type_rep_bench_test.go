package wasm

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
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
	for _, tc := range []struct {
		name  string
		value any
		size  uintptr
	}{
		{"TypeIdx", TypeIdx{}, unsafe.Sizeof(TypeIdx{})},
		{"HeapType", HeapType{}, unsafe.Sizeof(HeapType{})},
		{"RefType", RefType{}, unsafe.Sizeof(RefType{})},
		{"ValType", ValType{}, unsafe.Sizeof(ValType{})},
		{"StorageType", StorageType{}, unsafe.Sizeof(StorageType{})},
		{"FieldType", FieldType{}, unsafe.Sizeof(FieldType{})},
		{"CompType", CompType{}, unsafe.Sizeof(CompType{})},
		{"SubType", SubType{}, unsafe.Sizeof(SubType{})},
	} {
		t.Logf("%s: size=%d pointer-scanned=%v", tc.name, tc.size, typeContainsGoPointer(reflect.TypeOf(tc.value)))
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
			fields[i] = FieldType{Storage: StorageType{Val: I32}, Mut: mut}
		case 1:
			fields[i] = FieldType{Storage: StorageType{Val: I64}, Mut: mut}
		case 2:
			fields[i] = FieldType{Storage: StorageType{Val: V128}, Mut: mut}
		case 3:
			fields[i] = FieldType{Storage: StorageType{Packed: true, Pack: PackI8}, Mut: mut}
		case 4:
			fields[i] = FieldType{Storage: StorageType{Packed: true, Pack: PackI16}, Mut: mut}
		case 5:
			fields[i] = FieldType{Storage: StorageType{Val: RefVal(Ref(true, AbsHeap(HeapAny), false))}, Mut: mut}
		case 6:
			fields[i] = FieldType{Storage: StorageType{Val: RefVal(Ref(false, IndexedHeap(TypeIdx{Index: uint32(i), Rec: true}), true))}, Mut: mut}
		case 7:
			fields[i] = FieldType{Storage: StorageType{Val: FuncRef}, Mut: mut}
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
		params[i] = syntheticGCFields(1)[0].Storage.Val
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
