//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func callfreeFloatMergeModule(wide bool, condition int) []byte {
	typ, result, constant, add, reinterpret := byte(0x7d), wasm.I32, byte(0x43), byte(0x92), byte(0xbc)
	if wide {
		typ, result, constant, add, reinterpret = 0x7c, wasm.I64, 0x44, 0xa0, 0xbd
	}
	addFloat := func(body []byte, v float64) []byte {
		body = append(body, constant)
		if wide {
			return binary.LittleEndian.AppendUint64(body, math.Float64bits(v))
		}
		return binary.LittleEndian.AppendUint32(body, math.Float32bits(float32(v)))
	}
	var body []byte
	for i := byte(0); i < 12; i++ {
		body = addFloat(body, float64(i+1))
		body = append(body, 0x21, i)
	}
	body = append(body, 0x20, 0)
	for i := byte(1); i < 12; i++ {
		body = append(body, 0x20, i, add)
	}
	body = append(body, 0x1a)
	// Two scalar and two vector constants occupy the four non-local XMMs.
	for i := uint64(1); i <= 2; i++ {
		body = append(body, 0xfd, 12)
		body = binary.LittleEndian.AppendUint64(body, i+2)
		body = binary.LittleEndian.AppendUint64(body, i+4)
		body = append(body, 0x1a)
	}
	if condition >= 0 {
		body = append(body, 0x41, byte(condition), 0x04, typ) // if (result float)
	} else {
		body = append(body, 0x02, typ) // block (result float)
	}
	body = addFloat(body, 100)
	if condition >= 0 {
		body = append(body, 0x05)
		body = addFloat(body, 200)
	}
	body = append(body, 0x0b, reinterpret, 0x0b)
	code := append([]byte{1, 12, typ}, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
}

func TestCallFreeFloatMergePreservesResult(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, condition := range []int{-1, 0, 1} {
			for _, regMerge := range []bool{false, true} {
				t.Run(fmt.Sprintf("f64=%v/condition=%d/reg-merge=%v", wide, condition, regMerge), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithOptimizations(map[string]bool{
						"reg-merge": regMerge, "ext-fp-pins": true,
						"v128-const-cache": true, "frame-elide": false,
					})
					compiled, err := Compile(cfg, callfreeFloatMergeModule(wide, condition))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					instance, err := Instantiate(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					want := float64(100)
					if condition == 0 {
						want = 200
					}
					bits := uint64(math.Float32bits(float32(want)))
					if wide {
						bits = math.Float64bits(want)
					}
					for i := 0; i < 3; i++ {
						got, err := instance.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != bits {
							t.Fatalf("got %x, %v; want %g (%x)", got, err, want, bits)
						}
					}
				})
			}
		}
	}
}
