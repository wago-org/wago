//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestNativeGrownImagePublication(t *testing.T) {
	if !nativeAvailable {
		t.Skip("RV64 execution only")
	}
	b, err := NewCodeBuffer(1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	prefix := make([]byte, len(b.mem))
	for i := 0; i < len(prefix); i += 4 {
		binary.LittleEndian.PutUint32(prefix[i:], 0x00000013) // NOP
	}
	if err = b.Append(prefix); err != nil {
		t.Fatal(err)
	}
	old := pointer(b.mem)
	if err = b.Append(snippet(0)); err != nil {
		t.Fatal(err)
	}
	if pointer(b.mem) == old {
		t.Fatal("fixture did not grow the mapping")
	}
	p, err := PublishImage(b, len(b.Bytes()))
	if err != nil {
		if p != nil {
			p.Close()
		}
		t.Fatal(err)
	}
	defer p.Close()
	e, err := NewEngine(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if got, err := e.Call(p, len(prefix), 19, 23, 16); err != nil || got != 42 {
		t.Fatalf("grown publication result=%d err=%v", got, err)
	}
}

func TestNativeArithmeticTrapAndReuse(t *testing.T) {
	if !nativeAvailable {
		t.Skip("RV64 execution only")
	}
	e, err := NewEngine(DefaultNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p, err := MapCode(snippet(0))
	if err != nil {
		if p != nil {
			p.Close()
		}
		t.Fatal(err)
	}
	defer p.Close()
	for _, tc := range []struct{ a, b, want uint64 }{{19, 23, 42}, {0, 0, 0}, {^uint64(0), 1, 0}, {1 << 63, 1 << 63, 0}, {0xdeadbeef00000000, 7, 0xdeadbeef00000007}} {
		for i := 0; i < 32; i++ {
			if got, err := e.Call(p, 0, tc.a, tc.b, 16); err != nil || got != tc.want {
				t.Fatalf("got=%x want=%x err=%v", got, tc.want, err)
			}
		}
	}
	if got := testing.AllocsPerRun(100, func() {
		if v, err := e.Call(p, 0, 19, 23, 16); err != nil || v != 42 {
			panic("native arithmetic")
		}
	}); got != 0 {
		t.Fatalf("native steady-state allocations %g", got)
	}
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.Sw(enc.X10, enc.X12, 0)
	a.Addi(enc.X10, enc.Zero, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	trap, err := MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer trap.Close()
	for i := 0; i < 64; i++ {
		_, err := e.Call(trap, 0, 13, 0, 16)
		var tr *TrapError
		if !errors.As(err, &tr) || tr.Code != 13 {
			t.Fatalf("trap %v", err)
		}
		if got, err := e.Call(p, 0, 19, 23, 16); err != nil || got != 42 {
			t.Fatalf("trap reuse %d %v", got, err)
		}
	}
}

func TestNativeCrossThreadPublicationAndGC(t *testing.T) {
	if !nativeAvailable {
		t.Skip("RV64 execution only")
	}
	gcDone := make(chan struct{})
	var gc sync.WaitGroup
	gc.Add(1)
	go func() {
		defer gc.Done()
		for {
			select {
			case <-gcDone:
				return
			default:
				runtime.GC()
				runtime.Gosched()
			}
		}
	}()
	defer func() { close(gcDone); gc.Wait() }()
	for round := int32(0); round < 8; round++ {
		p, err := MapCode(snippet(round)) // written/published on a different thread from consumers
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				e, err := NewEngine(MinNativeStackBytes)
				if err != nil {
					t.Error(err)
					return
				}
				defer e.Close()
				for i := 0; i < 32; i++ {
					if got, err := e.Call(p, 0, 19, 23, 16); err != nil || got != uint64(42+round) {
						t.Errorf("thread publication got=%d err=%v", got, err)
					}
					runtime.Gosched()
				}
			}()
		}
		wg.Wait()
		if err = p.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
