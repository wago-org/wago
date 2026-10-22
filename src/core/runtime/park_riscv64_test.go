//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"bytes"
	"encoding/binary"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"golang.org/x/sys/unix"
)

var parkGPRegisters = [...]enc.Reg{enc.X8, enc.X9, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23, enc.X24}
var parkFPRegisters = [...]enc.FReg{enc.F8, enc.F9, enc.F18, enc.F19, enc.F20, enc.F21, enc.F22, enc.F23, enc.F24, enc.F25, enc.F26, enc.F27}
var parkFPBits = [...]uint64{0x7ff0000000000001, 0x7ff812345678abcd, 0x8000000000000000, 0xffffffff80000000, 0x000000007fc12345, 0x0123456789abcdef, 0xfedcba9876543210, 0, 0xffffffffffffffff, 0x3ff0000000000000, 0x0010000000000000, 0x8000000000000001}

const parkTraceBytes = 24 * 8

func rv64ReadFCSR() uint64
func rv64SetFCSR(value uint64)
func rv64ClobberParkFP()

func parkImageRV64(t *testing.T, stub uintptr, crossings int, cold bool) uintptr {
	t.Helper()
	var a enc.Asm
	// This trusted probe has a framed, independently checked entry descent.
	a.Ld(enc.X5, enc.X25, -72)
	a.Addi(enc.X6, enc.SP, -32)
	a.Branch(enc.GEU, enc.X6, enc.X5, 16)
	a.Constant64(enc.X5, int64(TrapStackFenceBreached))
	a.Sw(enc.X5, enc.X12, 0)
	a.Ret()
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X13, enc.SP, 8)
	for i, r := range parkGPRegisters {
		a.Constant64(r, int64(0x800+int(r)+i*32))
	}
	for i, r := range parkFPRegisters {
		a.Constant64(enc.X5, int64(parkFPBits[i]))
		a.FmvDX(r, enc.X5)
	}
	a.Constant64(enc.X5, 0x5f)
	a.WriteFcsr(enc.X5)
	for round := 0; round < crossings; round++ {
		a.Constant64(enc.X31, int64(stub))
		a.Jalr(enc.RA, enc.X31, 0)
		a.Ld(enc.X6, enc.SP, 8)
		if round != 0 {
			a.Addi(enc.X6, enc.X6, int32(round*parkTraceBytes))
		}
		for i, r := range parkGPRegisters {
			a.Sd(r, enc.X6, int32(i*8))
		}
		for i, r := range parkFPRegisters {
			a.FmvXD(enc.X5, r)
			a.Sd(enc.X5, enc.X6, int32((len(parkGPRegisters)+i)*8))
		}
		a.ReadFcsr(enc.X5)
		a.Sd(enc.X5, enc.X6, 21*8)
		a.Sd(enc.X25, enc.X6, 22*8)
		a.Sd(enc.SP, enc.X6, 23*8)
	}
	if cold {
		a.Constant64(enc.X5, int64(TrapUnreachable))
		// A2 is caller-saved across a host park; reload the stable trap pointer.
		a.Ld(enc.X12, enc.X25, -104)
		a.Sw(enc.X5, enc.X12, 0)
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

func parkFixtureRV64(t *testing.T, rounds int, cold bool) (*Engine, *JobMemory, []byte, []byte, []byte, uintptr) {
	t.Helper()
	e, j, ar := productionFixtureRV64(t, 0)
	ctrl, trap, trace := ar.Alloc(int(rv64ParkFrameBytes)), ar.Alloc(TrapBufferBytes), ar.Alloc(rounds*parkTraceBytes)
	j.SetCustomCtx(slicePtr(ctrl))
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	code, err := rv64ParkStub()
	if err != nil {
		t.Fatal(err)
	}
	stub := productionImageRV64(t, &enc.Asm{B: code})
	return e, j, ctrl, trap, trace, parkImageRV64(t, stub, rounds, cold)
}

func checkParkTraceRV64(t *testing.T, trace []byte, home, sp uintptr) {
	t.Helper()
	for start := 0; start < len(trace); start += parkTraceBytes {
		row := trace[start:]
		for i, r := range parkGPRegisters {
			if got := binary.LittleEndian.Uint64(row[i*8:]); got != uint64(0x800+int(r)+i*32) {
				t.Fatalf("round %d GP%d=%#x", start/parkTraceBytes, r, got)
			}
		}
		for i, r := range parkFPRegisters {
			if got := binary.LittleEndian.Uint64(row[(len(parkGPRegisters)+i)*8:]); got != parkFPBits[i] {
				t.Fatalf("round %d FP%d=%#x", start/parkTraceBytes, r, got)
			}
		}
		if binary.LittleEndian.Uint64(row[21*8:]) != 0x5f || binary.LittleEndian.Uint64(row[22*8:]) != uint64(home) || binary.LittleEndian.Uint64(row[23*8:]) != uint64(sp) {
			t.Fatal("FCSR/home/SP lost", row)
		}
	}
}

func TestProductionRV64InternalParkResumeGPFPFcsrAndCold(t *testing.T) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	old := rv64ReadFCSR()
	defer rv64SetFCSR(old)
	for _, cold := range []bool{false, true} {
		e, j, ctrl, trap, trace, entry := parkFixtureRV64(t, 3, cold)
		rv64SetFCSR(0x7f)
		enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
		parkedSP := uintptr(binary.LittleEndian.Uint64(ctrl))
		for round := 0; round < 3; round++ {
			if loadTrap(trap) != rv64ParkPending || binary.LittleEndian.Uint64(trap[8:]) != uint64(slicePtr(ctrl)) {
				t.Fatal("pending control publication", trap)
			}
			if got := rv64ReadFCSR(); got != uint64(0x7f-round*32) {
				t.Fatal("guest FCSR leaked to Go", round, got)
			}
			if binary.LittleEndian.Uint64(ctrl[unsafe.Offsetof(rv64ParkFrame{}.FCSR):]) != 0x5f {
				t.Fatal("guest FCSR not parked")
			}
			// Real Go execution between crossings: allocation, GC, scheduling and FP
			// clobbering occur only after the syscall bridge has reacquired a P.
			b := make([]byte, 4096)
			b[round] = 1
			goruntime.GC()
			goruntime.Gosched()
			goruntime.KeepAlive(b)
			rv64ClobberParkFP()
			rv64SetFCSR(uint64(0x5f - round*32))
			clearTrapUnlessInterrupted(trap)
			resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
		}
		if got := rv64ReadFCSR(); got != 0x1f {
			t.Fatal("resume leaked FCSR", got)
		}
		want := uint32(0)
		if cold {
			want = uint32(TrapUnreachable)
		}
		if loadTrap(trap) != want {
			t.Fatal("completion trap", loadTrap(trap))
		}
		checkParkTraceRV64(t, trace, j.LinMemBase(), parkedSP)
	}
}

func TestProductionRV64InternalParkResumeOnDifferentGoThread(t *testing.T) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	e, j, ctrl, trap, trace, entry := parkFixtureRV64(t, 1, false)
	tid := unix.Gettid()
	enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
	if loadTrap(trap) != rv64ParkPending {
		t.Fatal("not parked")
	}
	sp := uintptr(binary.LittleEndian.Uint64(ctrl))
	clearTrapUnlessInterrupted(trap)
	done := make(chan int, 1)
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		current := unix.Gettid()
		goruntime.GC()
		resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
		done <- current
	}()
	if next := <-done; next == tid {
		t.Fatal("test did not migrate OS threads", tid)
	}
	if loadTrap(trap) != 0 {
		t.Fatal("migrated completion", loadTrap(trap))
	}
	checkParkTraceRV64(t, trace, j.LinMemBase(), sp)
}

func TestProductionRV64InternalResumeRejectsBeforeStackWrites(t *testing.T) {
	e, j, ctrl, trap, trace, entry := parkFixtureRV64(t, 1, false)
	enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
	if loadTrap(trap) != rv64ParkPending {
		t.Fatal("not parked")
	}
	original := append([]byte(nil), ctrl...)
	before := append([]byte(nil), e.stack...)
	sp := binary.LittleEndian.Uint64(ctrl)
	for _, bad := range []uint64{sp - 1, uint64(e.StackLimit() - 16), uint64(e.stackTop - nativeWrapperSaveBytes)} {
		copy(ctrl, original)
		binary.LittleEndian.PutUint64(ctrl, bad)
		clear(trap)
		resumeNativeRV64Raw(slicePtr(ctrl), slicePtr(trap), e.stackTop)
		if loadTrap(trap) != uint32(TrapStackFenceBreached) || !bytes.Equal(e.stack, before) {
			t.Fatal("invalid saved SP wrote foreign stack", bad)
		}
	}
	copy(ctrl, original)
	for _, bad := range []uintptr{e.stackTop - 1, e.stackTop - 16, e.stackTop + 16, e.StackLimit() - 16, e.StackLimit() + uintptr(nativeWrapperSaveBytes), e.StackLimit() + uintptr(nativeWrapperSaveBytes) + 15} {
		clear(trap)
		resumeNativeRV64Raw(slicePtr(ctrl), slicePtr(trap), bad)
		if loadTrap(trap) != uint32(TrapStackFenceBreached) || !bytes.Equal(e.stack, before) {
			t.Fatal("invalid bridge top wrote foreign stack", bad)
		}
	}
	for _, bad := range []uint64{0, binary.LittleEndian.Uint64(original[8:]) + 1, binary.LittleEndian.Uint64(original[8:]) + 2} {
		copy(ctrl, original)
		binary.LittleEndian.PutUint64(ctrl[8:], bad)
		clear(trap)
		resumeNativeRV64Raw(slicePtr(ctrl), slicePtr(trap), e.stackTop)
		if loadTrap(trap) != uint32(TrapStackFenceBreached) || !bytes.Equal(e.stack, before) {
			t.Fatal("invalid saved RA wrote foreign stack", bad)
		}
	}
	copy(ctrl, original)
	clear(trap)
	resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
	if loadTrap(trap) != 0 {
		t.Fatal("resume after rejected state", loadTrap(trap))
	}
	checkParkTraceRV64(t, trace, j.LinMemBase(), uintptr(sp))
}

func TestProductionRV64InternalParkPreservesInterrupted(t *testing.T) {
	e, j, _, trap, trace, entry := parkFixtureRV64(t, 1, false)
	for _, code := range []uint32{uint32(TrapInterrupted), uint32(TrapUnreachable), 0xffffffff} {
		storeTrap(trap, code)
		enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
		if loadTrap(trap) != code {
			t.Fatal("park overwrote preexisting status", code, loadTrap(trap))
		}
	}
	for range 64 {
		storeTrap(trap, 0)
		done := make(chan struct{})
		go func() { storeTrap(trap, uint32(TrapInterrupted)); close(done) }()
		enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
		<-done
		if atomic.LoadUint32((*uint32)(unsafe.Pointer(&trap[0]))) != uint32(TrapInterrupted) {
			t.Fatal("pending erased interrupt")
		}
	}
}

func TestProductionRV64InternalParkNestedEntryAndWarmReuse(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, ctrl, trap, trace, entry := parkFixtureRV64(t, 1, false)
	inner, innerMemory, innerArena := productionFixtureRV64(t, 0)
	innerTrap, innerResult := innerArena.Alloc(TrapBufferBytes), innerArena.Alloc(8)
	var a enc.Asm
	a.Constant64(enc.X5, 123)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ret()
	innerEntry := productionImageRV64(t, &a)
	j.putU32(offMaxLinMemPages, 0x12345678)
	enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
	if loadTrap(trap) != rv64ParkPending {
		t.Fatal("not parked")
	}
	parked := append([]byte(nil), ctrl...)
	if err := inner.CallPrepared(innerEntry, nil, innerMemory.LinMemBase(), innerTrap, innerResult); err != nil || binary.LittleEndian.Uint64(innerResult) != 123 {
		t.Fatal("nested Go native invocation", err)
	}
	if !bytes.Equal(ctrl, parked) {
		t.Fatal("nested entry changed suspended activation")
	}
	clearTrapUnlessInterrupted(trap)
	resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
	if loadTrap(trap) != 0 || j.getU32(offMaxLinMemPages) != 0x12345678 {
		t.Fatal("nested completion clobbered max-pages or trap")
	}
	checkParkTraceRV64(t, trace, j.LinMemBase(), uintptr(binary.LittleEndian.Uint64(ctrl)))
	if n := testing.AllocsPerRun(100, func() {
		clearTrapUnlessInterrupted(trap)
		enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), slicePtr(trace), e.stackTop)
		if loadTrap(trap) != rv64ParkPending {
			panic("warm entry did not park")
		}
		clearTrapUnlessInterrupted(trap)
		resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
		if loadTrap(trap) != 0 {
			panic("warm completion trapped")
		}
	}); n != 0 {
		t.Fatal("warm park/resume allocations", n)
	}
}

func TestProductionRV64InternalResumeRebindsTrapForReparkAndCold(t *testing.T) {
	e, j, ctrl, oldTrap, trace, entry := parkFixtureRV64(t, 2, true)
	nextTrap, err := mmapRW(TrapBufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(nextTrap)
	nextTrap = nextTrap[:TrapBufferBytes]
	binary.LittleEndian.PutUint32(nextTrap[4:], 0xa5a5a5a5)
	binary.LittleEndian.PutUint64(nextTrap[16:], 0x1122334455667788)
	enterNative(entry, 0, j.LinMemBase(), slicePtr(oldTrap), slicePtr(trace), e.stackTop)
	if loadTrap(oldTrap) != rv64ParkPending {
		t.Fatal("not parked")
	}
	resumeNativeRV64(slicePtr(ctrl), slicePtr(nextTrap), e.stackTop)
	if loadTrap(nextTrap) != rv64ParkPending || binary.LittleEndian.Uint64(nextTrap[8:]) != uint64(slicePtr(ctrl)) || j.getU64(104) != uint64(slicePtr(nextTrap)) || binary.LittleEndian.Uint32(nextTrap[4:]) != 0xa5a5a5a5 || binary.LittleEndian.Uint64(nextTrap[16:]) != 0x1122334455667788 {
		t.Fatal("resume/repark used stale trap binding")
	}
	clearTrapUnlessInterrupted(nextTrap)
	// The reset intentionally clears source metadata. Re-seed the witness for
	// the actual resume/cold machinery rather than attributing reset to it.
	binary.LittleEndian.PutUint64(nextTrap[16:], 0x1122334455667788)
	resumeNativeRV64(slicePtr(ctrl), slicePtr(nextTrap), e.stackTop)
	if loadTrap(nextTrap) != uint32(TrapUnreachable) || loadTrap(oldTrap) != rv64ParkPending || binary.LittleEndian.Uint32(nextTrap[4:]) != 0xa5a5a5a5 || binary.LittleEndian.Uint64(nextTrap[16:]) != 0x1122334455667788 {
		t.Fatal("cold resume corrupted trap binding or neighboring fields")
	}
	checkParkTraceRV64(t, trace, j.LinMemBase(), uintptr(binary.LittleEndian.Uint64(ctrl)))
}

func TestProductionRV64InternalResumeMinimumFrameAndNilControl(t *testing.T) {
	e, j, ctrl, trap, _, _ := parkFixtureRV64(t, 1, false)
	code, err := rv64ParkStub()
	if err != nil {
		t.Fatal(err)
	}
	stub := productionImageRV64(t, &enc.Asm{B: code})
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.Constant64(enc.X31, int64(stub))
	a.Jalr(enc.RA, enc.X31, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	entry := productionImageRV64(t, &a)
	enterNative(entry, 0, j.LinMemBase(), slicePtr(trap), 0, e.stackTop)
	if loadTrap(trap) != rv64ParkPending || binary.LittleEndian.Uint64(ctrl) != uint64(e.stackTop-nativeWrapperSaveBytes-16) {
		t.Fatal("minimum parked frame not at exact upper bound")
	}
	before := append([]byte(nil), e.stack...)
	otherTrap, err := mmapRW(TrapBufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(otherTrap)
	bound := j.getU64(104)
	resumeNativeRV64Raw(slicePtr(ctrl), slicePtr(otherTrap), e.stackTop-16)
	if loadTrap(otherTrap) != uint32(TrapStackFenceBreached) || j.getU64(104) != bound || !bytes.Equal(before, e.stack) {
		t.Fatal("rejected resume rebound the active trap or wrote foreign stack")
	}
	clear(trap)
	resumeNativeRV64Raw(0, slicePtr(trap), e.stackTop)
	if loadTrap(trap) != uint32(TrapStackFenceBreached) || !bytes.Equal(before, e.stack) {
		t.Fatal("nil control did not reject before writes")
	}
	clear(trap)
	resumeNativeRV64(slicePtr(ctrl), slicePtr(trap), e.stackTop)
	if loadTrap(trap) != 0 {
		t.Fatal("exact upper bound rejected", loadTrap(trap))
	}
}
