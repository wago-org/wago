//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestThreadsSharedMemorySizeExecutionAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, pages := range []byte{0, 1, 3} {
			t.Run(fmt.Sprintf("imported=%t/pages=%d", imported, pages), func(t *testing.T) {
				sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
				if imported {
					entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
					entry = append(entry, 2, 3, pages, pages)
					sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
				}
				sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
				if !imported {
					sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, pages, pages})))
				}
				sections = append(sections,
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x3f, 0, 0x0b}))),
				)
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
				c, err := cfg.Compile(wasmtest.Module(sections...))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				loaded := publicArtifactRoundTrip(t, c)
				for _, compiled := range []*Compiled{c, loaded} {
					var opts any = InstantiateOptions{}
					if imported {
						memory, err := NewSharedMemory(uint32(pages), uint32(pages))
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						opts = testImports("env.memory", memory)
					}
					in, err := Instantiate(compiled, opts)
					if err != nil {
						t.Fatal(err)
					}
					got, err := in.Invoke("size")
					in.Close()
					if err != nil || len(got) != 1 || got[0] != uint64(pages) {
						t.Fatalf("size = %v, %v; want %d", got, err, pages)
					}
				}
			})
		}
	}
}
