//go:build linux && amd64 && tinygo && scheduler.threads

package runtime

import (
	"bytes"
	"encoding/binary"
	"errors"
	grt "runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// Native code publishes entry and waits for the collector to release it. This
// handshake makes collection while SP is foreign deterministic, not a replay
// probability. The aligned x86 loads/stores match the host atomic flag accesses.
func TestTinyGoForeignStackConcurrentGC(t *testing.T) {
	for _, tc := range []TrapCode{TrapNone, TrapLinMemOutOfBounds, TrapInterrupted} {
		t.Run(trapNamesForForeignTest(tc), func(t *testing.T) { testTinyGoForeignStackConcurrentGC(t, tc) })
	}
}

func trapNamesForForeignTest(tc TrapCode) string {
	if tc == TrapNone {
		return "return"
	}
	if tc == TrapInterrupted {
		return "interrupt"
	}
	return "trap-unwind"
}

func testTinyGoForeignStackConcurrentGC(t *testing.T, tc TrapCode) {
	eng, jm, ar := fixture(t)
	body := []byte{
		0x48, 0x83, 0xec, 0x20, // sub $32,rsp: simulate nested guest frame
		0xc7, 0x06, 1, 0, 0, 0, // movl $1,(rsi)
		0x83, 0x7e, 4, 0, // cmpl $0,4(rsi)
		0x74, 0xfa, // je -6
	}
	tail := stubTrap(tc)
	body = append(body, tail[:len(tail)-1]...)
	// Restore the trap reentry SP, as handler-jump traps and cancellation do.
	body = append(body, 0x48, 0x8b, 0x66, 0xe8, 0xc3)
	code, err := mmapExec(body)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	lin := jm.LinearMemory()
	entered := (*uint32)(unsafe.Pointer(&lin[0]))
	release := (*uint32)(unsafe.Pointer(&lin[4]))
	args, trap, results := ar.Alloc(16), ar.Alloc(TrapBufferBytes), ar.Alloc(16)
	done := make(chan error, 1)
	go func() { done <- eng.Call(slicePtr(code), args, lin, trap, results) }()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(entered) == 0 {
		if time.Now().After(deadline) {
			panic("native entry handshake timed out")
		}
		grt.Gosched()
	}
	for i := 0; i < 8; i++ {
		grt.GC()
	}
	atomic.StoreUint32(release, 1)
	select {
	case err := <-done:
		if tc == TrapNone && err != nil {
			t.Fatal(err)
		}
		if tc != TrapNone {
			var te *TrapError
			if !errors.As(err, &te) || te.Code != tc {
				t.Fatalf("trap = %v, want %v", err, tc)
			}
		}
	case <-time.After(5 * time.Second):
		panic("native return handshake timed out")
	}
	grt.GC()
	// Unregistration must complete before munmap; a subsequent GC must not read
	// the released address. Repeated Close is also supported.
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	grt.GC()
}

func TestTinyGoForeignStackGCHostReentry(t *testing.T) {
	eng, jm, ar := fixture(t)
	inner, innerJM, innerAR := fixture(t)
	// Insert a rendezvous immediately after the host trampoline returns into
	// the inner guest frame. No relative branch crosses this insertion.
	resumeAt := bytes.Index(stubHostNested, []byte{0x41, 0xff, 0x51, 0x38}) + 4
	body := append([]byte{}, stubHostNested[:resumeAt]...)
	body = append(body, 0xc7, 0x03, 1, 0, 0, 0, 0x83, 0x7b, 4, 0, 0x74, 0xfa)
	body = append(body, stubHostNested[resumeAt:]...)
	code, err := mmapExec(body)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	innerCode, err := mmapExec(stubAdd1)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(innerCode)
	args, results, trap, ctrl := hostCallFixture(t, jm, ar)
	binary.LittleEndian.PutUint32(args, 20)
	ia, ir, it := innerAR.Alloc(16), innerAR.Alloc(16), innerAR.Alloc(TrapBufferBytes)
	binary.LittleEndian.PutUint32(ia, 41)
	calls := 0
	host := func(_ uintptr, _ uint32, args, res []uint64) {
		calls++
		grt.GC() // parked outer frames and saved registers remain registered
		if err := inner.Call(slicePtr(innerCode), ia, innerJM.LinearMemory(), it, ir); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(ir) != 42 {
			t.Fatal("nested entry result")
		}
		grt.GC()
		res[0] = args[0] * 2
	}
	lin := jm.LinearMemory()
	entered, release := (*uint32)(unsafe.Pointer(&lin[0])), (*uint32)(unsafe.Pointer(&lin[4]))
	collected := make(chan struct{})
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for atomic.LoadUint32(entered) == 0 {
			if time.Now().After(deadline) {
				panic("resume handshake timed out")
			}
			grt.Gosched()
		}
		grt.GC() // collect while executing on the resumed foreign stack
		atomic.StoreUint32(release, 1)
		close(collected)
	}()
	if err := eng.CallWithHost(slicePtr(code), args, jm.LinearMemory(), trap, results, ctrl, host); err != nil {
		t.Fatal(err)
	}
	<-collected
	if calls != 1 || binary.LittleEndian.Uint32(results) != 362 {
		t.Fatal("parked native frames corrupted")
	}
	grt.GC()
}

func TestTinyGoForeignStackGCPreparedEntry(t *testing.T) {
	eng, jm, _ := fixture(t)
	// Prepared ABI keeps linear memory in RBX and returns the scalar in RAX.
	code, err := mmapExec([]byte{
		0xc7, 0x03, 1, 0, 0, 0,
		0x83, 0x7b, 4, 0,
		0x74, 0xfa,
		0xb8, 42, 0, 0, 0,
		0xc3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	lin := jm.LinearMemory()
	entered, release := (*uint32)(unsafe.Pointer(&lin[0])), (*uint32)(unsafe.Pointer(&lin[4]))
	done := make(chan uint64, 1)
	go func() {
		value, err := eng.EnterPreparedInt(slicePtr(code), jm.LinMemBase(), 0, 0, 0, 0)
		if err != nil {
			panic(err)
		}
		done <- value
	}()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(entered) == 0 {
		if time.Now().After(deadline) {
			panic("prepared entry handshake timed out")
		}
		grt.Gosched()
	}
	grt.GC()
	atomic.StoreUint32(release, 1)
	select {
	case result := <-done:
		if result != 42 {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		panic("prepared return timed out")
	}
	grt.GC()
}
