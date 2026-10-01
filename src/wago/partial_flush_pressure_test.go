//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func partialFlushPressureModule(count int, f32 bool) []byte {
	body := []byte{0, 0x42, 53} // lazy i64 prefix, before every eager floating-point value
	typ := wasm.F64
	for i := 0; i < count; i++ {
		value := float64(i + 2)
		if f32 {
			typ = wasm.F32
			body = binary.LittleEndian.AppendUint32(append(body, 0x43), math.Float32bits(float32(value*value)))
			body = append(body, 0x91) // f32.sqrt
		} else {
			body = binary.LittleEndian.AppendUint64(append(body, 0x44), math.Float64bits(value*value))
			body = append(body, 0x9f) // f64.sqrt
		}
	}
	// A fused condition flushes only the prefix. Under pressure a later float
	// already occupies slot0, which must survive the earlier i64's canonical store.
	body = append(body, 0x20, 0, 0x45, 0x04, 0x40, 0x0b)
	for i := 1; i < count; i++ {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64, typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestPartialFlushPreservesSpilledFloatPrefix(t *testing.T) {
	for _, f32 := range []bool{false, true} {
		for _, count := range []int{1, 14, 17, 33} {
			for _, flags := range []bool{false, true} {
				t.Run(fmt.Sprintf("f32=%t/values=%d/flags=%t", f32, count, flags), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithOptimization("st-flags", flags)
					c, err := Compile(cfg, partialFlushPressureModule(count, f32))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					want := math.Float64bits(2)
					if f32 {
						want = uint64(math.Float32bits(2))
					}
					for _, condition := range []uint64{0, 1} {
						got, err := in.Invoke("run", condition)
						if err != nil || len(got) != 2 || got[0] != 53 || got[1] != want {
							t.Errorf("condition=%d: got %x, %v; want [35 %x]", condition, got, err, want)
						}
					}
				})
			}
		}
	}
}

func BenchmarkCompilePartialFlushPressure(b *testing.B) {
	data := partialFlushPressureModule(33, false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c, err := Compile(nil, data)
		if err != nil {
			b.Fatal(err)
		}
		c.Close()
	}
}
