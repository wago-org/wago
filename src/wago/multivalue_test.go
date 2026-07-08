//go:build linux && amd64

package wago

import (
	"context"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func multiValueControlCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), // pair: () -> (i32, i64)
			wasmtest.ULEB(1), // choose: (i32) -> (i32, i64)
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("pair", 0, 0),
			wasmtest.ExportEntry("choose", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x12, 0x42, 0x13, 0x0b}),                                                 // i32.const 18; i64.const 19; end
			wasmtest.Code([]byte{0x20, 0x00, 0x04, 0x00, 0x10, 0x00, 0x05, 0x41, 0x07, 0x42, 0x09, 0x0b, 0x0b}), // local.get 0; if type 0; call pair; else constants; end; end
		)),
	)
}

func multiValueBranchPayloadModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), // block_br: () -> (i32, i64)
			wasmtest.ULEB(1), // br_if_pair: (i32) -> (i32, i64)
			wasmtest.ULEB(1), // br_table_pair: (i32) -> (i32, i64)
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("block_br", 0, 0),
			wasmtest.ExportEntry("br_if_pair", 0, 1),
			wasmtest.ExportEntry("br_table_pair", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0x02, 0x00, // block type 0: () -> (i32, i64)
				0x41, 0x2a, // i32.const 42
				0x42, 0x2b, // i64.const 43
				0x0c, 0x00, // br 0 with both result values
				0x41, 0x07, // unreachable fallback keeps the block type explicit
				0x42, 0x09,
				0x0b, // end block
				0x0b, // end func
			}),
			wasmtest.Code([]byte{
				0x02, 0x00, // block type 0
				0x41, 0x15, // i32.const 21
				0x42, 0x16, // i64.const 22
				0x20, 0x00, // local.get selector
				0x0d, 0x00, // br_if 0 carrying both values when selector is non-zero
				0x1a,       // fallthrough: drop i64 branch payload
				0x1a,       // fallthrough: drop i32 branch payload
				0x41, 0x1f, // i32.const 31
				0x42, 0x20, // i64.const 32
				0x0b, // end block
				0x0b, // end func
			}),
			wasmtest.Code([]byte{
				0x02, 0x00, // outer block type 0
				0x02, 0x00, // inner block type 0
				0x41, 0x0b, // i32.const 11
				0x42, 0x0c, // i64.const 12
				0x20, 0x00, // local.get selector
				0x0e, 0x01, 0x00, 0x01, // br_table [inner] default outer, carrying both values
				0x0b,       // end inner; selector 0 lands here with the pair still on stack
				0x1a,       // selector 0: drop i64 branch payload before returning a distinct pair
				0x1a,       // selector 0: drop i32 branch payload
				0x41, 0x0d, // i32.const 13
				0x42, 0x0e, // i64.const 14
				0x0b, // end outer; default exits here directly with the original pair
				0x0b, // end func
			}),
		)),
	)
}

func multiValueFusedBrIfV128PayloadModule(takenVec, fallthroughVec V128) []byte {
	v128Const := func(v V128) []byte { return append([]byte{0xfd, 0x0c}, v[:]...) }
	body := []byte{
		0x42, 0x2d, // i64.const 45: preserved result below the block
		0x02, 0x00, // block type 0: () -> (v128, i32)
		0x41, 0x11, // extra i32 below the branch payload; must be discarded on taken br_if
	}
	body = append(body, v128Const(takenVec)...)
	body = append(body,
		0x41, 0x26, // branch payload i32
		0x20, 0x00, // local.get selector
		0x41, 0x00, // i32.const 0
		0x47,       // i32.ne; fused with br_if by railshot
		0x0d, 0x00, // br_if 0 carrying (v128, i32)
		0x1a, // fallthrough: drop payload i32
		0x1a, // fallthrough: drop payload v128
		0x1a, // fallthrough: drop extra i32
	)
	body = append(body, v128Const(fallthroughVec)...)
	body = append(body,
		0x41, 0x33, // fallback i32
		0x0b, // end block
		0x0b, // end func
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64, wasm.V128, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestMultiValueDefaultConfigControlCallsAndCodec(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureMultiValue) {
		t.Fatal("default supported features should include multi-value")
	}

	cfg := NewRuntimeConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate with multi-value enabled: %v", err)
	}

	c, err := cfg.Compile(multiValueControlCallModule())
	if err != nil {
		t.Fatalf("Compile default multi-value module: %v", err)
	}
	if cfg.BoundsChecks() != BoundsChecksSignalsBased {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		c, err = Load(blob)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	}

	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	got, err := in.Invoke("choose", I32(0))
	if err != nil {
		t.Fatalf("Invoke choose(0): %v", err)
	}
	if want := []uint64{I32(7), I64(9)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("choose(0) = %#x, want %#x", got, want)
	}
	got, err = in.Invoke("choose", I32(1))
	if err != nil {
		t.Fatalf("Invoke choose(1): %v", err)
	}
	if want := []uint64{I32(18), I64(19)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("choose(1) = %#x, want %#x", got, want)
	}
}

func TestMultiValueFusedBrIfUsesSlotWidths(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	takenVec := V128{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
	fallthroughVec := V128{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf}
	c, err := NewRuntimeConfig().Compile(multiValueFusedBrIfV128PayloadModule(takenVec, fallthroughVec))
	if err != nil {
		t.Fatalf("Compile fused br_if multi-value module: %v", err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate fused br_if multi-value module: %v", err)
	}
	defer in.Close()

	for _, tc := range []struct {
		selector int32
		vec      V128
		i32      uint64
	}{
		{0, fallthroughVec, I32(0x33)},
		{1, takenVec, I32(0x26)},
	} {
		got, err := in.Invoke("f", I32(tc.selector))
		if err != nil {
			t.Fatalf("Invoke f(%d): %v", tc.selector, err)
		}
		lo, hi := hostV128Slots(tc.vec)
		want := []uint64{I64(45), lo, hi, tc.i32}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("f(%d) = %#x, want %#x", tc.selector, got, want)
		}
	}
}

func TestMultiValueBranchPayloadsAndTypedCall(t *testing.T) {
	cfg := NewRuntimeConfig()
	c, err := cfg.Compile(multiValueBranchPayloadModule())
	if err != nil {
		t.Fatalf("Compile default multi-value module: %v", err)
	}
	if cfg.BoundsChecks() != BoundsChecksSignalsBased {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		c, err = Load(blob)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	got, err := in.Invoke("block_br")
	if err != nil {
		t.Fatalf("Invoke block_br: %v", err)
	}
	if want := []uint64{I32(42), I64(43)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("block_br = %#x, want %#x", got, want)
	}

	out, err := in.Call(context.Background(), "block_br")
	if err != nil {
		t.Fatalf("Call block_br: %v", err)
	}
	if len(out) != 2 || out[0].Type() != ValI32 || out[0].I32() != 42 || out[1].Type() != ValI64 || out[1].I64() != 43 {
		t.Fatalf("Call block_br = %v, want i32(42), i64(43)", out)
	}

	for _, tc := range []struct {
		selector int32
		want     []uint64
	}{
		{0, []uint64{I32(31), I64(32)}},
		{1, []uint64{I32(21), I64(22)}},
	} {
		got, err = in.Invoke("br_if_pair", I32(tc.selector))
		if err != nil {
			t.Fatalf("Invoke br_if_pair(%d): %v", tc.selector, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("br_if_pair(%d) = %#x, want %#x", tc.selector, got, tc.want)
		}
		out, err = in.Call(context.Background(), "br_if_pair", ValueI32(tc.selector))
		if err != nil {
			t.Fatalf("Call br_if_pair(%d): %v", tc.selector, err)
		}
		if len(out) != 2 || out[0].Type() != ValI32 || out[0].I32() != int32(tc.want[0]) || out[1].Type() != ValI64 || out[1].I64() != int64(tc.want[1]) {
			t.Fatalf("Call br_if_pair(%d) = %v, want i32(%d), i64(%d)", tc.selector, out, int32(tc.want[0]), int64(tc.want[1]))
		}
	}

	for _, tc := range []struct {
		selector int32
		want     []uint64
	}{
		{0, []uint64{I32(13), I64(14)}},
		{1, []uint64{I32(11), I64(12)}},
		{2, []uint64{I32(11), I64(12)}},
	} {
		got, err = in.Invoke("br_table_pair", I32(tc.selector))
		if err != nil {
			t.Fatalf("Invoke br_table_pair(%d): %v", tc.selector, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("br_table_pair(%d) = %#x, want %#x", tc.selector, got, tc.want)
		}
		out, err = in.Call(context.Background(), "br_table_pair", ValueI32(tc.selector))
		if err != nil {
			t.Fatalf("Call br_table_pair(%d): %v", tc.selector, err)
		}
		if len(out) != 2 || out[0].Type() != ValI32 || out[0].I32() != int32(tc.want[0]) || out[1].Type() != ValI64 || out[1].I64() != int64(tc.want[1]) {
			t.Fatalf("Call br_table_pair(%d) = %v, want i32(%d), i64(%d)", tc.selector, out, int32(tc.want[0]), int64(tc.want[1]))
		}
	}
}
