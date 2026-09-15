//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func BenchmarkCallerArity(b *testing.B) {
	for _, arity := range [][2]int{{0, 0}, {1, 0}, {1, 1}, {4, 1}, {8, 4}, {16, 8}, {24, 12}, {32, 16}, {32, 32}, {48, 24}, {48, 48}, {64, 64}} {
		b.Run(fmt.Sprintf("%d-%d", arity[0], arity[1]), func(b *testing.B) {
			params, results := make([]wasm.ValType, arity[0]), make([]wasm.ValType, arity[1])
			body := []byte{}
			for i := range params {
				params[i] = wasm.I64
				body = append(body, 0x20, byte(i))
			}
			for i := range results {
				results[i] = wasm.I64
			}
			body = append(body, 0x10, 0, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
				wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c := benchMustCompile(b, data)
			defer c.Close()
			paths := []struct {
				name string
				fn   any
			}{
				{name: "legacy", fn: HostFunc(func(_ HostModule, _, r []uint64) {
					for i := range r {
						r[i] = uint64(i + 1)
					}
				})},
				{name: "caller", fn: CallerHostFunc(func(_ Caller, _, r []uint64) {
					for i := range r {
						r[i] = uint64(i + 1)
					}
				})},
				{name: "call", fn: HostCallFunc(func(call HostCall) {
					results := call.ResultSlots()
					for i := range results {
						results[i] = uint64(i + 1)
					}
				})},
			}
			for _, path := range paths {
				b.Run(path.name, func(b *testing.B) {
					fn := path.fn
					in, err := Instantiate(c, Imports{"env.f": fn})
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					args := make([]uint64, len(params))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, err := in.Invoke("g", args...)
						if err != nil || len(got) != len(results) {
							b.Fatalf("result = %v, %v", got, err)
						}
						for j, value := range got {
							if value != uint64(j+1) {
								b.Fatal("wrong result", got)
							}
						}
					}
				})
			}
		})
	}
}

func BenchmarkCallerGCLoop(b *testing.B) {
	if !SupportedFeatures().IsEnabled(CoreFeatureGC) {
		b.Skip("requires WasmGC")
	}
	for _, concrete := range []bool{false, true} {
		b.Run(fmt.Sprintf("concrete%t", concrete), func(b *testing.B) {
			var fn any = HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })
			if concrete {
				fn = CallerHostFunc(func(_ Caller, p, r []uint64) { r[0] = p[0] + 1 })
			}
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			mod, err := rt.Compile(hostRoundtripLoopGCModule(b, 0, true))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.step": fn}))
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if in.gc == nil || in.gcInvocationDomain() == nil {
				b.Fatal("benchmark has no collector domain")
			}
			for _, count := range []int32{0, 1, 8, 64, 1024} {
				b.Run(fmt.Sprintf("n%d", count), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, err := in.Invoke("run", I32(count), 1)
						if err != nil || len(got) != 1 || got[0] != uint64(count) {
							b.Fatalf("result=%v,%v", got, err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkCallerDomainLoop(b *testing.B) {
	if !SupportedFeatures().IsEnabled(CoreFeatureGC) {
		b.Skip("requires WasmGC")
	}
	for _, dynamic := range []bool{false, true} {
		b.Run(fmt.Sprintf("dynamic%t", dynamic), func(b *testing.B) {
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			fn := CallerHostFunc(func(_ Caller, p, r []uint64) { r[0] = p[0] + 1 })
			mod, err := rt.Compile(hostRoundtripLoopGCModule(b, 0, true))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			producer, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.step": fn}))
			if err != nil {
				b.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("run")
			if err != nil {
				b.Fatal(err)
			}
			imports := Imports{"env.step": fn, "env.producer": export}
			if dynamic {
				global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
				if err != nil {
					b.Fatal(err)
				}
				defer global.Close()
				imports["env.target"] = global
			}
			rootMod, err := rt.Compile(hostRoundtripLoopFixture(b, 0, false, true, dynamic))
			if err != nil {
				b.Fatal(err)
			}
			defer rootMod.Close()
			root, err := rt.Instantiate(context.Background(), rootMod, WithImports(imports))
			if err != nil {
				b.Fatal(err)
			}
			defer root.Close()
			flags := root.executionFlags.Load()
			if root.gc != nil || flags&executionFlagImportedGCDomain == 0 || (flags&executionFlagDynamicGCDomain != 0) != dynamic || root.gcInvocationDomains().len() == 0 {
				b.Fatal("incorrect benchmark domain topology")
			}
			for _, count := range []int32{0, 1, 8, 64, 1024} {
				b.Run(fmt.Sprintf("n%d", count), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						got, err := root.Invoke("run", I32(count), 1)
						if err != nil || len(got) != 1 || got[0] != uint64(count) {
							b.Fatalf("result=%v,%v", got, err)
						}
					}
				})
			}
		})
	}
}
