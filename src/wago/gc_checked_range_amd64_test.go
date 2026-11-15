//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"os"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcCheckedRangeMovingModule(mutate bool) []byte {
	worker := []byte{0,
		0x20, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x1a,
		0x20, 0, 0x10, 3,
		0x20, 0, 0x24, 1,
		0x20, 0, 0xfb, 22, 0,
		0x20, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0,
		0x41, 1, 0x6a, 0xfb, 5, 0, 0,
		0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	// Publish initialized objects to overwrite the evacuated nursery.
	helper := []byte{0}
	if mutate {
		helper = append(helper, 0x20, 0, 0x41, 0xcd, 0, 0xfb, 5, 0, 0)
	}
	for i := 0; i < 8; i++ {
		helper = append(helper, 0x41, 0xe3, 0, 0xfb, 0, 0, 0x24, 2)
	}
	helper = append(helper, 0x0b)
	global := []byte{0x63, 0, 1, 0xd0, 0, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 1, 0x64, 0, 1, 0x7f}, []byte{0x60, 1, 0x64, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{4})),
		wasmtest.Section(6, wasmtest.Vec(global, global, global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 41, 0xfb, 0, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xfb, 22, 0, 0xd0, 0, 0x24, 0, 0x10, 2, 0x0b}),
			append(wasmtest.ULEB(uint32(len(worker))), worker...),
			append(wasmtest.ULEB(uint32(len(helper))), helper...))),
	)
}

func TestGCCheckedRangeMovingCall(t *testing.T) {
	if os.Getenv("WAGO_AMD64_GC_CHECKED_RANGE") != "1" {
		t.Skip("trial flag is off")
	}
	for _, mutate := range []bool{false, true} {
		t.Run(map[bool]string{false: "move", true: "move-and-mutate"}[mutate], func(t *testing.T) { testGCCheckedRangeMovingCall(t, mutate) })
	}
}

func testGCCheckedRangeMovingCall(t *testing.T, mutate bool) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), gcCheckedRangeMovingModule(mutate))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	status := c.GCNativeRootAdmission()
	if !status.Required || !status.Exact {
		t.Fatalf("root admission: %+v", status)
	}
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 32, SurvivorBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	address := func() uintptr {
		view := in.gc.NativeView()
		entry := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
		return view.Spaces[entry[gc.NativeHandleSpaceOffset]].Base + uintptr(binary.LittleEndian.Uint32(entry))
	}
	beforeAddr, before := address(), in.gc.Stats()
	want := uint64(42)
	if mutate {
		want = 78
	}
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("moved receiver update: %v, %v; want%d", got, err, want)
	}
	after := in.gc.Stats()
	if after.MinorCollections <= before.MinorCollections || after.YoungBytesCopied <= before.YoungBytesCopied || address() == beforeAddr {
		t.Fatalf("receiver did not move: before=%+v after=%+v addr=%x/%x", before, after, beforeAddr, address())
	}
	if readGlobalObject(in.globalCells[0], in.c.Globals[0].Type) != 0 || uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)) != ref {
		t.Fatal("source root was not transferred through the worker")
	}
	value := unsafe.Slice((*byte)(offHeapPtr(address()+uintptr(gc.PayloadOffset))), 4)
	if got := binary.LittleEndian.Uint32(value); uint64(got) != want {
		t.Fatalf("actual moved object contains%d, want%d", got, want)
	}
}
