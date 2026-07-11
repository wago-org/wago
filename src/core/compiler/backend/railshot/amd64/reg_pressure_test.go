//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// regHeavyShiftChain builds a one-function module (with linear memory, so R15 is
// reserved as memSizeReg) whose body computes a deep left-spine of variable-count
// shifts inside a loop: acc = ((((p0 << c1) << c2) ...). Each i32.shl reserves RCX
// for the count and a neutral scratch for the value; nesting `depth` of them pins
// one register per level. With the loop making the params hot enough to pin, a
// large-enough depth exhausts the register file — the exact shape that made
// json-as/sqlite fail to link ("no register available to spill"). The module also
// serves as the correctness oracle for the unpinned recompile.
func regHeavyShiftChain(t *testing.T, nParams, depth int) *wasm.Module {
	t.Helper()
	params := make([]wasm.ValType, nParams)
	for i := range params {
		params[i] = i32
	}
	acc := byte(nParams)                       // accumulator local index (after the params)
	body := []byte{0x01, 0x01, 0x7f}           // one run of one i32 local
	body = append(body, 0x20, 0x00, 0x21, acc) // acc = p0
	body = append(body, 0x03, 0x40)            // loop (void) — runs once, boosts local scores
	body = append(body, 0x20, acc)             // acc
	for c := 0; c < depth; c++ {
		body = append(body, 0x20, byte(1+c%(nParams-1)), 0x74) // local.get p ; i32.shl
	}
	body = append(body, 0x21, acc) // acc = spine
	body = append(body, 0x0b)      // end loop
	body = append(body, 0x20, acc, 0x0b)

	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{i32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestExecRegHeavyUnpinnedRetry is the regression for the register-allocator
// exhaustion: a register-heavy nested-shift tree must compile (via the pinning-off
// retry) instead of failing to link, and must still compute the right value.
func TestExecRegHeavyUnpinnedRetry(t *testing.T) {
	const nParams, depth = 8, 7
	m := regHeavyShiftChain(t, nParams, depth)

	// The pinned attempt exhausts the file; assert the fix recompiled it unpinned.
	ms := &ModuleStats{}
	if _, err := CompileModuleWith(m, CompileOptions{Stats: ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !ms.Funcs[0].UnpinnedRetry {
		t.Fatalf("expected UnpinnedRetry (register-pressure recompile) to fire")
	}

	// Correctness: p0=5, all counts=1 → acc = 5 << depth.
	args := make([]uint64, nParams)
	args[0] = 5
	for i := 1; i < nParams; i++ {
		args[i] = 1
	}
	want := uint32(5) << depth
	if got := uint32(runAmd64u(t, m, args...)); got != want {
		t.Fatalf("reg-heavy shift chain = %d, want %d", got, want)
	}
}

// TestExecRegHeavyDeepCapped covers trees FAR deeper than the register file: the
// deferred-tree depth cap (maxDeferDepth) must break the chain into
// register-sized segments so it compiles at all, and still compute the right
// value. Depths past ~14 used to hard-fail with "no register available to spill".
func TestExecRegHeavyDeepCapped(t *testing.T) {
	const nParams = 8
	for _, depth := range []int{15, 20, 40, 100} {
		m := regHeavyShiftChain(t, nParams, depth)
		if _, err := CompileModuleWith(m, CompileOptions{}); err != nil {
			t.Fatalf("depth %d: compile: %v", depth, err)
		}
		args := make([]uint64, nParams)
		args[0] = 5
		for i := 1; i < nParams; i++ {
			args[i] = 1 // every shift count is 1, so the result is 5 << depth (0 once depth ≥ 32)
		}
		want := uint32(5) << depth
		if got := uint32(runAmd64u(t, m, args...)); got != want {
			t.Fatalf("depth %d: shift chain = %d, want %d", depth, got, want)
		}
	}
}
