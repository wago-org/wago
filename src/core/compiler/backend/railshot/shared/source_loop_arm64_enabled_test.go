//go:build wago_regalloccheck

package shared

import (
	"fmt"
	"github.com/wago-org/wago/internal/regalloccheck"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

// All control images are compile/decode only, including incorrect bytes.
func TestSourceLoopARMEncoderProofControls(t *testing.T) {
	for _, width := range []int{4, 8} {
		for _, padding := range []int{0, 5, 8} {
			t.Run(fmt.Sprintf("width%d/padding%d", width, padding), func(t *testing.T) { sourceLoopARMControl(t, "positive", width, padding, regalloccheck.Verified) })
		}
	}
	for _, tc := range []struct {
		name string
		want regalloccheck.Verdict
	}{
		{"dead scratch kill", regalloccheck.Verified}, {"duplicate swap source", regalloccheck.Rejected}, {"wrong swap order", regalloccheck.Rejected}, {"wrong condition", regalloccheck.Rejected}, {"wrong exit", regalloccheck.Rejected}, {"backedge-only live kill", regalloccheck.Rejected},
		{"wide condition", regalloccheck.Inconclusive}, {"wrong predicate", regalloccheck.Inconclusive}, {"wrong header target", regalloccheck.Inconclusive}, {"reserved destination", regalloccheck.Inconclusive}, {"shifted move", regalloccheck.Inconclusive}, {"shifted kill", regalloccheck.Inconclusive},
		{"missing GP observer", regalloccheck.Inconclusive}, {"missing effect observer", regalloccheck.Inconclusive}, {"changed length", regalloccheck.Inconclusive}, {"prologue changed word", regalloccheck.Inconclusive}, {"epilogue changed word", regalloccheck.Inconclusive}, {"mixed prologue", regalloccheck.Inconclusive}, {"mixed epilogue", regalloccheck.Inconclusive}, {"changed return control", regalloccheck.Inconclusive},
	} {
		t.Run(tc.name, func(t *testing.T) { sourceLoopARMControl(t, tc.name, 8, 5, tc.want) })
	}
}

func TestSourceLoopARMAlignmentBound(t *testing.T) {
	sourceLoopARMControl(t, "positive", 8, 9, regalloccheck.Inconclusive)
}

func sourceLoopARMControl(t *testing.T, mode string, width, padding int, want regalloccheck.Verdict) {
	t.Helper()
	s, m := sourceLoopFixture(t)
	b := BeginSourceLoop(s, m, 0, true)
	if b == nil {
		t.Fatal("not admitted")
	}
	defer b.Close()
	var a a64.Asm
	a.ObserveRegalloc(b.ObserveEffect)
	a.ObserveGPWrites(b.ObserveGPWrites)
	if mode == "missing GP observer" {
		a.ObserveGPWrites(nil)
	}
	if mode == "missing effect observer" {
		a.ObserveRegalloc(nil)
	}
	emitSourceBranchARMFrame(&a, false)
	for i := 0; i < padding; i++ {
		a.B = append(a.B, 0x1f, 0x20, 3, 0xd5)
	}
	header := a.Len()
	mov := func(dst, src a64.Reg) {
		if width == 4 {
			a.MovReg32(dst, src)
		} else {
			a.MovReg64(dst, src)
		}
	}
	src := a64.X1
	if mode == "duplicate swap source" {
		src = a64.X0
	}
	if mode == "wrong exit" {
		src = a64.X2
	}
	tmp := a64.X12
	if mode == "reserved destination" {
		tmp = a64.X16
	}
	first := a.Len()
	mov(tmp, src)
	if mode == "wrong swap order" {
		mov(a64.X0, tmp)
		mov(a64.X1, a64.X0)
	} else {
		mov(a64.X1, a64.X0)
		mov(a64.X0, tmp)
	}
	if mode == "shifted move" {
		a.PatchU32(first, 0xaa0107ec)
	}
	if mode == "dead scratch kill" || mode == "backedge-only live kill" || mode == "shifted kill" {
		r := a64.X12
		if mode == "backedge-only live kill" {
			r = a64.X1
		}
		at := a.Len()
		a.Eor32(r, r, r)
		if mode == "shifted kill" {
			a.PatchU32(at, 0x4a0c058c)
		}
	}
	condition := a64.X2
	if mode == "wrong condition" {
		condition = a64.X0
	}
	if mode == "wide condition" {
		a.CmpImm64(condition, 0)
	} else {
		a.CmpImm32(condition, 0)
	}
	predicate := a64.CondNE
	if mode == "wrong predicate" {
		predicate = a64.CondEQ
	}
	branch := a.Bcond(predicate)
	restore := a.Len()
	emitSourceBranchARMFrame(&a, true)
	a.Ret()
	for _, at := range []int{0, restore} {
		for j := 0; j < 12; j += 4 {
			a.PatchU32(at+j, 0xd503201f)
		}
	}
	switch mode {
	case "prologue changed word":
		a.PatchU32(0, 0xd503205f)
	case "epilogue changed word":
		a.PatchU32(restore, 0xd503205f)
	case "mixed prologue":
		a.PatchU32(4, 0xf2a00010)
	case "mixed epilogue":
		a.PatchU32(restore+4, 0xf2a00010)
	case "changed return control":
		a.PatchU32(a.Len()-4, 0xd65f0000)
	}
	if mode == "wrong header target" {
		header -= 4
	}
	if !a.PatchBranch19(branch, header) {
		t.Fatal("branch patch failed")
	}
	b.EndEmission(a.Len())
	code := a.B
	if mode == "changed length" {
		code = code[:len(code)-4]
	}
	r := b.Verify(code, true)
	if r.Verdict != want {
		t.Fatalf("result=%+v code=%x", r, code)
	}
	if b.Verify(code, true) != r {
		t.Fatal("cache changed")
	}
	if mode == "backedge-only live kill" {
		// A one-iteration return of original parameter1 is correct despite
		// the GP1 kill. The cyclic proof must also retain that local.
		decoded, ok := decodeSourceLoopARM64(code)
		if !ok {
			t.Fatal("control stopped decoding")
		}
		flat := regalloccheck.Graph{Widths: []uint8{4, 4, 4}, Blocks: make([]regalloccheck.Block, 1), Inputs: []regalloccheck.Binding{{Location: leafReg(0), Value: 1}, {Location: leafReg(1), Value: 2}, {Location: leafReg(2), Value: 3}}}
		for _, in := range decoded.instructions {
			if in.copy {
				e := in.effect
				if e.Size == 4 {
					e.ClearTo = 8
				}
				flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
			} else if in.writes != 0 {
				for reg := uint8(0); reg < 32; reg++ {
					if in.writes&(1<<reg) != 0 {
						flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: leafReg(reg), Size: 8}})
					}
				}
			}
		}
		flat.Blocks[0].Operations = append(flat.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: leafReg(0), Value: 2})
		if once := flat.Verify(regalloccheck.Limits{Blocks: 1, Values: 3, Operations: 128, Facts: 4096, Work: 65536}); once.Verdict != regalloccheck.Verified {
			t.Fatalf("singleiteration=%+v", once)
		}
	}
	journal, ledger, token := b.journal, b.ledger, b.attempt
	work, storage := s.sourceWork, s.sourceStorage
	b.Close()
	b.Close()
	if b.owner != nil || journal.Finalize(0, 0, nil).State == regalloccheck.JournalReady || ledger.EventCount() != 0 || token.owner != nil || s.sourceAttempt != nil || s.sourceWork != work || s.sourceStorage != storage {
		t.Fatal("retained proof/refunded history")
	}
}
