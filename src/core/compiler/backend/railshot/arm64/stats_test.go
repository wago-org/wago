//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// compileWithStats compiles m collecting per-function codegen stats and returns
// the module stats. guard selects guard-page-style bounds elision vs explicit
// inline bounds checks.
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

func TestCodegenStatsPeepholesArm64(t *testing.T) {
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
			name: "const-fold", in: nil, out: i32,
			body: []byte{0x00, 0x41, 0x03, 0x41, 0x04, 0x6a, 0x0b},
			peep: "const-fold",
		},
		{
			name: "alu-identity", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x00, 0x6a, 0x0b},
			peep: "alu-identity",
		},
		{
			name: "strength-reduce", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x08, 0x6c, 0x0b},
			peep: "strength-reduce",
		},
		{
			name: "add-shifted-index", in: i32x2, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x02, 0x74, 0x6a, 0x0b},
			peep: "lea-scaled-index",
		},
		{
			// local.set $0 (local.get $1 + local.get $2): all three
			// parameters are pinned, so this must select ADD W0,W1,W2 rather
			// than MOV W0,W1 followed by an in-place ADD.
			name: "local-three-operand-sink", in: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, out: i32,
			body: []byte{0x00, 0x20, 0x01, 0x20, 0x02, 0x6a, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "local-3op-sink",
		},
		{
			// local.set $0 (local.get $1 - clz(local.get $0)): the unary
			// RHS is realized into one scratch and SUB writes $0 directly.
			name: "local-three-operand-unary-rhs", in: []wasm.ValType{wasm.I32, wasm.I32}, out: i32,
			body: []byte{0x00, 0x20, 0x01, 0x20, 0x00, 0x67, 0x6b, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "local-3op-sink",
		},
		{
			// i32.lt_s; local.tee $2; br_if 0 retains NZCV across CSET
			// instead of materializing the bool and comparing it again.
			name: "compare-tee-branch", in: i32x2, out: nil,
			body: []byte{0x01, 0x01, 0x7f, 0x20, 0x00, 0x20, 0x01, 0x48, 0x22, 0x02, 0x0d, 0x00, 0x0b},
			peep: "cmp-tee-branch-fuse",
		},
		{
			// select; local.set $0 writes CSEL directly into $0 instead of
			// producing an owned result followed by a move into the pin.
			name: "select-local-sink", in: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, out: i32,
			body: []byte{0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x1b, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "select-local-sink",
		},
		{
			// A short side-effect-free result if consumed by local.set writes
			// each arm directly into the pinned local, bypassing mergeReg.
			name: "if-local-sink", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x71, 0x04, 0x7f, 0x20, 0x00, 0x41, 0x07, 0x6a, 0x05, 0x20, 0x00, 0x41, 0x03, 0x6b, 0x0b, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "if-local-sink",
		},
		{
			name: "compare-cset", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x41, 0x05, 0x48, 0x0b},
			peep: "compare-setcc",
		},
		{
			name: "float-local-sink", in: []wasm.ValType{wasm.F64, wasm.F64}, out: []wasm.ValType{wasm.F64},
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "float-local-sink",
		},
		{
			name: "float-minmax-local-sink", in: []wasm.ValType{wasm.F64, wasm.F64}, out: []wasm.ValType{wasm.F64},
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa4, 0x21, 0x00, 0x20, 0x00, 0x0b},
			peep: "float-minmax-local-sink",
		},
		{
			name: "extend-wrap-elim", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0xad, 0xa7, 0x0b},
			peep: "extend-wrap-elim",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.in, tc.out, tc.body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole[tc.peep]; got != 1 {
				t.Errorf("Peephole[%q] = %d, want 1 (all: %v)", tc.peep, got, s.Peephole)
			}
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

func TestCodegenStatsStoreAndBoundsArm64(t *testing.T) {
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
		t.Errorf("guard BoundsChecks = %d, want 0", guard.BoundsChecks)
	}
}

func TestCodegenStatsConversionLocalSinkArm64(t *testing.T) {
	// local.set $0 (i32.add (local.get $1)
	//   (i32.wrap_i64 (i64.extend_i32_u (local.get $0))))
	// The W-form ADD reads $0 directly and both conversions disappear.
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0x00, 0x20, 0x01, 0x20, 0x00, 0xad, 0xa7, 0x6a, 0x21, 0x00, 0x20, 0x00, 0x0b})
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["local-3op-sink"]; got != 1 {
		t.Fatalf("local-3op-sink = %d, want 1 (all: %v)", got, s.Peephole)
	}
	if got := s.Peephole["extend-wrap-elim"]; got != 1 {
		t.Fatalf("extend-wrap-elim = %d, want 1 (all: %v)", got, s.Peephole)
	}
}

func TestCodegenStatsCodegenNeutralArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	shapes := []struct {
		name    string
		mem     bool
		in, out []wasm.ValType
		body    []byte
	}{
		{"add-shifted", false, i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x41, 0x02, 0x74, 0x6a, 0x0b}},
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

func TestAllocFRegUsesHighCallerSavedVRegsArm64(t *testing.T) {
	var f fn
	for r := Reg(0); r < 16; r++ {
		f.fregUser[r] = &elem{}
	}
	if got := f.allocFReg(0); got != 16 {
		t.Fatalf("allocFReg with V0-V15 occupied = V%d, want V16", got)
	}
}
