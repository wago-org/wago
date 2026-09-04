//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func emitTwoTrapGroupsArm64(compact bool) (*fn, int) {
	a := &a64.Asm{}
	sc := &scratch{asm: a}
	f := &fn{a: a, sc: sc, stats: &CodegenStats{}, policy: CodegenPolicy{CompactNative: compact}}
	sc.trapSites[trapUnreachable] = append(sc.trapSites[trapUnreachable], f.trapSite(a.Branch()|1))
	sc.trapSites[trapMemOOB] = append(sc.trapSites[trapMemOOB], f.trapSite(a.Branch()|1))
	f.emitTrapStubs()
	return f, a.Len()
}

func TestModuleSharedTrapBodySeedsFromHostBoundaryArm64(t *testing.T) {
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
	m := modFuncs(t, funcDef{i32, i32, callee}, funcDef{i32, i32, callee})
	for _, workers := range []int{1, 2} {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Workers: workers}); err != nil {
			t.Fatal(err)
		}
		if got := stats.Funcs[1].Peephole["module-shared-trap-body"]; got != 1 {
			t.Fatalf("workers=%d internal function shares = %d, want host-boundary seed", workers, got)
		}
		if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true, Workers: workers}, 0); err == nil {
			t.Fatalf("workers=%d host-boundary seed did not preserve trap", workers)
		}
	}
}

func TestCompactNativeSharedTrapUnwindExecutesArm64(t *testing.T) {
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
	for _, arg := range []uint64{0, 1} {
		if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: true}, arg); err == nil {
			t.Fatalf("argument %d did not trap", arg)
		}
	}
}

func TestCompactNativeSharesFunctionLocalTrapUnwindArm64(t *testing.T) {
	before := sharedTrapBodyEnabled
	sharedTrapBodyEnabled = false
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	ordinary, ordinaryBytes := emitTwoTrapGroupsArm64(false)
	compact, compactBytes := emitTwoTrapGroupsArm64(true)
	if got, want := ordinaryBytes-compactBytes, 8; got != want {
		t.Fatalf("two-group trap unwind saving = %d bytes, want %d (ordinary=%d compact=%d)", got, want, ordinaryBytes, compactBytes)
	}
	if got := ordinary.stats.Peephole["cold-trap-unwind-share"]; got != 0 {
		t.Fatalf("ordinary shared trap unwind count = %d, want 0", got)
	}
	if got := compact.stats.Peephole["cold-trap-unwind-share"]; got != 1 {
		t.Fatalf("compact shared trap unwind count = %d, want 1", got)
	}
}

func TestCompactNativeSharesCompleteTrapBodyArm64(t *testing.T) {
	before := sharedTrapBodyEnabled
	sharedTrapBodyEnabled = true
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	_, ordinaryBytes := emitTwoTrapGroupsArm64(false)
	compact, compactBytes := emitTwoTrapGroupsArm64(true)
	if got, want := ordinaryBytes-compactBytes, 32; got != want {
		t.Fatalf("two-group complete trap saving = %d bytes, want %d (ordinary=%d compact=%d)", got, want, ordinaryBytes, compactBytes)
	}
	if got := compact.stats.Peephole["shared-trap-body"]; got != 1 {
		t.Fatalf("compact shared trap body count = %d, want 1", got)
	}
	if compact.stats.TrapStubs != 2 || compact.stats.TrapGroups != 2 {
		t.Fatalf("compact trap stats = stubs:%d groups:%d", compact.stats.TrapStubs, compact.stats.TrapGroups)
	}
}

func TestCompactNativeSharesModuleTrapBodiesArm64(t *testing.T) {
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
	for _, workers := range []int{1, 2} {
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{CompactNative: true, Stats: &stats, Workers: workers, Optimizations: map[string]bool{"shared-trap-body": enabled}}
			if _, err := CompileModuleWith(m, opts); err != nil {
				t.Fatal(err)
			}
			wantShares := 0
			if enabled {
				wantShares = 1
			}
			if got := stats.Funcs[1].Peephole["module-shared-trap-body"] + stats.Funcs[2].Peephole["module-shared-trap-body"]; got != wantShares {
				t.Fatalf("workers=%d enabled=%t module trap shares = %d, want %d", workers, enabled, got, wantShares)
			}
			if stats.NativeSize.AccountedBytes() != stats.NativeSize.TotalBytes {
				t.Fatalf("workers=%d enabled=%t module native ledger = %+v", workers, enabled, stats.NativeSize)
			}
			for _, arg := range []uint64{0, 1} {
				runOpts := CompileOptions{CompactNative: true, Workers: workers, Optimizations: map[string]bool{"shared-trap-body": enabled}}
				if _, err := runArm64WrapperWithOptions(t, m, runOpts, arg); err == nil {
					t.Fatalf("workers=%d enabled=%t argument %d did not trap", workers, enabled, arg)
				}
			}
		}
	}
}
