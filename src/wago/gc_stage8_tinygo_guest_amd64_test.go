//go:build linux && amd64 && (tinygo || wago_target_tinygo) && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func stage8TinyGoGuestModule() []byte {
	types := wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x5e, 0x63, 0, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	// Global array of 60000 nullable struct references.
	global := append([]byte{0x63, 1, 1, 0x41}, wasmtest.SLEB32(60000)...)
	global = append(global, 0xfb, 7, 1, 0x0b)
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x23, 0, 0x20, 0, 0x41, 42, 0xfb, 0, 0, 0xfb, 0x0e, 1, 0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x41}
	body = append(body, wasmtest.SLEB32(60000)...)
	body = append(body, 0x49, 0x0d, 0, 0x0b, 0x23, 0, 0x41)
	body = append(body, wasmtest.SLEB32(59999)...)
	body = append(body, 0xfb, 0x0b, 1, 0xfb, 2, 0, 0, 0x0b)
	read := []byte{0, 0x23, 0, 0x41, 0, 0xfb, 0x0b, 1, 0xfb, 2, 0, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2})), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("read", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), append(wasmtest.ULEB(uint32(len(read))), read...))))
}
func TestGCStage8TinyGoGuestRelocatedRead(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false), stage8TinyGoGuestModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Workers: 1, NurseryBytes: 4 << 20, NurseryRegionShift: 6, MatureEvacuation: true, ThroughputHeapBytes: 32 << 20, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err = in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	if err = in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	array := in.gc.GlobalSlot(0)
	for i := uint32(0); i < 60000; i++ {
		if i%1024 != 0 {
			if err = in.gc.ArraySet(array, i, gc.RefValue(gc.Null())); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	// Failed required LOS admission requests pressure without touching a live root.
	if _, err = in.gc.NewArrayDefault(1, 9000000); err == nil {
		t.Fatal("pressure trigger admitted")
	}
	generation := in.gc.NativeView().RefreshGeneration
	if err = in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if d := in.gc.CollectionDiagnostics(); d.MovedBytes == 0 || d.Evacuation != "moved" {
		t.Fatal("guest old graph did not relocate", d)
	}
	if in.gc.NativeView().RefreshGeneration <= generation {
		t.Fatal("stale native view")
	}
	if got, err := in.Invoke("read"); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal("guest relocation read", got, err)
	}
}
