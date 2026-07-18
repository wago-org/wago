//go:build linux && riscv64

package riscv64spike

import (
	"math"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func mapCode(t *testing.T, a *enc.Asm) ([]byte, uintptr) {
	t.Helper()
	code, err := MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	return code, uintptr(unsafe.Pointer(&code[0]))
}

func TestPublicMappings(t *testing.T) {
	rw, err := MapRW(0)
	if err != nil || len(rw) != pageSize {
		t.Fatalf("MapRW(0) = %d bytes, %v", len(rw), err)
	}
	if err := syscall.Munmap(rw); err != nil {
		t.Fatal(err)
	}
	var a enc.Asm
	a.Ret()
	code, err := MapExec(a.B)
	if err != nil || len(code) != pageSize {
		t.Fatalf("MapExec = %d bytes, %v", len(code), err)
	}
	if err := syscall.Munmap(code); err != nil {
		t.Fatal(err)
	}
}

func TestSpikeIntegerAndFloatingPointExec(t *testing.T) {
	var integer enc.Asm
	integer.Add(enc.A0, enc.A0, enc.A1)
	integer.Mul(enc.A0, enc.A0, enc.A1)
	integer.Ret()
	code, entry := mapCode(t, &integer)
	defer syscall.Munmap(code)
	if got := Call2(entry, 6, 7); got != 91 {
		t.Fatalf("integer result = %d, want 91", got)
	}

	var fp enc.Asm
	fp.FmvFromGPR(enc.F0, enc.A0, true)
	fp.FmvFromGPR(enc.F1, enc.A1, true)
	if !fp.Fadd(enc.F0, enc.F0, enc.F1, true, enc.RoundNearestEven) {
		t.Fatal("FADD.D encoding rejected")
	}
	fp.FmvToGPR(enc.A0, enc.F0, true)
	fp.Ret()
	fpCode, fpEntry := mapCode(t, &fp)
	defer syscall.Munmap(fpCode)
	bits := Call2(fpEntry, uintptr(math.Float64bits(1.25)), uintptr(math.Float64bits(2.5)))
	if got := math.Float64frombits(uint64(bits)); got != 3.75 {
		t.Fatalf("float result = %v, want 3.75", got)
	}
}

func TestSpikeBranchAndMemoryExec(t *testing.T) {
	var max enc.Asm
	ge := max.Bge(enc.A0, enc.A1)
	max.MovReg64(enc.A0, enc.A1)
	max.Ret()
	keepA0 := max.Len()
	max.Ret()
	if !max.PatchBranch13(ge, keepA0) {
		t.Fatal("branch patch rejected")
	}
	code, entry := mapCode(t, &max)
	defer syscall.Munmap(code)
	if got := Call2(entry, 4, 9); got != 9 {
		t.Fatalf("max(4,9) = %d", got)
	}
	if got := Call2(entry, 12, 3); got != 12 {
		t.Fatalf("max(12,3) = %d", got)
	}

	mem, err := MapRW(8)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(mem)
	var loadStore enc.Asm
	if !loadStore.Sd(enc.A1, enc.A0, 0) || !loadStore.Ld(enc.A0, enc.A0, 0) {
		t.Fatal("memory encoding rejected")
	}
	loadStore.Ret()
	memCode, memEntry := mapCode(t, &loadStore)
	defer syscall.Munmap(memCode)
	base := uintptr(unsafe.Pointer(&mem[0]))
	const value = uintptr(0x123456789abcdef0)
	if got := Call2(memEntry, base, value); got != value {
		t.Fatalf("memory result = %#x, want %#x", got, value)
	}
}

func TestForeignStackAndPinnedLinearMemory(t *testing.T) {
	stack, err := MapRW(64 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(stack)
	top := uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))

	var getSP enc.Asm
	getSP.MovReg64(enc.A0, enc.SP)
	getSP.Ret()
	code, entry := mapCode(t, &getSP)
	defer syscall.Munmap(code)
	got := enterNativeSpike(entry, 0, 0, top)
	lo := uintptr(unsafe.Pointer(&stack[0]))
	if got < lo || got >= top {
		t.Fatalf("native SP %#x is outside foreign stack [%#x,%#x)", got, lo, top)
	}

	var getMem enc.Asm
	getMem.MovReg64(enc.A0, enc.X25)
	getMem.Ret()
	memCode, memEntry := mapCode(t, &getMem)
	defer syscall.Munmap(memCode)
	mem, err := MapRW(1)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(mem)
	base := uintptr(unsafe.Pointer(&mem[0]))
	if got := Call3(memEntry, 0, 0, base); got != base {
		t.Fatalf("pinned linMem = %#x, want %#x", got, base)
	}
}

func TestGoContextSurvivesCalleeSavedClobberAndGC(t *testing.T) {
	var a enc.Asm
	for i, reg := range []enc.Reg{enc.X8, enc.X9, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23, enc.X24, enc.X25, enc.X26} {
		a.MovImm64(reg, uint64(0x11110000+i))
	}
	a.Add(enc.A0, enc.A0, enc.A1)
	a.Ret()
	code, entry := mapCode(t, &a)
	defer syscall.Munmap(code)

	for i := 0; i < 2000; i++ {
		if got := Call2(entry, uintptr(i), 1); got != uintptr(i+1) {
			t.Fatalf("call %d = %d", i, got)
		}
		if i%50 == 0 {
			runtime.GC()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for worker := 0; worker < 2; worker++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				if got := Call2(entry, uintptr(base+i), 2); got != uintptr(base+i+2) {
					t.Errorf("parallel call = %d", got)
					return
				}
			}
		}(worker * 10000)
	}
	wg.Wait()
}
