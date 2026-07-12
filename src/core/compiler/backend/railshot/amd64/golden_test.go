//go:build linux && amd64

package amd64

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Golden disassembly tests: the per-optimization shape regression net
// (docs/no-ir-plan.md P1). Each later phase adds its golden here alongside its
// CodegenStats counter, so a codegen change that silently drops an optimization
// is caught by shape, not just by a benchmark. These shell out to objdump and
// skip cleanly where it is unavailable.

// disasm returns the objdump Intel-syntax disassembly of raw machine code,
// lowercased for stable substring matching. Skips when objdump is absent.
func disasm(t *testing.T, code []byte) string {
	t.Helper()
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("objdump not found on PATH; skipping golden disassembly")
	}
	bin := filepath.Join(t.TempDir(), "code.bin")
	if err := os.WriteFile(bin, code, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-Mintel", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("objdump: %v\n%s", err, out)
	}
	return strings.ToLower(string(out))
}

// compileCode compiles m and returns the raw code blob (one function per module
// here, so the blob is that function).
func compileCode(t *testing.T, m *wasm.Module, guard bool) []byte {
	t.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cm.Code
}

// TestGoldenImmediateStore: a constant store lowers to a `mov [mem],imm` — the
// constant goes straight into the store as an immediate, no register load.
func TestGoldenImmediateStore(t *testing.T) {
	// (guard: no bounds-check noise) i32.const 16; i32.const 42; i32.store
	m := modMem(t, 1, nil, nil, []byte{0x00, 0x41, 0x10, 0x41, 0x2a, 0x36, 0x02, 0x00, 0x0b})
	d := disasm(t, compileCode(t, m, true))
	if !strings.Contains(d, "0x2a") || !strings.Contains(d, "[rbx") {
		t.Errorf("expected `mov dword ptr [rbx...],0x2a` immediate store, got:\n%s", d)
	}
}

// TestGoldenLeaScaledIndex: base + (index << k) lowers to a single scaled-index
// LEA (the AssemblyScript array-address shape).
func TestGoldenLeaScaledIndex(t *testing.T) {
	// local.get 0; local.get 1; i32.const 2; i32.shl; i32.add → lea [x + y*4]
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x02, 0x74, 0x6a, 0x0b})
	d := disasm(t, compileCode(t, m, false))
	if !strings.Contains(d, "lea") || !strings.Contains(d, "*4") {
		t.Errorf("expected scaled-index `lea ...[..*4]`, got:\n%s", d)
	}
}

// TestGoldenBoundsMode: an explicit-mode load carries an inline bounds compare +
// `ja` to the trap stub; guard-page mode elides it, so the code is strictly
// smaller and has no bounds branch.
func TestGoldenBoundsMode(t *testing.T) {
	// local.get 0; i32.load align=2 offset=0
	m := mod1Mem(t)
	explicitCode := compileCode(t, m, false)
	guardCode := compileCode(t, m, true)
	explicit := disasm(t, explicitCode)
	if !strings.Contains(explicit, "cmp") || !strings.Contains(explicit, "ja ") {
		t.Errorf("explicit-mode load missing bounds `cmp; ja`:\n%s", explicit)
	}
	if len(guardCode) >= len(explicitCode) {
		t.Errorf("guard code (%dB) not smaller than explicit (%dB) — bounds check not elided",
			len(guardCode), len(explicitCode))
	}
}

// TestGoldenMemoryCopyRepMovs: a dynamic-length memory.copy lowers a hybrid path
// that includes the `rep movs` block (the forward-safe fast path from #99).
func TestGoldenMemoryCopyRepMovs(t *testing.T) {
	// local.get 0 (dst); local.get 1 (src); local.get 2 (n); memory.copy 0 0
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil,
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b})
	d := disasm(t, compileCode(t, m, true))
	if !strings.Contains(d, "movs") {
		t.Errorf("expected `rep movs` in memory.copy lowering, got:\n%s", d)
	}
}

func TestGoldenFloatLocalSink(t *testing.T) {
	// local.get 0; local.get 1; f64.add; local.set 0; local.get 0
	m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64},
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x21, 0x00, 0x20, 0x00, 0x0b})
	d := disasm(t, compileCode(t, m, false))
	if !strings.Contains(d, "vaddsd xmm12,xmm12,xmm13") {
		t.Errorf("expected fused `vaddsd xmm12,xmm12,xmm13`, got:\n%s", d)
	}
	afterAdd := d[strings.Index(d, "vaddsd xmm12,xmm12,xmm13"):]
	if strings.Contains(afterAdd, "movsd  xmm12") || strings.Contains(afterAdd, "movsd xmm12") {
		t.Errorf("fused float local update should not move a scratch result back into xmm12:\n%s", d)
	}
}

func TestGoldenFloatConstPreloadBeforeLoop(t *testing.T) {
	// acc = 1; loop { acc *= 1.0000001; n-- }; return acc. The multiplier
	// constant should be materialized before the loop header, not on every trip.
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64}, []byte{
		0x01, 0x01, 0x7c,
		0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f, // f64.const 1
		0x21, 0x01, // local.set 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // br_if break (i32.eqz n)
		0x20, 0x01,
		0x44, 0x9b, 0xf2, 0xd7, 0x1a, 0x00, 0x00, 0xf0, 0x3f, // f64.const 1.0000001
		0xa2,       // f64.mul
		0x21, 0x01, // local.set 1
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, // br loop
		0x0b, 0x0b, // end loop/block
		0x20, 0x01, 0x0b, // local.get 1; end
	})
	d := disasm(t, compileCode(t, m, false))
	// The multiplier is loaded once from the trailing rip-relative constant pool,
	// before the loop header, into a register the loop body reuses (a rip-relative
	// movsd) — not rebuilt every trip. Verify the first constant load precedes the
	// loop test and that the loop body issues no per-trip pool load.
	loop := strings.Index(d, "\ttest")
	preload := strings.Index(d, "[rip")
	if loop < 0 || preload < 0 || preload > loop {
		t.Errorf("expected the f64 constant rip-loaded before the loop test, got:\n%s", d)
	}
	body := d[loop:]
	if end := strings.Index(body, "\tjmp"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "[rip") {
		t.Errorf("f64 constant re-loaded inside the loop body:\n%s", d)
	}
}

// mod1Mem builds a one-memory module whose exported function does a single
// i32.load of its i32 parameter (the guard-vs-explicit bounds shape probe).
func mod1Mem(t *testing.T) *wasm.Module {
	t.Helper()
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b})
}
