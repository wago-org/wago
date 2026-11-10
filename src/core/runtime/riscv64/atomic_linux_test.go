//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// The native probes use only a short four-instruction LR/SC retry envelope.
// No long JALR veneer, extra load/store, FENCE or SYSTEM lies in that envelope.
func atomicExchangeProbe(wide bool) []byte {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	if wide {
		a.LrD(enc.X5, enc.X10, enc.AtomicAcquire)
		a.ScD(enc.X6, enc.X11, enc.X10, enc.AtomicRelease)
	} else {
		a.LrW(enc.X5, enc.X10, enc.AtomicAcquire)
		a.ScW(enc.X6, enc.X11, enc.X10, enc.AtomicRelease)
	}
	a.Branch(enc.NE, enc.X6, enc.Zero, -8)
	a.Addi(enc.X10, enc.X5, 0)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	return a.B
}

func pendingCASProbe() []byte {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.LrW(enc.X5, enc.X10, enc.AtomicAcquire) // offset8: observed old word
	a.Branch(enc.NE, enc.X5, enc.Zero, 20)    // offset12→fail32
	a.ScW(enc.X6, enc.X11, enc.X10, enc.AtomicRelease)
	a.Branch(enc.NE, enc.X6, enc.Zero, -12) // retry8; any nonzero SC failure
	a.Addi(enc.X10, enc.Zero, 1)
	a.Jal(enc.Zero, 8)
	a.Addi(enc.X10, enc.Zero, 0) // fail32
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	return a.B
}

func TestAtomicProbeConstrainedRetryEnvelope(t *testing.T) {
	for _, code := range [][]byte{atomicExchangeProbe(false), atomicExchangeProbe(true), pendingCASProbe()} {
		if len(code)&3 != 0 {
			t.Fatal("unaligned probe")
		}
		for pc := 8; pc <= 20 && pc < len(code)-12; pc += 4 {
			w := binary.LittleEndian.Uint32(code[pc:])
			switch w & 127 {
			case 0x2f:
				if (w>>27)&31 != 2 && (w>>27)&31 != 3 {
					t.Fatal("unexpected atomic")
				}
			case 0x63:
				if (w>>12)&7 != uint32(enc.NE) {
					t.Fatal("unexpected retry condition")
				}
			case 0x13:
				// Exchange succeeds after its three-instruction envelope.
				if pc != 20 {
					t.Fatal("unexpected pair instruction")
				}
			default:
				t.Fatal("unconstrained LR/SC instruction", pc, w)
			}
		}
	}
}

func TestNativeAtomicWordSignExtensionWidthAndDoublewordExchange(t *testing.T) {
	if !nativeAvailable {
		t.Skip("RV64 execution only")
	}
	cell, err := mapRW(32)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(cell)
	e, err := NewEngine(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, wide := range []bool{false, true} {
		p, err := MapCode(atomicExchangeProbe(wide))
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		old := uint64(0xa5a5a5a580000001)
		desired := uint64(0xfeedface12345678)
		binary.LittleEndian.PutUint64(cell, old)
		binary.LittleEndian.PutUint64(cell[8:], 0x1122334455667788)
		got, err := e.Call(p, 0, uint64(pointer(cell)), desired, 16)
		if err != nil {
			t.Fatal(err)
		}
		wantOld := old
		wantNew := desired
		if !wide {
			wantOld = 0xffffffff80000001
			wantNew = 0xa5a5a5a512345678
		}
		if got != wantOld || binary.LittleEndian.Uint64(cell) != wantNew || binary.LittleEndian.Uint64(cell[8:]) != 0x1122334455667788 {
			t.Fatal("LR/SC width/signext", wide, got, cell)
		}
	}
}

func TestNativeAtomicPendingCASPreservesInterruptedAndRaces(t *testing.T) {
	if !nativeAvailable {
		t.Skip("RV64 execution only")
	}
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	cell, err := mapRW(32)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(cell)
	word := (*uint32)(unsafe.Pointer(&cell[0]))
	e, err := NewEngine(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p, err := MapCode(pendingCASProbe())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, old := range []uint32{0, 12, 0x10000, 0xffffffff} {
		atomic.StoreUint32(word, old)
		binary.LittleEndian.PutUint32(cell[4:], 0xa5a5a5a5)
		got, err := e.Call(p, 0, uint64(pointer(cell)), 0x10000, 16)
		if err != nil {
			t.Fatal(err)
		}
		want := uint64(0)
		wantWord := old
		if old == 0 {
			want = 1
			wantWord = 0x10000
		}
		if got != want || atomic.LoadUint32(word) != wantWord || binary.LittleEndian.Uint32(cell[4:]) != 0xa5a5a5a5 {
			t.Fatal("pending CAS", old, got, cell)
		}
	}
	for i := 0; i < 64; i++ {
		atomic.StoreUint32(word, 0)
		done := make(chan struct{})
		go func() { atomic.StoreUint32(word, 12); close(done) }()
		got, err := e.Call(p, 0, uint64(pointer(cell)), 0x10000, 16)
		<-done
		if err != nil || got > 1 {
			t.Fatal("racing pending CAS", got, err)
		}
		if atomic.LoadUint32(word) != 12 {
			t.Fatal("pending erased interrupt")
		}
	}
	atomic.StoreUint32(word, 12)
	if n := testing.AllocsPerRun(100, func() {
		if got, err := e.Call(p, 0, uint64(pointer(cell)), 0x10000, 16); err != nil || got != 0 {
			panic("atomic CAS reuse")
		}
	}); n != 0 {
		t.Fatal("warm CAS allocations", n)
	}
}
