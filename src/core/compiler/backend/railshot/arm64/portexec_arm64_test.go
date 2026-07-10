//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// TestPortedExecInternal tries to EXECUTE the ported backend's register-ABI
// internal entry (args in X0..X7, result X0) via the spike trampoline. For a
// leaf function with no linear memory this should need no basedata setup.
func TestPortedExecInternal(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name   string
		in     []wasm.ValType
		body   []byte
		a0, a1 uintptr
		want   uintptr
	}{
		{"add", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}, 40, 2, 42},
		{"sub", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b}, 100, 58, 42},
		{"mul", []wasm.ValType{wasm.I32, wasm.I32}, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}, 6, 7, 42},
		{
			"fib", i32,
			[]byte{
				0x01, 0x03, 0x7f,
				0x41, 0x00, 0x21, 0x01, 0x41, 0x01, 0x21, 0x02, 0x41, 0x00, 0x21, 0x03,
				0x02, 0x40, 0x03, 0x40,
				0x20, 0x03, 0x20, 0x00, 0x4e, 0x0d, 0x01,
				0x20, 0x01, 0x20, 0x02, 0x6a, 0x20, 0x02, 0x21, 0x01, 0x21, 0x02,
				0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03,
				0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
			}, 10, 0, 55,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := i32
			m := mod1(t, tc.in, out, tc.body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			mem, err := arm64spike.MapExec(cm.Code)
			if err != nil {
				t.Fatalf("map: %v", err)
			}
			internal := cm.InternalEntry[0]
			entry := uintptr(unsafe.Pointer(&mem[internal]))
			got := arm64spike.Call2(entry, tc.a0, tc.a1)
			// result is in X0; mask to i32 since upper bits may hold the wide reg
			if uint32(got) != uint32(tc.want) {
				t.Fatalf("%s(%d,%d) = %d, want %d (raw %#x)", tc.name, tc.a0, tc.a1, uint32(got), tc.want, got)
			}
		})
	}
}
