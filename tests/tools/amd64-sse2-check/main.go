//go:build linux && amd64

// This standalone probe also runs under TinyGo without compiling the standard-Go
// backend test package. It explicitly selects SSE2 code generation.
package main

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
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
}
