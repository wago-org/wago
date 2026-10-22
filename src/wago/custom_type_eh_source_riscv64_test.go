//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64CustomWideLiveAcrossException(t *testing.T) {
	carriers := []struct {
		wire byte
		typ  WasmType
	}{{0x7f, WasmI32}, {0x7e, WasmI64}, {0x7d, WasmF32}, {0x7c, WasmF64}, {0x7b, WasmV128}, {0x70, WasmFuncRef}, {0x6f, WasmExternRef}}
	for _, carrier := range carriers {
		t.Run(fmt.Sprintf("carrier%x", carrier.wire), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			if err := rt.Use(rv64WideDataExtension{size: 512, carrier: carrier.typ}); err != nil {
				t.Fatal(err)
			}
			body := []byte{0x10, 0, 0x02, 0x7f, 0x1f, 0x40, 1, 0, 0, 0, 0x41, 42, 0x08, 0, 0x0b, 0x00, 0x0b, 0x41, 42, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x10, 2, 0x0b}
			wire := rv64WideDataSource(carrier.wire, body, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))
			for pos := 8; pos < len(wire); {
				id := wire[pos]
				size, n := binary.Uvarint(wire[pos+1:])
				if n <= 0 {
					t.Fatal("invalid test section")
				}
				if id == 7 {
					withTag := append([]byte(nil), wire[:pos]...)
					withTag = append(withTag, wasmtest.Section(13, wasmtest.Vec([]byte{0, 4}))...)
					wire = append(withTag, wire[pos:]...)
					break
				}
				pos += 1 + n + int(size)
			}
			features := rt.cfg.frontendFeatures()
			features.ExceptionHandling = true
			compiled, err := compileWithFrontendFeaturesAndInstructions(rt.cfg, wire, features, rt.instructions)
			if err != nil {
				t.Fatal(err)
			}
			mod, err := rt.AdoptModule(compiled)
			if err != nil {
				compiled.Close()
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
					in, err := rt.Instantiate(context.Background(), entry.mod)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != rv64WideSum(512, 1) {
						t.Fatalf("sum=%v want=%d err=%v", got, rv64WideSum(512, 1), err)
					}
				})
			}
		})
	}
}
