//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func thunkPointerRV64(b []byte) uintptr { return uintptr(unsafe.Pointer(&b[0])) }
func thunkImageRV64(t *testing.T, code []byte) uintptr {
	t.Helper()
	mem, entry, err := coreruntime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := coreruntime.Unmap(mem); err != nil {
			t.Error(err)
		}
	})
	return entry
}

func thunkFixtureRV64(t *testing.T, capacity int) (*coreruntime.Engine, *coreruntime.JobMemory, []byte, []byte, []byte, []byte) {
	t.Helper()
	n, err := coreruntime.HostCtrlFrameBytesForSlots(capacity)
	if err != nil {
		t.Fatal(err)
	}
	e, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	j, err := coreruntime.NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	ar, err := coreruntime.NewArena(n + 8192)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	args, results, trap, ctrl := ar.Alloc(256*8), ar.Alloc(256*8), ar.Alloc(coreruntime.TrapBufferBytes), ar.Alloc(n)
	j.SetStackFence(e.StackLimit())
	j.SetCustomCtx(thunkPointerRV64(ctrl))
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	return e, j, args, results, trap, ctrl
}

func TestHostIndirectRV64ActualRawTransfersAndOwnedHome(t *testing.T) {
	if HostFixedFrameBytes != coreruntime.HostCtrlFrameBytes {
		t.Fatal("runtime/compiler host layout mismatch")
	}
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, slots := range []int{0, 1, 64, 65, 128, 256} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/owned=%t", slots, owned), func(t *testing.T) {
				e, j, args, results, trap, ctrl := thunkFixtureRV64(t, slots)
				for i := 0; i < slots; i++ {
					binary.LittleEndian.PutUint64(args[i*8:], 0xfeedface80000000^uint64(i))
				}
				code := HostIndirectSyncThunk(0xe1234567, slots, slots)
				if owned {
					code = HostIndirectOwnedSyncThunk(0xe1234567, slots, slots)
				}
				entry := thunkImageRV64(t, code)
				if owned {
					// Owned entry must use pinned X25 even with a different raw A1 carrier.
					var caller enc.Asm
					caller.Addi(enc.SP, enc.SP, -16)
					caller.Sd(enc.RA, enc.SP, 0)
					caller.Constant64(enc.X11, 0x1234)
					caller.Constant64(enc.X31, int64(entry))
					caller.Jalr(enc.RA, enc.X31, 0)
					caller.Ld(enc.RA, enc.SP, 0)
					caller.Addi(enc.SP, enc.SP, 16)
					caller.Ret()
					if err := caller.Err(); err != nil {
						t.Fatal(err)
					}
					entry = thunkImageRV64(t, caller.B)
				}
				calls := 0
				host := func(p uintptr, imp uint32, in, out []uint64) {
					calls++
					if p != thunkPointerRV64(ctrl) || imp != 0xe1234567 || len(in) != slots || len(out) != slots {
						panic("thunk wire metadata")
					}
					for i := range in {
						if in[i] != 0xfeedface80000000^uint64(i) || out[i] != 0 {
							panic("thunk carrier or result clearing")
						}
						out[i] = 0x7ff0000000000001 ^ uint64(i)
					}
				}
				if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil || calls != 1 {
					t.Fatal("thunk execution", calls, err)
				}
				for i := 0; i < slots; i++ {
					if got := binary.LittleEndian.Uint64(results[i*8:]); got != 0x7ff0000000000001^uint64(i) {
						t.Fatal("raw result", i, got)
					}
				}
				if alloc := testing.AllocsPerRun(50, func() {
					if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
						panic(err)
					}
				}); alloc != 0 {
					t.Fatal("warm thunk allocations", alloc)
				}
			})
		}
	}
}

func TestHostIndirectRV64RejectsMissingAndWideProtocolBeforeTransfer(t *testing.T) {
	e, j, args, results, trap, ctrl := thunkFixtureRV64(t, 128)
	code := HostIndirectSyncThunk(0, 65, 65)
	entry := thunkImageRV64(t, code)
	if err := coreruntime.InitHostCtrlFrame(ctrl); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), ctrl...)
	for _, bad := range []string{"nil-control", "nil-trampoline", "magic", "capacity-small", "capacity-large"} {
		t.Run(bad, func(t *testing.T) {
			copy(ctrl, original)
			j.SetCustomCtx(thunkPointerRV64(ctrl))
			switch bad {
			case "nil-control":
				j.SetCustomCtx(0)
			case "nil-trampoline":
				binary.LittleEndian.PutUint64(ctrl[HostTrampolineOffset:], 0)
			case "magic":
				binary.LittleEndian.PutUint32(ctrl[HostFixedFrameBytes:], 0)
			case "capacity-small":
				binary.LittleEndian.PutUint32(ctrl[HostFixedFrameBytes+4:], 64)
			case "capacity-large":
				binary.LittleEndian.PutUint32(ctrl[HostFixedFrameBytes+4:], 65536)
			}
			before := append([]byte(nil), ctrl...)
			witness := bytes.Repeat([]byte{0xa5}, len(results))
			copy(results, witness)
			err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results)
			var nativeTrap *coreruntime.TrapError
			if !errors.As(err, &nativeTrap) || nativeTrap.Code != coreruntime.TrapCalledFnNotLinked || binary.LittleEndian.Uint32(trap) != 0 || !bytes.Equal(ctrl, before) || !bytes.Equal(results, witness) {
				t.Fatal("invalid protocol wrote exchange area", bad, err)
			}
		})
	}
	copy(ctrl, original)
	j.SetCustomCtx(thunkPointerRV64(ctrl))
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) {}); err != nil {
		t.Fatal("reuse after invalid protocol", err)
	}
}

func TestHostIndirectRV64ChecksFullFrameBeforeGuestWrites(t *testing.T) {
	e, j, args, results, trap, ctrl := thunkFixtureRV64(t, 64)
	if err := coreruntime.InitHostCtrlFrame(ctrl); err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, HostIndirectSyncThunk(0, 1, 1))
	// Obtain the actual owning bridge address from a successful park. Its
	// minimum guest frame fits while the thunk's complete48B frame does not.
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) {}); err != nil {
		t.Fatal("initial park", err)
	}
	bridgeSP := uintptr(binary.LittleEndian.Uint64(ctrl[HostSavedPrefixBytes-8:]))
	addr := bridgeSP - 64
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	body := unsafe.Slice((*byte)(pointer), 64)
	for i := range body {
		body[i] = 0xa5
	}
	before := append([]byte(nil), body...)
	j.SetStackFence(bridgeSP - 32)
	err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) { t.Fatal("short frame called host") })
	if err == nil || binary.LittleEndian.Uint32(trap) != uint32(coreruntime.TrapStackFenceBreached) || !bytes.Equal(body, before) {
		t.Fatal("thunk frame wrote before guard", err)
	}
	j.SetStackFence(e.StackLimit())
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) {}); err != nil {
		t.Fatal("thunk reuse after stack trap", err)
	}
}

func TestHostIndirectRV64PassedHomeAndCallerHomeRestoration(t *testing.T) {
	e, callerHome, args, results, trap, rootCtrl := thunkFixtureRV64(t, 64)
	_, passedHome, _, _, _, activeCtrl := thunkFixtureRV64(t, 64)
	passedHome.SetStackFence(e.StackLimit())
	if err := passedHome.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	if err := coreruntime.InitHostCtrlFrame(activeCtrl); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(args, 73)
	entry := thunkImageRV64(t, HostIndirectSyncThunk(37, 1, 1))
	var caller enc.Asm
	caller.Addi(enc.SP, enc.SP, -16)
	caller.Sd(enc.RA, enc.SP, 0)
	caller.Sd(enc.X13, enc.SP, 8)
	caller.Constant64(enc.X11, int64(passedHome.LinMemBase()))
	// This trusted fixture explicitly shares the owning activation's bridge
	// cells. It qualifies passed-home selection, not general context switching.
	caller.Ld(enc.X5, enc.X25, -24)
	caller.Sd(enc.X5, enc.X11, -24)
	caller.Ld(enc.X5, enc.X25, -32)
	caller.Sd(enc.X5, enc.X11, -32)
	caller.Constant64(enc.X31, int64(entry))
	caller.Jalr(enc.RA, enc.X31, 0)
	caller.Ld(enc.X13, enc.SP, 8)
	caller.Sd(enc.X25, enc.X13, 8)
	caller.Ld(enc.RA, enc.SP, 0)
	caller.Addi(enc.SP, enc.SP, 16)
	caller.Ret()
	if err := caller.Err(); err != nil {
		t.Fatal(err)
	}
	entry = thunkImageRV64(t, caller.B)
	calls := 0
	host := func(p uintptr, index uint32, in, out []uint64) {
		calls++
		if p != thunkPointerRV64(activeCtrl) || index != 37 || len(in) != 1 || in[0] != 73 || len(out) != 1 {
			panic("passed-home host control")
		}
		out[0] = 91
	}
	if err := e.CallWithHostBase(entry, args, callerHome.LinMemBase(), trap, results, rootCtrl, host); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || binary.LittleEndian.Uint64(results) != 91 || binary.LittleEndian.Uint64(results[8:]) != uint64(callerHome.LinMemBase()) || binary.LittleEndian.Uint64(rootCtrl) != 0 {
		t.Fatal("passed-home selection or original pinned-home restoration")
	}
}
