//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCSharedCodeScalarDomainAdmission(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		name := "stack"
		if registerABI {
			name = "register"
		}
		t.Run(name, func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			im := append(wasmtest.Name("env"), wasmtest.Name("next")...)
			im = append(im, 0, 1)
			run := []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 0, 0x10, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
			run = append(wasmtest.ULEB(uint32(len(run))), run...)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(im)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", 0, 1), wasmtest.ExportEntry("run", 0, 2))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x41, 1, 0xfb, 0, 0, 0x1a, 0x0b}),
					run,
				)),
			)
			code, err := rt.Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer code.Close()
			settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
			first, err := rt.Instantiate(context.Background(), code, WithGC(settings), WithImports(testImports("env.next", slotHostFunc(func(HostModule, []uint64, []uint64) {}))))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			next, err := first.ExportedFunc("allocate")
			if err != nil {
				t.Fatal(err)
			}
			secondSettings := settings
			secondSettings.StressNurseryBytes = 128
			incompatible, err := rt.Instantiate(context.Background(), code, WithGC(secondSettings), WithImports(testImports("env.next", next)))
			if incompatible != nil {
				incompatible.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "incompatible") {
				t.Fatalf("same-code incompatible collector configuration = %v, want rejection", err)
			}
			second, err := rt.Instantiate(context.Background(), code, WithGC(settings), WithImports(testImports("env.next", next)))
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if first.base != second.base || first.gc == nil || first.gc != second.gc {
				t.Fatal("compatible same-code instances must share the collector domain")
			}
			for i := 0; i < 3; i++ {
				out, err := second.Invoke("run")
				if err != nil || len(out) != 1 || AsI32(out[0]) != 73 {
					t.Fatalf("same-code scalar relay = %v, %v; want [73], nil", out, err)
				}
			}
		})
	}
}
