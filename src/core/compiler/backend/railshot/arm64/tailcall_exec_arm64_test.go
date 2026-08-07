//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func TestDirectTailCallReusesARM64Frame(t *testing.T) {
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(t, funcDef{
		params:  i32x2,
		results: []wasm.ValType{wasm.I32},
		body: []byte{
			0x00,
			0x20, 0x00, // local.get 0
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x20, 0x01, // local.get 1
			0x0f,                         // return
			0x0b,                         // end if
			0x20, 0x00, 0x41, 0x01, 0x6b, // n-1
			0x20, 0x01, 0x20, 0x00, 0x6a, // acc+n
			0x12, 0x00, // return_call 0
			0x0b,
		},
	})
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	const n = uintptr(1_000_000)
	want := uint32((uint64(n) * uint64(n+1) / 2) & 0xffffffff)
	if got := uint32(arm64spike.Call2(entry, n, 0)); got != want {
		t.Fatalf("tail sum(%d) = %d, want %d", n, got, want)
	}
}
