//go:build linux && amd64

// This standalone probe also runs under TinyGo without compiling the standard-Go
// backend test package. It explicitly selects SSE2 code generation.
package main

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
)

func main() {
	m, err := wasm.DecodeModule([]byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 6, 1, 96, 1, 124, 1, 124, 3, 2, 1, 0, 10, 7, 1, 5, 0, 32, 0, 158, 11})
	if err != nil {
		panic(err)
	}
	cm, err := amd64.CompileModuleWith(m, amd64.CompileOptions{AMD64FeaturesSet: true})
	if err != nil {
		panic(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		panic(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		panic(err)
	}
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		panic(err)
	}
	defer arena.Close()
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		panic(err)
	}
	defer runtime.Unmap(mem)
	args, out, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(runtime.TrapBufferBytes)
	for _, v := range []float64{math.Copysign(0, -1), -0.5, 0.5, 1.5, 2.5, -2.5, math.Inf(1), math.Inf(-1)} {
		binary.LittleEndian.PutUint64(args, math.Float64bits(v))
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err != nil {
			panic(err)
		}
		if binary.LittleEndian.Uint64(out) != math.Float64bits(math.RoundToEven(v)) {
			panic("incorrect baseline rounding")
		}
	}
	println("forced-SSE2 f64 rounding passed")
	checkSIMD()
}

func checkVector(sub uint32, left, right, want [16]byte, unary bool) {
	params := []wasm.ValType{wasm.V128, wasm.V128}
	body := []byte{0x20, 0, 0x20, 1}
	if unary {
		params = params[:1]
		body = body[:2]
	}
	body = append(body, 0xfd)
	body = append(body, wasmtest.ULEB(sub)...)
	body = append(body, 0x0b)
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.V128}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	m, err := wasm.DecodeModule(module)
	if err != nil {
		panic(err)
	}
	cm, err := amd64.CompileModuleWith(m, amd64.CompileOptions{AMD64FeaturesSet: true})
	if err != nil {
		panic(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if cm.RequiredAMD64Features != 0 {
		panic("baseline vector has optional requirements")
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		panic(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		panic(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		panic(err)
	}
	defer ar.Close()
	mapped, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		panic(err)
	}
	defer runtime.Unmap(mapped)
	args, out, trap := ar.Alloc(32), ar.Alloc(16), ar.Alloc(runtime.TrapBufferBytes)
	copy(args, left[:])
	copy(args[16:], right[:])
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err != nil {
		panic(err)
	}
	for i, v := range want {
		if out[i] != v {
			panic("incorrect baseline SIMD result")
		}
	}
}

func checkSIMD() {
	var a, b, want [16]byte
	for i := 0; i < 4; i++ {
		x, y := uint32(0x80000001)+uint32(i), uint32(65537+i)
		binary.LittleEndian.PutUint32(a[i*4:], x)
		binary.LittleEndian.PutUint32(b[i*4:], y)
		binary.LittleEndian.PutUint32(want[i*4:], x*y)
	}
	checkVector(181, a, b, want, false)
	for i := 0; i < 8; i++ {
		x, y := int16(-32768+i), int16(-32768+i*127)
		binary.LittleEndian.PutUint16(a[i*2:], uint16(x))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(y))
		binary.LittleEndian.PutUint16(want[i*2:], uint16((int32(x)*int32(y)+16384)>>15))
	}
	checkVector(273, a, b, want, false)
	for i := 0; i < 16; i++ {
		a[i] = byte(i + 1)
		b[i] = byte(i * 17)
		want[i] = 0
		if b[i] < 16 {
			want[i] = a[b[i]]
		}
	}
	checkVector(14, a, b, want, false)
	binary.LittleEndian.PutUint64(a[:], math.Float64bits(2.5))
	binary.LittleEndian.PutUint64(a[8:], math.Float64bits(-2.5))
	binary.LittleEndian.PutUint64(want[:], math.Float64bits(2))
	binary.LittleEndian.PutUint64(want[8:], math.Float64bits(-2))
	checkVector(148, a, b, want, true)
	println("forced-SSE2 SIMD multiply, relaxed q15, swizzle and packed rounding passed")
}
