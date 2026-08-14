//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkMemoryLeafScalarABI(b *testing.B) {
	i32 := []wasm.ValType{wasm.I32}
	caller := []byte{0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00}
	for range 16 {
		caller = append(caller, 0x20, 0x00, 0x10, 0x01, 0x1a)
	}
	caller = append(caller, 0x20, 0x00, 0x0b)
	callee := []byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(callee))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		on   bool
	}{{"off", false}, {"on", true}} {
		b.Run(tc.name, func(b *testing.B) {
			cm, compileErr := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"abi-classes": tc.on, "inline": false}})
			if compileErr != nil {
				b.Fatal(compileErr)
			}
			eng, engineErr := coreruntime.NewEngine()
			if engineErr != nil {
				b.Fatal(engineErr)
			}
			defer eng.Close()
			jm, memoryErr := coreruntime.NewJobMemory(65536)
			if memoryErr != nil {
				b.Fatal(memoryErr)
			}
			defer jm.Close()
			arena, arenaErr := coreruntime.NewArena(4096)
			if arenaErr != nil {
				b.Fatal(arenaErr)
			}
			defer arena.Close()
			code, entry, mapErr := coreruntime.MapCode(cm.Code)
			if mapErr != nil {
				b.Fatal(mapErr)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			args[0] = 7
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if callErr := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); callErr != nil {
					b.Fatal(callErr)
				}
			}
		})
	}
}
