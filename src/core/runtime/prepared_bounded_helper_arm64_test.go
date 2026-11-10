//go:build (linux || darwin || windows) && arm64

package runtime

import (
	"encoding/binary"
	goruntime "runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func boundedHelperFixtureCode(index, slots uint32) []byte {
	var a a64.Asm
	a.MovReg64(a64.X26, a64.X1)
	a.MovReg64(a64.X19, a64.X3)
	a.StpPre(a64.LR, a64.X20, a64.SP, -16)
	a.SubImm64(a64.X10, a64.X26, offCustomCtx)
	must(a.Load64(a64.X10, a64.X10, 0))
	must(a.Load32(a64.X11, a64.X0, 0))
	must(a.Store64(a64.X11, a64.X10, hcArgs))
	a.MovImm64(a64.X11, 7)
	must(a.Store64(a64.X11, a64.X10, hcArgs+8))
	a.MovImm64(a64.X11, uint64(index))
	must(a.Store32(a64.X11, a64.X10, hcImportIdx))
	a.MovImm64(a64.X11, uint64(slots))
	must(a.Store32(a64.X11, a64.X10, hcNArgs))
	must(a.Load64(a64.X16, a64.X10, hcTrampoline))
	a.Blr(a64.X16)
	a.SubImm64(a64.X10, a64.X26, offCustomCtx)
	must(a.Load64(a64.X10, a64.X10, 0))
	must(a.Load32(a64.X11, a64.X10, hcArgs))
	must(a.Load32(a64.X12, a64.X10, hcArgs+8))
	a.Add32(a64.X11, a64.X11, a64.X12)
	must(a.Store32(a64.X11, a64.X19, 0))
	a.LdpPost(a64.LR, a64.X20, a64.SP, 16)
	a.Ret()
	return a.B
}

func TestBoundedHelperBorrowedFrame(t *testing.T) {
	const identity = uint32(0x40000028)
	for _, tc := range []struct {
		name             string
		index, slots     uint32
		wantCall, wantOK bool
	}{
		{"accepted", identity | 0x100, 2, true, true},
		{"different-helper", identity + 1, 2, false, false},
		{"overlapping-tag", identity | 0x80000000, 2, false, false},
		{"different-shape", identity, 3, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, jm, ar := fixture(t)
			code, err := mmapExec(boundedHelperFixtureCode(tc.index, tc.slots))
			if err != nil {
				t.Fatal(err)
			}
			defer munmap(code)
			args, results, trap, ctrl := hostCallFixture(t, jm, ar)
			binary.LittleEndian.PutUint32(args, 20)
			p, err := eng.PrepareHostScalarCall(runtimebridge.GrantHostScalarCall(), slicePtr(code), args, jm, trap, results, ctrl)
			if err != nil {
				t.Fatal(err)
			}
			eng.hostScratchInUse = true
			eng.hostArgs[0] = 12345
			calls := 0
			err = p.CallBoundedHelper(runtimebridge.GrantHostScalarCall(), 0xe00000ff, identity, func(frame uintptr, index uint32, values, out []uint64) {
				calls++
				goruntime.GC()
				if frame != slicePtr(ctrl) || index != tc.index || len(values) != 2 || values[0] != 20 || values[1] != 7 || len(out) != 0 {
					t.Fatalf("bad borrowed frame: %x %x %v %v", frame, index, values, out)
				}
				if slicePtr(ctrl)+hcArgs != uintptr(unsafe.Pointer(&values[0])) {
					t.Fatal("arguments were copied instead of borrowed")
				}
			})
			if (err == nil) != tc.wantOK || (calls != 0) != tc.wantCall {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if !eng.hostScratchInUse || eng.hostArgs[0] != 12345 {
				t.Fatal("enclosing scratch state changed")
			}
			if tc.wantOK && binary.LittleEndian.Uint32(results) != 27 {
				t.Fatalf("continuation result=%d", binary.LittleEndian.Uint32(results))
			}
		})
	}
}
