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

func preparedMixedResultModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.F64}, []wasm.ValType{wasm.I64, wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.I32}, []wasm.ValType{wasm.F32, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.F64}, []wasm.ValType{wasm.I32, wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("gp-first", 0, 0),
			wasmtest.ExportEntry("fp-first", 0, 1),
			wasmtest.ExportEntry("trap-two", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x20, 0x02, 0x0b}),
		)),
	)
}

func preparedSameBankResultModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F32}, []wasm.ValType{wasm.F64, wasm.F32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("int-pair", 0, 0),
			wasmtest.ExportEntry("fp-pair", 0, 1),
			wasmtest.ExportEntry("trap-pair", 0, 2),
			wasmtest.ExportEntry("three", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x6d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x00, 0x20, 0x00, 0x0b}),
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
		{Params: []ValType{ValI64, ValF64}, Results: []ValType{ValI64, ValF64}},
		{Params: []ValType{ValI32}, Results: []ValType{ValF32, ValI32}},
		{Results: []ValType{ValI64, ValF64}},
	} {
		if _, ok := preparedDirectMixedSignature(sig); !ok {
			t.Fatalf("mixed-result signature rejected: %+v", sig)
		}
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

func TestPreparedDirectMixedResults(t *testing.T) {
	if !preparedDirectFPSupported {
		t.Skip("architecture does not support direct prepared mixed entry")
	}
	compiled := MustCompile(preparedMixedResultModule())
	for i := range 3 {
		if !compiled.directPreparedAt(i) {
			t.Fatalf("function %d did not retain direct prepared metadata", i)
		}
	}
	disabled, err := Compile(NewRuntimeConfig().WithOptimizations(map[string]bool{"prepared-fp-entry": false}), preparedMixedResultModule())
	if err != nil {
		t.Fatal(err)
	}
	if disabled.directPreparedAt(0) || disabled.directPreparedAt(1) || disabled.directPreparedAt(2) {
		t.Fatal("disabled compiler policy retained mixed-result direct metadata")
	}
	if len(compiled.code) != len(disabled.code) {
		t.Fatalf("mixed-result admission changed generated code size: enabled=%d disabled=%d", len(compiled.code), len(disabled.code))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	gpFirst, err := in.PrepareFunction("gp-first")
	if err != nil {
		t.Fatal(err)
	}
	if !gpFirst.directMixedFast || gpFirst.scalarFast {
		t.Fatalf("gp-first direct/scalar = %v/%v, want true/false", gpFirst.directMixedFast, gpFirst.scalarFast)
	}
	got, err := gpFirst.Invoke(I64(1<<50|7), F64(-3.25))
	if err != nil || len(got) != 2 || AsI64(got[0]) != (1<<50|7) || math.Float64bits(AsF64(got[1])) != math.Float64bits(-3.25) {
		t.Fatalf("gp-first = %v, %v", got, err)
	}

	fpFirst, err := in.PrepareFunction("fp-first")
	if err != nil {
		t.Fatal(err)
	}
	got, err = fpFirst.Invoke(F32(1.5), uint64(0xffffffff00000000)|19)
	if err != nil || len(got) != 2 || math.Float32bits(AsF32(got[0])) != math.Float32bits(1.5) || got[1] != 19 {
		t.Fatalf("fp-first = %v, %v", got, err)
	}

	trapTwo, err := in.PrepareFunction("trap-two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trapTwo.Invoke(I32(8), I32(0), F64(1.25)); err == nil {
		t.Fatal("two-result division by zero did not trap")
	}
	got, err = trapTwo.Invoke(I32(8), I32(2), F64(1.25))
	if err != nil || len(got) != 2 || AsI32(got[0]) != 4 || math.Float64bits(AsF64(got[1])) != math.Float64bits(1.25) {
		t.Fatalf("two-result call after trap = %v, %v", got, err)
	}

	saved := preparedDirectFPEnabled
	preparedDirectFPEnabled = false
	t.Cleanup(func() { preparedDirectFPEnabled = saved })
	fallback, err := in.PrepareFunction("gp-first")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.directMixedFast {
		t.Fatal("disabled direct mixed option retained two-result fast path")
	}
	got, err = fallback.Invoke(I64(99), F64(2.75))
	if err != nil || len(got) != 2 || AsI64(got[0]) != 99 || math.Float64bits(AsF64(got[1])) != math.Float64bits(2.75) {
		t.Fatalf("fallback = %v, %v", got, err)
	}
}

func TestPreparedDirectSameBankResults(t *testing.T) {
	if !preparedDirectFPSupported || !preparedDirectIntSupported {
		t.Skip("architecture does not support direct prepared register entry")
	}
	module := preparedSameBankResultModule()
	compiled := MustCompile(module)
	for _, i := range []int{0, 1, 2} {
		if !compiled.directPreparedAt(i) {
			t.Fatalf("function %d did not retain direct prepared metadata", i)
		}
	}
	if compiled.directPreparedAt(3) {
		t.Fatal("three-result near miss retained direct prepared metadata")
	}
	withoutInt, err := Compile(NewRuntimeConfig().WithOptimizations(map[string]bool{"reg-abi": false}), module)
	if err != nil {
		t.Fatal(err)
	}
	if withoutInt.directPreparedAt(0) || withoutInt.directPreparedAt(2) {
		t.Fatal("disabled register ABI retained integer-pair direct metadata")
	}
	withoutFP, err := Compile(NewRuntimeConfig().WithOptimizations(map[string]bool{"prepared-fp-entry": false}), module)
	if err != nil {
		t.Fatal(err)
	}
	if withoutFP.directPreparedAt(1) {
		t.Fatal("disabled prepared FP policy retained FP-pair direct metadata")
	}
	if len(compiled.code) != len(withoutFP.code) {
		t.Fatalf("FP-pair admission changed generated code size: enabled=%d disabled=%d", len(compiled.code), len(withoutFP.code))
	}

	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	intPair, err := in.PrepareFunction("int-pair")
	if err != nil {
		t.Fatal(err)
	}
	if !intPair.directPairFast || intPair.directPairFP || intPair.scalarFast {
		t.Fatalf("int-pair direct/fp/scalar = %v/%v/%v, want true/false/false", intPair.directPairFast, intPair.directPairFP, intPair.scalarFast)
	}
	got, err := intPair.Invoke(I64(1<<50|7), uint64(0xffffffff00000000)|19)
	if err != nil || len(got) != 2 || AsI64(got[0]) != (1<<50|7) || got[1] != 19 {
		t.Fatalf("int-pair = %v, %v", got, err)
	}

	fpPair, err := in.PrepareFunction("fp-pair")
	if err != nil {
		t.Fatal(err)
	}
	if !fpPair.directPairFast || !fpPair.directPairFP || fpPair.scalarFast {
		t.Fatalf("fp-pair direct/fp/scalar = %v/%v/%v, want true/true/false", fpPair.directPairFast, fpPair.directPairFP, fpPair.scalarFast)
	}
	got, err = fpPair.Invoke(F64(-3.25), uint64(0xffffffff00000000)|F32(1.5))
	if err != nil || len(got) != 2 || math.Float64bits(AsF64(got[0])) != math.Float64bits(-3.25) || got[1] != F32(1.5) {
		t.Fatalf("fp-pair = %v, %v", got, err)
	}

	trapPair, err := in.PrepareFunction("trap-pair")
	if err != nil {
		t.Fatal(err)
	}
	if !trapPair.directPairFast || trapPair.directPairFP {
		t.Fatal("trap-pair did not select direct integer entry")
	}
	if _, err := trapPair.Invoke(I32(9), I32(8), I32(0)); err == nil {
		t.Fatal("integer-pair division by zero did not trap")
	}
	got, err = trapPair.Invoke(I32(9), I32(8), I32(2))
	if err != nil || len(got) != 2 || AsI32(got[0]) != 9 || AsI32(got[1]) != 4 {
		t.Fatalf("integer-pair call after trap = %v, %v", got, err)
	}

	savedInt, savedFP := preparedDirectIntEnabled, preparedDirectFPEnabled
	preparedDirectIntEnabled, preparedDirectFPEnabled = false, false
	t.Cleanup(func() {
		preparedDirectIntEnabled, preparedDirectFPEnabled = savedInt, savedFP
	})
	fallbackInt, err := in.PrepareFunction("int-pair")
	if err != nil {
		t.Fatal(err)
	}
	fallbackFP, err := in.PrepareFunction("fp-pair")
	if err != nil {
		t.Fatal(err)
	}
	if fallbackInt.directPairFast || fallbackFP.directPairFast {
		t.Fatal("disabled runtime policy retained same-bank fast path")
	}
	got, err = fallbackInt.Invoke(I64(99), I32(7))
	if err != nil || len(got) != 2 || AsI64(got[0]) != 99 || AsI32(got[1]) != 7 {
		t.Fatalf("integer fallback = %v, %v", got, err)
	}
}

func BenchmarkPreparedDirectSameBankResults(b *testing.B) {
	if !preparedDirectFPSupported || !preparedDirectIntSupported {
		b.Skip("architecture does not support direct prepared register entry")
	}
	compiled := MustCompile(preparedSameBankResultModule())
	bench := func(b *testing.B, export string, args []uint64, intBank, direct bool) {
		savedInt, savedFP := preparedDirectIntEnabled, preparedDirectFPEnabled
		if intBank {
			preparedDirectIntEnabled = direct
		} else {
			preparedDirectFPEnabled = direct
		}
		defer func() {
			preparedDirectIntEnabled, preparedDirectFPEnabled = savedInt, savedFP
		}()
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		defer in.Close()
		fn, err := in.PrepareFunction(export)
		if err != nil {
			b.Fatal(err)
		}
		if fn.directPairFast != direct || direct && fn.directPairFP == intBank {
			b.Fatalf("direct pair/fp selection = %v/%v, want direct=%v int=%v", fn.directPairFast, fn.directPairFP, direct, intBank)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := fn.Invoke(args...); err != nil {
				b.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name    string
		export  string
		args    []uint64
		intBank bool
	}{
		{name: "int", export: "int-pair", args: []uint64{I64(1 << 50), I32(19)}, intBank: true},
		{name: "fp", export: "fp-pair", args: []uint64{F64(-3.25), F32(1.5)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for _, direct := range []bool{false, true} {
				name := "general"
				if direct {
					name = "direct"
				}
				b.Run(name, func(b *testing.B) { bench(b, tc.export, tc.args, tc.intBank, direct) })
			}
		})
	}
}

func BenchmarkPreparedDirectMixedResults(b *testing.B) {
	if !preparedDirectFPSupported {
		b.Skip("architecture does not support direct prepared mixed entry")
	}
	compiled := MustCompile(preparedMixedResultModule())
	for _, direct := range []bool{false, true} {
		name := "general"
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
			fn, err := in.PrepareFunction("gp-first")
			if err != nil {
				b.Fatal(err)
			}
			args := []uint64{I64(1<<50 | 7), F64(-3.25)}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := fn.Invoke(args...); err != nil {
					b.Fatal(err)
				}
			}
		})
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
