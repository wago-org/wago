//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func emitTwoTrapGroupsArm64(objective OptimizationObjective) (*fn, int) {
	a := &a64.Asm{}
	sc := &scratch{asm: a}
	f := &fn{a: a, sc: sc, stats: &CodegenStats{}, policy: CodegenPolicy{Objective: objective}}
	sc.trapSites[trapUnreachable] = append(sc.trapSites[trapUnreachable], f.trapSite(a.Branch()|1))
	sc.trapSites[trapMemOOB] = append(sc.trapSites[trapMemOOB], f.trapSite(a.Branch()|1))
	f.emitTrapStubs()
	return f, a.Len()
}

func TestSizeObjectiveSharedTrapUnwindExecutesArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// if arg != 0: unreachable; otherwise load at 65536 from a one-page memory.
	// The two arms use distinct trap groups and therefore exercise the shared
	// terminal unwind through both incoming branches.
	m := modMem(t, 1, i32, i32, []byte{
		0x00,
		0x20, 0x00,
		0x04, 0x7f,
		0x00,
		0x05,
		0x41, 0x80, 0x80, 0x04,
		0x28, 0x02, 0x00,
		0x0b,
		0x0b,
	})
	size := OptimizeSize
	for _, arg := range []uint64{0, 1} {
		if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{Objective: &size}, arg); err == nil {
			t.Fatalf("argument %d did not trap", arg)
		}
	}
}

func TestSizeObjectiveSharesFunctionLocalTrapUnwindArm64(t *testing.T) {
	before := sharedTrapBodyEnabled
	sharedTrapBodyEnabled = false
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	balanced, balancedBytes := emitTwoTrapGroupsArm64(OptimizeBalanced)
	size, sizeBytes := emitTwoTrapGroupsArm64(OptimizeSize)
	if got, want := balancedBytes-sizeBytes, 8; got != want {
		t.Fatalf("two-group trap unwind saving = %d bytes, want %d (balanced=%d size=%d)", got, want, balancedBytes, sizeBytes)
	}
	if got := balanced.stats.Peephole["cold-trap-unwind-share"]; got != 0 {
		t.Fatalf("Balanced shared trap unwind count = %d, want 0", got)
	}
	if got := size.stats.Peephole["cold-trap-unwind-share"]; got != 1 {
		t.Fatalf("Size shared trap unwind count = %d, want 1", got)
	}
}

func TestSizeObjectiveSharesCompleteTrapBodyArm64(t *testing.T) {
	before := sharedTrapBodyEnabled
	sharedTrapBodyEnabled = true
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	_, balancedBytes := emitTwoTrapGroupsArm64(OptimizeBalanced)
	size, sizeBytes := emitTwoTrapGroupsArm64(OptimizeSize)
	if got, want := balancedBytes-sizeBytes, 32; got != want {
		t.Fatalf("two-group complete trap saving = %d bytes, want %d (balanced=%d size=%d)", got, want, balancedBytes, sizeBytes)
	}
	if got := size.stats.Peephole["shared-trap-body"]; got != 1 {
		t.Fatalf("Size shared trap body count = %d, want 1", got)
	}
	if size.stats.TrapStubs != 2 || size.stats.TrapGroups != 2 {
		t.Fatalf("Size trap stats = stubs:%d groups:%d", size.stats.TrapStubs, size.stats.TrapGroups)
	}
}

func TestSizeObjectiveSharesModuleTrapBodiesArm64(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	callee := []byte{
		0x00,
		0x20, 0x00,
		0x04, 0x7f,
		0x00,
		0x05,
		0x41, 0x01, 0x41, 0x00, 0x6e,
		0x0b,
		0x0b,
	}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{
			0x00,
			0x20, 0x00,
			0x04, 0x7f,
			0x41, 0x01, 0x10, 0x01,
			0x05,
			0x41, 0x00, 0x10, 0x02,
			0x0b,
			0x0b,
		}},
		funcDef{i32, i32, callee},
		funcDef{i32, i32, callee},
	)
	size := OptimizeSize
	for _, workers := range []int{1, 2} {
		var stats ModuleStats
		opts := CompileOptions{Objective: &size, Stats: &stats, Workers: workers}
		if _, err := CompileModuleWith(m, opts); err != nil {
			t.Fatal(err)
		}
		if got := stats.Funcs[1].Peephole["module-shared-trap-body"] + stats.Funcs[2].Peephole["module-shared-trap-body"]; got != 1 {
			t.Fatalf("workers=%d module trap shares = %d, want 1", workers, got)
		}
		if stats.NativeSize.AccountedBytes() != stats.NativeSize.TotalBytes {
			t.Fatalf("workers=%d module native ledger = %+v", workers, stats.NativeSize)
		}
		for _, arg := range []uint64{0, 1} {
			if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{Objective: &size, Workers: workers}, arg); err == nil {
				t.Fatalf("workers=%d argument %d did not trap through module body island", workers, arg)
			}
		}
	}
}
