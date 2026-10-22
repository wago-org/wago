//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func hostFixtureRV64(t *testing.T, capacity int) (*Engine, *JobMemory, []byte, []byte, []byte, []byte) {
	t.Helper()
	n, err := HostCtrlFrameBytesForSlots(capacity)
	if err != nil {
		t.Fatal(err)
	}
	e, j, _ := productionFixtureRV64(t, 0)
	arena, err := NewArena(n + 8192)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := arena.Close(); err != nil {
			t.Error(err)
		}
	})
	args, results, trap, ctrl := arena.Alloc(256*8), arena.Alloc(256*8), arena.Alloc(TrapBufferBytes), arena.Alloc(n)
	j.SetCustomCtx(slicePtr(ctrl))
	return e, j, args, results, trap, ctrl
}

func hostRoundtripRV64(t *testing.T, n, nres, rounds int, cold bool) uintptr {
	return hostRoundtripImportRV64(t, n, nres, rounds, cold, 7)
}

func hostRoundtripImportRV64(t *testing.T, n, nres, rounds int, cold bool, firstImport uint32) uintptr {
	t.Helper()
	var a enc.Asm
	a.Ld(enc.X5, enc.X25, -72)
	a.Addi(enc.X6, enc.SP, -32)
	a.Branch(enc.GEU, enc.X6, enc.X5, 16)
	a.Constant64(enc.X5, int64(TrapStackFenceBreached))
	a.Sw(enc.X5, enc.X12, 0)
	a.Ret()
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X13, enc.SP, 8)
	a.Sd(enc.X10, enc.SP, 16)
	wide := n > maxHostArity || nres > maxHostArity
	for round := 0; round < rounds; round++ {
		a.Ld(enc.X28, enc.X25, -offCustomCtx)
		argOffset := hcArgs
		if wide {
			argOffset = ctrlFrameSize + hostCtrlExtensionHead
		}
		a.Addi(enc.X6, enc.X28, int32(argOffset))
		a.Ld(enc.X7, enc.SP, 16)
		for range n {
			a.Ld(enc.X5, enc.X7, 0)
			a.Sd(enc.X5, enc.X6, 0)
			a.Addi(enc.X7, enc.X7, 8)
			a.Addi(enc.X6, enc.X6, 8)
		}
		a.Constant64(enc.X5, int64(firstImport)+int64(round))
		a.Sw(enc.X5, enc.X28, int32(hcImportIdx))
		a.Constant64(enc.X5, int64(uint32(n)|uint32(nres)<<16))
		a.Sw(enc.X5, enc.X28, int32(hcNArgs))
		a.Ld(enc.X31, enc.X28, int32(hcTrampoline))
		a.Jalr(enc.RA, enc.X31, 0)
		a.Ld(enc.X28, enc.X25, -offCustomCtx)
		if wide {
			a.Lwu(enc.X5, enc.X28, int32(ctrlFrameSize+4))
			a.Slli(enc.X5, enc.X5, 3)
			a.Add(enc.X6, enc.X28, enc.X5)
			a.Addi(enc.X6, enc.X6, int32(ctrlFrameSize+hostCtrlExtensionHead))
		} else {
			a.Addi(enc.X6, enc.X28, int32(hcResults))
		}
		a.Ld(enc.X7, enc.SP, 8)
		for range nres {
			a.Ld(enc.X5, enc.X6, 0)
			a.Sd(enc.X5, enc.X7, 0)
			a.Addi(enc.X6, enc.X6, 8)
			a.Addi(enc.X7, enc.X7, 8)
		}
	}
	if cold {
		a.Ld(enc.X12, enc.X25, -104)
		a.Constant64(enc.X5, int64(TrapUnreachable))
		a.Sw(enc.X5, enc.X12, 0)
		a.Constant64(enc.X5, 0x1234567800000001)
		a.Sd(enc.X5, enc.X12, 16)
		a.Ld(enc.SP, enc.X25, -int32(RV64TrapReentrySPOffset))
		a.Ld(enc.X31, enc.X25, -int32(RV64TrapContinuationOffset))
		a.Jalr(enc.Zero, enc.X31, 0)
	} else {
		a.Ld(enc.RA, enc.SP, 0)
		a.Addi(enc.SP, enc.SP, 32)
		a.Ret()
	}
	return productionImageRV64(t, &a)
}

func TestProductionRV64HostFrameLayoutSizingAndInvalidExtension(t *testing.T) {
	if rv64ParkFrameBytes != 208 || hcSavedLinMem != 88 || hcTrampoline != 208 || hcImportIdx != 216 || hcNArgs != 220 || hcArgs != 224 || hcResults != 736 || ctrlFrameSize != 1248 {
		t.Fatal("RV64 host layout drift")
	}
	for _, slots := range []int{-1, MaxSyncHostSlots + 1} {
		if n, err := HostCtrlFrameBytesForSlots(slots); err == nil || n != 0 {
			t.Fatal("invalid slots", slots, n, err)
		}
	}
	for _, slots := range []int{0, 1, 64, 65, 128, 256, MaxSyncHostSlots} {
		t.Run(fmt.Sprint(slots), func(t *testing.T) {
			n, err := HostCtrlFrameBytesForSlots(slots)
			if err != nil {
				t.Fatal(err)
			}
			arena, err := NewArena(n)
			if err != nil {
				t.Fatal(err)
			}
			defer arena.Close()
			ctrl := arena.Alloc(n)
			if err := InitHostCtrlFrame(ctrl); err != nil {
				t.Fatal(err)
			}
			cap, err := hostCtrlFrameCapacity(ctrl)
			want := slots
			if want < 64 {
				want = 64
			}
			if err != nil || cap != want || binary.LittleEndian.Uint64(ctrl[hcTrampoline:]) == 0 {
				t.Fatal("capacity/trampoline", cap, err)
			}
			if err := RegisterHostCtrlFrame(ctrl); err != nil {
				t.Fatal(err)
			}
			if slots > 64 {
				if err := RegisterHostCtrlFrame(ctrl); err == nil {
					t.Fatal("duplicate frame registration accepted")
				}
			}
			UnregisterHostCtrlFrame(ctrl)
			if slots > 64 {
				binary.LittleEndian.PutUint32(ctrl[ctrlFrameSize:], 0)
				if _, err := hostCtrlFrameCapacity(ctrl); err == nil {
					t.Fatal("bad wide magic accepted")
				}
			}
		})
	}
	for _, n := range []int{0, ctrlFrameSize - 1, ctrlFrameSize + 1, ctrlFrameSize + hostCtrlExtensionHead + 64*16, ctrlFrameSize + hostCtrlExtensionHead + 65*16 + 1} {
		ctrl := make([]byte, n)
		if err := InitHostCtrlFrame(ctrl); err == nil {
			t.Fatal("malformed frame accepted", n)
		}
	}
}

func TestProductionRV64HostRoundtripRawSlotsWideAndWarm(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	for _, n := range []int{0, 1, 2, 64, 65, 128, 256} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			nres := n
			if n == 0 {
				nres = 1
			}
			capacity := n
			if capacity < nres {
				capacity = nres
			}
			e, j, args, results, trap, ctrl := hostFixtureRV64(t, capacity)
			for i := 0; i < n; i++ {
				binary.LittleEndian.PutUint64(args[i*8:], parkFPBits[i%len(parkFPBits)]^uint64(i))
			}
			entry := hostRoundtripRV64(t, n, nres, 3, false)
			calls := 0
			host := func(active uintptr, imp uint32, in, out []uint64) {
				if active != slicePtr(ctrl) || imp != uint32(7+calls%3) || len(in) != n || len(out) != nres {
					panic("host wire metadata")
				}
				for i := range in {
					if in[i] != binary.LittleEndian.Uint64(args[i*8:]) {
						panic("raw argument carrier")
					}
				}
				for i := range out {
					if out[i] != 0 {
						panic("stale host result")
					}
					out[i] = parkFPBits[(i+1)%len(parkFPBits)] ^ uint64(i+calls%3)
				}
				calls++
			}
			if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatal("crossings", calls)
			}
			for i := 0; i < nres; i++ {
				if got := binary.LittleEndian.Uint64(results[i*8:]); got != parkFPBits[(i+1)%len(parkFPBits)]^uint64(i+2) {
					t.Fatal("raw host result", i, got)
				}
			}
			if alloc := testing.AllocsPerRun(50, func() {
				if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
					panic(err)
				}
			}); alloc != 0 {
				t.Fatal("warm host loop allocations", alloc)
			}
		})
	}
}

func TestProductionRV64HostGoCallbackGCNestedPanicCancelAndColdReuse(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	entry := hostRoundtripRV64(t, 1, 1, 2, false)
	inner, im, ia := productionFixtureRV64(t, 0)
	it, ir := ia.Alloc(TrapBufferBytes), ia.Alloc(8)
	var a enc.Asm
	a.Constant64(enc.X5, 31)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ret()
	innerEntry := productionImageRV64(t, &a)
	calls := 0
	host := func(_ uintptr, _ uint32, in, out []uint64) {
		b := make([]byte, 4096)
		goruntime.GC()
		goruntime.Gosched()
		goruntime.KeepAlive(b)
		if err := inner.CallPrepared(innerEntry, nil, im.LinMemBase(), it, ir); err != nil || binary.LittleEndian.Uint64(ir) != 31 {
			panic("nested invocation")
		}
		out[0] = in[0] + 9
		calls++
	}
	binary.LittleEndian.PutUint64(args, 13)
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil || calls != 2 || binary.LittleEndian.Uint64(results) != 22 {
		t.Fatal("callback/nested", calls, err)
	}
	marker := new(int)
	func() {
		defer func() {
			if recover() != marker {
				t.Error("callback panic lost")
			}
		}()
		_ = e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) { panic(marker) })
	}()
	if e.hostScratchInUse {
		t.Fatal("panic retained engine scratch lease")
	}
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
		t.Fatal("reuse after panic", err)
	}
	cancelCalls := 0
	err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) { cancelCalls++; storeTrap(trap, uint32(TrapInterrupted)) })
	if err == nil || cancelCalls != 1 || loadTrap(trap) != uint32(TrapInterrupted) {
		t.Fatal("host cancel resumed guest", cancelCalls, err)
	}
	storeTrap(trap, 0)
	cold := hostRoundtripRV64(t, 1, 1, 1, true)
	if err := e.CallWithHostBase(cold, args, j.LinMemBase(), trap, results, ctrl, host); err == nil || loadTrap(trap) != uint32(TrapUnreachable) || binary.LittleEndian.Uint64(trap[16:]) != 0x1234567800000001 {
		t.Fatal("cold trap/source", err)
	}
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
		t.Fatal("reuse after cold", err)
	}
}

func TestProductionRV64HostPublishesExactForeignWideControl(t *testing.T) {
	e, j, args, results, trap, root := hostFixtureRV64(t, 64)
	_, _, _, _, _, active := hostFixtureRV64(t, 128)
	if err := InitHostCtrlFrame(active); err != nil {
		t.Fatal(err)
	}
	if err := RegisterHostCtrlFrame(active); err != nil {
		t.Fatal(err)
	}
	defer UnregisterHostCtrlFrame(active)
	j.SetCustomCtx(slicePtr(active))
	entry := hostRoundtripRV64(t, 65, 65, 1, false)
	seen := false
	host := func(p uintptr, _ uint32, in, out []uint64) {
		if p != slicePtr(active) || len(in) != 65 || len(out) != 65 {
			panic("wrong active wide frame")
		}
		seen = true
		for i := range out {
			out[i] = uint64(i + 1)
		}
	}
	if err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, root, host); err != nil || !seen {
		t.Fatal("foreign wide loop", err)
	}
	if binary.LittleEndian.Uint64(root) != 0 || binary.LittleEndian.Uint64(results[64*8:]) != 65 {
		t.Fatal("root frame or wide result corrupted")
	}
}

func TestProductionRV64HostRejectsOverCapacityBeforeCallback(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.Ld(enc.X28, enc.X25, -offCustomCtx)
	a.Constant64(enc.X5, 65)
	a.Sw(enc.X5, enc.X28, int32(hcNArgs))
	a.Ld(enc.X31, enc.X28, int32(hcTrampoline))
	a.Jalr(enc.RA, enc.X31, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	entry := productionImageRV64(t, &a)
	before := bytes.Repeat([]byte{0xa5}, len(results))
	copy(results, before)
	called := false
	err := e.CallWithHostBase(entry, args, j.LinMemBase(), trap, results, ctrl, func(uintptr, uint32, []uint64, []uint64) { called = true })
	if err == nil || !strings.Contains(err.Error(), "capacity") || called || !bytes.Equal(before, results) {
		t.Fatal("overcapacity host call", called, err)
	}
	if uintptr(unsafe.Sizeof(rv64ParkFrame{})) != uintptr(hcTrampoline) {
		t.Fatal("host prefix mismatch")
	}
}

func TestProductionRV64HostScalarFixedViewAndPreparedPortals(t *testing.T) {
	for _, mode := range []string{"scalar-handled", "scalar-fallback", "expanded", "fixed", "view", "view-fallback", "fixed-view", "prepared", "prepared-fixed"} {
		t.Run(mode, func(t *testing.T) {
			e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
			nres := 1
			if mode == "expanded" || mode == "fixed" || mode == "prepared-fixed" {
				nres = 2
			}
			entry := hostRoundtripImportRV64(t, 2, nres, 1, false, 0)
			binary.LittleEndian.PutUint64(args, 0xdeadbeef80000001)
			binary.LittleEndian.PutUint64(args[8:], 0x1122334455667788)
			const packed = uint64(0xabcdef1280000001)
			generic, portal := 0, 0
			host := func(p uintptr, imp uint32, in, out []uint64) {
				generic++
				if p != slicePtr(ctrl) || imp != 0 || len(in) != 2 || in[0] != 0xdeadbeef80000001 || in[1] != 0x1122334455667788 {
					panic("generic portal input")
				}
				for i := range out {
					out[i] = uint64(101 + i)
				}
			}
			scalar := func(p uintptr, imp, raw uint32, a0, a1 uint64) (uint64, bool) {
				portal++
				if p != slicePtr(ctrl) || imp != 0 || raw != uint32(2|nres<<16) || a0 != 0xdeadbeef80000001 || a1 != 0x1122334455667788 {
					panic("scalar portal raw input")
				}
				return packed, mode != "scalar-fallback"
			}
			fixed := func(a0, a1 uint64) uint64 {
				portal++
				if a0 != 0xdeadbeef80000001 || a1 != 0x1122334455667788 {
					panic("fixed input")
				}
				return packed
			}
			view := func(p uintptr, imp uint32, in, out []uint64) bool {
				portal++
				if p != slicePtr(ctrl) || imp != 0 || len(in) != 2 || len(out) != nres || uintptr(unsafe.Pointer(&in[0])) != slicePtr(ctrl)+uintptr(hcArgs) || uintptr(unsafe.Pointer(&out[0])) != slicePtr(ctrl)+uintptr(hcResults) {
					panic("view borrowed layout")
				}
				if mode == "view-fallback" {
					return false
				}
				out[0] = packed
				return true
			}
			fixedView := func(in, out []uint64) {
				portal++
				if in[0] != 0xdeadbeef80000001 || in[1] != 0x1122334455667788 || len(out) != nres || uintptr(unsafe.Pointer(&in[0])) != slicePtr(ctrl)+uintptr(hcArgs) || uintptr(unsafe.Pointer(&out[0])) != slicePtr(ctrl)+uintptr(hcResults) {
					panic("fixed view input")
				}
				out[0] = packed
			}
			var err error
			switch mode {
			case "scalar-handled", "scalar-fallback":
				err = e.CallWithHostBaseScalar(entry, args, j.LinMemBase(), trap, results, ctrl, host, scalar)
			case "expanded":
				err = e.CallWithHostBaseScalarExpanded(entry, args, j.LinMemBase(), trap, results, ctrl, host, scalar)
			case "fixed":
				err = e.CallWithHostBaseScalarFixed(entry, args, j.LinMemBase(), trap, results, ctrl, uint32(2|nres<<16), host, scalar, fixed)
			case "view", "view-fallback":
				err = e.CallWithHostBaseView(entry, args, j.LinMemBase(), trap, results, ctrl, host, view)
			case "fixed-view":
				err = e.CallWithHostBaseFixedView(entry, args, j.LinMemBase(), trap, results, ctrl, uint32(2|nres<<16), host, fixedView)
			case "prepared":
				var p *PreparedHostScalarCall
				p, err = e.PrepareHostScalarCall(runtimebridge.GrantHostScalarCall(), entry, args, j, trap, results, ctrl)
				if err == nil {
					err = p.Call(host, scalar)
				}
			case "prepared-fixed":
				var p *PreparedHostScalarCall
				p, err = e.PrepareHostScalarFixedCall(runtimebridge.GrantHostScalarCall(), entry, args, j, trap, results, ctrl, uint32(2|nres<<16))
				if err == nil {
					err = p.CallFixed(host, scalar, fixed)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			fallback := strings.HasSuffix(mode, "fallback")
			if portal != 1 || fallback && generic != 1 || !fallback && generic != 0 {
				t.Fatal("portal route", portal, generic)
			}
			want := packed
			if fallback {
				want = 101
			}
			if nres == 2 {
				want = packed & 0xffffffff
			}
			if got := binary.LittleEndian.Uint64(results); got != want {
				t.Fatal("portal result", mode, got, want)
			}
			if nres == 2 && binary.LittleEndian.Uint64(results[8:]) != packed>>32 {
				t.Fatal("packed high result")
			}
		})
	}
}

func TestProductionRV64UnknownParkedControlBoundsBeforeRead(t *testing.T) {
	e, j, _ := productionFixtureRV64(t, 0)
	for _, ptr := range []uintptr{0, 1, e.StackLimit(), e.StackLimit() + 1, e.stackTop - 16, e.stackTop, ^uintptr(0) - 15} {
		if frame, err := e.parkedHostCtrlFrame(ptr); err == nil || frame != nil {
			t.Fatal("unowned control extent admitted", ptr, err)
		}
	}
	ptr := e.StackLimit() + 16
	frame := unsafe.Slice((*byte)(offHeapPointer(ptr)), ctrlFrameSize)
	clear(frame)
	binary.LittleEndian.PutUint64(frame[hcSavedLinMem:], uint64(j.LinMemBase()))
	bridge := e.stackTop - uintptr(nativeWrapperSaveBytes)
	binary.LittleEndian.PutUint64(frame, uint64(bridge-16))
	binary.LittleEndian.PutUint64(frame[8:], 0x4000)
	binary.LittleEndian.PutUint64(frame[int(unsafe.Offsetof(rv64ParkFrame{}.BridgeSP)):], uint64(bridge))
	if got, err := e.parkedHostCtrlFrame(ptr); err != nil || len(got) != ctrlFrameSize {
		t.Fatal("bounded fixed stack frame", err)
	}
	for _, offset := range []int{0, 8, hcSavedLinMem, int(unsafe.Offsetof(rv64ParkFrame{}.BridgeSP))} {
		previous := binary.LittleEndian.Uint64(frame[offset:])
		binary.LittleEndian.PutUint64(frame[offset:], previous+1)
		if got, err := e.parkedHostCtrlFrame(ptr); err == nil || got != nil {
			t.Fatal("bad saved continuation", offset, err)
		}
		binary.LittleEndian.PutUint64(frame[offset:], previous)
	}
	// A fixed instance frame has independent lifetime/extent registration.
	_, _, _, _, _, owned := hostFixtureRV64(t, 64)
	if err := RegisterHostCtrlFrame(owned); err != nil {
		t.Fatal(err)
	}
	defer UnregisterHostCtrlFrame(owned)
	if got, err := e.parkedHostCtrlFrame(slicePtr(owned)); err != nil || len(got) != ctrlFrameSize {
		t.Fatal("registered fixed owner", err)
	}
}
