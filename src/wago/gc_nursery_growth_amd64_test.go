//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCDefaultNurseryKeepsManyIdleInstancesSmall(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModuleN([]byte{0x5e, 0x7f, 0x01}, 4, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	var instances []*Instance
	defer func() {
		for _, in := range instances {
			in.Close()
		}
	}()
	var backing uint64
	for range 32 {
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, in)
		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
		view := in.gc.NativeView()
		if view.NurseryAllocBytes != 64<<10 || view.Spaces[gc.NativeSpaceNursery].Bytes != 128<<10 {
			t.Fatal("cold instance reserved more than the initial nursery")
		}
		backing += uint64(view.Spaces[gc.NativeSpaceNursery].Bytes)
	}
	if backing != 4<<20 {
		t.Fatalf("32 idle-instance nursery backing = %d, want 4 MiB", backing)
	}
}

func TestGCNativeArrayAllocationAfterNurseryGrowth(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	types := [][]byte{{0x5e, 0x7f, 0x01}, {0x5e, 0x6e, 0x01}}
	body := []byte{
		0x00, 0x20, 0x00, 0x41, 0x10, 0xfb, 0x06, 0x00, // array.new i32[16] from parameter
		0x41, 0x0f, 0xfb, 0x0b, 0x00, 0x0b, // read the last payload element
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcInstructionBenchmarkModuleWithTypes(types, body))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true, PoisonFreed: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	parent, err := in.gc.NewArrayDefault(in.requireGCDomainType(1), 4096)
	if err != nil {
		t.Fatal(err)
	}
	in.gc.NewGlobalSlot(parent)
	for i := 0; i < 64<<10; i++ {
		child, err := in.gc.NewArrayWithRoots(in.requireGCDomainType(0), 16, gc.I32Value(int32(i)), gc.EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		if err := in.gc.ArraySet(parent, uint32(i&4095), gc.RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	if got := in.gc.NativeView().NurseryAllocBytes; got != 2<<20 {
		t.Fatalf("native Eden = %d, want 2 MiB", got)
	}
	if err := in.gc.CollectMinor(gc.EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	before := in.gc.Stats()
	for i := uint64(0); i < 40000; i++ {
		got, err := fn.Invoke(i)
		if err != nil || len(got) != 1 || got[0] != i {
			t.Fatalf("native payload read %d = %v, %v", i, got, err)
		}
	}
	after := in.gc.Stats()
	if after.Allocations-before.Allocations != 40000 || after.MinorCollections <= before.MinorCollections {
		t.Fatalf("native allocation did not cross collection: before %+v after %+v", before, after)
	}
	view := in.gc.NativeView()
	state := unsafe.Slice((*byte)(offHeapPtr(view.StructAllocState)), gc.NativeStructAllocStateSize)
	if binary.LittleEndian.Uint32(state[gc.NativeStructAllocCountOffset:]) == 0 || binary.LittleEndian.Uint32(state[gc.NativeStructAllocCursorOffset:]) == 0 {
		t.Fatal("generated allocation did not consume a native reservation")
	}
	for i := uint32(0); i < 4096; i++ {
		child, err := in.gc.ArrayGet(parent, i)
		if err != nil {
			t.Fatal(err)
		}
		value, err := in.gc.ArrayGet(child.Ref, 15)
		if err != nil || value.I32() != (64<<10)-4096+int32(i) {
			t.Fatalf("retained graph read %d = %v, %v", i, value, err)
		}
	}
}
