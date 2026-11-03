//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
	"testing"
	"unsafe"
)

func TestContiguousEdenMatureNativeArrayAllocTrapCancelsBatch(t *testing.T) {
	compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 65536, NurseryRegionShift: 6}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.gc.PrepareNativeAllocation(32) {
		t.Fatal("failed to prepare native allocation batch")
	}
	if _, err := in.Invoke("len", math.MaxUint32); err == nil {
		t.Fatal("overflowing native array allocation succeeded")
	}
	view := in.gc.NativeView()
	state := unsafe.Slice((*byte)(offHeapPtr(view.StructAllocState)), corergc.NativeStructAllocChunkEndOffset+4)
	if count := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCountOffset:]); count != 0 {
		t.Fatalf("trapping fallback retained %d handles", count)
	}
	if chunkEnd := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocChunkEndOffset:]); chunkEnd != 0 {
		t.Fatalf("trapping fallback retained chunk end %d", chunkEnd)
	}
}
