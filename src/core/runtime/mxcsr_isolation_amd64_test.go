//go:build (linux || darwin || windows) && amd64 && !tinygo

package runtime

import (
	"math"
	"testing"
	"unsafe"
)

type testMXCSRRawCall struct {
	code, linMem, stackTop uintptr
	state, after           uint32
	result                 uintptr
}

func testEnterNativeIntWithMXCSR(call *testMXCSRRawCall)

func TestEnterNativeIntRawIsolatesMXCSRControl(t *testing.T) {
	// movabs $1.0, rax; movq rax, xmm0; movabs $2^-53, rax;
	// movq rax, xmm1; addsd xmm1, xmm0; movq xmm0, rax; ret.
	guest, err := mmapExec([]byte{
		0x48, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f,
		0x66, 0x48, 0x0f, 0x6e, 0xc0,
		0x48, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xa0, 0x3c,
		0x66, 0x48, 0x0f, 0x6e, 0xc8,
		0xf2, 0x0f, 0x58, 0xc1,
		0x66, 0x48, 0x0f, 0x7e, 0xc0,
		0xc3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = munmap(guest) }()

	linMem, err := mmapRW(64)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = munmap(linMem) }()
	engine, err := NewEngineWithStackBytes(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	call := testMXCSRRawCall{
		code:     uintptr(unsafe.Pointer(&guest[0])),
		linMem:   uintptr(unsafe.Pointer(&linMem[32])),
		stackTop: engine.StackTop(),
		state:    0x5f80, // upward rounding, all exceptions masked
	}
	testEnterNativeIntWithMXCSR(&call)
	if got, want := uint64(call.result), math.Float64bits(1); got != want {
		t.Fatalf("guest result = %#x; want nearest-even %#x", got, want)
	}
	if call.after != call.state {
		t.Fatalf("MXCSR after raw entry = %#x; want caller state %#x", call.after, call.state)
	}
}
