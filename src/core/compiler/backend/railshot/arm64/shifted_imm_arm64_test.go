//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSizeShiftedAddImmediateArm64(t *testing.T) {
	// local.get 0; i64.const 4096; i64.add
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64},
		[]byte{0x00, 0x20, 0x00, 0x42, 0x80, 0x20, 0x7c, 0x0b})
	before := shiftedAddSubImmediateEnabled
	t.Cleanup(func() { shiftedAddSubImmediateEnabled = before })
	compile := func(objective OptimizationObjective, enabled bool) *ModuleStats {
		shiftedAddSubImmediateEnabled = enabled
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
	if balanced.Peephole["shifted-add-sub-immediate"] != 0 {
		t.Fatalf("Balanced unexpectedly selected shifted immediate: %v", balanced.Peephole)
	}
	if got := compact.Peephole["shifted-add-sub-immediate"]; got != 1 {
		t.Fatalf("Size shifted-immediate hits = %d, want 1", got)
	}
	if compact.CodeBytes > rollback.CodeBytes {
		t.Fatalf("shifted-immediate code = %d bytes, rollback = %d; want no growth", compact.CodeBytes, rollback.CodeBytes)
	}

	shiftedAddSubImmediateEnabled = true
	size := OptimizeSize
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Objective: &size}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4103 {
		t.Fatalf("result = %d, want 4103", got)
	}

	// local.get 0; i64.const 4096; i64.eq
	compare := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x42, 0x80, 0x20, 0x51, 0x0b})
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(compare, CompileOptions{Objective: &size, Stats: stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["shifted-add-sub-immediate"]; got != 1 {
		t.Fatalf("Size shifted compare hits = %d, want 1", got)
	}
	got, err = runArm64WrapperWithOptions(t, compare, CompileOptions{Objective: &size}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("compare result = %d, want 1", got)
	}
}
