//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func publicScalarFloatModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.F64}, []wasm.ValType{wasm.F32, wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.F32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.F32, wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(5))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(6, []byte{2, 0x7d, 1, 0x43, 0, 0, 0, 0x80, 0x0b, 0x7c, 1, 0x44, 0, 0, 0, 0, 0, 0, 0, 0x80, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pair", 0, 0), wasmtest.ExportEntry("add", 0, 1), wasmtest.ExportEntry("trunc", 0, 2), wasmtest.ExportEntry("sat", 0, 3), wasmtest.ExportEntry("store", 0, 4), wasmtest.ExportEntry("initial", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x20, 1, 0x24, 1, 0x23, 0, 0x23, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x92, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xaa, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfc, 7, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x39, 0, 0, 0x20, 0, 0x2b, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x0b}),
		)),
	)
}

func TestPublicScalarFloatArtifactCallsStateAndTrapReuse(t *testing.T) {
	data := publicScalarFloatModule()
	c, err := Compile(artifactTestConfig(true), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.requiredFeatures.IsEnabled(CoreFeatureNonTrappingFloatToIntConversion) {
		t.Fatal("saturating conversion missing artifact feature")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	in, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	initial, err := in.Invoke("initial")
	if err != nil || !slices.Equal(initial, []uint64{0x80000000, 0x8000000000000000}) {
		t.Fatal("loaded FP literal global bits", initial, err)
	}
	for _, pair := range [][2]uint64{{0x80000000, 0x8000000000000000}, {0x7f800001, 0x7ff0000000000001}, {0xffc0dead, 0xfff80000deadbeef}} {
		args := []uint64{pair[0] | 0xdeadbeef00000000, pair[1]}
		got, err := in.Invoke("pair", args...)
		if err != nil || !slices.Equal(got, pair[:]) {
			t.Fatal("public FP raw global/return", got, pair, err)
		}
		got, err = in.Invoke("store", 0xfeedface00000001, pair[1])
		if err != nil || !slices.Equal(got, pair[1:]) {
			t.Fatal("public FP unaligned memory", got, err)
		}
	}
	want := uint64(math.Float32bits(1.0))
	args := []uint64{uint64(math.Float32bits(1)), uint64(math.Float32bits(float32(math.Ldexp(1, -24))))}
	got, err := in.Invoke("add", args...)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatal("public FP static nearest", got, err)
	}
	fn, err := in.WasmFunc("add")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH == "riscv64" && fn.directFloatFast {
		t.Fatal("RV64 fabricated register entry proof")
	}
	got, err = fn.Invoke(args...)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatal("public FP WasmFunc", got, err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	got, err = session.Invoke2(args[0], args[1])
	session.Close()
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatal("public FP session", got, err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		got, err := in.Invoke("add", args...)
		if err != nil || len(got) != 1 || got[0] != want {
			panic("public FP warm call")
		}
	}); allocations != 0 {
		t.Fatal("public FP warm allocations", allocations)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Ldexp(1, 31)} {
		_, err := in.Invoke("trunc", math.Float64bits(value))
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapTruncOverflow {
			t.Fatal("public FP conversion trap", err)
		}
		got, err := in.Invoke("trunc", math.Float64bits(-17.75))
		if err != nil || !slices.Equal(got, []uint64{0xffffffef}) {
			t.Fatal("public FP trap reuse", got, err)
		}
	}
	for _, tc := range []struct {
		value float64
		want  uint64
	}{{math.NaN(), 0}, {-1, 0}, {math.Inf(1), math.MaxUint64}, {17.75, 17}} {
		got, err := in.Invoke("sat", math.Float64bits(tc.value))
		if err != nil || !slices.Equal(got, []uint64{tc.want}) {
			t.Fatal("public trunc_sat", got, tc.want, err)
		}
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(SupportedFeatures().SetEnabled(CoreFeatureNonTrappingFloatToIntConversion, false))
	if c, err := Compile(cfg, data); err == nil || c != nil {
		t.Fatal("disabled nontrapping feature admitted", c, err)
	}
}
