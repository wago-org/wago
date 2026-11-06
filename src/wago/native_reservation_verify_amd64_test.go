//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"
)

func TestNativeGeneratedReservationVerify(t *testing.T) {
	for _, shift := range []uint8{0, 6, 12} {
		for _, length := range []uint64{4096, 4097, 8192, 8193} {
			t.Run(fmt.Sprintf("shift=%d/length=%d", shift, length), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDynamicTypedDefaultArrayModule(2))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, NurseryRegionShift: shift}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if got, err := in.Invoke("run", length); err != nil || len(got) != 1 || got[0] != length {
					t.Fatalf("invoke %v %v", got, err)
				}
				state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().LargeArrayAllocState)), corergc.NativeLargeArrayAllocStateSize)
				if binary.LittleEndian.Uint32(state[4:]) != 1 || binary.LittleEndian.Uint32(state[8:]) != 1 {
					t.Fatalf("not consumed: %v", state)
				}
				t.Logf("start=%d cursor=%d end=%d", binary.LittleEndian.Uint32(state[16:]), binary.LittleEndian.Uint32(state[20:]), binary.LittleEndian.Uint32(state[24:]))
				if err := in.gc.Verify(nil); err != nil {
					t.Fatalf("generated publication Verify: %v", err)
				}
				if err := in.gc.ForcePromote(in.gc.GlobalSlot(0)); err != nil {
					t.Fatal(err)
				}
				if err := in.gc.Verify(nil); err != nil {
					t.Fatalf("generated publication after ForcePromote Verify: %v", err)
				}
			})
		}
	}
}
