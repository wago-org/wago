//go:build wago_regalloccheck

package regalloccheck

import "testing"

func constantDef(loc Location, id ValueID) Operation {
	return Operation{Kind: DefineConstant, Location: loc, Value: id}
}

func TestGraphConstantMaterializationsPreserveAliases(t *testing.T) {
	a, b, c := Register(GP, 0), Register(GP, 1), Register(GP, 2)
	g := Graph{Widths: []uint8{8}, ConstValues: []ValueID{1}, Blocks: []Block{{Operations: []Operation{
		constantDef(a, 1), copyTo(b, a, 8), constantDef(c, 1), use(a, 1), use(b, 1), use(c, 1),
	}}}}
	if r := g.Verify(Limits{}); r.Verdict != Verified {
		t.Fatal(r)
	}
	// The existing dynamic epoch rule must still retire every prior alias.
	g.ConstValues = nil
	g.Blocks[0].Operations[0].Kind = Define
	g.Blocks[0].Operations[2].Kind = Define
	if r := g.Verify(Limits{}); r.Verdict != Rejected {
		t.Fatal("dynamic epoch retained stale alias", r)
	}
}

func TestGraphConstantDefinitionDoesNotRepairOtherClobbers(t *testing.T) {
	a, b := Register(GP, 0), Register(GP, 1)
	for _, e := range []Effect{
		{Kind: Kill, Dst: a, Size: 1},
		{Kind: Copy, Dst: a, Src: Slot(99), Size: 4},
		{Kind: Call},
	} {
		g := Graph{Widths: []uint8{8}, ConstValues: []ValueID{1}, Blocks: []Block{{Operations: []Operation{constantDef(a, 1), machine(e), constantDef(b, 1), use(a, 1)}}}}
		if r := g.Verify(Limits{}); r.Verdict != Rejected {
			t.Fatal("rematerialization repaired clobbered alias", e, r)
		}
		g.Blocks[0].Operations[3] = use(b, 1)
		if r := g.Verify(Limits{}); r.Verdict != Verified {
			t.Fatal("actual rematerialization unavailable", e, r)
		}
	}
	// A partial constant definition preserves the untouched high carrier bytes.
	g := Graph{Widths: []uint8{4, 8}, ConstValues: []ValueID{1}, Inputs: []Binding{{a, 2}}, Blocks: []Block{{Operations: []Operation{constantDef(a, 1), copyTo(b, a, 8), use(b, 1), use(b, 2)}}}}
	if r := g.Verify(Limits{}); r.Verdict != Rejected {
		t.Fatal("partial constant recreated old whole carrier", r)
	}
}

func TestGraphConstantsMeetAndLoop(t *testing.T) {
	a, b := Register(GP, 0), Register(GP, 1)
	join := Graph{Widths: []uint8{8}, ConstValues: []ValueID{1}, Blocks: []Block{
		{Operations: []Operation{constantDef(a, 1)}, Edges: []Edge{{To: 1}, {To: 2}}},
		{Operations: []Operation{constantDef(b, 1)}, Edges: []Edge{{To: 3}}},
		{Edges: []Edge{{To: 3}}},
		{Operations: []Operation{use(b, 1)}},
	}}
	if r := join.Verify(Limits{}); r.Verdict != Rejected {
		t.Fatal("missing predecessor alias survived", r)
	}
	join.Blocks[2].Operations = []Operation{constantDef(b, 1)}
	if r := join.Verify(Limits{}); r.Verdict != Verified {
		t.Fatal(r)
	}
	loop := Graph{Widths: []uint8{8, 8}, ConstValues: []ValueID{1}, Blocks: []Block{
		{Operations: []Operation{constantDef(a, 1)}, Edges: []Edge{{To: 1}}},
		{Operations: []Operation{constantDef(b, 1), use(a, 1), use(b, 1)}, Edges: []Edge{{To: 1}, {To: 2, Parameters: []Parameter{{From: 1, To: 2, Location: b}}}}},
		{Operations: []Operation{use(b, 2)}},
	}}
	if r := loop.Verify(Limits{}); r.Verdict != Verified {
		t.Fatal(r)
	}
	loop.Blocks[1].Operations = append([]Operation{kill(a, 8)}, loop.Blocks[1].Operations...)
	if r := loop.Verify(Limits{}); r.Verdict != Rejected {
		t.Fatal("loop rematerialization revived unrelated cache", r)
	}
}

func TestGraphConstantClassificationValidation(t *testing.T) {
	r := Register(GP, 0)
	for _, g := range []Graph{
		{Widths: []uint8{8}, ConstValues: []ValueID{0}, Blocks: []Block{{}}},
		{Widths: []uint8{8}, ConstValues: []ValueID{2}, Blocks: []Block{{}}},
		{Widths: []uint8{8, 8}, ConstValues: []ValueID{1, 1}, Blocks: []Block{{}}},
		{Widths: []uint8{8}, ConstValues: []ValueID{1, 1}, Blocks: []Block{{}}},
		{Widths: []uint8{8}, Blocks: []Block{{Operations: []Operation{constantDef(r, 1)}}}},
		{Widths: []uint8{8}, ConstValues: []ValueID{1}, Blocks: []Block{{Operations: []Operation{def(r, 1)}}}},
		{Widths: []uint8{8}, ConstValues: []ValueID{1}, Inputs: []Binding{{r, 1}}, Blocks: []Block{{Edges: []Edge{{To: 0, Parameters: []Parameter{{From: 1, To: 1, Location: r}}}}}}},
	} {
		if got := g.Verify(Limits{}); got.Verdict != Inconclusive || got.Reason != InvalidGraph {
			t.Fatal("invalid immutable classification admitted", got)
		}
	}
	// Independent entry literal preloads and edge source aliases are permitted.
	g := Graph{Widths: []uint8{8}, ConstValues: []ValueID{1}, Inputs: []Binding{{r, 1}}, Blocks: []Block{{Operations: []Operation{use(r, 1)}}}}
	if got := g.Verify(Limits{}); got.Verdict != Verified {
		t.Fatal(got)
	}
	if got := g.Verify(Limits{Work: 2}); got.Verdict != Inconclusive || got.Reason != ResourceLimit {
		t.Fatal("membership initialization unmetered", got)
	}
	if got := g.Verify(Limits{Values: 1, Facts: 2}); got.Verdict != Inconclusive || got.Reason != ResourceLimit {
		t.Fatal("constant facts unmetered", got)
	}
}
