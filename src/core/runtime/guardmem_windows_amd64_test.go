//go:build windows && amd64 && wago_guardpage

package runtime

import (
	"encoding/binary"
	"testing"
)

// The Windows exception dispatcher uses R8 while entering the lazy-commit
// continuation. Keep a second linear-memory address live in R8 across the
// first fault so this test covers the complete VEH -> thunk -> retry boundary.
var stubGuardCommitPreservesR8 = []byte{
	0x48, 0x89, 0xf3, // mov rbx, rsi
	0x49, 0x89, 0xcc, // mov r12, rcx
	0x41, 0xb8, 0x00, 0x00, 0x01, 0x00, // mov r8d, 65536
	0x45, 0x89, 0xc2, // mov r10d, r8d
	0x42, 0xc7, 0x04, 0x13, 0x44, 0x33, 0x22, 0x11, // mov dword [rbx+r10], 0x11223344
	0x42, 0xc7, 0x44, 0x03, 0x04, 0x88, 0x77, 0x66, 0x55, // mov dword [rbx+r8+4], 0x55667788
	0x42, 0x8b, 0x44, 0x03, 0x04, // mov eax, [rbx+r8+4]
	0x41, 0x89, 0x04, 0x24, // mov [r12], eax
	0xc7, 0x02, 0x00, 0x00, 0x00, 0x00, // mov dword [rdx], 0
	0xc3, // ret
}

func TestGuardCommitPreservesR8AcrossContinuation(t *testing.T) {
	if err := InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := NewJobMemoryGuarded(2*wasmPageBytes, 2*wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, err := mmapExec(stubGuardCommitPreservesR8)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(code)

	secondPage := jm.LinMemBase() + wasmPageBytes
	if ok, _, callErr := procVirtualFree.Call(secondPage, wasmPageBytes, memDecommit); ok == 0 {
		t.Fatalf("decommit second Wasm page: %v", callErr)
	}
	trap := ar.Alloc(TrapBufferBytes)
	results := ar.Alloc(8)
	if err := eng.CallGuarded(slicePtr(code), nil, jm.LinMemBase(), trap, results, jm); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(results); got != 0x55667788 {
		t.Fatalf("result after lazy commit = %#x, want %#x", got, uint32(0x55667788))
	}
	lin := jm.HostBytes()
	if got := binary.LittleEndian.Uint32(lin[wasmPageBytes:]); got != 0x11223344 {
		t.Fatalf("first lazy-committed store = %#x, want %#x", got, uint32(0x11223344))
	}
	if got := binary.LittleEndian.Uint32(lin[wasmPageBytes+4:]); got != 0x55667788 {
		t.Fatalf("R8-addressed store after continuation = %#x, want %#x", got, uint32(0x55667788))
	}
}
