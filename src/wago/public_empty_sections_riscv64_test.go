//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64EmptySectionsModule(vector bool) []byte {
	params, results := []wasm.ValType(nil), []wasm.ValType{wasm.I32}
	body := []byte{0x41, 7, 0x0b}
	if vector {
		params, results = []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}
		body = []byte{0x20, 0, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(2, []byte{0}),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, []byte{0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(9, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestRV64PublicEmptySupportedSections(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("empty-sections-vector%t", vector), rv64EmptySectionsModule(vector), artifact)
				if c.requiredFeatures&(CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes) != 0 {
					t.Fatal("empty section introduced feature requirements")
				}
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				var args []uint64
				want := []uint64{7}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					want = args
				}
				if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
					t.Fatal("empty-section execution", got, err)
				}
			})
		}
	}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("state-only-artifact%t", artifact), func(t *testing.T) {
			source := wasmtest.Module(wasmtest.Section(2, []byte{0}), wasmtest.Section(4, []byte{0}), wasmtest.Section(9, []byte{0}))
			c := rv64TableStageCompile(t, "empty-sections-state-only", source, artifact)
			rv64CheckStateOnlyCode(t, c, 0)
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			rv64CheckStateOnlyCode(t, c, 1)
		})
	}
}

func TestRV64PublicEmptySectionsFeatureAndMalformedGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source validation checks require the compiler")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1)
	c, err := Compile(cfg, rv64EmptySectionsModule(false))
	if c != nil {
		defer c.Close()
	}
	if err != nil {
		t.Fatal("inert sections require no extra core features", err)
	}
	if c, err := Compile(cfg, rv64EmptySectionsModule(true)); c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("empty sections bypassed disabled SIMD", err)
	}
	for _, id := range []byte{2, 4, 9} {
		for _, source := range [][]byte{
			wasmtest.Module(wasmtest.Section(id, nil)),
			wasmtest.Module(wasmtest.Section(id, []byte{0, 255})),
			wasmtest.Module(wasmtest.Section(id, []byte{0}), wasmtest.Section(id, []byte{0})),
			wasmtest.Module([]byte{id, 2, 0}),
		} {
			if c, err := Compile(nil, source); c != nil || err == nil {
				if c != nil {
					_ = c.Close()
				}
				t.Fatal("malformed empty-section source admitted", id, err)
			}
		}
	}
}
