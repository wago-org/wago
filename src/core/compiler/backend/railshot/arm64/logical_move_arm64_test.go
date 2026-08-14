//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestSizeLogicalMoveImmediateArm64(t *testing.T) {
	body := []byte{0x00, 0x42}
	body = append(body, wasmtest.SLEB64(0x00ff00ff00ff00ff)...)
	body = append(body, 0x0b)
	m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)

	before := logicalMoveImmediateEnabled
	t.Cleanup(func() { logicalMoveImmediateEnabled = before })
	compile := func(objective OptimizationObjective, enabled bool) *ModuleStats {
		logicalMoveImmediateEnabled = enabled
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Stats: stats, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			t.Cleanup(func() { cm.CodeImage.Close() })
		}
		return stats
	}

	balanced := compile(OptimizeBalanced, true).Funcs[0]
	rollback := compile(OptimizeSize, false).Funcs[0]
	compact := compile(OptimizeSize, true).Funcs[0]
	if got := balanced.Peephole["logical-move-immediate"]; got != 0 {
		t.Fatalf("Balanced logical MOV hits = %d, want 0", got)
	}
	if got := compact.Peephole["logical-move-immediate"]; got != 1 {
		t.Fatalf("Size logical MOV hits = %d, want 1", got)
	}
	if compact.CodeBytes >= rollback.CodeBytes {
		t.Fatalf("logical MOV code = %d bytes, rollback = %d; want smaller", compact.CodeBytes, rollback.CodeBytes)
	}

	logicalMoveImmediateEnabled = true
	size := OptimizeSize
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Objective: &size})
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(0x00ff00ff00ff00ff); got != want {
		t.Fatalf("result = %#x, want %#x", got, want)
	}
}
