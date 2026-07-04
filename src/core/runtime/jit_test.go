//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"errors"
	"testing"
)

const linMemBytes = 65536 // one wasm page

// fixture wires up an Engine, off-heap JobMemory, and an Arena for call buffers.
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
	// Stack fence just below the foreign stack so any active fence check passes.
	jm.SetStackFence(uintptr(0))
	t.Cleanup(func() {
		_ = eng.Close()
		_ = jm.Close()
		_ = ar.Close()
	})
	return eng, jm, ar
}

// Test 1: mmap executable memory (item a). Skips with a clear message if a
// hardened kernel denies the W->X transition.
func TestMmapExec(t *testing.T) {
	mem, err := mmapExec(stubAdd1)
	if err != nil {
		t.Skipf("PROT_EXEC mapping denied (hardened kernel? needs memfd dual-map fallback): %v", err)
	}
	defer munmap(mem)
	if len(mem) == 0 {
		t.Fatal("empty executable mapping")
	}
}

// Test 2: enter native code on the foreign stack via the trampoline; read the
// result back (items b). add1(41) == 42, no trap.
func TestAdd1(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubAdd1)
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

// Test 3: trap readback -> Go error (item c).
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

// Test 4: zero-copy linear memory, both directions (item d).
func TestLinearMemoryZeroCopy(t *testing.T) {
	eng, jm, ar := fixture(t)
	lin := jm.LinearMemory()
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)

	// native -> Go: stub stores 0xCAFEBABE at linMem[64].
	storeCode, err := mmapExec(stubMemStore)
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

	// Go -> native: Go writes linMem[128], stub loads it into results.
	loadCode, err := mmapExec(stubMemLoad)
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

// The synchronous host-import round trip is covered by TestHostCallRoundtrip and
// TestHostCallDeepStack in hostcall_test.go.
