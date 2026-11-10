//go:build wago_regalloccheck

package regalloccheck

import (
	"strconv"
	"testing"
)

func TestFlowWideRoundtripAndPartialClobber(t *testing.T) {
	for _, width := range []int{32, 64} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			a, b := Register(FP, 0), Register(FP, 31)
			g := Graph{Widths: []uint8{uint8(width)}, Inputs: []Binding{{a, 1}}, Blocks: []Block{{Operations: []Operation{
				copyTo(Slot(-64), a, width), copyTo(b, Slot(-64), width), use(b, 1),
			}}}}
			verifyFlow(t, g, Verified, NoFailure)
			g.Blocks[0].Operations = append(g.Blocks[0].Operations[:2], kill(b.next(width-1), 1), use(b, 1))
			verifyFlow(t, g, Rejected, UnknownInput)
		})
	}
}

func TestStateAndFlowGP32SwapClearsBothHighHalves(t *testing.T) {
	a, b := Register(GP, 0), Register(GP, 1)
	var s State
	left, right := s.Seed(a, 8), s.Seed(b, 8)
	e := Effect{Kind: Swap, Dst: a, Src: b, Size: 4}
	s.Apply(e)
	s.Expect("swapped low left", a, right[:4])
	s.Expect("swapped low right", b, left[:4])
	for _, loc := range []Location{a.next(4), b.next(4)} {
		for _, v := range s.Read(loc, 4) {
			if v.value != 0 {
				t.Fatal("GP32 swap retained high provenance", loc)
			}
		}
	}
	g := Graph{Widths: []uint8{8, 8}, Inputs: []Binding{{a, 1}, {b, 2}}, Blocks: []Block{{Operations: []Operation{machine(e), use(a, 2)}}}}
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowWideRedefinitionForgetsHighAliases(t *testing.T) {
	a, saved := Register(FP, 0), Register(FP, 1)
	// A new dynamic definition invalidates every old alias. Replacing only the
	// low sixteen bytes must not make the old upper bytes a valid new value.
	g := Graph{Widths: []uint8{64}, Blocks: []Block{{Operations: []Operation{
		def(a, 1), copyTo(saved, a, 64), def(a, 1), copyTo(saved, a, 16), use(saved, 1),
	}}}}
	verifyFlow(t, g, Rejected, UnknownInput)
	g.Blocks[0].Operations[3] = copyTo(saved, a, 64)
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowWideParametersRenameEveryAliasSimultaneously(t *testing.T) {
	a, b := Register(FP, 0), Register(FP, 1)
	g := Graph{Widths: []uint8{64, 64}, Inputs: []Binding{{a, 1}, {b, 2}}, Blocks: []Block{
		{Edges: []Edge{{To: 1, Parameters: []Parameter{{1, 2, a}, {2, 1, b}}}}},
		{Operations: []Operation{use(a, 2), use(b, 1)}, Edges: []Edge{{To: 1, Parameters: []Parameter{{2, 2, a}, {1, 1, b}}}}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[1].Operations = append(g.Blocks[1].Operations, kill(a.next(63), 1))
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowWideConstantPreservesHighAliases(t *testing.T) {
	a, saved := Register(FP, 0), Register(FP, 1)
	g := Graph{Widths: []uint8{64}, ConstValues: []ValueID{1}, Blocks: []Block{{Operations: []Operation{
		constantDef(a, 1), copyTo(saved, a, 64), constantDef(a, 1), use(saved, 1),
	}}}}
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowWideDuplicateParametersRetainAllHighAliases(t *testing.T) {
	a, b := Register(FP, 0), Register(FP, 1)
	g := Graph{Widths: []uint8{64, 64, 64}, Inputs: []Binding{{a, 1}}, Blocks: []Block{
		{Operations: []Operation{copyTo(b, a, 64)}, Edges: []Edge{{To: 1, Parameters: []Parameter{{1, 2, a}, {1, 3, b}}}}},
		{Operations: []Operation{use(a, 1), use(a, 2), use(a, 3), use(b, 1), use(b, 2), use(b, 3)}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[1].Operations = append([]Operation{kill(b.next(63), 1)}, g.Blocks[1].Operations...)
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowWideOverlappingFrameCopyAndSwap(t *testing.T) {
	g := Graph{Widths: []uint8{64}, Inputs: []Binding{{Slot(0), 1}}, Blocks: []Block{{Operations: []Operation{
		copyTo(Slot(1), Slot(0), 64), use(Slot(1), 1),
	}}}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[0].Operations = []Operation{machine(Effect{Kind: Swap, Dst: Slot(0), Src: Slot(1), Size: 64})}
	verifyFlow(t, g, Inconclusive, InvalidGraph)
	a, b := Register(FP, 0), Register(FP, 1)
	g = Graph{Widths: []uint8{64, 64}, Inputs: []Binding{{a, 1}, {b, 2}}, Blocks: []Block{{Operations: []Operation{
		machine(Effect{Kind: Swap, Dst: a, Src: b, Size: 64}), use(a, 2), use(b, 1),
	}}}}
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowWideClearAndCall(t *testing.T) {
	a, b := Register(FP, 0), Register(FP, 1)
	g := Graph{Widths: []uint8{64}, Inputs: []Binding{{a, 1}}, Blocks: []Block{{Operations: []Operation{
		copyTo(b, a, 64), machine(Effect{Kind: Copy, Dst: b, Src: a, Size: 16, ClearTo: 64}), use(a, 1), use(b, 1),
	}}}}
	verifyFlow(t, g, Rejected, UnknownInput)
	g.Blocks[0].Operations = []Operation{copyTo(Slot(0), a, 64), machine(Effect{Kind: Call}), use(Slot(0), 1)}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[0].Operations = append(g.Blocks[0].Operations, use(a, 1))
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowWideValidationAndBudgets(t *testing.T) {
	a := Register(FP, 0)
	for _, e := range []Effect{
		{Kind: Kill, Dst: a.next(63), Size: 2},
		{Kind: Kill, Dst: a, Size: 65},
		{Kind: Kill, Dst: a, Size: 16, ClearTo: 65},
		{Kind: Kill, Dst: Register(GP, 0), Size: 64},
		{Kind: Kill, Dst: Slot(2147483616), Size: 64},
	} {
		g := Graph{Blocks: []Block{{Operations: []Operation{machine(e)}}}}
		verifyFlow(t, g, Inconclusive, InvalidGraph)
	}
	g := Graph{Widths: []uint8{64}, Inputs: []Binding{{a, 1}}, Blocks: []Block{{Operations: []Operation{use(a, 1)}}}}
	for _, l := range []Limits{{Facts: 63}, {Work: 63}} {
		r := g.Verify(l)
		if r.Verdict != Inconclusive || r.Reason != ResourceLimit {
			t.Fatal(r)
		}
	}
	// This is the last valid signed frame start for a full-width carrier.
	g.Inputs[0].Location = Slot(2147483584)
	g.Blocks[0].Operations[0].Location = Slot(2147483584)
	verifyFlow(t, g, Verified, NoFailure)
}

func TestJournalWideEffectsAndCoverageGap(t *testing.T) {
	for _, gap := range []bool{false, true} {
		j := NewEmissionJournal(JournalLimits{})
		defer j.Close()
		if !j.BeginEmission(0) {
			t.Fatal(j.Result())
		}
		e := Effect{Kind: Copy, Dst: Register(FP, 31), Src: Slot(-64), Size: 64}
		j.ObserveEffect(e)
		if gap {
			j.ObserveGap()
		}
		if !j.EndEmission(8) {
			t.Fatal(j.Result())
		}
		r := j.Finalize(8, 8, nil)
		want := JournalReady
		if gap {
			want = JournalIncomplete
		}
		if r.State != want {
			t.Fatal(r)
		}
		observed, ok := j.Event(0)
		if !ok || observed.Effect != e {
			t.Fatal(observed, ok)
		}
	}
}
