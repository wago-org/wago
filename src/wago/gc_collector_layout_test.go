package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCCollectorLayoutEqualMatchesDeepEqual(t *testing.T) {
	base := gc.TypeDesc{ID: 7, Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageI32, Offset: 4}}, Elem: gc.StorageI64, Size: 16, ElemSize: 8, Align: 8, HasRefs: true, Final: true, Super: 3, HasSuper: true}
	check := func(a, b gc.TypeDesc) {
		t.Helper()
		if got, want := gcCollectorLayoutEqual(a, b), reflect.DeepEqual(a, b); got != want {
			t.Fatalf("layout equality = %v, DeepEqual = %v: %#v vs %#v", got, want, a, b)
		}
	}
	check(base, base)
	// Mutate every scalar field, so additions to TypeDesc must also be handled.
	for i := 0; i < reflect.TypeOf(base).NumField(); i++ {
		name := reflect.TypeOf(base).Field(i).Name
		t.Run(name, func(t *testing.T) {
			changed := base
			field := reflect.ValueOf(&changed).Elem().Field(i)
			switch field.Kind() {
			case reflect.Bool:
				field.SetBool(!field.Bool())
			case reflect.Uint8, reflect.Uint32:
				field.SetUint(field.Uint() + 1)
			case reflect.Slice:
				field.Set(reflect.Zero(field.Type()))
			default:
				t.Fatalf("add a mutation for descriptor field %s", name)
			}
			check(base, changed)
		})
	}
	copied := base
	copied.Fields = append([]gc.FieldDesc(nil), base.Fields...)
	check(base, copied)
	copied.Fields[0].Offset++
	check(base, copied)
	copied.Fields[0].Offset--
	copied.Fields[0].Kind++
	check(base, copied)
	for _, a := range [][]gc.FieldDesc{nil, {}, {{Kind: gc.StorageI32}}, {{Kind: gc.StorageI64}}, {{Kind: gc.StorageI32, Offset: 4}, {Kind: gc.StorageI64, Offset: 8}}} {
		for _, b := range [][]gc.FieldDesc{nil, {}, {{Kind: gc.StorageI32}}, {{Kind: gc.StorageI64}}, {{Kind: gc.StorageI32, Offset: 4}, {Kind: gc.StorageI64, Offset: 8}}} {
			x, y := base, base
			x.Fields, y.Fields = a, b
			check(x, y)
		}
	}
}
