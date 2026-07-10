package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func benchClassResetModule(memoryPages uint32) []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b})
	body := []byte{
		0x23, 0x00, // global.get 0
		0x41, 0x01, // i32.const 1
		0x6a,       // i32.add
		0x24, 0x00, // global.set 0
		0x23, 0x00, // global.get 0
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	if memoryPages != 0 {
		memory := append([]byte{0x00}, wasmtest.ULEB(memoryPages)...)
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memory)))
		body = append([]byte{0x41, 0x00, 0x41, 0x01, 0x3a, 0x00, 0x00}, body...) // memory[0] = 1
	}
	sections = append(sections,
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(body, 0x0b)))),
	)
	return wasmtest.Module(sections...)
}

func benchmarkClassAcquireRelease(b *testing.B, policy ResetPolicy, memoryPages uint32) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	mod, err := rt.Compile(benchClassResetModule(memoryPages))
	if err != nil {
		b.Fatal(err)
	}
	class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{
		MinInstances: 1,
		MaxInstances: 1,
		Reset:        policy,
	}})
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		_ = class.Close()
		_ = rt.Close()
	}()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lease, err := class.Acquire(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := lease.Instance().Invoke("run"); err != nil {
			b.Fatal(err)
		}
		if err := lease.Release(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassAcquireRelease(b *testing.B) {
	for _, memoryPages := range []uint32{0, 1, 2, 4, 8, 16, 64} {
		name := "globals"
		if memoryPages != 0 {
			name = fmt.Sprintf("memory-%d-pages", memoryPages)
		}
		b.Run(name, func(b *testing.B) {
			b.Run("reinstantiate", func(b *testing.B) {
				benchmarkClassAcquireRelease(b, ResetReinstantiate, memoryPages)
			})
			b.Run("memory-snapshot", func(b *testing.B) {
				benchmarkClassAcquireRelease(b, ResetMemorySnapshot, memoryPages)
			})
		})
	}
}
