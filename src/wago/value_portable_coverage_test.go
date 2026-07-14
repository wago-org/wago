package wago

import "testing"

func TestValuePortableSurface(t *testing.T) {
	cases := []struct {
		value Value
		want  string
	}{
		{ValueI32(-1), "i32(-1)"},
		{ValueI64(-2), "i64(-2)"},
		{ValueF32(1.5), "f32(1.5)"},
		{ValueF64(2.5), "f64(2.5)"},
		{ValueOf(ValV128, 0), "v128(…)"},
		{ValueFuncRef(NullFuncRef()), "funcref(null)"},
		{ValueExternRef(NullExternRef()), "externref(null)"},
		{ValueOf(ValFuncRef, 1), "funcref(opaque)"},
		{ValueOf(ValExternRef, 1), "externref(opaque)"},
		{ValueOf(ValType(99), 0), "unknown(…)"},
	}
	for _, tc := range cases {
		if got := tc.value.String(); got != tc.want {
			t.Errorf("Value(%v).String() = %q, want %q", tc.value.Type(), got, tc.want)
		}
	}
	if v := ValueI32(-3); v.Type() != ValI32 || v.I32() != -3 || v.Bits() != I32(-3) {
		t.Fatalf("i32 value = %+v", v)
	}
	if v := ValueI64(-4); v.Type() != ValI64 || v.I64() != -4 {
		t.Fatalf("i64 value = %+v", v)
	}
	if v := ValueF32(3.25); v.Type() != ValF32 || v.F32() != 3.25 {
		t.Fatalf("f32 value = %+v", v)
	}
	if v := ValueF64(4.5); v.Type() != ValF64 || v.F64() != 4.5 {
		t.Fatalf("f64 value = %+v", v)
	}
	if v := ValueOf(ValFuncRef, 7); v.FuncRef().token != 7 {
		t.Fatalf("funcref = %+v", v.FuncRef())
	}
	if v := ValueOf(ValExternRef, 8); v.ExternRef().token != 8 {
		t.Fatalf("externref = %+v", v.ExternRef())
	}
}

func TestHostGlobalConstructorsAndCompiledGlobalMetadata(t *testing.T) {
	vec := V128{1, 2, 3, 4, 5}
	for _, tc := range []struct {
		name string
		g    *Global
		bits uint64
	}{
		{"i32", NewGlobalI32(-3, true), I32(-3)},
		{"i64", NewGlobalI64(-4, true), I64(-4)},
		{"f32", NewGlobalF32(1.5, true), F32(1.5)},
		{"f64", NewGlobalF64(2.5, true), F64(2.5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.g.Close()
			if got := tc.g.Get(); got != tc.bits {
				t.Fatalf("Get = %#x, want %#x", got, tc.bits)
			}
		})
	}
	v := NewGlobalV128(vec, true)
	defer v.Close()
	if got := v.GetV128(); got != vec {
		t.Fatalf("GetV128 = %#v, want %#v", got, vec)
	}
	updated := V128{9, 8, 7}
	if err := v.SetV128(updated); err != nil || v.GetV128() != updated {
		t.Fatalf("SetV128 = %v, value %#v", err, v.GetV128())
	}
	immutable := NewGlobalI32(0, false)
	defer immutable.Close()
	if err := immutable.SetV128(vec); err == nil {
		t.Fatal("SetV128 accepted immutable non-v128 global")
	}
	scalar := NewGlobalI32(0, true)
	defer scalar.Close()
	if err := scalar.Set(0x11223344); err != nil || scalar.Get() != 0x11223344 {
		t.Fatalf("Set scalar = %v, value %#x", err, scalar.Get())
	}
	if got := scalar.GetV128(); got[0] != 0x44 || got[1] != 0x33 || got[2] != 0x22 || got[3] != 0x11 {
		t.Fatalf("scalar GetV128 = %x", got)
	}
	if err := immutable.Set(1); err == nil {
		t.Fatal("Set accepted immutable global")
	}
	if err := v.Set(1); err == nil {
		t.Fatal("Set accepted v128 global")
	}
	ref := &Global{Type: ValExternRef, Mutable: true}
	if ref.Get() != 0 || ref.Set(1) == nil {
		t.Fatal("reference scalar global access changed")
	}
	if (*Global)(nil).GetV128() != (V128{}) {
		t.Fatal("nil GetV128 was non-zero")
	}

	c := &Compiled{GlobalImports: []GlobalImportDef{{}, {}}, Globals: []GlobalDef{{}, {}, {}}}
	if c.ImportedGlobalCount() != 2 || c.LocalGlobalCount() != 1 || c.GlobalSlot(3) != 24 {
		t.Fatalf("global metadata = imports %d locals %d slot %d", c.ImportedGlobalCount(), c.LocalGlobalCount(), c.GlobalSlot(3))
	}
}

func TestInstanceGlobalAndCodeBaseAPIs(t *testing.T) {
	vec := V128{9, 8, 7, 6}
	cell := NewGlobalV128(V128{}, true)
	defer cell.Close()
	in := &Instance{
		base: 0xfeed,
		c: &Compiled{
			Entry:         []int{4, 12},
			Globals:       []GlobalDef{{Type: ValV128, Mutable: true}, {Type: ValI32}},
			GlobalExports: map[string]int{"vec": 0, "fixed": 1},
			Exports:       map[string]int{"fn": 0},
		},
		globalCells: []*Global{cell, NewGlobalI32(1, false)},
	}
	defer in.globalCells[1].Close()

	base, entries := in.CodeBase()
	if base != in.base || len(entries) != 2 || entries[0] != 4 {
		t.Fatalf("CodeBase = %#x, %v", base, entries)
	}
	entries[0] = 99
	if in.c.Entry[0] != 4 {
		t.Fatal("CodeBase exposed compiled entry storage")
	}
	if err := in.SetGlobalV128("vec", vec); err != nil {
		t.Fatalf("SetGlobalV128: %v", err)
	}
	if got, err := in.GlobalV128("vec"); err != nil || got != vec {
		t.Fatalf("GlobalV128 = %x, %v", got, err)
	}
	for _, name := range []string{"missing", "fn", "fixed"} {
		if err := in.SetGlobalV128(name, vec); err == nil {
			t.Errorf("SetGlobalV128(%q) unexpectedly succeeded", name)
		}
	}
	if _, err := in.GlobalV128("fixed"); err == nil {
		t.Fatal("GlobalV128 accepted non-v128 global")
	}
}

func TestRuntimeNewFuncRefGlobalPortableBoundaries(t *testing.T) {
	if _, err := (*Runtime)(nil).NewFuncRefGlobal(NullFuncRef(), true); err == nil {
		t.Fatal("nil Runtime accepted NewFuncRefGlobal")
	}
	rt := NewRuntime()
	g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatalf("NewFuncRefGlobal(null): %v", err)
	}
	if got, err := g.GetValue(); err != nil || !got.FuncRef().IsNull() {
		t.Fatalf("null global = %v, %v", got, err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close global: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if _, err := rt.NewFuncRefGlobal(NullFuncRef(), true); err == nil {
		t.Fatal("closed Runtime accepted NewFuncRefGlobal")
	}
}
