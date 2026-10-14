//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicRootedLocalTailModule(vector bool) []byte {
	callee, caller, wrapper := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
	}, []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
	}, []byte{0x00}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	if vector {
		types[1] = []byte{0x60, 0x02, 0x64, 0x00, 0x7b, 0x02, 0x7f, 0x7b}
		types[2] = wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32, wasm.V128})
		callee = append(callee, 0x20, 0x01)
		caller = append(caller, 0x20, 0x00)
		wrapper = append(wrapper, 0x20, 0x00)
	}
	callee = append(callee, 0x0b)
	caller = append(caller, 0x12, 0x00, 0x0b)
	wrapper = append(wrapper, 0x12, 0x01, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
		)),
	)
}

func TestRV64PublicRootedLocalTailExecution(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(map[bool]string{false: "scalar", true: "swar"}[vector], func(t *testing.T) {
			cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall)
			compiled, err := cfg.Compile(rv64PublicRootedLocalTailModule(vector))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			roots := compiled.genericGCFrameRoots()
			tails := rv64NativeTailModule(compiled)
			if roots == nil || len(roots.safepoints) != 2 || len(roots.callsites) != 0 || tails == nil || len(tails.Plans) != 2 {
				t.Fatalf("rooted local tails: roots=%+v tails=%+v", roots, tails)
			}
			for _, gcConfig := range []GCConfig{
				{},
				{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
			} {
				rt := NewRuntime(WithRuntimeConfig(cfg))
				mod, err := rt.Module(compiled)
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithGC(gcConfig))
				mod.Close()
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				args, want := []uint64(nil), []uint64{42}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					want = append(want, args...)
				}
				for range 64 {
					if got, invokeErr := in.Invoke("run", args...); invokeErr != nil || !slices.Equal(got, want) {
						in.Close()
						rt.Close()
						t.Fatalf("rooted local tail = %v, %v; want %v", got, invokeErr, want)
					}
				}
				in.Close()
				rt.Close()
			}
		})
	}
}
