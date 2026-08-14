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

func preparedMixedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.F64, wasm.I64, wasm.F64}, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I64, wasm.F32, wasm.I32}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fp", 0, 0), wasmtest.ExportEntry("gp", 0, 1), wasmtest.ExportEntry("trap", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0x20, 0x00, 0xb9, // f64.convert_i64_s(g0)
				0x20, 0x01, 0xa0, // + f0
				0x20, 0x02, 0xb9, 0xa0, // + f64.convert_i64_s(g1)
				0x20, 0x03, 0xa0, // + f1
				0x0b,
			}),
			wasmtest.Code([]byte{
				0x20, 0x01, // g0
				0x20, 0x03, 0xad, // i64.extend_i32_u(g1)
				0x7c, 0x0b, // i64.add
			}),
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x6d, 0x0b}),
		)),
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

func TestPreparedDirectMixed(t *testing.T) {
	if !preparedDirectFPSupported {
		t.Skip("architecture does not support direct prepared mixed entry")
	}
	in, err := Instantiate(MustCompile(preparedMixedModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fp, err := in.PrepareFunction("fp")
	if err != nil {
		t.Fatalf("prepare fp: %v", err)
	}
	if !fp.directMixedFast || !fp.directMixedResultFP || fp.directFPFast || fp.directIntFast {
		t.Fatalf("fp mixed/result selection = %v/%v fp=%v int=%v", fp.directMixedFast, fp.directMixedResultFP, fp.directFPFast, fp.directIntFast)
	}
	got, err := fp.Invoke(I64(3), F64(1.25), I64(5), F64(2.5))
	if err != nil || len(got) != 1 || math.Float64bits(AsF64(got[0])) != math.Float64bits(11.75) {
		t.Fatalf("mixed fp result = %v, %v; want 11.75", got, err)
	}

	gp, err := in.PrepareFunction("gp")
	if err != nil {
		t.Fatalf("prepare gp: %v", err)
	}
	if !gp.directMixedFast || gp.directMixedResultFP || gp.directFPFast || gp.directIntFast {
		t.Fatalf("gp mixed/result selection = %v/%v fp=%v int=%v", gp.directMixedFast, gp.directMixedResultFP, gp.directFPFast, gp.directIntFast)
	}
	got, err = gp.Invoke(F64(-99), I64(1<<40), F32(17), I32(7))
	if err != nil || len(got) != 1 || got[0] != (1<<40)+7 {
		t.Fatalf("mixed GP result = %v, %v; want %d", got, err, uint64(1<<40)+7)
	}

	trap, err := in.PrepareFunction("trap")
	if err != nil {
		t.Fatalf("prepare trap: %v", err)
	}
	if !trap.directMixedFast || trap.directMixedResultFP {
		t.Fatalf("trap mixed/result selection = %v/%v", trap.directMixedFast, trap.directMixedResultFP)
	}
	if _, err := trap.Invoke(F64(123), I32(7), I32(0)); err == nil {
		t.Fatal("mixed division by zero did not trap")
	}
	got, err = trap.Invoke(F64(123), I32(8), I32(2))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 4 {
		t.Fatalf("mixed call after trap = %v, %v; want 4", got, err)
	}
}

func TestPreparedDirectMixedSignatureBounds(t *testing.T) {
	valid := FuncSig{Params: []ValType{ValI64, ValF64, ValI32, ValF32}, Results: []ValType{ValF64}}
	if resultFP, ok := preparedDirectMixedSignature(valid); !ok || !resultFP {
		t.Fatalf("valid mixed signature = resultFP %v, ok %v", resultFP, ok)
	}
	for _, sig := range []FuncSig{
		{Params: []ValType{ValI64, ValI64, ValI64, ValF64}},
		{Params: []ValType{ValI64, ValI32}},
		{Params: []ValType{ValF64, ValF32}},
		{Params: []ValType{ValI64, ValV128}},
		{Params: []ValType{ValI64, ValF64}, Results: []ValType{ValI32, ValI32}},
	} {
		if _, ok := preparedDirectMixedSignature(sig); ok {
			t.Fatalf("near-miss mixed signature admitted: %+v", sig)
		}
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

func BenchmarkPreparedDirectMixed(b *testing.B) {
	if !preparedDirectFPSupported {
		b.Skip("architecture does not support direct prepared mixed entry")
	}
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), preparedMixedModule())
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
			fn, err := in.PrepareFunction("fp")
			if err != nil {
				b.Fatal(err)
			}
			if fn.directMixedFast != direct {
				b.Fatalf("direct selection = %v, want %v", fn.directMixedFast, direct)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fn.Invoke(I64(3), F64(1.25), I64(5), F64(2.5)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
