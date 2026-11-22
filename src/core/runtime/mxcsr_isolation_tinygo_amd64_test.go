//go:build linux && amd64 && tinygo

package runtime

import (
	"testing"
	"unsafe"
)

func TestTinyGoEntryRestoresMXCSR(t *testing.T) {
	// Read the current state and replace it with the first argument (EDI).
	swapCode, err := mmapExec([]byte{
		0x0f, 0xae, 0x5c, 0x24, 0xfc, // stmxcsr [rsp-4]
		0x89, 0x7c, 0x24, 0xf8, // mov [rsp-8], edi
		0x0f, 0xae, 0x54, 0x24, 0xf8, // ldmxcsr [rsp-8]
		0x8b, 0x44, 0x24, 0xfc, // mov eax, [rsp-4]
		0xc3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(swapCode)
	value := funcValue{fnptr: uintptr(unsafe.Pointer(&swapCode[0]))}
	swap := *(*func(uint32) uint32)(unsafe.Pointer(&value))

	// Capture guest MXCSR through RCX, then change it before returning.
	guest, err := mmapExec([]byte{
		0x0f, 0xae, 0x19, // stmxcsr [rcx]
		0xc7, 0x44, 0x24, 0xfc, 0x80, 0x3f, 0x00, 0x00, // mov [rsp-4], 0x3f80
		0x0f, 0xae, 0x54, 0x24, 0xfc, // ldmxcsr [rsp-4]
		0xc3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(guest)
	linMem, err := mmapRW(64)
	if err != nil {
		t.Fatal(err)
	}
	defer munmap(linMem)
	engine, err := NewEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	guestEntry := uintptr(unsafe.Pointer(&guest[0]))
	base := uintptr(unsafe.Pointer(&linMem[32]))
	result := uintptr(unsafe.Pointer(&linMem[40]))
	const upward = 0x5f80
	for _, prepared := range []bool{false, true} {
		original := swap(upward)
		if prepared {
			_, err = engine.EnterPreparedInt(guestEntry, base, 0, uint64(result), 0, 0)
		} else {
			enterNative(guestEntry, 0, base, 0, result, engine.stackTop)
		}
		restored := swap(original)
		if err != nil {
			t.Fatal(err)
		}
		if got := *(*uint32)(unsafe.Pointer(&linMem[40])); got != 0x1f80 || restored != upward {
			t.Fatalf("prepared=%t guest MXCSR=%#x, restored MXCSR=%#x", prepared, got, restored)
		}
	}
}
