//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsExceptionModule(imported bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))}
	if imported {
		throw := append(wasmtest.Name("env"), wasmtest.Name("throw")...)
		tag := append(wasmtest.Name("env"), wasmtest.Name("tag")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(throw, 0, 0), append(tag, 4, 0, 0))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})))
	} else {
		sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})))
	}
	sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 2})))
	if !imported {
		sections = append(sections, wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("throw", 0, 0), wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("tag", 4, 0))))
	run := wasmtest.Code([]byte{
		0x41, 0, 0x41, 0xe4, 0, 0x36, 2, 0, // caller memory[0] = 100
		0x02, 0x7f, 0x1f, 0x40, 1, 0, 0, 0, // payload block; try_table catch tag 0 -> block
		0x20, 0, 0x10, 0, 0x00, 0x0b, 0x00, 0x0b,
		0x41, 0, 0x28, 2, 0, 0x6a, 0x3f, 0, 0x6a, 0x0b,
	})
	code := [][]byte{run}
	if !imported {
		throw := wasmtest.Code([]byte{0x41, 0, 0x20, 0, 0x36, 2, 0, 0x41, 1, 0x40, 0, 0x1a, 0x20, 0, 0x08, 0, 0x0b})
		code = append([][]byte{throw}, code...)
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code...)))
	return wasmtest.Module(sections...)
}

func TestThreadsSharedExceptionsGrowthCrossInstanceAndCodec(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureExceptionHandling)
	for _, imported := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/cache=%t/regabi=%t", imported, cache, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).
						WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					compiled, err := cfg.Compile(threadsExceptionModule(imported))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
						imports := NewImports()
						if imported {
							mod, err := rt.Compile(threadsExceptionModule(false))
							if err != nil {
								t.Fatal(err)
							}
							defer mod.Close()
							producer, err := rt.Instantiate(context.Background(), mod)
							if err != nil {
								t.Fatal(err)
							}
							defer producer.Close()
							fn, err := producer.ExportedFunc("throw")
							if err != nil {
								t.Fatal(err)
							}
							tag, err := producer.ExportedTag("tag")
							if err != nil {
								t.Fatal(err)
							}
							imports.Function("env", "throw", fn).Tag("env", "tag", tag)
						}
						mod, err := rt.Module(c)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
						in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for _, value := range []uint64{7, 11} {
							if _, err := in.Invoke("throw", value); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
								t.Fatalf("uncaught shared exception: %v", err)
							}
							want := 2*value + 2
							if imported {
								want = value + 101
							}
							got, err := in.Invoke("run", value)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("caught shared exception: %v, %v; want %d", got, err, want)
							}
						}
					}
				})
			}
		}
	}
}
