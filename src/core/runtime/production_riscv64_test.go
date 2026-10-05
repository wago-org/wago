//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"golang.org/x/sys/unix"
)

const linMemBytes = 65536

func productionFixtureRV64(t *testing.T, memoryBytes int) (*Engine, *JobMemory, *Arena) {
	t.Helper()
	e, err := NewEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	j, err := NewJobMemory(memoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	j.SetStackFence(e.StackLimit())
	a, err := NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	return e, j, a
}

func productionImageRV64(t *testing.T, a *enc.Asm) uintptr {
	t.Helper()
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	mem, entry, err := MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Unmap(mem); err != nil {
			t.Error(err)
		}
	})
	return entry
}

func TestProductionRV64WrapperArgumentsAndResults(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, linMemBytes)
	args, trap, results := arena.Alloc(16), arena.Alloc(TrapBufferBytes), arena.Alloc(24)
	binary.LittleEndian.PutUint64(args, 0xdeadbeef80000000)
	binary.LittleEndian.PutUint64(args[8:], 0x123456789abcdef0)
	var a enc.Asm
	a.Lwu(enc.X5, enc.X10, 0)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ld(enc.X5, enc.X10, 8)
	a.Sd(enc.X5, enc.X13, 8)
	a.Sd(enc.X11, enc.X13, 16)
	a.Ret()
	entry := productionImageRV64(t, &a)
	for range 10 {
		if err := e.Call(entry, args, j.LinearMemory(), trap, results); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint64(results) != 0x80000000 || binary.LittleEndian.Uint64(results[8:]) != 0x123456789abcdef0 || binary.LittleEndian.Uint64(results[16:]) != uint64(j.LinMemBase()) {
			t.Fatal("production wrapper mapping", results)
		}
	}
}

func TestProductionRV64ZeroMemoryPreparedWrapper(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, 0)
	trap, results := arena.Alloc(TrapBufferBytes), arena.Alloc(8)
	if j.LinMemBase() == 0 || j.getU64(offActualLinMemByteSize64) != 0 {
		t.Fatal("zero logical memory")
	}
	if err := j.RebindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	var a enc.Asm
	a.Addi(enc.X5, enc.Zero, 17)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ret()
	if err := e.CallPrepared(productionImageRV64(t, &a), nil, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 17 {
		t.Fatal(results, err)
	}
}

func TestProductionRV64UnsupportedHostAndGuard(t *testing.T) {
	e, _, arena := productionFixtureRV64(t, 0)
	trap, results := arena.Alloc(TrapBufferBytes), arena.Alloc(8)
	binary.LittleEndian.PutUint64(results, 0x5555)
	called := false
	host := func(uintptr, uint32, []uint64, []uint64) { called = true }
	if err := e.CallWithHost(0, nil, nil, trap, results, nil, host); !errors.Is(err, ErrUnsupported) || called || binary.LittleEndian.Uint64(results) != 0x5555 {
		t.Fatal("host rejection", err, called)
	}
	if m, err := NewJobMemoryGuarded(0, 65536); m != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("guard rejection", m, err)
	}
	if HostInterruptSupported() {
		t.Fatal("unimplemented asynchronous cancellation claimed")
	}
	if HostCtrlFrameBytes != 0 {
		t.Fatal("unimplemented host control frame advertised")
	}
	for _, slots := range []int{-1, 0, 64, 256, MaxSyncHostSlots} {
		if size, err := HostCtrlFrameBytesForSlots(slots); size != 0 || !errors.Is(err, ErrUnsupported) {
			t.Fatal("host frame sizing must reject", slots, size, err)
		}
	}
	ctrl := []byte{0x55, 0xaa, 0x33}
	if err := InitHostCtrlFrame(ctrl); !errors.Is(err, ErrUnsupported) {
		t.Fatal("host frame initialization", err)
	}
	if err := RegisterHostCtrlFrame(ctrl); !errors.Is(err, ErrUnsupported) {
		t.Fatal("host frame registration", err)
	}
	UnregisterHostCtrlFrame(ctrl)
	if !bytes.Equal(ctrl, []byte{0x55, 0xaa, 0x33}) {
		t.Fatal("unsupported frame mutated control bytes")
	}
}

func probeNativeWrapperRaw(code, trace, linMem, trap, results, top uintptr) uint64

//go:nosplit
func probeNativeWrapper(code, trace, linMem, trap, results, top uintptr) uint64 {
	nativeEnterSyscall()
	mask := probeNativeWrapperRaw(code, trace, linMem, trap, results, top)
	nativeExitSyscall()
	return mask
}

func TestProductionRV64SavedRegistersAndLiveGoState(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, linMemBytes)
	trace, trap, results := arena.Alloc(64), arena.Alloc(TrapBufferBytes), arena.Alloc(8)
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	for k, reg := range []enc.Reg{enc.GP, enc.TP, enc.X27, enc.SP, enc.RA, enc.X26} {
		a.Sd(reg, enc.X10, int32(k*8))
	}
	for _, reg := range []enc.Reg{enc.X8, enc.X9, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23, enc.X24, enc.X25, enc.X26} {
		a.Constant64(reg, int64(0xdeadbeef00000000+uint64(reg)))
	}
	a.Addi(enc.X5, enc.Zero, 42)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	entry := productionImageRV64(t, &a)
	for k := 0; k < 64; k++ {
		if mask := probeNativeWrapper(entry, slicePtr(trace), j.LinMemBase(), slicePtr(trap), slicePtr(results), e.stackTop); mask != 0 {
			t.Fatalf("production ABI sentinel %#x", mask)
		}
		if binary.LittleEndian.Uint64(results) != 42 {
			t.Fatal("lost result")
		}
		if k%16 == 0 {
			goruntime.GC()
			goruntime.Gosched()
		}
	}
}

func TestProductionRV64GCWhileGuestActiveWithOneP(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, arena := productionFixtureRV64(t, 0)
	args, trap, results := arena.Alloc(32), arena.Alloc(TrapBufferBytes), arena.Alloc(8)
	binary.LittleEndian.PutUint64(args[8:], 100000000) // bounded 100 ms nanosleep
	started := (*uint64)(unsafe.Pointer(&args[16]))
	collected := (*uint64)(unsafe.Pointer(&args[24]))
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X10, enc.SP, 8)
	a.Sd(enc.X13, enc.SP, 16)
	a.Addi(enc.X5, enc.Zero, 1)
	a.Sd(enc.X5, enc.X10, 16)
	a.Addi(enc.X11, enc.Zero, 0)
	a.Addi(enc.X17, enc.Zero, unix.SYS_NANOSLEEP)
	a.B = binary.LittleEndian.AppendUint32(a.B, 0x00000073) // Linux fixture ECALL
	a.Ld(enc.X10, enc.SP, 8)
	a.Ld(enc.X13, enc.SP, 16)
	a.Ld(enc.X5, enc.X10, 24)
	a.Sd(enc.X5, enc.X13, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 32)
	a.Ret()
	entry := productionImageRV64(t, &a)
	done, stop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for atomic.LoadUint64(started) == 0 {
			select {
			case <-stop:
				return
			default:
				goruntime.Gosched()
			}
		}
		goruntime.GC()
		atomic.StoreUint64(collected, 1)
	}()
	err := e.Call(entry, args, j.LinearMemory(), trap, results)
	close(stop)
	<-done
	if err != nil || binary.LittleEndian.Uint64(results) != 1 {
		t.Fatalf("GC did not finish during production guest entry: result=%d err=%v", binary.LittleEndian.Uint64(results), err)
	}
}

func TestProductionRV64GrownCodeCrossThreadPublication(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		for round := int32(0); round < 4; round++ {
			// Publish on this locked OS thread; consumers lock other threads.
			goruntime.LockOSThread()
			image, err := NewCodeBuffer(1)
			if err != nil {
				goruntime.UnlockOSThread()
				t.Fatal(err)
			}
			var a enc.Asm
			a.Addi(enc.X5, enc.Zero, 42+round)
			a.Sd(enc.X5, enc.X13, 0)
			a.Ret()
			err = image.AppendZeros(5000) // force growth beyond the initial page
			if err == nil {
				err = image.Append(a.B)
			}
			var mem []byte
			var base uintptr
			if err == nil && transfer {
				mem, base, err = image.Take()
				if err == nil {
					err = SealCode(mem)
				}
			} else if err == nil {
				err = image.Seal()
				base = image.Base()
			}
			if err != nil {
				goruntime.UnlockOSThread()
				_ = image.Close()
				_ = Unmap(mem)
				t.Fatal(err)
			}
			var workers sync.WaitGroup
			for range 2 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					goruntime.LockOSThread()
					defer goruntime.UnlockOSThread()
					e, j, arena := productionFixtureRV64(t, 0)
					trap, results := arena.Alloc(TrapBufferBytes), arena.Alloc(8)
					for k := 0; k < 16; k++ {
						if err := e.Call(base+5000, nil, j.LinearMemory(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != uint64(42+round) {
							t.Errorf("production publication result=%d err=%v", binary.LittleEndian.Uint64(results), err)
						}
						if k == 0 {
							goruntime.GC()
						}
					}
				}()
			}
			workers.Wait()
			// Keep the publishing thread reserved until all consumers finish.
			// A consumer therefore cannot reuse it and mask a local-only flush.
			goruntime.UnlockOSThread()
			if err := image.Close(); err != nil {
				t.Fatal(err)
			}
			if err := Unmap(mem); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestProductionRV64ColdTrapUnwindsAllGuestFrames(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, linMemBytes)
	args, trap, results := arena.Alloc(8), arena.Alloc(TrapBufferBytes), arena.Alloc(8)
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -32)
	a.Sd(enc.RA, enc.SP, 0)
	first := a.JumpLong(true)
	a.Addi(enc.X5, enc.Zero, 99) // must never run after nested cold trap
	a.Sd(enc.X5, enc.X13, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 32)
	a.Ret()
	secondEntry := a.Len()
	a.Addi(enc.SP, enc.SP, -48)
	a.Sd(enc.RA, enc.SP, 0)
	second := a.JumpLong(true)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 48)
	a.Ret()
	trapEntry := a.Len()
	a.Addi(enc.SP, enc.SP, -112)
	a.Ld(enc.X12, enc.X25, -104)
	a.Addi(enc.X5, enc.Zero, int32(TrapDivZero))
	a.Sw(enc.X5, enc.X12, 0)
	a.Constant64(enc.X5, int64(uint64(0x1234)<<32|3))
	a.Sd(enc.X5, enc.X12, 16)
	a.Ld(enc.SP, enc.X25, -int32(RV64TrapReentrySPOffset))
	a.Ld(enc.X5, enc.X25, -int32(RV64TrapContinuationOffset))
	a.Jalr(enc.Zero, enc.X5, 0)
	if err := a.PatchLong(first, int64(secondEntry)); err != nil {
		t.Fatal(err)
	}
	if err := a.PatchLong(second, int64(trapEntry)); err != nil {
		t.Fatal(err)
	}
	trapCode := productionImageRV64(t, &a)
	var good enc.Asm
	good.Addi(enc.X5, enc.Zero, 17)
	good.Sd(enc.X5, enc.X13, 0)
	good.Ret()
	goodCode := productionImageRV64(t, &good)
	maxPages := j.getU32(offMaxLinMemPages)
	for range 10 {
		binary.LittleEndian.PutUint64(results, 0x5555)
		err := e.Call(trapCode, args, j.LinearMemory(), trap, results)
		var guestTrap *TrapError
		if !errors.As(err, &guestTrap) || guestTrap.Code != TrapDivZero || len(guestTrap.Frames) != 1 || guestTrap.Frames[0].FunctionIndex != 2 || guestTrap.Frames[0].ProgramCounter != 0x1234 || !guestTrap.Frames[0].HasProgramCounter {
			t.Fatal("cold trap/source", err)
		}
		if binary.LittleEndian.Uint64(results) != 0x5555 || j.getU32(offMaxLinMemPages) != maxPages {
			t.Fatal("trap ran continuation or damaged basedata")
		}
		if err := e.Call(goodCode, nil, j.LinearMemory(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 17 {
			t.Fatal("cold trap reuse", results, err)
		}
	}
}

func TestProductionRV64BridgeBoundsBeforeWrites(t *testing.T) {
	e, j, arena := productionFixtureRV64(t, linMemBytes)
	trap := arena.Alloc(TrapBufferBytes)
	var a enc.Asm
	a.Ret()
	entry := productionImageRV64(t, &a)
	for k := range e.stack {
		e.stack[k] = 0x5a
	}
	before := append([]byte(nil), e.stack...)
	fence := e.StackLimit()
	for _, top := range []uintptr{e.stackTop - 1, fence - 16, fence + uintptr(nativeWrapperSaveBytes), fence + uintptr(nativeWrapperSaveBytes) + 15} {
		clear(trap)
		enterNativeRaw(entry, 0, j.LinMemBase(), slicePtr(trap), 0, top)
		if TrapCode(loadTrap(trap)) != TrapStackFenceBreached || !bytes.Equal(e.stack, before) {
			t.Fatal("bridge writes before bounds", top)
		}
	}
	clear(trap)
	enterNativeRaw(entry, 0, j.LinMemBase(), slicePtr(trap), 0, fence+uintptr(nativeWrapperSaveBytes)+16)
	if loadTrap(trap) != 0 || !bytes.Equal(e.stack[:int(fence-slicePtr(e.stack))], before[:int(fence-slicePtr(e.stack))]) {
		t.Fatal("exact bridge capacity")
	}
}
