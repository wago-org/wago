//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCScalarStructGetLiveVectorJoin(t *testing.T) {
	vector := []byte{0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	body := append(vector, 0x20, 0, 0x04, 0x40,
		0x23, 0, 0xfb, 2, 0, 0, 0x1a,
		0x05, 0x23, 0, 0xfb, 2, 0, 0, 0x1a, 0x0b, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5f, 1, 0x7f, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x0b}), wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 17); err != nil {
		t.Fatal(err)
	}
	for _, fallback := range []bool{false, true} {
		if fallback && runtime.GOARCH != "arm64" {
			continue // the new physical-check helper join is ARM64-specific
		}
		view := in.gc.NativeView()
		saved := view.HandleCount
		if fallback {
			view.HandleCount = 0
		}
		for _, branch := range []uint64{0, 1} {
			out, err := in.Invoke("run", branch)
			if err != nil || len(out) != 2 || out[0] != 0x0706050403020100 || out[1] != 0x0f0e0d0c0b0a0908 {
				view.HandleCount = saved
				t.Fatalf("fallback%v branch%d: %x,%v", fallback, branch, out, err)
			}
		}
		view.HandleCount = saved
	}
}

func TestGCScalarStructGetCodec(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcScalarStructGetModule(0x7c, true, true, 20))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	in, err := Instantiate(loaded, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	const bits = uint64(0x7ff8123456789abc)
	if _, err := in.Invoke("init", bits); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"global", "local"} {
		out, err := in.Invoke(name)
		if err != nil || len(out) != 1 || out[0] != bits {
			t.Fatalf("codec %s: %x,%v", name, out, err)
		}
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	saved := view.HandleCount
	if runtime.GOARCH == "arm64" {
		view.HandleCount = 0
	}
	out, err := in.Invoke("global")
	view.HandleCount = saved
	if err != nil || len(out) != 1 || out[0] != bits {
		t.Fatalf("codec helper join: %x,%v", out, err)
	}
}
