//go:build wago_regalloccheck

package shared

import (
	"fmt"
	"math/bits"

	"github.com/wago-org/wago/internal/regalloccheck"
)

const scalarGraphChecks = true

// ScalarGraphTarget is optional checked-only instrumentation. The production
// ScalarTarget interface stays unchanged; uninstrumented test targets are not
// evidence of a verified emitted function.
type ScalarGraphTarget interface {
	ObserveScalarGraph(func(regalloccheck.Effect), func(uint32)) func()
	ScalarGraphReturnLocation() regalloccheck.Location
	ScalarGraphLocalOffset(int) int32
	ScalarGraphBranchDestination(int) (int, bool, bool)
}

type scalarGraphState struct{ graph *scalarGraphRecorder }
type scalarGraphImage struct{ locals, stack []regalloccheck.ValueID }
type scalarGraphSnapshot struct {
	image          scalarGraphImage
	block, edge    int
	registerResult bool
	resultReg      uint8
}
type scalarGraphBranch struct {
	scalarGraphSnapshot
	conditional bool
}
type scalarGraphRecorder struct {
	s                     *ScalarState
	target                ScalarGraphTarget
	model                 regalloccheck.Graph
	limits                regalloccheck.Limits
	current, construction int
	pending               map[int]scalarGraphBranch
	registerRestore       bool
	registerRestoreReg    uint8
	restoreNext           bool
	restoreOverride       *scalarGraphSnapshot
	restoreObservers      func()
	writes, copies        uint32
	lastWrite             uint32
	complete              bool
	folded, result        regalloccheck.ValueID
	returned              bool
	failure               *regalloccheck.Result
}

func (s *ScalarState) checkGraphBegin(target ScalarTarget) ScalarTarget {
	t, ok := target.(ScalarGraphTarget)
	if !ok {
		return target
	}
	g := &scalarGraphRecorder{s: s, target: t, limits: regalloccheck.DefaultLimits(), pending: make(map[int]scalarGraphBranch)}
	if config, ok := target.(interface{ ScalarGraphLimits() regalloccheck.Limits }); ok {
		l := config.ScalarGraphLimits()
		for _, p := range [][2]*int{{&g.limits.Blocks, &l.Blocks}, {&g.limits.Values, &l.Values}, {&g.limits.Operations, &l.Operations}, {&g.limits.Facts, &l.Facts}, {&g.limits.Work, &l.Work}} {
			if *p[1] < 0 {
				g.fail(regalloccheck.InvalidGraph, "invalid graph limits")
			}
			if *p[1] > 0 {
				*p[0] = min(*p[0], *p[1])
			}
		}
	}
	g.model.Blocks = []regalloccheck.Block{{}}
	s.graph = g
	g.restoreObservers = t.ObserveScalarGraph(g.observe, g.observeWrites)
	return &scalarGraphTarget{ScalarTarget: target, g: g}
}

// Graphs and observers are function-local. Restore before checking/reporting,
// including errors and panics, and preserve a preexisting emission panic.
func (s *ScalarState) checkGraphEnd() {
	failure := recover()
	g := s.graph
	if g != nil {
		g.restoreObservers()
		s.graph, s.target, s.regs = nil, nil, nil
		if failure == nil {
			r := regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: regalloccheck.UnsupportedOperation, Message: "incomplete scalar emission"}
			if g.failure != nil {
				r = *g.failure
			} else if g.complete && g.returned && len(g.pending) == 0 {
				r = g.model.Verify(g.limits)
			}
			if report, ok := g.target.(interface {
				ScalarGraphResult(regalloccheck.Graph, regalloccheck.Result)
			}); ok {
				report.ScalarGraphResult(g.model, r)
			}
			if r.Verdict == regalloccheck.Rejected {
				panic(fmt.Sprintf("regalloccheck: shared scalar graph: block %d operation %d: %s", r.Block, r.Operation, r.Message))
			}
		}
	}
	if failure != nil {
		panic(failure)
	}
}
func (s *ScalarState) checkGraphComplete() {
	if s.graph != nil {
		s.graph.complete = true
	}
}
func (g *scalarGraphRecorder) fail(reason regalloccheck.FailureReason, message string) {
	if g.failure == nil {
		g.failure = &regalloccheck.Result{Verdict: regalloccheck.Inconclusive, Reason: reason, Block: -1, Operation: -1, Message: message}
	}
}
func (g *scalarGraphRecorder) reserve(n int) bool {
	if g.failure != nil {
		return false
	}
	if n > g.limits.Operations-g.construction {
		g.fail(regalloccheck.ResourceLimit, "scalar graph construction limit")
		return false
	}
	g.construction += n
	return true
}
func (g *scalarGraphRecorder) appendAt(block int, op regalloccheck.Operation) {
	if g.reserve(1) {
		g.model.Blocks[block].Operations = append(g.model.Blocks[block].Operations, op)
	}
}
func (g *scalarGraphRecorder) append(op regalloccheck.Operation) { g.appendAt(g.current, op) }
func (g *scalarGraphRecorder) newBlock() int {
	if g.failure != nil {
		return g.current
	}
	if len(g.model.Blocks) >= g.limits.Blocks {
		g.fail(regalloccheck.ResourceLimit, "scalar graph block limit")
		return g.current
	}
	g.model.Blocks = append(g.model.Blocks, regalloccheck.Block{})
	return len(g.model.Blocks) - 1
}
func (g *scalarGraphRecorder) edge(block, to int) int {
	if !g.reserve(1) {
		return 0
	}
	b := &g.model.Blocks[block]
	b.Edges = append(b.Edges, regalloccheck.Edge{To: to})
	return len(b.Edges) - 1
}
func (s *ScalarState) graphValue(id scalarID) regalloccheck.ValueID {
	return regalloccheck.ValueID(s.node(id).order)
}
func (s *ScalarState) checkGraphAdd(id scalarID) {
	g := s.graph
	if g == nil || id == 0 || g.failure != nil {
		return
	}
	if len(g.model.Widths) >= g.limits.Values {
		g.fail(regalloccheck.ResourceLimit, "scalar graph value limit")
		return
	}
	if int(s.graphValue(id)) != len(g.model.Widths)+1 {
		g.fail(regalloccheck.InvalidGraph, "unstable scalar graph identity")
		return
	}
	width := uint8(4)
	if s.node(id).wide {
		width = 8
	}
	g.model.Widths = append(g.model.Widths, width)
}
func (s *ScalarState) graphLocation(id scalarID) (regalloccheck.Location, bool) {
	n := s.node(id)
	switch n.kind {
	case ScalarRegister:
		return regalloccheck.Register(regalloccheck.GP, n.reg), true
	case ScalarFrame:
		return regalloccheck.Slot(s.target.SpillOffset(int(n.slot))), true
	case scalarBorrow:
		return regalloccheck.Slot(s.graph.target.ScalarGraphLocalOffset(int(n.slot))), true
	default:
		return regalloccheck.Location{}, false
	}
}
func (s *ScalarState) checkGraphSeed(id scalarID) {
	g := s.graph
	if g == nil || !g.reserve(1) {
		return
	}
	if loc, ok := s.graphLocation(id); ok {
		g.model.Inputs = append(g.model.Inputs, regalloccheck.Binding{Location: loc, Value: s.graphValue(id)})
	}
}
func (s *ScalarState) checkGraphUse(id scalarID) {
	if s.graph == nil {
		return
	}
	if loc, ok := s.graphLocation(id); ok {
		s.graph.append(regalloccheck.Operation{Kind: regalloccheck.Use, Location: loc, Value: s.graphValue(id), Where: "scalar semantic input"})
	}
}
func (s *ScalarState) checkGraphInputs(left, right scalarID) {
	g := s.graph
	if g == nil {
		return
	}
	s.checkGraphUse(left)
	if loc, ok := s.graphLocation(right); ok && loc.Bank == regalloccheck.Frame {
		g.folded = s.graphValue(right)
	} else {
		s.checkGraphUse(right)
	}
}
func (s *ScalarState) checkGraphDefine(id scalarID, reg uint8) {
	if s.graph != nil {
		if s.graph.writes&(1<<reg) == 0 {
			s.graph.fail(regalloccheck.UnsupportedOperation, "missing observed scalar definition")
			return
		}
		s.graph.append(regalloccheck.Operation{Kind: regalloccheck.Define, Location: regalloccheck.Register(regalloccheck.GP, reg), Value: s.graphValue(id), Where: "trusted scalar definition"})
	}
}
func (s *ScalarState) checkGraphResult(id scalarID) {
	if s.graph != nil {
		s.checkGraphUse(id)
		s.graph.result = s.graphValue(id)
	}
}
func (g *scalarGraphRecorder) image() scalarGraphImage {
	x := scalarGraphImage{locals: make([]regalloccheck.ValueID, len(g.s.locals)), stack: make([]regalloccheck.ValueID, len(g.s.stack))}
	for i, id := range g.s.locals {
		x.locals[i] = g.s.graphValue(id)
	}
	for i, id := range g.s.stack {
		x.stack[i] = g.s.graphValue(id)
	}
	return x
}
func (s *ScalarState) checkGraphElse() {
	if s.graph != nil {
		s.graph.restoreNext = true
	}
}
func (s *ScalarState) checkGraphRestoreCarrier(registerResult bool, resultReg uint8) {
	if s.graph != nil {
		s.graph.registerRestore = registerResult
		s.graph.registerRestoreReg = resultReg
	}
}
func (s *ScalarState) checkGraphRestore(depth int) scalarGraphSnapshot {
	g := s.graph
	if g == nil || g.failure != nil {
		return scalarGraphSnapshot{}
	}
	if g.restoreOverride != nil {
		x := *g.restoreOverride
		g.restoreOverride = nil
		return x
	}
	x := scalarGraphSnapshot{image: g.image(), block: g.current, registerResult: g.registerRestore, resultReg: g.registerRestoreReg}
	g.registerRestore = false
	to := g.newBlock()
	x.edge = g.edge(g.current, to)
	g.current = to
	return x
}
func (s *ScalarState) checkGraphRestored(x scalarGraphSnapshot) {
	if s.graph != nil && s.graph.failure == nil {
		s.graph.bind(x, s.graph.image())
	}
}
func (g *scalarGraphRecorder) bind(x scalarGraphSnapshot, to scalarGraphImage) {
	if g.failure != nil {
		return
	}
	if len(x.image.locals) != len(to.locals) || len(x.image.stack) < len(to.stack) {
		g.fail(regalloccheck.InvalidGraph, "scalar edge shape")
		return
	}
	seen := make(map[regalloccheck.ValueID]regalloccheck.ValueID)
	add := func(from, to regalloccheck.ValueID, loc regalloccheck.Location) {
		// A shared local/result alias may promise two carriers. Check both,
		// while the graph's simultaneous rename needs one parameter per name.
		g.appendAt(x.block, regalloccheck.Operation{Kind: regalloccheck.Use, Location: loc, Value: from, Where: "scalar outgoing carrier"})
		if old, ok := seen[to]; ok {
			if old != from {
				g.appendAt(x.block, regalloccheck.Operation{Kind: regalloccheck.Use, Location: loc, Value: old, Where: "scalar outgoing alias carrier"})
			}
			return
		}
		seen[to] = from
		if g.reserve(1) {
			e := &g.model.Blocks[x.block].Edges[x.edge]
			e.Parameters = append(e.Parameters, regalloccheck.Parameter{From: from, To: to, Location: loc})
		}
	}
	for i, id := range to.locals {
		add(x.image.locals[i], id, regalloccheck.Slot(g.target.ScalarGraphLocalOffset(i)))
	}
	for i, id := range to.stack {
		loc := regalloccheck.Slot(g.s.target.SpillOffset(i + 1))
		if x.registerResult && i == len(to.stack)-1 {
			loc = regalloccheck.Register(regalloccheck.GP, x.resultReg)
		}
		add(x.image.stack[i], id, loc)
	}
}
func (g *scalarGraphRecorder) observe(e regalloccheck.Effect) {
	if g.failure != nil {
		return
	}
	if e.Kind == regalloccheck.Read && g.folded != 0 {
		g.append(regalloccheck.Operation{Kind: regalloccheck.Use, Location: e.Src, Value: g.folded, Where: "observed scalar folded read"})
		g.folded = 0
	}
	if e.Kind == regalloccheck.Copy && e.Dst.Bank == regalloccheck.GP {
		// ARM large-positive frame loads report their synthetic frame Copy after
		// the destination write; direct transfers report Copy before that write.
		// This allowance is restricted to those admitted target-operation orders.
		if e.Src.Bank != regalloccheck.Frame || g.lastWrite&(1<<uint(e.Dst.Index)) == 0 {
			g.copies |= 1 << uint(e.Dst.Index)
		}
	}
	g.append(regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
}
func (g *scalarGraphRecorder) observeWrites(mask uint32) {
	g.writes |= mask
	g.lastWrite = mask
	unmatched := mask &^ g.copies
	g.copies &^= mask
	for unmatched != 0 {
		reg := bits.TrailingZeros32(unmatched)
		unmatched &^= 1 << reg
		g.append(regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regalloccheck.GP, uint8(reg)), Size: 8}})
	}
}
func (g *scalarGraphRecorder) begin() { g.writes, g.copies, g.lastWrite = 0, 0, 0 }
func (g *scalarGraphRecorder) end(definition bool) {
	mask := uint32(0)
	if definition {
		mask = g.writes
	}
	for mask != 0 {
		reg := bits.TrailingZeros32(mask)
		mask &^= 1 << reg
		g.append(regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regalloccheck.GP, uint8(reg)), Size: 8}})
	}
	if g.folded != 0 {
		g.fail(regalloccheck.UnsupportedOperation, "missing scalar folded read")
		g.folded = 0
	}
}

// This checked-only wrapper changes no target decision. Effects come from the
// encoder, expected values from ScalarState's semantic nodes. GP writes are
// reconciled per target operation: transfer copies preserve provenance;
// definitions kill observed destinations before the independent result contract.
type scalarGraphTarget struct {
	ScalarTarget
	g *scalarGraphRecorder
}

func (t *scalarGraphTarget) Constant(r uint8, v int64, w bool) {
	t.g.begin()
	t.ScalarTarget.Constant(r, v, w)
	t.g.end(true)
}
func (t *scalarGraphTarget) Move(d, s uint8, w bool) {
	t.g.begin()
	t.ScalarTarget.Move(d, s, w)
	t.g.end(false)
}
func (t *scalarGraphTarget) NormalizeI32(r uint8) {
	t.g.begin()
	t.ScalarTarget.NormalizeI32(r)
	t.g.end(false)
}
func (t *scalarGraphTarget) Load(r uint8, off int32, w bool) {
	t.g.begin()
	t.ScalarTarget.Load(r, off, w)
	t.g.end(false)
}
func (t *scalarGraphTarget) Store(off int32, r uint8, w bool) {
	t.g.begin()
	t.ScalarTarget.Store(off, r, w)
	t.g.end(false)
}
func (t *scalarGraphTarget) Binary(op IntOp, w bool, d, l uint8, r ScalarOperand) {
	t.g.begin()
	t.ScalarTarget.Binary(op, w, d, l, r)
	t.g.end(true)
}
func (t *scalarGraphTarget) ScaledAdd(w bool, d, l, r, shift uint8) bool {
	t.g.begin()
	ok := t.ScalarTarget.ScaledAdd(w, d, l, r, shift)
	t.g.end(true)
	return ok
}
func (t *scalarGraphTarget) split(site int, conditional bool) {
	g := t.g
	if g.failure != nil {
		return
	}
	x := scalarGraphSnapshot{image: g.image(), block: g.current}
	x.edge = g.edge(g.current, -1)
	g.pending[site] = scalarGraphBranch{scalarGraphSnapshot: x, conditional: conditional}
	to := g.newBlock()
	if conditional {
		g.edge(g.current, to)
	}
	g.current = to
}
func (t *scalarGraphTarget) BranchZero(r uint8) int {
	t.g.begin()
	site := t.ScalarTarget.BranchZero(r)
	t.g.end(false)
	t.split(site, true)
	return site
}
func (t *scalarGraphTarget) BranchCompare(op IntOp, w bool, l uint8, r ScalarOperand) int {
	t.g.begin()
	site := t.ScalarTarget.BranchCompare(op, w, l, r)
	t.g.end(false)
	t.split(site, true)
	return site
}
func (t *scalarGraphTarget) Jump() int {
	t.g.begin()
	site := t.ScalarTarget.Jump()
	t.g.end(false)
	t.split(site, false)
	return site
}
func (t *scalarGraphTarget) Patch(site, pos int) error {
	if err := t.ScalarTarget.Patch(site, pos); err != nil {
		return err
	}
	g := t.g
	if g.failure != nil {
		return nil
	}
	actual, conditional, decoded := g.target.ScalarGraphBranchDestination(site)
	if !decoded {
		g.fail(regalloccheck.UnsupportedOperation, "unrecognized scalar branch encoding")
		return nil
	}
	if actual != pos || conditional != g.pending[site].conditional {
		g.failure = &regalloccheck.Result{Verdict: regalloccheck.Rejected, Reason: regalloccheck.ProvenanceMismatch, Block: g.current, Operation: -1, Message: "emitted scalar branch target/kind differs from semantic edge"}
		return nil
	}
	x, ok := g.pending[site]
	if !ok || pos != t.Position() {
		g.fail(regalloccheck.UnsupportedOperation, "unmodeled scalar branch destination")
		return nil
	}
	delete(g.pending, site)
	g.model.Blocks[x.block].Edges[x.edge].To = g.current
	if x.conditional && g.restoreNext {
		g.restoreNext = false
		g.restoreOverride = &x.scalarGraphSnapshot
	} else {
		x.registerResult = false
		if len(g.s.stack) > 0 && g.s.node(g.s.stack[len(g.s.stack)-1]).kind == ScalarRegister {
			x.registerResult = true
			x.resultReg = g.s.node(g.s.stack[len(g.s.stack)-1]).reg
		}
		g.bind(x.scalarGraphSnapshot, g.image())
	}
	return nil
}
func (t *scalarGraphTarget) Return(r uint8, w bool, slots int) {
	t.g.begin()
	t.ScalarTarget.Return(r, w, slots)
	t.g.end(false)
	t.g.append(regalloccheck.Operation{Kind: regalloccheck.Use, Location: t.g.target.ScalarGraphReturnLocation(), Value: t.g.result, Where: "scalar ABI return"})
	t.g.returned = true
}
