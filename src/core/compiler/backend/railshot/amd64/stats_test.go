//go:build linux && amd64

package amd64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// compileWithStats compiles m collecting per-function codegen stats and returns
// the module stats. guard selects guard-page (elide) vs explicit bounds mode.
func compileWithStats(t *testing.T, m *wasm.Module, guard bool) *ModuleStats {
	t.Helper()
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(ms.Funcs) != len(m.Code) {
		t.Fatalf("stats funcs = %d, want %d", len(ms.Funcs), len(m.Code))
	}
	return &ms
}

// TestCodegenStatsPeepholes checks that each instruction-selection rewrite bumps
// its named counter exactly once for a body built to trigger it precisely once,
// and does not fire the others. This is the trustworthiness net the plan's exit
// criterion asks for (docs/no-ir-plan.md P1).
func TestCodegenStatsPeepholes(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []struct {
		name string
		in   []wasm.ValType
		out  []wasm.ValType
		body []byte
		peep string
	}{
		{
			// (0 locals) i32.const 3; i32.const 4; i32.add  → folded to const 7
			name: "const-fold", in: nil, out: i32,
			body: []byte{0x00, 0x41, 0x03, 0x41, 0x04, 0x6a, 0x0b},
			peep: "const-fold",
		},
		{
			// (0 locals) local.get 0; i32.const 0; i32.add  → x+0 → x
			name: "alu-identity", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x00, 0x6a, 0x0b},
			peep: "alu-identity",
		},
		{
			// (0 locals) local.get 0; i32.const 8; i32.mul  → x*8 → x<<3
			name: "strength-reduce", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x08, 0x6c, 0x0b},
			peep: "strength-reduce",
		},
		{
			// (0 locals) local.get 0; local.get 1; i32.const 2; i32.shl; i32.add → lea [x+y*4]
			name: "lea-scaled-index", in: i32x2, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x02, 0x74, 0x6a, 0x0b},
			peep: "lea-scaled-index",
		},
		{
			// (0 locals) local.get 0; i32.const 5; i32.lt_s — a compare RETURNED (not
			// fused into a branch) is materialized via SETcc: the stFlags opportunity (P3).
			name: "compare-setcc", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x05, 0x48, 0x0b},
			peep: "compare-setcc",
		},
		{
			// local.get 0; local.get 1; f64.add; local.set 0 sinks the result straight
			// into local 0's pinned XMM register instead of producing a scratch result
			// and moving it back in local.set.
			name: "float-local-sink", in: []wasm.ValType{wasm.F64, wasm.F64}, out: []wasm.ValType{wasm.F64},
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "float-local-sink",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.in, tc.out, tc.body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole[tc.peep]; got != 1 {
				t.Errorf("Peephole[%q] = %d, want 1 (all: %v)", tc.peep, got, s.Peephole)
			}
			// No unrelated rewrite from this list should have fired.
			for _, other := range cases {
				if other.peep == tc.peep {
					continue
				}
				if got := s.Peephole[other.peep]; got != 0 {
					t.Errorf("unexpected Peephole[%q] = %d for body %q", other.peep, got, tc.name)
				}
			}
		})
	}
}

// TestCodegenStatsStoreAndBounds checks the immediate-store peephole and that the
// bounds-check counter tracks the bounds mode: one inline check in explicit mode,
// none in guard-page mode.
func TestCodegenStatsStoreAndBounds(t *testing.T) {
	// (0 locals) i32.const 16; i32.const 42; i32.store align=2 offset=0
	body := []byte{0x00, 0x41, 0x10, 0x41, 0x2a, 0x36, 0x02, 0x00, 0x0b}
	m := modMem(t, 1, nil, nil, body)

	explicit := compileWithStats(t, m, false).Funcs[0]
	if explicit.Peephole["store-imm"] != 1 {
		t.Errorf("explicit store-imm = %d, want 1", explicit.Peephole["store-imm"])
	}
	if explicit.BoundsChecks != 1 {
		t.Errorf("explicit BoundsChecks = %d, want 1", explicit.BoundsChecks)
	}
	if explicit.TrapStubs < 1 {
		t.Errorf("explicit TrapStubs = %d, want >=1", explicit.TrapStubs)
	}

	guard := compileWithStats(t, m, true).Funcs[0]
	if guard.Peephole["store-imm"] != 1 {
		t.Errorf("guard store-imm = %d, want 1", guard.Peephole["store-imm"])
	}
	if guard.BoundsChecks != 0 {
		t.Errorf("guard BoundsChecks = %d, want 0 (guard-page elides)", guard.BoundsChecks)
	}
}

// TestCodegenStatsSizeCounters checks the always-on finalize counters are filled.
func TestCodegenStatsSizeCounters(t *testing.T) {
	// A memory-touching function keeps a real frame (a register-homed call-free
	// scalar leaf now elides its frame — TestFrameElide*):
	//   (0 locals) i32.const 0; local.get 0; i32.store; i32.const 0; i32.load
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x41, 0x00, 0x20, 0x00, 0x36, 0x02, 0x00, 0x41, 0x00, 0x28, 0x02, 0x00, 0x0b})
	s := compileWithStats(t, m, false).Funcs[0]
	if s.CodeBytes <= 0 {
		t.Errorf("CodeBytes = %d, want > 0", s.CodeBytes)
	}
	if s.FrameBytes <= 0 {
		t.Errorf("FrameBytes = %d, want > 0", s.FrameBytes)
	}
	if s.Name != "f" { // mod1 exports the function as "f"
		t.Errorf("Name = %q, want \"f\"", s.Name)
	}
}

// TestModuleStatsReport checks the explain dump renders the module-pin line and
// the per-function counters in a stable, greppable form.
func TestModuleStatsReport(t *testing.T) {
	ms := &ModuleStats{
		ModuleGlobalPins: []ModuleGlobalPinInfo{{Global: 2, Reg: "r14"}, {Global: 4, Reg: "r13"}},
		Funcs: []*CodegenStats{{
			FuncIdx: 0, Name: "serialize", CodeBytes: 128, FrameBytes: 48, MaxSpillSlots: 2,
			Flushes: 3, Condenses: 5, BoundsChecks: 4, TrapStubs: 1, PinnedLocals: 3,
			Calls:    map[string]int{"regabi": 2, "host": 1},
			Peephole: map[string]int{"store-imm": 7, "lea-scaled-index": 2},
		}},
	}
	out := ms.String()
	for _, want := range []string{
		"module-pinned globals (K=2): g2→r14 g4→r13",
		`fn#0 "serialize"`,
		"code=128B frame=48B spill_hi=2",
		"bounds=4 elidable=0 inloop=0 hoistable=0 trapStubs=1",
		"calls: host=1 regabi=2", // sorted keys
		"peep:  lea-scaled-index=2 store-imm=7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

// TestCodegenStatsCodegenNeutral proves that collecting stats does not change a
// single emitted byte: the counters live in CodegenStats, never in the Asm
// buffer. This is the guardrail that lets every later phase trust the dashboard
// without suspecting it perturbs the code it measures.
func TestCodegenStatsCodegenNeutral(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	shapes := []struct {
		name    string
		mem     bool
		in, out []wasm.ValType
		body    []byte
	}{
		{"lea", false, i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x02, 0x74, 0x6a, 0x0b}},
		{"strength", false, i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x08, 0x6c, 0x0b}},
		{"store", true, nil, nil, []byte{0x00, 0x41, 0x10, 0x41, 0x2a, 0x36, 0x02, 0x00, 0x0b}},
		{"load", true, i32, i32, []byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}},
	}
	for _, sh := range shapes {
		for _, guard := range []bool{false, true} {
			var m *wasm.Module
			if sh.mem {
				m = modMem(t, 1, sh.in, sh.out, sh.body)
			} else {
				m = mod1(t, sh.in, sh.out, sh.body)
			}
			off, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard})
			if err != nil {
				t.Fatalf("%s off: %v", sh.name, err)
			}
			on, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, Stats: &ModuleStats{}})
			if err != nil {
				t.Fatalf("%s on: %v", sh.name, err)
			}
			if !bytes.Equal(off.Code, on.Code) {
				t.Errorf("%s guard=%v: stats collection changed emitted code (%d vs %d bytes)",
					sh.name, guard, len(off.Code), len(on.Code))
			}
		}
	}
}

func TestParsePinGlobalK(t *testing.T) {
	cases := map[string]int{"": -1, "auto": -1, "bogus": -1, "0": 0, "1": 1, "2": 2, "3": 3}
	for in, want := range cases {
		if got := parsePinGlobalK(in); got != want {
			t.Errorf("parsePinGlobalK(%q) = %d, want %d", in, got, want)
		}
	}
}
