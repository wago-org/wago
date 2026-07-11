//go:build (linux || darwin) && arm64

package runtime

import (
	"encoding/binary"
	"errors"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

const linMemBytes = 65536

func fixture(t *testing.T) (*Engine, *JobMemory, *Arena) {
	t.Helper()
	eng, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		t.Fatalf("NewJobMemory: %v", err)
	}
	ar, err := NewArena(4096)
	if err != nil {
		t.Fatalf("NewArena: %v", err)
	}
	jm.SetStackFence(0)
	t.Cleanup(func() {
		_ = eng.Close()
		_ = jm.Close()
		_ = ar.Close()
	})
	return eng, jm, ar
}

func stubAdd1() []byte {
	var a a64.Asm
	must(a.Load32(a64.X4, a64.X0, 0))
	a.AddImm32(a64.X4, a64.X4, 1)
	must(a.Store32(a64.X4, a64.X3, 0))
	must(a.Store32(a64.ZR, a64.X2, 0))
	a.Ret()
	return a.B
}

func stubMemStore() []byte {
	var a a64.Asm
	must(a.Load64(a64.X4, a64.X0, 0))
	must(a.Load32(a64.X5, a64.X0, 8))
	a.StrIdx(a64.X5, a64.X1, a64.X4, 4)
	must(a.Store32(a64.ZR, a64.X2, 0))
	a.Ret()
	return a.B
}

func stubMemLoad() []byte {
	var a a64.Asm
	must(a.Load64(a64.X4, a64.X0, 0))
	a.LdrIdx(a64.X5, a64.X1, a64.X4, 4, false, false)
	must(a.Store32(a64.X5, a64.X3, 0))
	must(a.Store32(a64.ZR, a64.X2, 0))
	a.Ret()
	return a.B
}

func stubTrap(code TrapCode) []byte {
	var a a64.Asm
	a.MovImm64(a64.X4, uint64(code))
	must(a.Store32(a64.X4, a64.X2, 0))
	a.Ret()
	return a.B
}

func must(ok bool) {
	if !ok {
		panic("arm64 runtime test stub encoding failed")
	}
}

func TestMmapExec(t *testing.T) {
	mem, err := mmapExec(stubAdd1())
	if err != nil {
		t.Skipf("PROT_EXEC mapping denied (hardened kernel? needs memfd dual-map fallback): %v", err)
	}
	defer munmap(mem)
	if len(mem) == 0 {
		t.Fatal("empty executable mapping")
	}
}

func TestAdd1(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubAdd1())
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, 41)

	if err := eng.Call(slicePtr(code), serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := binary.LittleEndian.Uint32(results); got != 42 {
		t.Fatalf("add1(41) = %d, want 42", got)
	}
}

func TestTrapReadback(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubTrap(TrapLinMemOutOfBounds))
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)

	err = eng.Call(slicePtr(code), serArgs, jm.LinearMemory(), trap, results)
	var te *TrapError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TrapError, got %v", err)
	}
	if te.Code != TrapLinMemOutOfBounds {
		t.Fatalf("trap code = %v, want %v", te.Code, TrapLinMemOutOfBounds)
	}
}

func TestLinearMemoryZeroCopy(t *testing.T) {
	eng, jm, ar := fixture(t)
	lin := jm.LinearMemory()
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)

	storeCode, err := mmapExec(stubMemStore())
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(storeCode)
	binary.LittleEndian.PutUint32(serArgs[0:], 64)
	binary.LittleEndian.PutUint32(serArgs[8:], 0xCAFEBABE)
	if err := eng.Call(slicePtr(storeCode), serArgs, lin, trap, results); err != nil {
		t.Fatalf("store Call: %v", err)
	}
	if got := binary.LittleEndian.Uint32(lin[64:]); got != 0xCAFEBABE {
		t.Fatalf("native store not visible to Go: linMem[64] = %#x", got)
	}

	loadCode, err := mmapExec(stubMemLoad())
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(loadCode)
	binary.LittleEndian.PutUint32(lin[128:], 0x12345678)
	binary.LittleEndian.PutUint32(serArgs[0:], 128)
	if err := eng.Call(slicePtr(loadCode), serArgs, lin, trap, results); err != nil {
		t.Fatalf("load Call: %v", err)
	}
	if got := binary.LittleEndian.Uint32(results); got != 0x12345678 {
		t.Fatalf("Go store not visible to native: results = %#x", got)
	}
}
