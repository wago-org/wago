//go:build windows && amd64 && !tinygo

package runtime

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func TestWindowsPreparedTypedI64RequiresIntegerProof(t *testing.T) {
	if !windowsTypedI64HostAvailable() {
		t.Skip("Windows typed i64 bridge disabled")
	}
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	access := runtimebridge.GrantHostScalarCall()
	args, trap := make([]byte, 8), make([]byte, TrapBufferBytes)
	results, ctrl := make([]byte, 8), make([]byte, ctrlFrameSize)
	if p, err := e.PrepareScalarHost(access, args, trap, results, ctrl, 0x10001); err != nil || p != nil {
		t.Fatalf("ordinary scalar mode admitted: %v, %v", p, err)
	}
	if p, err := e.PrepareScalarHostView(access, args, trap, results, ctrl, 0x10001); err != nil || p != nil {
		t.Fatalf("view mode admitted: %v, %v", p, err)
	}
	if p, err := e.PrepareWindowsTypedI64Host(access, args, trap, results, ctrl, 0x10002); err == nil || p != nil {
		t.Fatalf("other shape admitted: %v, %v", p, err)
	}
	p, err := e.PrepareWindowsTypedI64Host(access, args, trap, results, ctrl, 0x10001)
	if err != nil || p == nil {
		t.Fatalf("typed i64 preparation: %v, %v", p, err)
	}
	context := new(uint64)
	call := func(unsafe.Pointer, uint64, uint64) uint64 { t.Fatal("unproved callback ran"); return 0 }
	if err := p.Call(1, 1, unsafe.Pointer(context), call); err == nil {
		t.Fatal("ordinary scalar call admitted")
	}
	if err := p.CallInteger(1, 1, unsafe.Pointer(context), call); err == nil {
		t.Fatal("integer call without detached proof admitted")
	}
	memory, err := NewJobMemory(0)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	if err := p.DetachNumericContext(access, memory); err != nil {
		t.Fatal(err)
	}
	if err := p.EnableIntegerGuestContext(access); err != nil {
		t.Fatal(err)
	}
	if err := p.CallIntegerI32(1, 1, func(int32) int32 { t.Fatal("i32 callback ran"); return 0 }); err == nil {
		t.Fatal("i32 owner admitted")
	}
}
