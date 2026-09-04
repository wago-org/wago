//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func v128ArrayCodeWithLocals(locals, body []byte) []byte {
	fn := append(append([]byte(nil), locals...), body...)
	return append(wasmtest.ULEB(uint32(len(fn))), fn...)
}

func v128ArrayModule() []byte {
	arrayType := []byte{0x5e, 0x7b, 0x01}      // (array (mut v128))
	refLocal := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null type 0) local
	twoRefLocals := []byte{0x01, 0x02, 0x63, 0x00}

	uniform := []byte{
		0x20, 0x00, 0x20, 0x01, 0xfb, 0x06, 0x00, // array.new 0
		0x20, 0x02, 0xfb, 0x0b, 0x00, 0x0b, // array.get 0
	}
	fixed := []byte{
		0x20, 0x00, 0x20, 0x01, 0xfb, 0x08, 0x00, 0x02, // array.new_fixed 0 2
		0x20, 0x02, 0xfb, 0x0b, 0x00, 0x0b,
	}
	set := []byte{
		0x41, 0x01, 0xfb, 0x07, 0x00, 0x21, 0x01, // default array -> local 1
		0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0xfb, 0x0e, 0x00,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b,
	}
	fill := []byte{
		0x41, 0x03, 0xfb, 0x07, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0x41, 0x03, 0xfb, 0x10, 0x00,
		0x20, 0x01, 0x41, 0x02, 0xfb, 0x0b, 0x00, 0x0b,
	}
	copyBody := []byte{
		0x20, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x00, 0x21, 0x01,
		0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x02,
		0x20, 0x02, 0x41, 0x00, 0x20, 0x01, 0x41, 0x00, 0x41, 0x02, 0xfb, 0x11, 0x00, 0x00,
		0x20, 0x02, 0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
	}
	newData := []byte{
		0x41, 0x00, 0x41, 0x01, 0xfb, 0x09, 0x00, 0x00,
		0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b,
	}
	initData := []byte{
		0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x41, 0x00, 0x41, 0x01, 0xfb, 0x12, 0x00, 0x00,
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
	}
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	passiveData := append([]byte{0x01}, wasmtest.ULEB(uint32(len(data)))...)
	passiveData = append(passiveData, data...)
	vecGlobal := append([]byte{0x7b, 0x00, 0xfd, 0x0c}, data...) // immutable v128
	vecGlobal = append(vecGlobal, 0x0b)
	arrayGlobal := []byte{0x64, 0x00, 0x00, 0x23, 0x00, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x0b}
	getGlobal := []byte{0x23, 0x01, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b}
	getDefault := []byte{0x41, 0x01, 0xfb, 0x07, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b}
	simdConsume := []byte{
		0x41, 0x00, 0x41, 0x01, 0xfb, 0x09, 0x00, 0x00,
		0x41, 0x00, 0xfb, 0x0b, 0x00, 0xfd, 0x4d, 0x0b, // v128.not
	}
	dropData := []byte{0xfc, 0x09, 0x00, 0x0b}

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32, wasm.I32}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(3),
			wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4),
			wasmtest.ULEB(4), wasmtest.ULEB(5),
		)),
		wasmtest.Section(6, wasmtest.Vec(vecGlobal, arrayGlobal)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("uniform", 0, 0),
			wasmtest.ExportEntry("fixed", 0, 1),
			wasmtest.ExportEntry("set", 0, 2),
			wasmtest.ExportEntry("fill", 0, 3),
			wasmtest.ExportEntry("copy", 0, 4),
			wasmtest.ExportEntry("new_data", 0, 5),
			wasmtest.ExportEntry("init_data", 0, 6),
			wasmtest.ExportEntry("global", 0, 7),
			wasmtest.ExportEntry("default", 0, 8),
			wasmtest.ExportEntry("simd_consume", 0, 9),
			wasmtest.ExportEntry("drop_data", 0, 10),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(uniform),
			wasmtest.Code(fixed),
			v128ArrayCodeWithLocals(refLocal, set),
			v128ArrayCodeWithLocals(refLocal, fill),
			v128ArrayCodeWithLocals(twoRefLocals, copyBody),
			wasmtest.Code(newData),
			v128ArrayCodeWithLocals(refLocal, initData),
			wasmtest.Code(getGlobal),
			wasmtest.Code(getDefault),
			wasmtest.Code(simdConsume),
			wasmtest.Code(dropData),
		)),
		wasmtest.Section(11, wasmtest.Vec(passiveData)),
	)
}

func v128ArrayFixedCountModule(count uint32) []byte {
	arrayType := []byte{0x5e, 0x7b, 0x01}
	funcType := append([]byte{0x60}, wasmtest.ULEB(count)...)
	for i := uint32(0); i < count; i++ {
		funcType = append(funcType, 0x7b)
	}
	funcType = append(funcType, 0x01, 0x7b) // one v128 result
	body := make([]byte, 0, int(count)*2+8)
	for i := uint32(0); i < count; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(i)...)
	}
	body = append(body, 0xfb, 0x08, 0x00)
	body = append(body, wasmtest.ULEB(count)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(count-1))...)
	body = append(body, 0xfb, 0x0b, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fixed", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCArrayV128FixedConstructorSlotBound(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	c, err := Compile(cfg, v128ArrayFixedCountModule(32))
	if err != nil {
		t.Fatalf("32 v128 values: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		_ = c.Close()
		t.Fatal(err)
	}
	args := make([]uint64, 0, 64)
	for i := uint64(0); i < 32; i++ {
		args = append(args, i, ^i)
	}
	got, err := in.Invoke("fixed", args...)
	if err != nil || !reflect.DeepEqual(got, []uint64{31, ^uint64(31)}) {
		_ = in.Close()
		_ = c.Close()
		t.Fatalf("32-value fixed result = %#x, %v", got, err)
	}
	_ = in.Close()
	_ = c.Close()
	large, err := Compile(cfg, v128ArrayFixedCountModule(128))
	if err != nil {
		t.Fatalf("128 v128 values: %v", err)
	}
	_ = large.Close()
}

func TestGCVectorGenericCodecReload(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, tc := range []struct {
		name       string
		module     []byte
		export     string
		args, want []uint64
		check      func(*Compiled) bool
	}{
		{name: "array", module: v128ArrayModule(), export: "global", want: []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}, check: func(c *Compiled) bool {
			return c.stagedGCArrayProduct() == stagedGCArrayProductGeneric && c.usesGCArrayHelpers()
		}},
		{name: "struct", module: v128StructModule(), export: "global", want: []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}, check: func(c *Compiled) bool {
			return c.stagedGCStructProduct() == stagedGCStructGeneric && c.usesGCStructHelpers()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(cfg, tc.module)
			if err != nil {
				t.Fatal(err)
			}
			blob, err := compiled.MarshalBinary()
			_ = compiled.Close()
			if err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if err := loaded.UnmarshalBinary(blob); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if !tc.check(&loaded) {
				t.Fatal("codec reload lost generic GC helper admission")
			}
			in, err := Instantiate(&loaded, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke(tc.export, tc.args...)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s = %#x, %v; want %#x", tc.export, got, err, tc.want)
			}
		})
	}
}

func BenchmarkGCArrayV128Set(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128ArrayModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("set", uint64(i), ^uint64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGCArrayV128HelpersPreserveBothSlots(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	c, err := Compile(cfg, v128ArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.GCTypeDescs) == 0 || c.GCTypeDescs[0].Elem != gc.StorageV128 || c.GCTypeDescs[0].ElemSize != 16 {
		t.Fatalf("v128 array descriptor = %+v", c.GCTypeDescs)
	}
	if c.requiredFeatures&CoreFeatureSIMD == 0 {
		t.Fatalf("required features = %#x, want SIMD", c.requiredFeatures)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	aLo, aHi := uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
	bLo, bHi := uint64(0x1716151413121110), uint64(0x1f1e1d1c1b1a1918)
	cases := []struct {
		name string
		args []uint64
		want []uint64
	}{
		{name: "uniform", args: []uint64{aLo, aHi, 3, 2}, want: []uint64{aLo, aHi}},
		{name: "fixed", args: []uint64{aLo, aHi, bLo, bHi, 1}, want: []uint64{bLo, bHi}},
		{name: "set", args: []uint64{bLo, bHi}, want: []uint64{bLo, bHi}},
		{name: "fill", args: []uint64{aLo, aHi}, want: []uint64{aLo, aHi}},
		{name: "copy", args: []uint64{bLo, bHi}, want: []uint64{bLo, bHi}},
		{name: "new_data", want: []uint64{aLo, aHi}},
		{name: "init_data", want: []uint64{aLo, aHi}},
		{name: "global", want: []uint64{aLo, aHi}},
		{name: "default", want: []uint64{0, 0}},
		{name: "simd_consume", want: []uint64{^aLo, ^aHi}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := in.Invoke(tc.name, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result = %#x, want %#x", got, tc.want)
			}
		})
	}
	if _, err := in.Invoke("uniform", aLo, aHi, 1, 1); err == nil {
		t.Fatal("out-of-bounds v128 array.get succeeded")
	}
	allocs := testing.AllocsPerRun(100, func() {
		got, err := in.Invoke("set", bLo, bHi)
		if err != nil || len(got) != 2 || got[0] != bLo || got[1] != bHi {
			panic("v128 array helper failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("v128 array helper allocations = %v, want 0", allocs)
	}
	if stats := in.gc.Stats(); stats.FullCollections == 0 || stats.LiveObjects > 2 {
		t.Fatalf("generic GC boundary collection stats = %+v, want collections and at most global+current objects", stats)
	}
	if _, err := in.Invoke("drop_data"); err != nil {
		t.Fatalf("data.drop: %v", err)
	}
	if _, err := in.Invoke("new_data"); err == nil {
		t.Fatal("non-empty v128 array.new_data succeeded after data.drop")
	}
	if _, err := in.Invoke("init_data"); err == nil {
		t.Fatal("non-empty v128 array.init_data succeeded after data.drop")
	}
}
