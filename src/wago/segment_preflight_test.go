package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestActiveDataFailureRespectsCoreVersionAndArtifact(t *testing.T) {
	importMemory := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	importMemory = append(importMemory, 2, 0, 1)
	segment := func(offset int32, value byte) []byte {
		out := []byte{0, 0x41}
		out = append(out, wasmtest.SLEB32(offset)...)
		return append(out, 0x0b, 1, value)
	}
	module := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(importMemory)),
		wasmtest.Section(11, wasmtest.Vec(segment(0, 'a'), segment(65536, 'b'))),
	)
	for _, features := range []CoreFeatures{CoreFeaturesV1, CoreFeaturesV2} {
		for _, artifact := range []bool{false} {
			t.Run(fmt.Sprintf("core1=%t/artifact=%t", features == CoreFeaturesV1, artifact), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit)
				c, err := CompileWithConfig(cfg, module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()

				ownerModule := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, []byte{1, 0}),
					wasmtest.Section(5, []byte{1, 1, 1, 1}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0), wasmtest.ExportEntry("load", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x2d, 0, 0, 0x0b}))),
				)
				ownerCompiled, err := CompileWithConfig(cfg, ownerModule)
				if err != nil {
					t.Fatal(err)
				}
				defer ownerCompiled.Close()
				owner, err := Instantiate(ownerCompiled)
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				memory, err := owner.ExportedMemory("memory")
				if err != nil {
					t.Fatal(err)
				}
				defer memory.Close()
				instance, err := Instantiate(c, NewImports().Memory("env", "memory", memory))
				if instance != nil || err == nil {
					if instance != nil {
						instance.Close()
					}
					t.Fatal("out-of-bounds initialization succeeded")
				}
				want := byte('a')
				if features == CoreFeaturesV1 {
					want = 0
				}
				if got := memory.UnsafeBytes()[0]; got != want {
					t.Fatalf("imported memory[0] = %d, want %d", got, want)
				}
				if got, err := owner.Invoke("load", 0); err != nil || len(got) != 1 || got[0] != uint64(want) {
					t.Fatalf("owner load after failed instantiation = %v, %v; want %d", got, err, want)
				}
			})
		}
	}
}

func TestMemory64ActiveDataGlobalOffsetPreservesHighBits(t *testing.T) {
	if !supportsMemory64Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("memory64 execution is unavailable on this platform")
	}
	module := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "offset", wasm.I64, false))),
		wasmtest.Section(5, []byte{1, 4, 1}),
		wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x23, 0, 0x0b, 1, 'a'})),
	)
	for _, preflight := range []bool{false, true} {
		for _, artifact := range []bool{false} {
			t.Run(fmt.Sprintf("preflight=%t/artifact=%t", preflight, artifact), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFeatures(CoreFeatureMemory64).
					WithFeature(CoreFeatureExtendedConstExpressions, false).
					WithFeature(CoreFeatureBulkMemoryOperations, !preflight)
				c, err := CompileWithConfig(cfg, module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()

				offset := NewGlobalI64(1<<32, false)
				defer offset.Close()
				in, err := Instantiate(c, NewImports().Global("env", "offset", offset))
				if in != nil || err == nil {
					if in != nil {
						in.Close()
					}
					t.Fatalf("memory64 initialization accepted out-of-bounds offset: %v", err)
				}
			})
		}
	}
}
