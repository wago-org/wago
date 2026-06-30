//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// hotnessWAT: 6 integer locals. Params n(0),b(1),c(2),d(3) are touched once in
// straight-line code; the declared locals i(4) and acc(5) are hammered inside a
// loop. So by use-count the hot locals are the high-index ones — exactly the set
// the legacy "first N" heuristic would drop.
const hotnessWAT = `(module (func (export "f")
  (param $n i32) (param $b i32) (param $c i32) (param $d i32) (result i32)
  (local $i i32) (local $acc i32)
  (block $done (loop $L
    local.get $i local.get $n i32.ge_s br_if $done
    local.get $acc local.get $i i32.add local.set $acc
    local.get $i i32.const 1 i32.add local.set $i
    br $L))
  local.get $acc
  local.get $b i32.add
  local.get $c i32.add
  local.get $d i32.add))`

func runI32With(t *testing.T, m *wasm.Module, opts CompileOptions, args ...int32) int32 {
	t.Helper()
	code, err := CompileFunctionWith(m, 0, opts)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	for i, a := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], uint64(uint32(a)))
	}
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return int32(binary.LittleEndian.Uint32(results))
}

func TestScanHintsLoopWeighting(t *testing.T) {
	m := watToModule(t, hotnessWAT)
	h := scanHints(m.Code[0].Body, 6)

	// i(4) and acc(5) live in the loop and must outscore the straight-line
	// params; i is touched more than acc.
	if !(h.localScore[4] > h.localScore[5]) {
		t.Errorf("score[i]=%d should exceed score[acc]=%d", h.localScore[4], h.localScore[5])
	}
	if !(h.localScore[5] > h.localScore[0]) {
		t.Errorf("score[acc]=%d should exceed score[n]=%d", h.localScore[5], h.localScore[0])
	}
	// n(0) is used inside the loop (×loop weight); b/c/d only once straight-line.
	if !(h.localScore[0] > h.localScore[1]) {
		t.Errorf("score[n]=%d should exceed score[b]=%d", h.localScore[0], h.localScore[1])
	}
	if h.localScore[1] != h.localScore[2] || h.localScore[2] != h.localScore[3] {
		t.Errorf("b/c/d should tie: %d %d %d", h.localScore[1], h.localScore[2], h.localScore[3])
	}
	if h.loopDepthMax != 1 {
		t.Errorf("loopDepthMax = %d, want 1", h.loopDepthMax)
	}
	if h.callCount != 0 {
		t.Errorf("callCount = %d, want 0", h.callCount)
	}
}

func TestPinningOrderHotnessVsFirstN(t *testing.T) {
	// Six integer locals; hand the scores so the hot locals are high-index.
	mk := func(mode LocalPinningMode) *cg {
		g := &cg{
			nLocals:     6,
			localParams: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32},
			opts:        CompileOptions{LocalPinning: mode},
		}
		g.hints = funcHints{localScore: []int64{10, 1, 1, 1, 50, 31}}
		return g
	}

	first := mk(PinFirstN).pinningOrder()
	if len(first) < 4 || first[0] != 0 || first[3] != 3 {
		t.Fatalf("PinFirstN order = %v, want index order starting 0..3", first)
	}

	hot := mk(PinHotness).pinningOrder()
	want := []int{4, 5, 0, 1, 2, 3} // by score desc, ties by index
	for i, w := range want {
		if hot[i] != w {
			t.Fatalf("PinHotness order = %v, want %v", hot, want)
		}
	}
	// The decisive difference: with 4 pinned registers, hotness keeps the hot
	// acc(5) while first-N drops it.
	top4 := func(o []int) map[int]bool {
		s := map[int]bool{}
		for _, x := range o[:4] {
			s[x] = true
		}
		return s
	}
	if top4(hot)[5] == false || top4(first)[5] == true {
		t.Errorf("hot top4 should include acc(5) and first-N should not: hot=%v first=%v", hot[:4], first[:4])
	}
}

// All three pinning modes must produce the same (correct) result; only which
// locals occupy registers changes. n=5 → sum 0..4 = 10; +b+c+d = +60 → 70.
func TestPinningModesAgreeAndDiffer(t *testing.T) {
	m := watToModule(t, hotnessWAT)
	const want = int32(70)
	for _, mode := range []LocalPinningMode{PinHotness, PinFirstN, PinNone} {
		got := runI32With(t, m, CompileOptions{LocalPinning: mode}, 5, 10, 20, 30)
		if got != want {
			t.Errorf("mode %d: got %d, want %d", mode, got, want)
		}
	}

	// Selection actually diverges: hotness and first-N must emit different code,
	// and disabling pinning must differ from both.
	hot, _ := CompileFunctionWith(m, 0, CompileOptions{LocalPinning: PinHotness})
	first, _ := CompileFunctionWith(m, 0, CompileOptions{LocalPinning: PinFirstN})
	none, _ := CompileFunctionWith(m, 0, CompileOptions{LocalPinning: PinNone})
	if bytes.Equal(hot, first) {
		t.Error("PinHotness and PinFirstN produced identical code; selection did not diverge")
	}
	if bytes.Equal(hot, none) || bytes.Equal(first, none) {
		t.Error("PinNone produced code identical to a pinning mode")
	}
}
