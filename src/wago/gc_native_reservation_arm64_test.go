//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func TestGCARM64LiveStructDoesNotPrepareUnusedHandles(t *testing.T) {
	body := []byte{0, 0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x23, 0, 0xfb, 2, 0, 0, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
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
	for i := uint64(0); i < 47; i++ {
		got, err := in.Invoke("run", i)
		if err != nil || len(got) != 1 || got[0] != i {
			t.Fatalf("run=%v,%v", got, err)
		}
		view := in.gc.NativeView()
		state := (*[gc.NativeStructAllocStateSize]byte)(unsafe.Pointer(view.StructAllocState))
		if n := *(*uint32)(unsafe.Pointer(&state[gc.NativeStructAllocCountOffset])); n != 0 {
			t.Fatalf("live struct prepared %d unused handles after %d calls", n, i+1)
		}
	}
	if got := in.gc.Stats().Allocations; got != 47 {
		t.Fatalf("allocations=%d", got)
	}
}

func TestGCARM64WideArrayDoesNotPrepareUnconsumableState(t *testing.T) {
	body := []byte{0, 0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x23, 0, 0xfb, 0x0f, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7e, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 64 << 10}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2000; i++ {
		n := uint64(32 + 32*(i%2))
		got, err := in.Invoke("run", n)
		if err != nil || len(got) != 1 || got[0] != n {
			t.Fatalf("run=%v,%v", got, err)
		}
	}
	view := in.gc.NativeView()
	state := (*[gc.NativeLargeArrayAllocStateSize]byte)(unsafe.Pointer(view.LargeArrayAllocState))
	if n := *(*uint32)(unsafe.Pointer(&state[gc.NativeStructAllocCountOffset])); n != 0 {
		t.Fatalf("prepared %d large-array reservations without ARM64 consumer", n)
	}
	if got := in.gc.Stats().Allocations; got != 2000 {
		t.Fatalf("allocations=%d", got)
	}
}
