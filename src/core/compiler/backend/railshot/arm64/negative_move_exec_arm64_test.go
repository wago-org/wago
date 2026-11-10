//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// A selected negative i32 must remain zero-extended when widened to i64.
// The select forces materialization before the unsigned conversion.
func TestNegativeConstantUnsignedWidening(t *testing.T) {
	for _, v := range []int32{-1, -17, -32768, -65536, -65537, -2147483648} {
		body := []byte{0, 0x41}
		body = append(body, wasmtest.SLEB32(v)...)
		body = append(body, 0x20, 0, 0x20, 0, 0x45, 0x1b, 0xad, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
		for _, guard := range []bool{false, true} {
			for _, x := range []uint64{0, 1, 0x80000000, 0xffffffff} {
				want := uint64(uint32(x))
				if x == 0 {
					want = uint64(uint32(v))
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: false}, x)
				if err != nil || got != want {
					t.Fatalf("v=%d guard=%v x=%x got=%x want=%x err=%v", v, guard, x, got, want, err)
				}
			}
		}
	}
}

func TestSingleNegativeMove32Policy(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := singleNegativeMove32Enabled
	defer func() { singleNegativeMove32Enabled = old }()
	singleNegativeMove32Enabled = true
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{0, 0x41, 0x6f, 0x0b})
	for _, enabled := range []bool{false, true} {
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: false, Stats: stats, Optimizations: map[string]bool{"single-negative-move32": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		hits := stats.Funcs[0].Peephole["single-negative-move32"]
		if enabled && hits != 1 || !enabled && hits != 0 {
			t.Fatalf("enabled=%v hits=%d", enabled, hits)
		}
	}
}
