//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func convertReadModuleArm64(t testing.TB) *wasm.Module {
	return mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x00,
		0x20, 0x00, // local.get 0 (borrowed entry pin)
		0xac, // i64.extend_i32_s
		0x0b,
	})
}

func TestConvertReadSwitchAndExecutionArm64(t *testing.T) {
	m := convertReadModuleArm64(t)
	compile := func(on bool) *CodegenStats {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"convert-read": on}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if on.Peephole["convert-read"] != 1 || off.Peephole["convert-read"] != 0 {
		t.Fatalf("direct conversion reads enabled/disabled = %d/%d", on.Peephole["convert-read"], off.Peephole["convert-read"])
	}
	if on.CodeBytes+4 != off.CodeBytes {
		t.Fatalf("direct conversion code = %d bytes, rollback = %d; want one word removed", on.CodeBytes, off.CodeBytes)
	}

	saved := convertReadEnabled
	defer func() { convertReadEnabled = saved }()
	for _, x := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
		convertReadEnabled = true
		gotOn := uint64(runArm64Internal2(t, m, uintptr(x), 0))
		convertReadEnabled = false
		gotOff := uint64(runArm64Internal2(t, m, uintptr(x), 0))
		want := uint64(int64(int32(x)))
		if gotOn != want || gotOff != gotOn {
			t.Fatalf("x=%#x: enabled=%#x disabled=%#x, want %#x", x, gotOn, gotOff, want)
		}
	}
}
