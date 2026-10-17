//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsTableModule(imported bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(entry, 1, 0x70, 1, 2, 3))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0}, []byte{1}, []byte{2}, []byte{2}, []byte{2})))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 3})))
	}
	sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0), wasmtest.ExportEntry("memory", 2, 0),
			wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("remap", 0, 3), wasmtest.ExportEntry("init-dropped", 0, 4), wasmtest.ExportEntry("grow-copy", 0, 5))))
	elements := [][]byte{{1, 0, 2, 1, 0}} // passive funcs [multiply, add]
	if !imported {
		elements = append([][]byte{{0, 0x41, 0, 0x0b, 2, 0, 1}}, elements...)
	}
	sections = append(sections, wasmtest.Section(9, wasmtest.Vec(elements...)))
	segment := byte(0)
	if !imported {
		segment = 1
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x28, 2, 0, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x6c, 0x0b}),
		wasmtest.Code([]byte{0x41, 0, 0x20, 1, 0x36, 2, 0, 0x20, 1, 0x20, 0, 0x11, 0, 0, 0x3f, 0, 0x6a, 0x0b}),
		wasmtest.Code([]byte{0x41, 0, 0x41, 0, 0x41, 2, 0xfc, 12, segment, 0, 0xfc, 13, segment, 0xfc, 16, 0, 0x0b}),
		wasmtest.Code([]byte{0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, segment, 0, 0xfc, 16, 0, 0x0b}),
		wasmtest.Code([]byte{0xd0, 0x70, 0x41, 1, 0xfc, 15, 0, 0x1a, 0x41, 2, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0, 0xfc, 16, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsSharedTablesElementsAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/cache=%t/regabi=%t", imported, cache, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).
						WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					compiled, err := cfg.Compile(threadsTableModule(imported))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
						imports := NewImports()
						if imported {
							providerMod, err := rt.Compile(threadsTableModule(false))
							if err != nil {
								t.Fatal(err)
							}
							defer providerMod.Close()
							provider, err := rt.Instantiate(context.Background(), providerMod)
							if err != nil {
								t.Fatal(err)
							}
							defer provider.Close()
							memory, err := provider.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							bytes := memory.UnsafeBytes()
							binary.LittleEndian.PutUint32(bytes, 9)
							table, err := provider.ExportedTable("table")
							if err != nil {
								t.Fatal(err)
							}
							imports.Table("env", "table", table)
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
						check := func(name string, want uint64, args ...uint64) {
							t.Helper()
							got, err := in.Invoke(name, args...)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("%s: %v, %v; want %d", name, got, err, want)
							}
						}
						want := uint64(15)
						if imported {
							want = 17
						}
						check("run", want, 0, 7)
						check("run", 22, 1, 7)
						check("remap", 2)
						check("run", 22, 0, 7)
						check("run", 15, 1, 7)
						if _, err := in.Invoke("init-dropped"); err == nil {
							t.Fatal("dropped element initialization succeeded")
						}
						check("run", 22, 0, 7)
						check("grow-copy", 3)
						check("run", 22, 2, 7)
						if _, err := in.Invoke("run", 3, 7); err == nil {
							t.Fatal("out-of-bounds indirect call succeeded")
						}
					}
				})
			}
		}
	}
}
