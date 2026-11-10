//go:build wago_regalloccheck

package regalloccheck

import "testing"

func returnAreaGraph(width uint8, operations ...Operation) Graph {
	return Graph{
		Widths: []uint8{width, width},
		Inputs: []Binding{{Register(GP, 1), 1}, {Register(GP, 2), 2}},
		Blocks: []Block{{Operations: operations}},
	}
}

func TestReturnAreaEmittedCopyAndInvalidation(t *testing.T) {
	sink, source, wrong := Location{Bank: ReturnArea}, Register(GP, 1), Register(GP, 2)
	for _, width := range []uint8{4, 8} {
		g := returnAreaGraph(width, copyTo(sink, source, 8), use(sink, 1))
		verifyFlow(t, g, Verified, NoFailure)
		// The stored bytes survive a later register clobber; the sink is not a
		// live alias or a semantic definition of that register's next value.
		g.Blocks[0].Operations = []Operation{copyTo(sink, source, 8), kill(source, 8), use(sink, 1)}
		verifyFlow(t, g, Verified, NoFailure)
		g.Blocks[0].Operations = []Operation{copyTo(sink, wrong, 8), use(sink, 1)}
		verifyFlow(t, g, Rejected, ProvenanceMismatch)
		g.Blocks[0].Operations = []Operation{kill(source, 8), copyTo(sink, source, 8), use(sink, 1)}
		verifyFlow(t, g, Rejected, UnknownInput)
		g.Blocks[0].Operations = []Operation{copyTo(sink, source, 8), copyTo(sink, wrong, 8), use(sink, 1)}
		verifyFlow(t, g, Rejected, ProvenanceMismatch)
		for _, invalidation := range []Operation{kill(sink, 8), machine(Effect{Kind: Call}), def(source, 1)} {
			g.Blocks[0].Operations = []Operation{copyTo(sink, source, 8), invalidation, use(sink, 1)}
			verifyFlow(t, g, Rejected, UnknownInput)
		}
	}
}

func TestReturnAreaRejectsSeedingAndSemanticRepair(t *testing.T) {
	sink, source := Location{Bank: ReturnArea}, Register(GP, 1)
	for _, g := range []Graph{
		{Widths: []uint8{8}, Inputs: []Binding{{sink, 1}}, Blocks: []Block{{Operations: []Operation{use(sink, 1)}}}},
		{Widths: []uint8{8}, Blocks: []Block{{Operations: []Operation{def(sink, 1), use(sink, 1)}}}},
		{Widths: []uint8{8}, ConstValues: []ValueID{1}, Blocks: []Block{{Operations: []Operation{constantDef(sink, 1), use(sink, 1)}}}},
		{Widths: []uint8{8, 8}, Inputs: []Binding{{source, 1}}, Blocks: []Block{
			{Operations: []Operation{copyTo(sink, source, 8)}, Edges: []Edge{{To: 1, Parameters: []Parameter{{From: 1, To: 2, Location: sink}}}}},
			{Operations: []Operation{use(sink, 2)}},
		}},
		returnAreaGraph(8, copyTo(sink, source, 8), kill(sink, 8), def(sink, 1), use(sink, 1)),
	} {
		verifyFlow(t, g, Inconclusive, InvalidGraph)
	}
	var state State
	rejects(t, func() { state.Seed(sink, 8) })
	// The failed entry assumption must not leave any known output bytes.
	rejects(t, func() { state.ExpectKnown("unseeded return", sink, state.Read(sink, 8)) })
}

func TestReturnAreaEffectBoundsAndSinkDirection(t *testing.T) {
	sink, source := Location{Bank: ReturnArea}, Register(GP, 1)
	for _, effect := range []Effect{
		{Kind: Copy, Dst: Location{Bank: ReturnArea, Index: 1}, Src: source, Size: 8},
		{Kind: Copy, Dst: Location{Bank: ReturnArea, Index: -1}, Src: source, Size: 8},
		{Kind: Copy, Dst: Location{Bank: ReturnArea, Byte: 1}, Src: source, Size: 8},
		{Kind: Copy, Dst: sink, Src: source, Size: 4},
		{Kind: Copy, Dst: sink, Src: source, Size: 16},
		{Kind: Copy, Dst: sink, Src: source, Size: 8, ClearTo: 8},
		{Kind: Copy, Dst: sink, Src: Location{Bank: GP, Index: 1, Byte: 1}, Size: 8},
		{Kind: Copy, Dst: sink, Src: Register(GP, 32), Size: 8},
		{Kind: Copy, Dst: sink, Src: Register(FP, 1), Size: 8},
		{Kind: Copy, Dst: sink, Src: Slot(16), Size: 8},
		{Kind: Copy, Dst: sink, Src: Location{Bank: Unknown}, Size: 8},
		{Kind: Copy, Dst: source, Src: sink, Size: 8},
		{Kind: Copy, Dst: sink, Src: sink, Size: 8},
		{Kind: Read, Src: sink, Size: 8},
		{Kind: Swap, Dst: sink, Src: source, Size: 8},
		{Kind: Kill, Dst: sink, Size: 4},
		{Kind: Kill, Dst: sink, Size: 8, ClearTo: 16},
		{Kind: Kill, Dst: sink, Src: sink, Size: 8},
		{Kind: Call, Dst: sink},
	} {
		if machineEffectValid(effect) {
			t.Fatalf("unsupported sink effect admitted: %+v", effect)
		}
		g := returnAreaGraph(8, machine(effect))
		verifyFlow(t, g, Inconclusive, InvalidGraph)
	}
	// GP zero is target-neutral here: the future RV adapter must separately
	// authenticate a zero-source store rather than borrowing an input identity.
	if !machineEffectValid(Effect{Kind: Copy, Dst: sink, Src: Register(GP, 0), Size: 8}) {
		t.Fatal("target-neutral register zero rejected")
	}
	for _, loc := range []Location{{Bank: ReturnArea, Index: 1}, {Bank: ReturnArea, Byte: 1}, {Bank: ReturnArea, Byte: 255}} {
		g := returnAreaGraph(8, copyTo(sink, source, 8), use(loc, 1))
		verifyFlow(t, g, Inconclusive, InvalidGraph)
	}
	for _, width := range []uint8{1, 2, 7, 9, 16} {
		g := Graph{Widths: []uint8{width}, Blocks: []Block{{Operations: []Operation{use(sink, 1)}}}}
		verifyFlow(t, g, Inconclusive, InvalidGraph)
	}
}

func TestReturnAreaJoinsAndResourceLimits(t *testing.T) {
	sink, source := Location{Bank: ReturnArea}, Register(GP, 1)
	g := Graph{Widths: []uint8{8}, Inputs: []Binding{{source, 1}}, Blocks: []Block{
		{Edges: []Edge{{To: 1}, {To: 2}}},
		{Operations: []Operation{copyTo(sink, source, 8)}, Edges: []Edge{{To: 3}}},
		{Operations: []Operation{copyTo(sink, source, 8)}, Edges: []Edge{{To: 3}}},
		{Operations: []Operation{use(sink, 1)}},
	}}
	verifyFlow(t, g, Verified, NoFailure)
	if result := g.Verify(Limits{Work: 1}); result.Verdict != Inconclusive || result.Reason != ResourceLimit {
		t.Fatal("sink analysis bypassed work budget", result)
	}
	g.Blocks[2].Operations = nil
	verifyFlow(t, g, Rejected, UnknownInput)
	g.Blocks[2].Operations = []Operation{copyTo(sink, source, 8), kill(sink, 8)}
	verifyFlow(t, g, Rejected, UnknownInput)
}

func TestReturnAreaStateDoesNotInventUpperBytes(t *testing.T) {
	sink, source := Location{Bank: ReturnArea}, Register(GP, 1)
	var state State
	low := state.Seed(source, 4)
	state.Apply(Effect{Kind: Copy, Dst: sink, Src: source, Size: 8})
	state.Expect("serialized low word", sink, low)
	upper := sink.next(4)
	rejects(t, func() { state.ExpectKnown("unproven upper word", upper, state.Read(upper, 4)) })
	state.Apply(Effect{Kind: Call})
	rejects(t, func() { state.Expect("call invalidated sink", sink, low) })
}
