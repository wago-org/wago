package wago

import (
	"reflect"
	"testing"
	"unsafe"
)

func descriptorContainsGoPointer(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return descriptorContainsGoPointer(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if descriptorContainsGoPointer(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

func TestPublicTypeDescriptorLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout contract records Wago's supported 64-bit targets")
	}
	for _, tc := range []struct {
		name  string
		value any
		got   uintptr
		want  uintptr
	}{
		{"HeapTypeDescriptor", HeapTypeDescriptor{}, unsafe.Sizeof(HeapTypeDescriptor{}), 8},
		{"ReferenceTypeDescriptor", ReferenceTypeDescriptor{}, unsafe.Sizeof(ReferenceTypeDescriptor{}), 12},
		{"ValueTypeDescriptor", ValueTypeDescriptor{}, unsafe.Sizeof(ValueTypeDescriptor{}), 16},
		{"StorageTypeDescriptor", StorageTypeDescriptor{}, unsafe.Sizeof(StorageTypeDescriptor{}), 20},
		{"FieldTypeDescriptor", FieldTypeDescriptor{}, unsafe.Sizeof(FieldTypeDescriptor{}), 24},
		{"DefinedTypeDescriptor", DefinedTypeDescriptor{}, unsafe.Sizeof(DefinedTypeDescriptor{}), 152},
	} {
		if tc.got != tc.want {
			t.Errorf("%s size = %d, want %d", tc.name, tc.got, tc.want)
		}
		t.Logf("%s: size=%d pointer-scanned=%v", tc.name, tc.got, descriptorContainsGoPointer(reflect.TypeOf(tc.value)))
	}
}
