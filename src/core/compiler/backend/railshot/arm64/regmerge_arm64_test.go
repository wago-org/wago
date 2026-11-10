//go:build (linux || darwin) && arm64

package arm64

import (
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func runArm64Internal2(t *testing.T, m *wasm.Module, a0, a1 uintptr) uintptr {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	return arm64spike.Call2(uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]])), a0, a1)
}

func TestRegMergeBlockResultArm64(t *testing.T) {
	body := []byte{0x00, 0x02, 0x7f, 0x20, 0x00, 0x20, 0x01, 0x0d, 0x00, 0x1a, 0x41, 0xe7, 0x07, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	cases := []struct{ x, c, want uint32 }{{5, 1, 5}, {5, 0, 999}, {^uint32(2), 42, ^uint32(2)}}
	saved := regMergeEnabled
	defer func() { regMergeEnabled = saved }()
	for _, on := range []bool{false, true} {
		regMergeEnabled = on
		for _, tc := range cases {
			if got := uint32(runArm64Internal2(t, m, uintptr(tc.x), uintptr(tc.c))); got != tc.want {
				t.Errorf("regMerge=%v sel(%d,%d)=%d want %d", on, tc.x, tc.c, got, tc.want)
			}
		}
	}
}

func TestRegMergeIfElseArm64(t *testing.T) {
	body := []byte{0x00, 0x20, 0x00, 0x41, 0x00, 0x48, 0x04, 0x7f, 0x41, 0x00, 0x20, 0x00, 0x6b, 0x05, 0x20, 0x00, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	saved := regMergeEnabled
	defer func() { regMergeEnabled = saved }()
	for _, on := range []bool{false, true} {
		regMergeEnabled = on
		for _, tc := range []struct{ x, want int32 }{{-5, 5}, {7, 7}, {0, 0}} {
			if got := int32(runArm64Internal2(t, m, uintptr(uint32(tc.x)), 0)); got != tc.want {
				t.Errorf("regMerge=%v abs(%d)=%d want %d", on, tc.x, got, tc.want)
			}
		}
	}
}

func TestRegMergeIfElseFloatArm64(t *testing.T) {
	// Keep the function ABI integer while exercising an f64 merge internally.
	body := []byte{
		0x00, 0x20, 0x00, 0xbf,
		0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x63,
		0x04, 0x7c, 0x20, 0x00, 0xbf, 0x9a, 0x05, 0x20, 0x00, 0xbf, 0x0b,
		0xbd, 0x0b,
	}
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
	saved := regMergeEnabled
	defer func() { regMergeEnabled = saved }()
	for _, on := range []bool{false, true} {
		regMergeEnabled = on
		for _, tc := range []struct{ x, want float64 }{{-3.5, 3.5}, {2, 2}, {0, 0}} {
			got := math.Float64frombits(uint64(runArm64Internal2(t, m, uintptr(math.Float64bits(tc.x)), 0)))
			if got != tc.want {
				t.Errorf("regMerge=%v fabs(%g)=%g want %g", on, tc.x, got, tc.want)
			}
		}
	}
}
