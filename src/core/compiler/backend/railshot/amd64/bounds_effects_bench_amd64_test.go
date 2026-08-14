//go:build (linux || darwin) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkBoundsCertificateAcrossSafeCalls(b *testing.B) {
	caller := make([]byte, 0, 16*9+8)
	for range 16 {
		caller = append(caller, 0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, 0x10, 0x01)
	}
	caller = append(caller, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code([]byte{0x0b}))),
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
			cm, compileErr := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"call-effect-bounds": tc.on, "inline": false}})
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
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if callErr := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); callErr != nil {
					b.Fatal(callErr)
				}
			}
		})
	}
}
