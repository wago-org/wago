//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCExternLocalSharedCodeCallerOwner(t *testing.T) {
	// The stack ABI exposes the caller frame below the callee wrapper. Both
	// instances share code, so a return PC alone cannot identify its owner.
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", false)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	settings := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	noopData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("noop", 0, 0), wasmtest.ExportEntry("marker", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	nc, err := Compile(cfg, noopData)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nm, err := rt.Module(nc)
	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close()
	ni, err := rt.Instantiate(context.Background(), nm, WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer ni.Close()
	noop, err := ni.ExportedFunc("noop")
	if err != nil {
		t.Fatal(err)
	}
	marker, err := ni.ExportedFunc("marker")
	if err != nil {
		t.Fatal(err)
	}
	run := []byte{1, 1, 0x6e, 0x20, 0, 0xfb, 26, 0x21, 1, 0x10, 0, 0x20, 1, 0xfb, 27, 0x0b}
	run = append(wasmtest.ULEB(uint32(len(run))), run...)
	tick := []byte{0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("env", "tick", 2), instructionFuncImport("env", "marker", 3))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("tick", 0, 3), wasmtest.ExportEntry("identity", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(run, wasmtest.Code(tick), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	c, err := Compile(cfg, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	a, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.tick", noop, "env.marker", marker)), WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	target, err := a.ExportedFunc("tick")
	if err != nil {
		t.Fatal(err)
	}
	b, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.tick", target, "env.marker", marker)), WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.gc != b.gc || a.base != b.base {
		t.Fatal("fixture does not share collector and compiled code")
	}
	foreign, err := b.NewExternRef("B-local-across-A-allocation")
	if err != nil {
		t.Fatal(err)
	}
	word := ValueExternRef(foreign).Bits()
	before := a.gc.Stats().FullCollections
	out, err := b.Invoke("run", word)
	if err != nil || len(out) != 1 || out[0] != word {
		t.Fatalf("valid B-local across A allocation: %x,%v; want%x", out, err, word)
	}
	if a.gc.Stats().FullCollections <= before {
		t.Fatal("callee did not collect")
	}
}
