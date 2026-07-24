//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestWazeroPortManyParamsAndResults ports wazero's adversarial 100-value mixed
// GP/FP/SIMD call graph from internal/integration_test/engine/adhoc_test.go. It
// exercises 120 ABI slots per call, nested wrapper calls, reversed parameters,
// wide returns, and mixed arithmetic. Expectations are computed from WebAssembly
// value semantics, not from Wago's output.
func TestWazeroPortManyParamsAndResults(t *testing.T) {
	mod, params := wazeroManyValuesModule()
	in := instantiateWazeroPortModule(t, mod)
	defer in.Close()

	t.Run("many constants", func(t *testing.T) {
		assertWazeroWideResults(t, in, "call_many_consts", nil, wazeroManyConstResults())
	})
	t.Run("last vector survives 100-value argument staging", func(t *testing.T) {
		assertWazeroWideResults(t, in, "call_many_consts_and_pick_last_vector", nil,
			[]uint64{0x5f5f5f5f5f5f5f5f, 0x5f5f5f5f5f5f5f5f})
	})
	t.Run("swapper", func(t *testing.T) {
		assertWazeroWideResults(t, in, "swapper", params, wazeroSwappedResults(params))
	})
	t.Run("doubler", func(t *testing.T) {
		assertWazeroWideResults(t, in, "doubler", params, wazeroDoubledResults(params))
	})
	t.Run("nested main", func(t *testing.T) {
		swapped := wazeroSwappedResults(params)
		assertWazeroWideResults(t, in, "main", params, wazeroDoubledResults(swapped))
	})
}

func assertWazeroWideResults(t *testing.T, in *Instance, export string, args, want []uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s: %v", export, err)
	}
	if !reflect.DeepEqual(got, want) {
		for i := 0; i < len(got) && i < len(want); i++ {
			if got[i] != want[i] {
				t.Fatalf("%s result[%d] = %#x, want %#x (got %d slots, want %d)", export, i, got[i], want[i], len(got), len(want))
			}
		}
		t.Fatalf("%s results differ: got %d slots, want %d", export, len(got), len(want))
	}
}

func wazeroManyValuesModule() ([]byte, []uint64) {
	mainParams, mainResults := make([]wasm.ValType, 0, 100), make([]wasm.ValType, 0, 100)
	swapParams, swapResults := make([]wasm.ValType, 0, 100), make([]wasm.ValType, 0, 100)
	doubleParams, doubleResults := make([]wasm.ValType, 0, 100), make([]wasm.ValType, 0, 100)
	constResults, pickParams := make([]wasm.ValType, 0, 100), make([]wasm.ValType, 0, 100)
	for i := 0; i < 20; i++ {
		mainParams = append(mainParams, wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128)
		mainResults = append(mainResults, wasm.V128, wasm.F64, wasm.F32, wasm.I64, wasm.I32)
		swapParams = append(swapParams, wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128)
		swapResults = append(swapResults, wasm.V128, wasm.F64, wasm.F32, wasm.I64, wasm.I32)
		doubleParams = append(doubleParams, wasm.V128, wasm.F64, wasm.F32, wasm.I64, wasm.I32)
		doubleResults = append(doubleResults, wasm.V128, wasm.F64, wasm.F32, wasm.I64, wasm.I32)
		constResults = append(constResults, wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128)
		pickParams = append(pickParams, wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128)
	}

	var mainBody []byte
	for i := 0; i < 100; i++ {
		mainBody = append(mainBody, 0x20)
		mainBody = append(mainBody, wasmtest.ULEB(uint32(i))...)
	}
	mainBody = append(mainBody, 0x10, 0x01, 0x10, 0x02, 0x0b)

	var swapBody []byte
	for i := 99; i >= 0; i-- {
		swapBody = append(swapBody, 0x20)
		swapBody = append(swapBody, wasmtest.ULEB(uint32(i))...)
	}
	swapBody = append(swapBody, 0x0b)

	var doubleBody []byte
	for i := 0; i < 100; i += 5 {
		doubleBody = append(doubleBody, 0x20)
		doubleBody = append(doubleBody, wasmtest.ULEB(uint32(i))...)
		for j, op := range []byte{0xa0, 0x92, 0x7c, 0x6a} { // f64.add, f32.add, i64.add, i32.add
			idx := uint32(i + j + 1)
			doubleBody = append(doubleBody, 0x20)
			doubleBody = append(doubleBody, wasmtest.ULEB(idx)...)
			doubleBody = append(doubleBody, 0x20)
			doubleBody = append(doubleBody, wasmtest.ULEB(idx)...)
			doubleBody = append(doubleBody, op)
		}
	}
	doubleBody = append(doubleBody, 0x0b)

	var constantsBody []byte
	for i := 0; i < 100; i += 5 {
		b := byte(i)
		constantsBody = append(constantsBody, 0x41)
		constantsBody = append(constantsBody, wasmtest.SLEB32(int32(i))...)
		constantsBody = append(constantsBody, 0x42)
		constantsBody = append(constantsBody, wasmtest.SLEB64(int64(i))...)
		constantsBody = append(constantsBody, 0x43, b, b, b, b)
		constantsBody = append(constantsBody, 0x44, b, b, b, b, b, b, b, b)
		constantsBody = append(constantsBody, 0xfd, 0x0c)
		for j := 0; j < 16; j++ {
			constantsBody = append(constantsBody, b)
		}
	}
	constantsBody = append(constantsBody, 0x0b)

	types := [][]byte{
		wasmtest.FuncType(mainParams, mainResults),
		wasmtest.FuncType(swapParams, swapResults),
		wasmtest.FuncType(doubleParams, doubleResults),
		wasmtest.FuncType(nil, constResults),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		wasmtest.FuncType(nil, constResults),
		wasmtest.FuncType(pickParams, []wasm.ValType{wasm.V128}),
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3),
			wasmtest.ULEB(4), wasmtest.ULEB(5), wasmtest.ULEB(6),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("main", 0, 0),
			wasmtest.ExportEntry("swapper", 0, 1),
			wasmtest.ExportEntry("doubler", 0, 2),
			wasmtest.ExportEntry("call_many_consts", 0, 3),
			wasmtest.ExportEntry("call_many_consts_and_pick_last_vector", 0, 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(mainBody),
			wasmtest.Code(swapBody),
			wasmtest.Code(doubleBody),
			wasmtest.Code([]byte{0x10, 0x05, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x05, 0x10, 0x06, 0x0b}),
			wasmtest.Code(constantsBody),
			wasmtest.Code([]byte{0x20, 0x63, 0x0b}),
		)),
	)

	params := make([]uint64, 0, 120)
	for i := uint64(0); i < 100; i += 5 {
		params = append(params, i, i+1, i+2, i+3, i+3, i+3)
	}
	return mod, params
}

func wazeroManyConstResults() []uint64 {
	out := make([]uint64, 0, 120)
	for i := byte(0); i < 100; i += 5 {
		word32 := uint64(i) * 0x01010101
		word64 := uint64(i) * 0x0101010101010101
		out = append(out, uint64(i), uint64(i), word32, word64, word64, word64)
	}
	return out
}

func wazeroSwappedResults(params []uint64) []uint64 {
	out := make([]uint64, 0, len(params))
	for group := 19; group >= 0; group-- {
		base := group * 6
		out = append(out,
			params[base+4], params[base+5], // v128
			params[base+3], // f64
			params[base+2], // f32
			params[base+1], // i64
			params[base],   // i32
		)
	}
	return out
}

func wazeroDoubledResults(params []uint64) []uint64 {
	out := make([]uint64, 0, len(params))
	for group := 0; group < 20; group++ {
		base := group * 6
		f64 := math.Float64frombits(params[base+2])
		f32 := math.Float32frombits(uint32(params[base+3]))
		out = append(out,
			params[base], params[base+1],
			math.Float64bits(f64+f64),
			uint64(math.Float32bits(f32+f32)),
			params[base+4]+params[base+4],
			uint64(uint32(params[base+5])+uint32(params[base+5])),
		)
	}
	return out
}
