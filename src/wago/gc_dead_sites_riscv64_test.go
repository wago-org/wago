//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64DeadCollectorSitesPreserveLaterLiveRoots(t *testing.T) {
	// The branch skips a call and allocation. Later live sites must still
	// publish the local containing 42 while the callee forces collection.
	body := []byte{1, 1, 0x63, 0,
		0x02, 0x40, 0x0c, 0, 0x10, 0, 0xfb, 1, 0, 0x1a, 0x0b,
		0x41, 42, 0xfb, 0, 0, 0x21, 0,
		0x10, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, candidate := range []*Compiled{c} {
		for _, profile := range []corergc.Profile{corergc.ProfileThroughput, corergc.ProfileTiny} {
			in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run")
			in.Close()
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("live roots after dead sites: %v, %v", got, err)
			}
		}
	}
}

func TestRV64DeadCollectorTargetDeclarationArtifact(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0, 0x14, 0, 0x1a, 0x0b}))),
	)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, candidate := range []*Compiled{c} {
		in, err := Instantiate(candidate, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if in.gc != nil {
			t.Fatal("dead collector declaration acquired a collector")
		}
		_, err = in.Invoke("run")
		in.Close()
		if err == nil {
			t.Fatal("unreachable body returned without a trap")
		}
	}
}
