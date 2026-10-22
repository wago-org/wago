//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	"testing"
)

func TestRV64CustomWideLiveAcrossGCAllocation(t *testing.T) {
	carriers := []struct {
		wire byte
		typ  WasmType
	}{{0x7f, WasmI32}, {0x7e, WasmI64}, {0x7d, WasmF32}, {0x7c, WasmF64}, {0x7b, WasmV128}, {0x70, WasmFuncRef}, {0x6f, WasmExternRef}}
	for _, size := range []int32{32, 512} {
		for _, carrier := range carriers {
			t.Run(fmt.Sprintf("size%d-carrier%x", size, carrier.wire), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
				defer rt.Close()
				if err := rt.Use(rv64WideDataExtension{size: size, carrier: carrier.typ}); err != nil {
					t.Fatal(err)
				}
				// Keep a real object above the opaque custom payload during the
				// second collection. Both its field and every custom cell survive.
				wire := rv64WideDataSource(carrier.wire, []byte{0x10, 0, 0x41, 42, 0xfb, 0, 4, 0xfb, 1, 4, 0x1a, 0xfb, 2, 4, 0, 0x41, 42, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x10, 2, 0x0b}, []byte{0x5f, 1, 0x7f, 0})
				mod, err := rt.Compile(wire)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				blob, err := mod.c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := rt.AdoptModule(loaded)
				if err != nil {
					t.Fatal(err)
				}
				defer artifact.Close()
				for _, entry := range []struct {
					name string
					mod  *Module
				}{{"source", mod}, {"artifact", artifact}} {
					t.Run(entry.name, func(t *testing.T) {
						in, err := rt.Instantiate(context.Background(), entry.mod, WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						got, err := in.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != rv64WideSum(size, 1) {
							t.Fatalf("sum=%v want=%d err=%v", got, rv64WideSum(size, 1), err)
						}
					})
				}
			})
		}
	}
}
