//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcFinalCastStructGetModuleWithType(structType, body []byte) []byte {
	// type 1: () -> i32
	types := wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcs := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1)))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0)))
	code := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	return wasmtest.Module(types, funcs, exports, code)
}

func gcFinalCastStructGetModule(body []byte) []byte {
	// type 0: (struct (field (mut (ref null 0))))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x63, 0x00, 0x01})...)
	return gcFinalCastStructGetModuleWithType(structType, body)
}

func gcFinalCastStructGetBytes() []byte {
	return gcFinalCastStructGetModule([]byte{
		0xd0, 0x00, // ref.null type 0: initializer
		0xfb, 0x00, 0x00, // struct.new type 0
		0xfb, 0x16, 0x00, // ref.cast (ref type 0)
		0xfb, 0x02, 0x00, 0x00, // struct.get type 0 field 0
		0xd1, // ref.is_null
		0x0b,
	})
}

func TestGCFinalCastStructGet(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructGetBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("run = %v, want [1]", got)
	}
}

func TestGCFinalCastStructGetTrapOrder(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "nullable null reaches access",
			body: []byte{0xd0, 0x00, 0xfb, 0x17, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x0b},
			want: "null reference",
		},
		{
			name: "nonnullable null fails cast",
			body: []byte{0xd0, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x0b},
			want: "cast failure",
		},
		{
			name: "i31 fails cast",
			body: []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x0b},
			want: "cast failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructGetModule(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("run"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func gcFinalCastOnlyBytes() []byte {
	structType := []byte{0x5f, 0x00}
	return gcFinalCastStructGetModuleWithType(structType, []byte{
		0xfb, 0x01, 0x00, // struct.new_default type 0
		0xfb, 0x16, 0x00, // ref.cast (ref type 0)
		0xd1, // ref.is_null
		0x0b,
	})
}

func TestGCFinalCastOnly(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastOnlyBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("run = %v, %v; want [0]", got, err)
	}
}

func TestGCFinalCastOnlyTrapOrder(t *testing.T) {
	structType := []byte{0x5f, 0x00}
	tests := []struct {
		name string
		body []byte
		want uint64
		trap string
	}{
		{name: "nullable null succeeds", body: []byte{0xd0, 0x00, 0xfb, 0x17, 0x00, 0xd1, 0x0b}, want: 1},
		{name: "nonnullable null fails", body: []byte{0xd0, 0x00, 0xfb, 0x16, 0x00, 0xd1, 0x0b}, trap: "cast failure"},
		{name: "i31 fails", body: []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x00, 0xd1, 0x0b}, trap: "cast failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructGetModuleWithType(structType, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if tc.trap != "" {
				if err == nil || !strings.Contains(err.Error(), tc.trap) {
					t.Fatalf("run error = %v, want %q", err, tc.trap)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("run = %v, %v; want [%d]", got, err, tc.want)
			}
		})
	}
}

func BenchmarkGCFinalCastOnly(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastOnlyBytes())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("run = %v, %v; want [0]", got, err)
		}
	}
}

func gcFinalCastStructScalarGetBytes() []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01})...)
	return gcFinalCastStructGetModuleWithType(structType, []byte{
		0x41, 0x2a, // i32.const 42
		0xfb, 0x00, 0x00, // struct.new type 0
		0xfb, 0x16, 0x00, // ref.cast (ref type 0)
		0xfb, 0x02, 0x00, 0x00, // struct.get type 0 field 0
		0x0b,
	})
}

func TestGCFinalCastStructScalarGet(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructScalarGetBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("run = %v, %v; want [42]", got, err)
	}
}

func BenchmarkGCFinalCastStructScalarGet(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructScalarGetBytes())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			b.Fatalf("run = %v, %v; want [42]", got, err)
		}
	}
}

func gcFinalCastArrayLenModule(body []byte) []byte {
	structType := []byte{0x5f, 0x00}      // keep generic ref.cast helper classification live
	arrayType := []byte{0x5e, 0x7f, 0x01} // type 1: (array (mut i32))
	types := wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcs := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2)))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0)))
	return wasmtest.Module(types, funcs, exports, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func gcFinalCastArrayLenBytes() []byte {
	return gcFinalCastArrayLenModule([]byte{
		0x41, 0x03, // i32.const 3
		0xfb, 0x07, 0x01, // array.new_default type 1
		0xfb, 0x16, 0x01, // ref.cast (ref type 1)
		0xfb, 0x0f, // array.len
		0x0b,
	})
}

func TestGCFinalCastArrayLen(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("run = %v, %v; want [3]", got, err)
	}
}

func TestGCFinalCastArrayLenPreservesPinnedLocals(t *testing.T) {
	structType := []byte{0x5f, 0x00}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	types := wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(params, []wasm.ValType{wasm.I32})))
	funcs := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2)))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0)))
	body := []byte{
		0x41, 0x05, 0x21, 0x04, // local 4 = 5
		0x41, 0x06, 0x21, 0x05, // local 5 = 6
		0x41, 0x07, 0x21, 0x06, // local 6 = 7
		0x41, 0x03,
		0xfb, 0x07, 0x01, // array.new_default type 1
		0xfb, 0x16, 0x01, // ref.cast (ref type 1)
		0xfb, 0x0f, // array.len
	}
	for i := byte(0); i < 7; i++ {
		body = append(body, 0x20, i, 0x6a) // local.get i; i32.add
	}
	body = append(body, 0x0b)
	fn := append([]byte{0x01, 0x03, 0x7f}, body...) // one local run: 3 × i32
	code := wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(fn))), fn...)))
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(types, funcs, exports, code))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 1, 2, 3, 4); err != nil || len(got) != 1 || got[0] != 31 {
		t.Fatalf("run = %v, %v; want [31]", got, err)
	}
}

func TestGCFinalCastArrayLenTrapOrder(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "nullable null reaches length", body: []byte{0xd0, 0x01, 0xfb, 0x17, 0x01, 0xfb, 0x0f, 0x0b}, want: "null reference"},
		{name: "nonnullable null fails cast", body: []byte{0xd0, 0x01, 0xfb, 0x16, 0x01, 0xfb, 0x0f, 0x0b}, want: "cast failure"},
		{name: "i31 fails cast", body: []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x01, 0xfb, 0x0f, 0x0b}, want: "cast failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenModule(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("run"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func BenchmarkGCFinalCastArrayLen(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenBytes())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 3 {
			b.Fatalf("run = %v, %v; want [3]", got, err)
		}
	}
}

func BenchmarkGCFinalCastStructGet(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructGetBytes())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := instance.Invoke("run")
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != 1 || got[0] != 1 {
			b.Fatalf("run = %v, want [1]", got)
		}
	}
}
