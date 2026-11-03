//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"
)

func TestContiguousEdenMatureNativeDynamicTypedDefaultArrayUsesLargeReservation(t *testing.T) {
	for _, length := range []uint64{4096, 8192} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDynamicTypedDefaultArrayModule(2))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, NurseryRegionShift: 6, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()

			if got, err := in.Invoke("run", length); err != nil || len(got) != 1 || got[0] != length {
				t.Fatalf("typed dynamic array = %v, %v", got, err)
			}
			state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().LargeArrayAllocState)), corergc.NativeLargeArrayAllocStateSize)
			if count := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCountOffset:]); count != 1 {
				t.Fatalf("large reservation count = %d, want 1", count)
			}
			if cursor := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCursorOffset:]); cursor != 1 {
				t.Fatalf("large reservation cursor = %d, want 1 native allocation", cursor)
			}
			if got := in.gc.Stats().Allocations; got != 2 {
				t.Fatalf("semantic allocations = %d, want 2", got)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
