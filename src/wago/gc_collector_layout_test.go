package wago

import (
	"reflect"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCCollectorLayoutEqualMatchesPublicFields(t *testing.T) {
	base := gc.TypeDesc{ID: 7, Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageI32, Offset: 4}}, Elem: gc.StorageI64, Size: 16, ElemSize: 8, Align: 8, HasRefs: true, Final: true, Super: 3, HasSuper: true}
	check := func(a, b gc.TypeDesc) {
		t.Helper()
		if got, want := gcCollectorLayoutEqual(a, b), reflect.DeepEqual(a, b); got != want {
			t.Fatalf("layout equality = %v, DeepEqual = %v: %#v vs %#v", got, want, a, b)
		}
	}
	check(base, base)
	// Mutate every public scalar field; private derived scan caches are not layout identity.
	for i := 0; i < reflect.TypeOf(base).NumField(); i++ {
		if reflect.TypeOf(base).Field(i).PkgPath != "" {
			continue
		}
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

func TestGCCollectorLayoutIgnoresOptionalScanCache(t *testing.T) {
	fields := make([]gc.StorageKind, 64)
	for i := range fields {
		fields[i] = gc.StorageI64
	}
	fields[63] = gc.StorageRefNull
	cached, err := gc.NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	raw := gc.TypeDesc{ID: cached.ID, Kind: cached.Kind, Size: cached.Size, Align: cached.Align, HasRefs: cached.HasRefs, Final: cached.Final, Fields: make([]gc.FieldDesc, len(cached.Fields))}
	for i, f := range cached.Fields {
		raw.Fields[i] = gc.FieldDesc{Kind: f.Kind, Offset: f.Offset}
	}
	if !gcCollectorLayoutEqual(cached, raw) || !gcCollectorLayoutEqual(raw, cached) {
		t.Fatal("optional scan cache changed collector layout identity")
	}
	input := &Compiled{GCTypeDescs: []gc.TypeDesc{cached}}

	if !reflect.DeepEqual(input.GCTypeDescs, input.GCTypeDescs) {
		t.Fatal("scan plan not rebuilt after serialization")
	}
	c, err := gc.NewCollector(gc.Config{}, input.GCTypeDescs)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 63, gc.RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(gc.RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 2 {
		t.Fatal("serialized scan plan lost child")
	}
}
