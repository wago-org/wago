//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestNativeNonCollectingGCCallChecksOwnedThunk(t *testing.T) {
	for _, helper := range []uint32{gcStructGet, gcStructRefCast, gcFunctionSubtypeTest, gcArrayGet, gcArrayLen, gcStructAllocDefault, gcArrayAllocFixed} {
		for _, id := range []uint32{0, 1} {
			payload, ok := shared.EncodeGCDispatch(helper, id)
			if !ok {
				t.Fatal("dispatch")
			}
			a := enc.Asm{}
			site := a.JumpLong(true)
			for len(a.B)&15 != 0 {
				a.Addi(enc.Zero, enc.Zero, 0)
			}
			target := len(a.B)
			a.B = append(a.B, HostIndirectOwnedSyncThunk(gcStructDispatchBit|payload, 4, 1)...)
			if err := a.PatchLong(site, int64(target)); err != nil {
				t.Fatal(err)
			}
			want := id == 0 && helper != gcStructAllocDefault && helper != gcArrayAllocFixed
			if got := NativeNonCollectingGCCall(a.B, uint32(site+8), 4); got != want {
				t.Fatalf("helper%d/id%d=%t,want%t", helper, id, got, want)
			}
			if NativeNonCollectingGCCall(a.B, uint32(site+8), 3) {
				t.Fatal("short transport capacity admitted")
			}
			binary.LittleEndian.PutUint32(a.B[target:], 0x13)
			if NativeNonCollectingGCCall(a.B, uint32(site+8), 4) {
				t.Fatal("changed thunk guard admitted")
			}
		}
	}
}
