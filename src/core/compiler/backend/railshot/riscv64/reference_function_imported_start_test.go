//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestReferenceFunctionImportedStartSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-wide%t", vector, wide), func(t *testing.T) {
				decode := func() *wasm.Module {
					m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImportedStart(vector, wide))
					if err != nil {
						t.Fatal(err)
					}
					return m
				}
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				m := decode()
				image, err := CompileModuleWith(m, opts)
				if err != nil || image == nil || !image.NativeReferenceFunctions || *m.Start != 1 || len(image.Entry) != 5 || image.NativeTail == nil || len(image.NativeTail.Plans) != 1 {
					t.Fatal("imported private start staging", err)
				}
				for _, index := range []wasm.FuncIdx{0, 2, 4, 7, 8, wasm.FuncIdx(^uint32(0))} {
					m := decode()
					*m.Start = index
					pressure := 0
					badOpts := opts
					badOpts.MemoryPressure = func() { pressure++ }
					if image, err := CompileModuleWith(m, badOpts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
						t.Fatal("bad imported start reached allocation", index, err)
					}
				}
			})
		}
	}
}
