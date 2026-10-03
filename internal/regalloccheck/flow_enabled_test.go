//go:build wago_regalloccheck

package regalloccheck

import (
	"strconv"
	"strings"
	"testing"
)

func machine(e Effect) Operation { return Operation{Kind: Machine, Effect: e} }
func def(loc Location, id ValueID) Operation {
	return Operation{Kind: Define, Location: loc, Value: id}
}
func use(loc Location, id ValueID) Operation {
	return Operation{Kind: Use, Location: loc, Value: id, Where: "test use"}
}
func copyTo(dst, src Location, size int) Operation {
	return machine(Effect{Kind: Copy, Dst: dst, Src: src, Size: size})
}
func kill(loc Location, size int) Operation { return machine(Effect{Kind: Kill, Dst: loc, Size: size}) }

func verifyFlow(t *testing.T, g Graph, verdict Verdict, reason FailureReason) Result {
	t.Helper()
	got := g.Verify(Limits{})
	if got.Verdict != verdict || got.Reason != reason {
		t.Fatalf("got %+v, want verdict %d reason %d", got, verdict, reason)
	}
	return got
}

func TestFlowDiamondParameters(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8, 8, 8}, Blocks: []Block{
		{Edges: []Edge{{To: 1}, {To: 2}}},
		{Operations: []Operation{def(r, 1)}, Edges: []Edge{{To: 3, Parameters: []Parameter{{From: 1, To: 3, Location: r}}}}},
		{Operations: []Operation{def(r, 2)}, Edges: []Edge{{To: 3, Parameters: []Parameter{{From: 2, To: 3, Location: r}}}}},
		{Operations: []Operation{use(r, 3)}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[2].Operations = append(g.Blocks[2].Operations, kill(r, 8))
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowDuplicateParameterRetainsSource(t *testing.T) {
	r, other := Register(GP, 1), Register(GP, 2)
	g := Graph{Widths: []uint8{8, 8, 8}, Inputs: []Binding{{r, 1}}, Blocks: []Block{
		{Operations: []Operation{copyTo(other, r, 8)}, Edges: []Edge{{To: 1, Parameters: []Parameter{{1, 2, r}, {1, 3, other}}}}},
		{Operations: []Operation{use(r, 1), use(r, 2), use(r, 3), use(other, 1), use(other, 2), use(other, 3)}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowParallelParameterSwapAndSelfEdge(t *testing.T) {
	a, b := Register(GP, 1), Register(GP, 2)
	g := Graph{Widths: []uint8{8, 8}, Inputs: []Binding{{a, 1}, {b, 2}}, Blocks: []Block{
		{Edges: []Edge{{To: 1, Parameters: []Parameter{{1, 2, a}, {2, 1, b}}}}},
		{Operations: []Operation{use(a, 2), use(b, 1)}, Edges: []Edge{{To: 1, Parameters: []Parameter{{2, 2, a}, {1, 1, b}}}}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowChecksOutgoingCarrierBeforeRenaming(t *testing.T) {
	a, wrong := Register(GP, 1), Register(GP, 2)
	g := Graph{Widths: []uint8{8, 8}, Inputs: []Binding{{a, 1}}, Blocks: []Block{
		{Edges: []Edge{{To: 1, Parameters: []Parameter{{1, 2, wrong}}}}},
		{Operations: []Operation{use(a, 2)}},
	}}
	// Renaming finds the source at a, but the promised edge carrier is wrong.
	got := verifyFlow(t, g, Rejected, UnknownInput)
	if !strings.Contains(got.Message, "edge 0 parameter 0") {
		t.Fatal(got)
	}
}

func TestFlowEntryBoundarySurvivesBackedge(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8}, Inputs: []Binding{{r, 1}}, Blocks: []Block{
		{Operations: []Operation{use(r, 1)}, Edges: []Edge{{To: 1}}},
		{Operations: []Operation{kill(r, 8)}, Edges: []Edge{{To: 0}}},
	}}
	got := verifyFlow(t, g, Rejected, UnknownInput)
	if got.Block != 0 {
		t.Fatal(got)
	}
	// A fact generated only by the backedge cannot prove the first iteration.
	g.Inputs = nil
	g.Blocks[1].Operations = []Operation{def(r, 1)}
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowLoopDefinitionInvalidatesOldAliases(t *testing.T) {
	r, saved := Register(GP, 1), Slot(0)
	g := Graph{Widths: []uint8{8}, Blocks: []Block{
		{Operations: []Operation{def(r, 1), copyTo(saved, r, 8)}, Edges: []Edge{{To: 1}}},
		{Operations: []Operation{def(r, 1), use(saved, 1)}, Edges: []Edge{{To: 1}}},
	}}
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowLoopRequiresConvergence(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8}, Inputs: []Binding{{r, 1}}, Blocks: []Block{
		{Edges: []Edge{{To: 1}}},
		{Operations: []Operation{use(r, 1)}, Edges: []Edge{{To: 2}}},
		{Edges: []Edge{{To: 3}}},
		{Operations: []Operation{kill(r, 8)}, Edges: []Edge{{To: 1}}},
	}}
	got := verifyFlow(t, g, Rejected, UnknownInput)
	if got.Block != 1 {
		t.Fatal(got)
	}
}

func TestFlowUnvisitedIsNotUnknown(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8}, Blocks: []Block{
		{},
		{Operations: []Operation{use(r, 1), {Kind: Unsupported, Where: "unreachable"}}, Edges: []Edge{{To: 0}}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[0].Edges = []Edge{{To: 1}}
	verifyFlow(t, g, Inconclusive, UnsupportedOperation)
	g.Blocks[1].Operations = g.Blocks[1].Operations[:1]
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowPartialWritesAndBanks(t *testing.T) {
	gp, fp := Register(GP, 1), Register(FP, 1)
	for _, tc := range []struct {
		name    string
		effect  Effect
		at      Location
		id      ValueID
		verdict Verdict
		reason  FailureReason
	}{
		{"separate banks", Effect{Kind: Kill, Dst: gp, Size: 8}, fp, 2, Verified, NoFailure},
		{"GP32 upper half", Effect{Kind: Copy, Dst: gp, Src: gp, Size: 4}, gp, 1, Rejected, UnknownInput},
		{"vector upper half", Effect{Kind: Kill, Dst: fp.next(8), Size: 8}, fp, 2, Rejected, UnknownInput},
		{"legacy scalar self copy", Effect{Kind: Copy, Dst: fp, Src: fp, Size: 4}, fp, 2, Verified, NoFailure},
		{"cleared scalar lanes", Effect{Kind: Copy, Dst: fp, Src: fp, Size: 4, ClearTo: 16}, fp, 2, Rejected, UnknownInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := Graph{Widths: []uint8{8, 16}, Inputs: []Binding{{gp, 1}, {fp, 2}}, Blocks: []Block{{Operations: []Operation{machine(tc.effect), use(tc.at, tc.id)}}}}
			verifyFlow(t, g, tc.verdict, tc.reason)
		})
	}
}

func TestFlowCopiesOverlapSimultaneously(t *testing.T) {
	g := Graph{Widths: []uint8{16}, Inputs: []Binding{{Slot(0), 1}}, Blocks: []Block{{Operations: []Operation{copyTo(Slot(8), Slot(0), 16), use(Slot(8), 1)}}}}
	verifyFlow(t, g, Verified, NoFailure)
}

func TestFlowCallsPreserveOnlyFrame(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8}, Inputs: []Binding{{r, 1}}, Blocks: []Block{{Operations: []Operation{copyTo(Slot(0), r, 8), machine(Effect{Kind: Call}), copyTo(r, Slot(0), 8), use(r, 1)}}}}
	verifyFlow(t, g, Verified, NoFailure)
	g.Blocks[0].Operations[2] = copyTo(r, Slot(8), 8)
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestFlowProvenanceIsIndependentOfLocation(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8, 8}, Blocks: []Block{{Operations: []Operation{def(r, 1), def(r, 2), use(r, 1)}}}}
	verifyFlow(t, g, Rejected, ProvenanceMismatch)
}

func TestFlowPredecessorOrderDoesNotChangeVerdict(t *testing.T) {
	r := Register(GP, 1)
	for _, reverse := range []bool{false, true} {
		for _, corrupt := range []bool{false, true} {
			g := Graph{Widths: []uint8{8, 8, 8}, Blocks: []Block{
				{Edges: []Edge{{To: 1}, {To: 2}}},
				{Operations: []Operation{def(r, 1)}, Edges: []Edge{{To: 3, Parameters: []Parameter{{1, 3, r}}}}},
				{Operations: []Operation{def(r, 2)}, Edges: []Edge{{To: 3, Parameters: []Parameter{{2, 3, r}}}}},
				{Operations: []Operation{use(r, 3)}},
			}}
			if reverse {
				g.Blocks[0].Edges[0], g.Blocks[0].Edges[1] = g.Blocks[0].Edges[1], g.Blocks[0].Edges[0]
			}
			if corrupt {
				g.Blocks[2].Operations = append(g.Blocks[2].Operations, kill(r, 8))
			}
			want, reason := Verified, NoFailure
			if corrupt {
				want, reason = Rejected, UnknownInput
			}
			verifyFlow(t, g, want, reason)
		}
	}
}

func TestFlowResourceBounds(t *testing.T) {
	r := Register(GP, 1)
	g := Graph{Widths: []uint8{8, 8}, Inputs: []Binding{{r, 1}}, Blocks: []Block{{Operations: []Operation{use(r, 1)}, Edges: []Edge{{To: 1}}}, {}}}
	for _, tc := range []struct {
		name   string
		limits Limits
	}{
		{"blocks", Limits{Blocks: 1}}, {"values", Limits{Values: 1}},
		{"operations", Limits{Operations: 1}}, {"facts", Limits{Facts: 1}},
		{"work", Limits{Work: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 5; i++ {
				got := g.Verify(tc.limits)
				if got.Verdict != Inconclusive || got.Reason != ResourceLimit {
					t.Fatal(got)
				}
			}
		})
	}
	got := g.Verify(Limits{Work: -1})
	if got.Verdict != Inconclusive || got.Reason != InvalidGraph {
		t.Fatal(got)
	}
}

func TestFlowInvalidModels(t *testing.T) {
	r := Register(GP, 1)
	cases := []Graph{
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Kill, Dst: Slot(0), Size: -1, ClearTo: 1})}}}},
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Kill, Dst: Slot(0), Size: 0, ClearTo: 1})}}}},
		{},
		{Widths: []uint8{0}, Blocks: []Block{{}}},
		{Widths: []uint8{17}, Blocks: []Block{{}}},
		{Widths: []uint8{8}, Blocks: []Block{{Operations: []Operation{use(r, 2)}}}},
		{Widths: []uint8{16}, Inputs: []Binding{{r, 1}}, Blocks: []Block{{}}},
		{Widths: []uint8{8}, Blocks: []Block{{Edges: []Edge{{To: 1}}}}},
		{Widths: []uint8{8, 4}, Blocks: []Block{{Edges: []Edge{{To: 0, Parameters: []Parameter{{1, 2, r}}}}}}},
		{Widths: []uint8{8}, Blocks: []Block{{Edges: []Edge{{To: 0, Parameters: []Parameter{{1, 1, r}, {1, 1, r}}}}}}},
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Kind(99)})}}}},
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Copy, Src: r, Dst: r, Size: 9})}}}},
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Kill, Dst: Slot(2147483647), Size: 2})}}}},
		{Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Copy, Src: r, Dst: r.next(1), Size: 4})}}}},
	}
	for i, g := range cases {
		got := g.Verify(Limits{})
		if got.Verdict != Inconclusive || got.Reason != InvalidGraph {
			t.Errorf("case %d: %+v", i, got)
		}
	}
}

func BenchmarkFlowLinearCopies(b *testing.B) {
	for _, count := range []int{16, 256, 4096} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			g := Graph{Widths: []uint8{8}, Inputs: []Binding{{Register(GP, 1), 1}}, Blocks: []Block{{}}}
			for i := 0; i < count; i++ {
				g.Blocks[0].Operations = append(g.Blocks[0].Operations, copyTo(Slot(int32(i*8)), Register(GP, 1), 8))
			}
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, use(Slot(int32((count-1)*8)), 1))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if r := g.Verify(Limits{}); r.Verdict != Verified {
					b.Fatal(r)
				}
			}
		})
	}
}

// The oracle explores concrete tiny machine states until no new (block,state)
// pair exists. It does not use the verifier's lattice, worklist, or transfer code.
func TestFlowMatchesFiniteExecutionOracle(t *testing.T) {
	r0, r1 := Register(GP, 0), Register(GP, 1)
	choices := [][]Operation{
		nil,
		{kill(r0, 1)},
		{copyTo(r0, r1, 1)},
		{machine(Effect{Kind: Swap, Dst: r0, Src: r1, Size: 1})},
		{def(r0, 3)},
	}
	for a := range choices {
		for b := range choices {
			for c := range choices {
				g := Graph{Widths: []uint8{1, 1, 1}, Inputs: []Binding{{r0, 1}, {r1, 2}}, Blocks: []Block{
					{Edges: []Edge{{To: 1}}},
					{Operations: append([]Operation{use(r0, 1)}, choices[a]...), Edges: []Edge{{To: 2}, {To: 3}}},
					{Operations: choices[b], Edges: []Edge{{To: 3}}},
					{Operations: choices[c], Edges: []Edge{{To: 1}}},
				}}
				type concrete struct {
					block int
					regs  [2]ValueID
				}
				pending := []concrete{{regs: [2]ValueID{1, 2}}}
				seen := map[concrete]bool{}
				valid := true
				for len(pending) != 0 {
					state := pending[len(pending)-1]
					pending = pending[:len(pending)-1]
					if seen[state] {
						continue
					}
					seen[state] = true
					for _, op := range g.Blocks[state.block].Operations {
						switch op.Kind {
						case Use:
							if state.regs[op.Location.Index] != op.Value {
								valid = false
							}
						case Define:
							for i, value := range state.regs {
								if value == op.Value {
									state.regs[i] = 0
								}
							}
							state.regs[op.Location.Index] = op.Value
						case Machine:
							e := op.Effect
							switch e.Kind {
							case Kill:
								state.regs[e.Dst.Index] = 0
							case Copy:
								state.regs[e.Dst.Index] = state.regs[e.Src.Index]
							case Swap:
								state.regs[e.Dst.Index], state.regs[e.Src.Index] = state.regs[e.Src.Index], state.regs[e.Dst.Index]
							}
						}
					}
					for _, edge := range g.Blocks[state.block].Edges {
						pending = append(pending, concrete{edge.To, state.regs})
					}
				}
				got := g.Verify(Limits{})
				if (got.Verdict == Verified) != valid || got.Verdict == Inconclusive {
					t.Fatalf("choices %d/%d/%d: oracle valid=%v, analysis=%+v", a, b, c, valid, got)
				}
			}
		}
	}
}

func TestFlowTransferWorkScalesWithTouchedBytes(t *testing.T) {
	last := 0
	for _, count := range []int{64, 128, 256, 512} {
		g := Graph{Widths: []uint8{8}, Inputs: []Binding{{Register(GP, 1), 1}}, Blocks: []Block{{}}}
		for i := 0; i < count; i++ {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, copyTo(Slot(int32(i*8)), Register(GP, 1), 8))
		}
		r := verifyFlow(t, g, Verified, NoFailure)
		if last != 0 && r.Work > last*2 {
			t.Fatalf("doubling independent copies exceeded linear work: %d -> %d", last, r.Work)
		}
		last = r.Work
	}
}

func TestFlowRejectsPartiallyOverlappingSwap(t *testing.T) {
	for _, loc := range []Location{Slot(0), Register(FP, 1)} {
		g := Graph{Widths: []uint8{8}, Inputs: []Binding{{loc, 1}}, Blocks: []Block{{Operations: []Operation{machine(Effect{Kind: Swap, Dst: loc, Src: loc.next(4), Size: 8})}}}}
		verifyFlow(t, g, Inconclusive, InvalidGraph)
		g.Blocks[0].Operations = []Operation{machine(Effect{Kind: Swap, Dst: loc, Src: loc, Size: 8}), use(loc, 1)}
		verifyFlow(t, g, Verified, NoFailure)
	}
}

func TestFlowCallWorkScalesLinearly(t *testing.T) {
	last := 0
	for _, count := range []int{64, 128, 256} {
		g := Graph{Widths: []uint8{8}, Blocks: []Block{{}}}
		for i := 0; i < count; i++ {
			g.Inputs = append(g.Inputs, Binding{Slot(int32(i * 8)), 1})
		}
		for i := 0; i < count; i++ {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, machine(Effect{Kind: Call}))
		}
		r := verifyFlow(t, g, Verified, NoFailure)
		if last != 0 && r.Work > last*2 {
			t.Fatalf("calls scanned growing frame: %d -> %d", last, r.Work)
		}
		last = r.Work
	}
}

func TestFlowEmptyParametersConsumeReplayBudget(t *testing.T) {
	b := &flowBudget{limits: DefaultLimits()}
	s := newImage(b)
	params := make([]Parameter, 1024)
	for i := range params {
		params[i] = Parameter{From: ValueID(i + 1), To: ValueID(i + 1), Location: Slot(0)}
	}
	s.parameters(params)
	if b.work != 2*len(params) {
		t.Fatalf("empty renames bypassed replay budget: %d", b.work)
	}
}

func BenchmarkFlowCallsWithFrame(b *testing.B) {
	for _, count := range []int{64, 256, 1024} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			g := Graph{Widths: []uint8{8}, Blocks: []Block{{}}}
			for i := 0; i < count; i++ {
				g.Inputs = append(g.Inputs, Binding{Slot(int32(i * 8)), 1})
				g.Blocks[0].Operations = append(g.Blocks[0].Operations, machine(Effect{Kind: Call}))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if r := g.Verify(Limits{}); r.Verdict != Verified {
					b.Fatal(r)
				}
			}
		})
	}
}

func TestFlowDeletedFactsRetainStorageCreditsUntilRelease(t *testing.T) {
	b := &flowBudget{limits: DefaultLimits()}
	s := newImage(b)
	loc := Register(GP, 1)
	s.add(loc, symbol{1, 0})
	s.add(loc, symbol{2, 0})
	s.remove(loc, symbol{1, 0})
	if s.count != 1 || b.facts != 2 {
		t.Fatalf("deleted map capacity lost its storage credit: count=%d credits=%d", s.count, b.facts)
	}
	if !s.has(loc, symbol{2, 0}) {
		t.Fatal("removing inline identity lost alias set")
	}
	s.add(loc, symbol{3, 0})
	if !s.has(loc, symbol{2, 0}) || !s.has(loc, symbol{3, 0}) {
		t.Fatal("inline reuse lost aliases")
	}
	s.clear(loc, 1)
	if s.count != 0 || b.facts != 3 {
		t.Fatalf("clear released retained capacity: %d/%d", s.count, b.facts)
	}
	s.release()
	if b.facts != 0 {
		t.Fatalf("release retained %d credits", b.facts)
	}
}

func TestFlowSnapshotCreditsRemainUntilRestored(t *testing.T) {
	limits := DefaultLimits()
	limits.Facts = 2
	b := &flowBudget{limits: limits}
	s := newImage(b)
	s.add(Register(GP, 1), symbol{1, 0})
	facts := s.snapshot(Register(GP, 1), Register(GP, 2), 1, nil)
	defer func() {
		if _, ok := recover().(flowLimit); !ok {
			t.Fatal("restoration released still-live snapshot credits too early")
		}
	}()
	s.restore(facts)
}

func TestFlowAliasCopyOverflowsInlineSnapshot(t *testing.T) {
	g := Graph{Blocks: []Block{{Operations: []Operation{copyTo(Slot(0), Register(GP, 0), 1), kill(Register(GP, 0), 1), machine(Effect{Kind: Call})}}}}
	for i := 0; i < 65; i++ {
		g.Widths = append(g.Widths, 1)
		id := ValueID(i + 1)
		g.Inputs = append(g.Inputs, Binding{Register(GP, 0), id})
		g.Blocks[0].Operations = append(g.Blocks[0].Operations, use(Slot(0), id))
	}
	verifyFlow(t, g, Verified, NoFailure)
}
