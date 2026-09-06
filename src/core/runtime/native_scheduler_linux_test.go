//go:build linux && amd64 && !tinygo

package runtime

import (
	"encoding/binary"
	grt "runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestForeignCallAllowsGoGCWithOneP(t *testing.T) {
	previous := grt.GOMAXPROCS(1)
	defer grt.GOMAXPROCS(previous)
	eng, jm, ar := fixture(t)
	// A bounded 40ms nanosleep on the foreign stack. This fixture never waits on
	// the observer, so a regression returns a diagnostic instead of hanging.
	code, err := mmapExec([]byte{0x31, 0xf6, 0xb8, 35, 0, 0, 0, 0x0f, 0x05, 0xc3})
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	args := ar.Alloc(16)
	binary.LittleEndian.PutUint64(args[8:], 40000000)
	trap := ar.Alloc(TrapBufferBytes)
	result := ar.Alloc(16)
	var collected atomic.Bool
	done := make(chan struct{})
	go func() { grt.GC(); collected.Store(true); close(done) }()
	if err := eng.Call(slicePtr(code), args, jm.LinearMemory(), trap, result); err != nil {
		t.Fatal(err)
	}
	concurrent := collected.Load()
	<-done
	if !concurrent {
		t.Fatal("Go GC did not run during bounded foreign call with one P")
	}
}

func TestForeignCallRetainsBufferOwners(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec([]byte{0x31, 0xf6, 0xb8, 35, 0, 0, 0, 0x0f, 0x05, 0xc3})
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)
	trap := ar.Alloc(TrapBufferBytes)
	for _, prepared := range []bool{false, true} {
		var active, early atomic.Bool
		var collections atomic.Uint32
		done := make(chan struct{})
		active.Store(true)
		go func() {
			defer close(done)
			for active.Load() {
				grt.GC()
				collections.Add(1)
				time.Sleep(time.Millisecond)
			}
		}()
		err := callWithFinalizedBuffers(eng, jm, slicePtr(code), trap, prepared, &active, &early)
		active.Store(false)
		<-done
		if err != nil {
			t.Fatal(err)
		}
		if collections.Load() == 0 {
			t.Fatal("no GC cycle observed during bounded native call")
		}
		if early.Load() {
			t.Fatalf("prepared=%v: a buffer owner finalized before native return", prepared)
		}
	}
}

//go:noinline
func callWithFinalizedBuffers(eng *Engine, jm *JobMemory, code uintptr, trap []byte, prepared bool, active, early *atomic.Bool) error {
	owned := func() []byte {
		buffer := new([8192]byte)
		grt.SetFinalizer(buffer, func(*[8192]byte) {
			if active.Load() {
				early.Store(true)
			}
		})
		return buffer[:]
	}
	args, results := owned(), owned()
	binary.LittleEndian.PutUint64(args[8:], 100000000) // bounded 100 ms nanosleep
	if prepared {
		return eng.CallPrepared(code, args, jm.LinMemBase(), trap, results)
	}
	// The prefix supplies basedata. No pointer to the owner is used after Call.
	linMem := owned()[4096:]
	return eng.Call(code, args, linMem, trap, results)
}
