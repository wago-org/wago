package shared

import (
	"reflect"
	"testing"
)

func TestResidencyStatsRemainPointerFree(t *testing.T) {
	typ := reflect.TypeOf(ResidencyStats{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type.Kind() == reflect.Pointer || typ.Field(i).Type.Kind() == reflect.Map || typ.Field(i).Type.Kind() == reflect.Slice || typ.Field(i).Type.Kind() == reflect.Interface {
			t.Fatalf("field %s has pointer-bearing kind %s", typ.Field(i).Name, typ.Field(i).Type.Kind())
		}
	}
	if (ResidencyStats{}).Active() {
		t.Fatal("zero stats reported active")
	}
	if !(ResidencyStats{Candidates: 1}).Active() {
		t.Fatal("nonzero stats reported inactive")
	}
}
