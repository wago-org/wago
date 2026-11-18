//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

// Staged fixture: the production admission policy must actually select at least
// three backed homes; a passing result through the old two-home path is not proof.
func TestStreamResidencyWideCycle(t *testing.T) {
	streamResidencyEnable(t)
	oldWide := streamingWideHomesEnabled
	streamingWideHomesEnabled = true
	t.Cleanup(func() { streamingWideHomesEnabled = oldWide })
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	// Preserve all three old reads on the stack, then rotate bindings. Repeat
	// twice; a=2,b=3,c=5 becomes a=5,b=2,c=3, weighted checksum523.
	body := []byte{0, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x20, 2, 0x21, 1, 0x21, 0, 0x21, 2,
		0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0d, 0, 0x0b,
		0x20, 0, 0x41, 100 | 0x80, 0, 0x6c, 0x20, 1, 0x41, 10, 0x6c, 0x6a, 0x20, 2, 0x6a, 0x0b}
	m := mod1(t, params, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		streamingResidencyEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			streamResidencySelected(t, opts, on, false)
			if on && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["stream-residency-home"] < 3 {
				t.Fatal("wide fixture did not select >=3 backed homes", opts.Stats.Funcs[0].Peephole)
			}
			if got := runCompiledAmd64u(t, cm, 2, 3, 5, 2); got != 523 {
				t.Fatalf("on%v intr%v got%d want523", on, intr, got)
			}
		}
	}
}

func TestStreamResidencyStableBranchSnapshots(t *testing.T) {
	streamResidencyEnable(t)
	oldWide := streamingWideHomesEnabled
	streamingWideHomesEnabled = true
	t.Cleanup(func() { streamingWideHomesEnabled = oldWide })
	cases := []struct {
		name      string
		condition []byte
		args      []uint64
		want      uint64
		direct    bool
	}{
		{"direct-true", []byte{0x20, 0, 0x20, 1, 0x49}, []uint64{4, 9}, 0, true},
		{"direct-false", []byte{0x20, 0, 0x20, 1, 0x49}, []uint64{9, 4}, 7, true},
		{"same-cell-equal", []byte{0x20, 0, 0x20, 0, 0x46}, []uint64{9, 4}, 0, false},
		{"same-cell-not-less", []byte{0x20, 0, 0x20, 0, 0x49}, []uint64{9, 4}, 7, false},
		{"old-compare-before-update", []byte{0x20, 0, 0x20, 1, 0x49, 0x41, 30, 0x21, 0}, []uint64{4, 9}, 0, false},
		{"old-compare-false-before-update", []byte{0x20, 0, 0x20, 1, 0x49, 0x41, 0, 0x21, 0}, []uint64{9, 4}, 7, false},
		{"old-boolean-before-zero", []byte{0x20, 0, 0x41, 0, 0x21, 0}, []uint64{9, 4}, 0, false},
		{"current-boolean", []byte{0x20, 0}, []uint64{9, 4}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{1, 1, 0x7f, 0x03, 0x7f, 0x02, 0x40}
			body = append(body, tc.condition...)
			body = append(body, 0x0d, 0, 0x41, 7, 0x21, 2, 0x0b, 0x20, 2, 0x0b, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, on := range []bool{false, true} {
				streamingResidencyEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					cm, e := CompileModuleWith(m, opts)
					if e != nil {
						t.Fatal(e)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamResidencySelected(t, opts, on, false)
					if on && tc.direct && diagnosticsEnabled && !regallocCheckEnabled && opts.Stats.Funcs[0].Peephole["stream-residency-branch"] == 0 {
						t.Fatal("stable backed branch did not activate", opts.Stats.Funcs[0].Peephole)
					}
					for repeat := 0; repeat < 3; repeat++ {
						if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
							t.Fatalf("on%v intr%v repeat%d got%d want%d", on, intr, repeat, got, tc.want)
						}
					}
				}
			}
		})
	}
}

// Run the independently expected pressure fixtures under wider selection too.
// Each fixture requires real backing evacuation, not only successful fallback.
func TestStreamResidencyWidePressure(t *testing.T) {
	oldWide := streamingWideHomesEnabled
	streamingWideHomesEnabled = true
	t.Cleanup(func() { streamingWideHomesEnabled = oldWide })
	t.Run("ordinary-and-fixed-div", TestStreamResidencyPressure)
	t.Run("memory-address-under-pressure", TestStreamResidencyMemoryPressure)
	t.Run("dead-path-restore", TestStreamResidencyDeadPathRestore)
	t.Run("dropped-load-exact-trap", TestStreamResidencyDroppedLoadTrap)
}

func TestStreamResidencyDeadCanonicalGeneration(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(map[bool]string{false: "map-only-no-store", true: "detached-must-store"}[alias], func(t *testing.T) {
			s := streamResidencyUnitState()
			cell := s.residency.homes[0]
			var detached streamID
			if alias {
				detached = s.borrowBacking(cell, 0)
			}
			s.release(s.locals[0])
			s.locals[0] = s.add(streamNode{kind: shared.ScalarConstant, constant: 37, refs: 1})
			refs := uint32(1)
			if alias {
				refs++
			}
			if s.residency.cells[cell].refs != refs {
				t.Fatal("incorrect old-generation setup")
			}
			before, spills, slot := s.target.a.Len(), s.Spills, s.nextSlot
			s.commitBackingHomes()
			var want encoder.Asm
			if alias {
				want.Store32(RSP, s.target.SpillOffset(slot), Reg(s.homeRegs[0]))
			}
			want.MovImm32(Reg(s.homeRegs[0]), 37)
			if !bytes.Equal(s.target.a.B[before:], want.B) {
				t.Fatalf("alias%v native got%x want%x", alias, s.target.a.B[before:], want.B)
			}
			expectedSpills := 0
			if alias {
				expectedSpills = 1
			}
			if s.Spills-spills != expectedSpills {
				t.Fatalf("alias%v spill delta%d", alias, s.Spills-spills)
			}
			if alias {
				if o := s.operand(detached); o.Kind != shared.ScalarFrame || o.Offset != s.target.SpillOffset(slot) {
					t.Fatal("lost surviving old generation")
				}
				s.release(detached)
				if len(s.freeSlots) != 1 || s.freeSlots[0] != slot {
					t.Fatal("old snapshot slot not released exactly once")
				}
			} else if s.nextSlot != slot {
				t.Fatal("dead generation allocated an unnecessary spill")
			}
		})
	}
}

func TestStreamResidencySixHotLocals(t *testing.T) {
	streamResidencyEnable(t)
	oldWide := streamingWideHomesEnabled
	streamingWideHomesEnabled = true
	t.Cleanup(func() { streamingWideHomesEnabled = oldWide })
	ps := []wasm.ValType{wasm.I32}
	body := []byte{1, 5, 0x7f}
	// Exercise the ordinary entry bank, not the <=96-byte prepared-entry bank.
	for i := 0; i < 64; i++ {
		body = append(body, 0x01)
	}
	for i := byte(1); i < 6; i++ {
		body = append(body, 0x41, i, 0x21, i)
	}
	body = append(body, 0x03, 0x40)
	for i := byte(1); i < 6; i++ {
		body = append(body, 0x20, i, 0x41, 1, 0x6a, 0x21, i)
	}
	body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1)
	for i := byte(2); i < 6; i++ {
		body = append(body, 0x20, i, 0x6a)
	}
	body = append(body, 0x0b)
	m := mod1(t, ps, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		streamingResidencyEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			streamResidencySelected(t, opts, on, false)
			if on && diagnosticsEnabled && !regallocCheckEnabled {
				homes := opts.Stats.Funcs[0].Peephole["stream-residency-home"]
				if homes < 5 {
					t.Fatalf("need >=5 actual homes, got%d", homes)
				}
				t.Logf("interruptible%v actualHomes%d", intr, homes)
			}
			if got := runCompiledAmd64u(t, cm, 3); got != 30 {
				t.Fatalf("on%v intr%v got%d want30", on, intr, got)
			}
		}
	}
}
