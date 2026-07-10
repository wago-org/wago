//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// TestFloatExec runs f(x i32) = i32(f32(x) + 2.0) under qemu — the whole float
// pipeline (i32→f32 convert, f32 const load, f32 add, f32→i32 trunc) happens in
// V-registers, with int args/result so the spike trampoline can drive it.
func TestFloatExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; f32.convert_i32_s; f32.const 2.0; f32.add; i32.trunc_f32_s
	body := []byte{
		0x00,
		0x20, 0x00, // local.get 0
		0xb2,                         // f32.convert_i32_s
		0x43, 0x00, 0x00, 0x00, 0x40, // f32.const 2.0
		0x92, // f32.add
		0xa8, // i32.trunc_f32_s
		0x0b,
	}
	m := mod1(t, i32, i32, body)
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	for _, x := range []uintptr{5, 0, 40} {
		if got := arm64spike.Call2(entry, x, 0); uint32(got) != uint32(x+2) {
			t.Fatalf("f(%d) = %d, want %d", x, uint32(got), x+2)
		}
	}
}
