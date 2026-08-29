//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

const wideGCStructFields = 403

func wideGCStructModule() []byte {
	leaf := []byte{0x5f, 0x01, 0x7f, 0x00}
	wide := append([]byte{0x5f}, wasmtest.ULEB(wideGCStructFields)...)
	for i := 0; i < wideGCStructFields; i++ {
		wide = append(wide, 0x63, 0x00, 0x00)
	}
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00,
		0x21, 0x00}
	for i := 0; i < wideGCStructFields; i++ {
		body = append(body, 0x20, 0x00)
	}
	body = append(body, 0xfb, 0x00, 0x01)
	body = append(body, 0xfb, 0x02, 0x01)
	body = append(body, wasmtest.ULEB(wideGCStructFields-1)...)
	body = append(body, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(leaf, wide, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestWideGCStructHelperUsesCheckedExtendedHostFrame(t *testing.T) {
	base, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wideGCStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if err := base.validateCached(); err != nil {
		t.Fatal(err)
	}
	if got := int(base.syncHostSlots); got != wideGCStructFields+1 {
		t.Fatalf("sync host capacity = %d, want %d", got, wideGCStructFields+1)
	}
	if base.hostCtrlFrameBytes() <= coreruntime.HostCtrlFrameBytes {
		t.Fatalf("wide control frame = %d, inline = %d", base.hostCtrlFrameBytes(), coreruntime.HostCtrlFrameBytes)
	}
	for _, codec := range []bool{false, true} {
		compiled := base
		if codec {
			compiled = roundTripCompiled(t, base)
			defer compiled.Close()
			if compiled.syncHostSlots != base.syncHostSlots || compiled.hostCtrlFrameBytes() != base.hostCtrlFrameBytes() {
				t.Fatalf("codec capacity/frame = %d/%d, want %d/%d", compiled.syncHostSlots, compiled.hostCtrlFrameBytes(), base.syncHostSlots, base.hostCtrlFrameBytes())
			}
		}
		in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		var got []uint64
		for i := 0; i < 20 && err == nil; i++ {
			got, err = in.Invoke("run")
		}
		if closeErr := in.Close(); err == nil {
			err = closeErr
		}
		if err != nil || len(got) != 1 || got[0] != 7 {
			t.Fatalf("wide helper result = %v, %v; want [7]", got, err)
		}
	}
}
