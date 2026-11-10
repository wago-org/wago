//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The callee has a loop so it cannot be inlined. Its result clobbers a register
// that a call-free caller could otherwise reserve for a persistent constant.
func callPresenceFloatModule(wide bool) []byte {
	typ, result := wasm.F32, wasm.I32
	constant, multiply, reinterpret := byte(0x43), byte(0x94), byte(0xbc)
	appendConst := func(body []byte, value float64) []byte {
		body = append(body, constant)
		if wide {
			return binary.LittleEndian.AppendUint64(body, math.Float64bits(value))
		}
		return binary.LittleEndian.AppendUint32(body, math.Float32bits(float32(value)))
	}
	if wide {
		typ, result, constant, multiply, reinterpret = wasm.F64, wasm.I64, 0x44, 0xa2, 0xbd
	}
	body := []byte{0x20, 0}
	body = appendConst(body, 2)
	body = append(body, multiply, 0x21, 0, 0x10, 1, 0x1a, 0x20, 0)
	body = appendConst(body, 2)
	body = append(body, multiply, reinterpret, 0x0b)
	callee := appendConst([]byte{0x03, 0x40, 0x0b}, 3)
	callee = append(callee, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{result}), wasmtest.FuncType(nil, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(callee))),
	)
}

func TestCallPresenceFloatConstants(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			t.Run(fmt.Sprintf("f64=%v/stack-reg=%v", wide, stackReg), func(t *testing.T) {
				// Keep a real frame here to isolate constant lifetime from frame elision.
				frameElide := "frame-elide"
				if runtime.GOARCH == "arm64" {
					frameElide = "frame-elide-reghomed"
				}
				cfg := NewRuntimeConfig().WithOptimization("stack-reg", stackReg).WithOptimization(frameElide, false)
				compiled, err := Compile(cfg, callPresenceFloatModule(wide))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				for _, input := range []float64{1, 2, -3} {
					arg, want := uint64(math.Float32bits(float32(input))), uint64(math.Float32bits(float32(input*4)))
					if wide {
						arg, want = math.Float64bits(input), math.Float64bits(input*4)
					}
					got, err := instance.Invoke("run", arg)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("input %v: got %x, %v; want %x", input, got, err, want)
					}
				}
			})
		}
	}
}

func BenchmarkCallPresenceCompile(b *testing.B) {
	module := callPresenceFloatModule(true)
	b.ReportAllocs()
	for range b.N {
		compiled, err := Compile(nil, module)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}

func BenchmarkCallPresenceInvoke(b *testing.B) {
	compiled, err := Compile(nil, callPresenceFloatModule(true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	arg := math.Float64bits(2)
	got, err := instance.Invoke("run", arg)
	if err != nil || got[0] != math.Float64bits(8) {
		b.Fatalf("got %x, %v; want 8", got, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := instance.Invoke("run", arg); err != nil {
			b.Fatal(err)
		}
	}
}
