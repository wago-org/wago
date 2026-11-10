//go:build linux && riscv64 && !tinygo

package riscv64

import (
	"encoding/binary"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"golang.org/x/sys/unix"
)

func TestNativeCallAllowsGCWhileGuestIsActiveWithOneP(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	e, err := NewEngine(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	args, err := mapRW(32)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(args)
	// A bounded 100 ms Linux nanosleep. Guest entry announces itself before
	// sleeping, then returns the observer's GC-completion flag before leaving
	// the foreign stack. Thus a GC between calls cannot satisfy this gate.
	binary.LittleEndian.PutUint64(args[8:], 100000000)
	started := (*uint64)(unsafe.Pointer(&args[16]))
	collected := (*uint64)(unsafe.Pointer(&args[24]))
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sd(enc.X11, enc.SP, 8)
	a.Addi(enc.X5, enc.Zero, 1)
	a.Sd(enc.X5, enc.X11, 0)
	a.Addi(enc.X11, enc.Zero, 0)
	a.Addi(enc.X17, enc.Zero, unix.SYS_NANOSLEEP)
	// Canonical fixed-width ECALL, solely for this Linux scheduler fixture.
	a.B = binary.LittleEndian.AppendUint32(a.B, 0x00000073)
	a.Ld(enc.X11, enc.SP, 8)
	a.Ld(enc.X10, enc.X11, 8)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	p, err := MapCode(a.B)
	if err != nil {
		if p != nil {
			p.Close()
		}
		t.Fatal(err)
	}
	defer p.Close()
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		for atomic.LoadUint64(started) == 0 {
			select {
			case <-stop:
				return
			default:
				runtime.Gosched()
			}
		}
		runtime.GC()
		atomic.StoreUint64(collected, 1)
	}()
	got, err := e.Call(p, 0, uint64(pointer(args)), uint64(pointer(args[16:])), 16)
	close(stop)
	<-done
	if err != nil || got != 1 {
		t.Fatalf("GC did not finish while guest was active: result=%d err=%v", got, err)
	}
}
