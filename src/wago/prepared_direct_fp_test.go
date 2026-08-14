package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func preparedFPAddModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xa0, 0x0b}))),
	)
}

func TestPreparedDirectFP(t *testing.T) {
	if !preparedDirectFPSupported {
		t.Skip("architecture does not support direct prepared FP entry")
	}
	config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, preparedFPAddModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("f64 add did not retain direct prepared metadata")
	}
	disabled, err := Compile(config.WithOptimizations(map[string]bool{"prepared-fp-entry": false}), preparedFPAddModule())
	if err != nil {
		t.Fatalf("compile disabled: %v", err)
	}
	if disabled.directPreparedAt(0) {
		t.Fatal("disabled prepared FP compiler option retained direct metadata")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directFPFast || !fn.isolatedFast || fn.directIntFast {
		t.Fatalf("direct FP/isolated/int selection = %v/%v/%v, want true/true/false", fn.directFPFast, fn.isolatedFast, fn.directIntFast)
	}
	got, err := fn.Invoke(F64(1.25), F64(2.5))
	if err != nil || len(got) != 1 || math.Float64bits(AsF64(got[0])) != math.Float64bits(3.75) {
		t.Fatalf("direct f64 add = %v, %v; want 3.75", got, err)
	}

	saved := preparedDirectFPEnabled
	preparedDirectFPEnabled = false
	t.Cleanup(func() { preparedDirectFPEnabled = saved })
	fallback, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare fallback: %v", err)
	}
	if fallback.directFPFast {
		t.Fatal("disabled direct FP option still selected the trampoline")
	}
	got, err = fallback.Invoke(F64(1.25), F64(2.5))
	if err != nil || len(got) != 1 || math.Float64bits(AsF64(got[0])) != math.Float64bits(3.75) {
		t.Fatalf("fallback f64 add = %v, %v; want 3.75", got, err)
	}
}

func TestPreparedDirectF32Bits(t *testing.T) {
	if !preparedDirectFPSupported {
		t.Skip("architecture does not support direct prepared FP entry")
	}
	add := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x92, 0x0b}))),
	)
	in, err := Instantiate(MustCompile(add), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directFPFast {
		t.Fatal("f32 add did not select direct FP entry")
	}
	got, err := fn.Invoke(F32(1.25), F32(2.5))
	if err != nil || len(got) != 1 || math.Float32bits(AsF32(got[0])) != math.Float32bits(3.75) {
		t.Fatalf("direct f32 add = %v, %v; want 3.75", got, err)
	}
}

func BenchmarkPreparedDirectFP(b *testing.B) {
	if !preparedDirectFPSupported {
		b.Skip("architecture does not support direct prepared FP entry")
	}
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), preparedFPAddModule())
	if err != nil {
		b.Fatal(err)
	}
	for _, direct := range []bool{false, true} {
		name := "scalar"
		if direct {
			name = "direct"
		}
		b.Run(name, func(b *testing.B) {
			saved := preparedDirectFPEnabled
			preparedDirectFPEnabled = direct
			defer func() { preparedDirectFPEnabled = saved }()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("add")
			if err != nil {
				b.Fatal(err)
			}
			if fn.directFPFast != direct {
				b.Fatalf("direct selection = %v, want %v", fn.directFPFast, direct)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fn.Invoke(F64(1.25), F64(2.5)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPreparedDirectFPCompile(b *testing.B) {
	if !preparedDirectFPSupported {
		b.Skip("architecture does not support direct prepared FP entry")
	}
	module := preparedFPAddModule()
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimizations(map[string]bool{"prepared-fp-entry": enabled})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Compile(config, module); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
